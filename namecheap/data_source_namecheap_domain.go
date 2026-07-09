package namecheap_provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// dataSourceNamecheapDomain exposes read-only information about a single domain.
//
// It combines two API commands so the exported attributes match the issue's
// contract in full:
//   - namecheap.domains.getInfo supplies the DNS-oriented fields (provider type,
//     nameservers, premium flags);
//   - namecheap.domains.getList supplies the domain lifecycle fields
//     (created/expires/is_expired/is_locked/auto_renew/whois_guard), which
//     getInfo does not return.
//
// The two-call cost is documented on the registry page.
func dataSourceNamecheapDomain() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceNamecheapDomainRead,
		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "The domain name to look up (e.g. example.com). Must be a registered root domain, not a subdomain.",
				ValidateFunc: validateDomainIsNotSubdomain,
			},
			"dns_provider_type": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The DNS provider currently serving the domain (e.g. NAMECHEAP, FreeDNS, Custom).",
			},
			"is_our_dns": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain is using Namecheap's DNS.",
			},
			"is_premium": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain is a premium domain.",
			},
			"is_premium_dns": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain has an active PremiumDNS subscription.",
			},
			"nameservers": {
				Type:        schema.TypeList,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "The nameservers currently configured for the domain.",
			},
			"created": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Registration date as an RFC3339 timestamp (UTC).",
			},
			"expires": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Expiration date as an RFC3339 timestamp (UTC).",
			},
			"expires_in_days": {
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "Whole calendar days until the domain expires (negative if already expired).",
			},
			"is_expired": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain has expired.",
			},
			"is_locked": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the registrar lock is enabled.",
			},
			"auto_renew": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether auto-renew is enabled.",
			},
			"whois_guard": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "WhoisGuard status (e.g. ENABLED, DISABLED, NOTPRESENT).",
			},
		},
	}
}

func dataSourceNamecheapDomainRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	resp, err := client.Domains.GetInfoWithContext(ctx, domain)
	if err != nil {
		return dataSourceDomainReadError(domain, err)
	}

	if resp == nil || resp.DomainDNSGetListResult == nil {
		return diag.Errorf("Namecheap returned no information for domain %q; it may not exist or may not be associated with this account", domain)
	}

	info := resp.DomainDNSGetListResult

	if info.IsPremium != nil {
		_ = data.Set("is_premium", *info.IsPremium)
	}
	if info.PremiumDnsSubscription != nil && info.PremiumDnsSubscription.IsActive != nil {
		_ = data.Set("is_premium_dns", *info.PremiumDnsSubscription.IsActive)
	}
	if info.DnsDetails != nil {
		if info.DnsDetails.ProviderType != nil {
			_ = data.Set("dns_provider_type", *info.DnsDetails.ProviderType)
		}
		if info.DnsDetails.IsUsingOurDNS != nil {
			_ = data.Set("is_our_dns", *info.DnsDetails.IsUsingOurDNS)
		}
		if info.DnsDetails.Nameservers != nil {
			_ = data.Set("nameservers", *info.DnsDetails.Nameservers)
		}
	}

	// getInfo does not carry the domain lifecycle fields (created/expires/
	// is_expired/is_locked/auto_renew/whois_guard). Fetch them from the account
	// portfolio listing, which does. getInfo above has already confirmed the
	// domain exists, so a portfolio miss leaves the lifecycle fields at their
	// zero values (with a warning) rather than failing the whole lookup.
	lifecycleDiags := setDomainLifecycleFromList(ctx, client, data, domain)
	if lifecycleDiags.HasError() {
		return lifecycleDiags
	}

	data.SetId(domain)
	// Propagate any non-error diagnostics (e.g. the portfolio-miss warning)
	// without discarding the successful read.
	return lifecycleDiags
}
