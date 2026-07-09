package namecheap_provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// dataSourceNamecheapDomainRecords exposes a read-only view of a domain's live
// DNS record set via namecheap.domains.dns.getHosts, together with the domain's
// nameservers (namecheap.domains.dns.getList) and email routing type. The record
// object shape mirrors the namecheap_domain_records resource so the output
// composes into resource inputs without transformation.
func dataSourceNamecheapDomainRecords() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceNamecheapDomainRecordsRead,
		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "The domain whose DNS records to read (e.g. example.com). Must be a registered root domain, not a subdomain.",
				ValidateFunc: validateDomainIsNotSubdomain,
			},
			"email_type": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The email routing type configured for the domain (e.g. NONE, FWD, MXE, MX, OX, GMAIL).",
			},
			"nameservers": {
				Type:        schema.TypeList,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "The custom nameservers configured for the domain; empty when the domain is using Namecheap's DNS.",
			},
			"records": {
				Type:        schema.TypeList,
				Computed:    true,
				Description: "The live DNS host records for the domain. Field shapes mirror the namecheap_domain_records resource record block.",
				Elem: &schema.Resource{
					Schema: domainRecordElemSchema(),
				},
			},
		},
	}
}

func dataSourceNamecheapDomainRecordsRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	// Read the DNS/nameserver state first (mirrors the resource read ordering):
	// a domain on custom nameservers exposes those, otherwise the record set.
	nsResp, err := client.DomainsDNS.GetListWithContext(ctx, domain)
	if err != nil {
		return dataSourceDomainReadError(domain, err)
	}
	if err := validateGetListResponse(nsResp); err != nil {
		return dataSourceDomainReadError(domain, err)
	}

	// A domain on custom nameservers is not served by Namecheap's DNS, so
	// getHosts would fail (the resource read guards the same way, see
	// resourceRecordRead in namecheap_domain_record.go). Expose the nameservers
	// and an empty record set instead of calling getHosts.
	if !*nsResp.DomainDNSGetListResult.IsUsingOurDNS {
		nameservers := []string{}
		if nsResp.DomainDNSGetListResult.Nameservers != nil {
			nameservers = *nsResp.DomainDNSGetListResult.Nameservers
		}
		if err := data.Set("nameservers", nameservers); err != nil {
			return diag.FromErr(err)
		}
		if err := data.Set("records", []map[string]interface{}{}); err != nil {
			return diag.FromErr(err)
		}
		if err := data.Set("email_type", ""); err != nil {
			return diag.FromErr(err)
		}
		data.SetId(domain)
		return nil
	}

	// Namecheap DNS: no custom nameservers to expose; read the live record set.
	if err := data.Set("nameservers", []string{}); err != nil {
		return diag.FromErr(err)
	}

	hostsResp, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return dataSourceDomainReadError(domain, err)
	}
	if err := validateGetHostsResponse(hostsResp); err != nil {
		return dataSourceDomainReadError(domain, err)
	}

	if err := data.Set("email_type", derefString(hostsResp.DomainDNSGetHostsResult.EmailType)); err != nil {
		return diag.FromErr(err)
	}

	records := []map[string]interface{}{}
	if hostsResp.DomainDNSGetHostsResult.Hosts != nil {
		// Filter Namecheap's default parking records (CNAME www->parkingpage,
		// URL @->http://www.<domain>) exactly as the namecheap_domain_records
		// resource does on read (issue #260); otherwise composing this data
		// source into that resource writes the parking records back and drifts.
		filtered := filterDefaultParkingRecords(hostsResp.DomainDNSGetHostsResult.Hosts, &domain)
		for i := range *filtered {
			records = append(records, flattenHostRecord(&(*filtered)[i]))
		}
	}
	if err := data.Set("records", records); err != nil {
		return diag.FromErr(err)
	}

	data.SetId(domain)
	return nil
}
