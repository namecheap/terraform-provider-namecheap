package namecheap_provider

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// domainGoneErrorCodes are the Namecheap API error numbers that mean the domain
// is no longer readable through this account: 2019166 "Domain not found" and
// 2016166 "Domain is not associated with your account". The SDK surfaces these
// as a returned *namecheap.APIError (not a nil result), so Read matches them to
// drop the resource from state rather than failing every refresh.
var domainGoneErrorCodes = map[int]bool{2019166: true, 2016166: true}

// isDomainGoneError reports whether err is one of the "domain no longer in this
// account" API errors (see domainGoneErrorCodes).
func isDomainGoneError(err error) bool {
	var apiErr *namecheap.APIError
	return errors.As(err, &apiErr) && domainGoneErrorCodes[apiErr.Number]
}

// resourceNamecheapDomainContacts manages a domain's WHOIS contact information
// (the Registrant, Tech, Admin and AuxBilling blocks) via the Namecheap
// namecheap.domains.getContacts / setContacts API.
//
// Semantics worth calling out:
//   - Create and Update both map onto a single setContacts call (the API has no
//     separate create/update for contacts).
//   - Read maps getContacts back into state, so a dashboard-side change surfaces
//     as drift on the next refresh.
//   - Delete is a state-only removal. Namecheap exposes no "delete contacts"
//     operation, so the contacts remain on the domain at their last-set values;
//     destroying this resource simply stops Terraform from managing them. This
//     mirrors the abandon-on-destroy semantics of other registrar resources and
//     is surfaced as a warning diagnostic.
//   - Registrant is required. Tech, Admin and AuxBilling are optional and, when
//     omitted, default to the Registrant values. That defaulting is applied in
//     CustomizeDiff so it is visible in the plan rather than happening invisibly
//     server-side.
//
// This resource is mutually exclusive, per domain, with an inline contacts block
// on the domain resource: manage a domain's contacts in exactly one place.
func resourceNamecheapDomainContacts() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceContactsCreate,
		ReadContext:   resourceContactsRead,
		UpdateContext: resourceContactsUpdate,
		DeleteContext: resourceContactsDelete,

		CustomizeDiff: customizeContactsDiff,

		Importer: &schema.ResourceImporter{
			StateContext: func(ctx context.Context, data *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
				domain := strings.ToLower(data.Id())
				if err := data.Set("domain", domain); err != nil {
					return nil, err
				}
				// Normalize the ID too, so importing "EXAMPLE.COM" does not leave
				// an uppercase ID that silently flips on the first update (which
				// sets a lowercased ID).
				data.SetId(domain)
				return []*schema.ResourceData{data}, nil
			},
		},

		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "Purchased available domain name on your account whose WHOIS contacts are managed.",
				ValidateFunc: validateDomainIsNotSubdomain,
			},
			"registrant": {
				Type:        schema.TypeList,
				Required:    true,
				MaxItems:    1,
				Description: "Registrant contact. Required.",
				Elem:        &schema.Resource{Schema: contactBlockSchema()},
			},
			"tech": {
				Type:        schema.TypeList,
				Optional:    true,
				Computed:    true,
				MaxItems:    1,
				Description: "Tech contact. Optional; defaults to the registrant contact when omitted.",
				Elem:        &schema.Resource{Schema: contactBlockSchema()},
			},
			"admin": {
				Type:        schema.TypeList,
				Optional:    true,
				Computed:    true,
				MaxItems:    1,
				Description: "Admin contact. Optional; defaults to the registrant contact when omitted.",
				Elem:        &schema.Resource{Schema: contactBlockSchema()},
			},
			"aux_billing": {
				Type:        schema.TypeList,
				Optional:    true,
				Computed:    true,
				MaxItems:    1,
				Description: "AuxBilling contact. Optional; defaults to the registrant contact when omitted.",
				Elem:        &schema.Resource{Schema: contactBlockSchema()},
			},
		},
	}
}

func resourceContactsCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return setDomainContacts(ctx, data, meta)
}

func resourceContactsUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return setDomainContacts(ctx, data, meta)
}

// setDomainContacts backs both Create and Update: it assembles the four contact
// blocks (defaulting the optional ones to the registrant) and issues a single
// setContacts call.
func setDomainContacts(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	registrant := expandContactBlock(data.Get("registrant"))

	args := &namecheap.DomainsSetContactsArgs{
		DomainName: domain,
		Registrant: registrant,
		Tech:       contactOrDefault(data.Get("tech"), registrant),
		Admin:      contactOrDefault(data.Get("admin"), registrant),
		AuxBilling: contactOrDefault(data.Get("aux_billing"), registrant),
	}

	resp, err := client.Domains.SetContactsWithContext(ctx, args)
	if err != nil {
		return diagFromClientError(err)
	}
	// Guard against a Status=OK response that nonetheless reports the update did
	// not take effect, so the provider does not claim success and write state
	// for contacts the API rejected.
	if resp != nil && resp.DomainSetContactResult != nil &&
		resp.DomainSetContactResult.IsSuccess != nil && !*resp.DomainSetContactResult.IsSuccess {
		return diag.Errorf("Namecheap reported the contact update for %q was not successful (setContacts returned IsSuccess=false)", domain)
	}

	data.SetId(domain)

	return resourceContactsRead(ctx, data, meta)
}

func resourceContactsRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	resp, err := client.Domains.GetContactsWithContext(ctx, domain)
	if err != nil {
		// A domain removed from the account is reported by the API as a
		// Status=ERROR ("Domain not found" / "not associated"), which the SDK
		// surfaces as an *namecheap.APIError. Treat it as gone and drop the
		// resource from state so Terraform plans a recreate rather than failing
		// every refresh; any other error is real.
		if isDomainGoneError(err) {
			data.SetId("")
			return nil
		}
		return diagFromClientError(err)
	}

	if resp == nil || resp.DomainContactsResult == nil {
		// Defensive: a Status=OK response carrying no result also means the
		// domain's contacts are unavailable; drop from state as above.
		data.SetId("")
		return nil
	}

	result := resp.DomainContactsResult

	if err := data.Set("registrant", flattenContactInfo(result.Registrant)); err != nil {
		return diag.FromErr(err)
	}
	if err := data.Set("tech", flattenContactInfo(result.Tech)); err != nil {
		return diag.FromErr(err)
	}
	if err := data.Set("admin", flattenContactInfo(result.Admin)); err != nil {
		return diag.FromErr(err)
	}
	if err := data.Set("aux_billing", flattenContactInfo(result.AuxBilling)); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// resourceContactsDelete removes the resource from state without calling the
// API: Namecheap has no operation to delete a domain's contacts, so they remain
// at their last-set values. A warning documents this so the behavior is not
// silent.
func resourceContactsDelete(_ context.Context, data *schema.ResourceData, _ interface{}) diag.Diagnostics {
	domain := strings.ToLower(data.Get("domain").(string))
	data.SetId("")

	return diag.Diagnostics{
		{
			Severity: diag.Warning,
			Summary:  "Domain contacts cannot be deleted",
			Detail: "The Namecheap API has no operation to delete a domain's WHOIS contacts. Removing this resource stops " +
				"Terraform from managing the contacts of " + domain + ", but the last-applied contact values remain on the domain. " +
				"Update them through another namecheap_domain_contacts resource or the Namecheap dashboard if they must change.",
		},
	}
}
