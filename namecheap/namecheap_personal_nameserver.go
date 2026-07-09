package namecheap_provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// nameserverIDSeparator joins the domain and nameserver host into the resource
// ID. Neither a domain nor a nameserver hostname may contain a slash, so it is
// an unambiguous separator for both composition and import parsing.
const nameserverIDSeparator = "/"

// nameserverNotFoundErrorCode is the Namecheap API error number returned by
// domains.ns.getInfo and domains.ns.delete when the personal nameserver does not
// exist. Namecheap signals a missing nameserver as a Status=ERROR response (not
// an empty Status=OK body), which the SDK surfaces as a non-nil *namecheap.APIError.
const nameserverNotFoundErrorCode = 5013160

// isNameserverNotFoundError reports whether err is the Namecheap
// "Nameserver not found" API error (code 5013160). The provider treats that as
// "the resource no longer exists" rather than a hard failure.
func isNameserverNotFoundError(err error) bool {
	var apiErr *namecheap.APIError
	return errors.As(err, &apiErr) && apiErr.Number == nameserverNotFoundErrorCode
}

// resourceNamecheapPersonalNameserver manages a personal (glue/vanity)
// nameserver such as ns1.example.com registered against a domain on the account
// via the namecheap.domains.ns.* API family. This is distinct from assigning
// custom nameservers to a domain (namecheap_domain_records' nameservers
// argument, which calls domains.dns.setCustom): this resource registers the
// nameserver host and its glue IP so it can itself be used as a nameserver.
func resourceNamecheapPersonalNameserver() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceNameserverCreate,
		ReadContext:   resourceNameserverRead,
		UpdateContext: resourceNameserverUpdate,
		DeleteContext: resourceNameserverDelete,

		Importer: &schema.ResourceImporter{
			StateContext: resourceNameserverImport,
		},

		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "The registered domain the personal nameserver belongs to (e.g. \"example.com\"). Must be a root domain present on the account, not a subdomain.",
				ValidateFunc: validateDomainIsNotSubdomain,
			},
			"nameserver": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "The fully qualified hostname of the personal nameserver to register (e.g. \"ns1.example.com\").",
				ValidateFunc: validation.StringIsNotEmpty,
			},
			"ip": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "The IP address the personal nameserver resolves to (the glue record's address).",
				ValidateFunc: validation.IsIPAddress,
			},
		},
	}
}

// nameserverID composes the stable resource ID "<domain>/<nameserver>".
func nameserverID(domain, nameserver string) string {
	return domain + nameserverIDSeparator + nameserver
}

// nameserverSplitDomain parses a root domain into its SLD/TLD parts, which the
// Namecheap domains.ns.* API requires as separate parameters.
func nameserverSplitDomain(domain string) (sld string, tld string, err error) {
	parsed, err := namecheap.ParseDomain(domain)
	if err != nil {
		return "", "", fmt.Errorf("parse domain %q: %w", domain, err)
	}
	return parsed.SLD, parsed.TLD, nil
}

func resourceNameserverCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	nameserver := strings.ToLower(data.Get("nameserver").(string))
	ip := data.Get("ip").(string)

	sld, tld, err := nameserverSplitDomain(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	if _, err := client.DomainsNS.CreateWithContext(ctx, sld, tld, nameserver, ip); err != nil {
		return diagFromClientError(err)
	}

	data.SetId(nameserverID(domain, nameserver))

	return nil
}

func resourceNameserverRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	nameserver := strings.ToLower(data.Get("nameserver").(string))

	sld, tld, err := nameserverSplitDomain(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	resp, err := client.DomainsNS.GetInfoWithContext(ctx, sld, tld, nameserver)
	if err != nil {
		// A nameserver deleted out-of-band is reported by the API as a
		// Status=ERROR "Nameserver not found" (code 5013160). Treat it as gone
		// and drop it from state so Terraform plans a recreate instead of
		// failing the refresh with a hard diagnostic.
		if isNameserverNotFoundError(err) {
			data.SetId("")
			return nil
		}
		return diagFromClientError(err)
	}

	// Defensive: a Status=OK response carrying no result element also means the
	// nameserver is no longer registered; drop it from state as above.
	if resp == nil || resp.DomainNameserverInfoResult == nil {
		data.SetId("")
		return nil
	}

	result := resp.DomainNameserverInfoResult
	if result.IP != nil {
		if err := data.Set("ip", *result.IP); err != nil {
			return diag.FromErr(err)
		}
	}
	if err := data.Set("domain", domain); err != nil {
		return diag.FromErr(err)
	}
	if err := data.Set("nameserver", nameserver); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceNameserverUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	nameserver := strings.ToLower(data.Get("nameserver").(string))

	sld, tld, err := nameserverSplitDomain(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	// domain and nameserver are ForceNew, so only the IP can change here. The
	// Namecheap ns.update command requires both the previous and the new IP.
	oldIPRaw, newIPRaw := data.GetChange("ip")
	oldIP := oldIPRaw.(string)
	newIP := newIPRaw.(string)

	if _, err := client.DomainsNS.UpdateWithContext(ctx, sld, tld, nameserver, oldIP, newIP); err != nil {
		return diagFromClientError(err)
	}

	return nil
}

func resourceNameserverDelete(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	nameserver := strings.ToLower(data.Get("nameserver").(string))

	sld, tld, err := nameserverSplitDomain(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	if _, err := client.DomainsNS.DeleteWithContext(ctx, sld, tld, nameserver); err != nil {
		// Deleting an already-absent nameserver is a no-op success, so a
		// "Nameserver not found" (5013160) is swallowed to keep destroy
		// idempotent (e.g. when it was removed out-of-band before this run).
		if isNameserverNotFoundError(err) {
			return nil
		}
		return diagFromClientError(err)
	}

	return nil
}

// resourceNameserverImport accepts an ID of the form "<domain>/<nameserver>"
// (e.g. "example.com/ns1.example.com") and seeds domain and nameserver so the
// subsequent Read can populate the IP.
func resourceNameserverImport(_ context.Context, data *schema.ResourceData, _ interface{}) ([]*schema.ResourceData, error) {
	parts := strings.SplitN(data.Id(), nameserverIDSeparator, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf(
			"invalid import ID %q: expected format \"<domain>%s<nameserver>\" (e.g. \"example.com%sns1.example.com\")",
			data.Id(), nameserverIDSeparator, nameserverIDSeparator,
		)
	}

	domain := strings.ToLower(parts[0])
	nameserver := strings.ToLower(parts[1])

	if err := data.Set("domain", domain); err != nil {
		return nil, err
	}
	if err := data.Set("nameserver", nameserver); err != nil {
		return nil, err
	}
	data.SetId(nameserverID(domain, nameserver))

	return []*schema.ResourceData{data}, nil
}
