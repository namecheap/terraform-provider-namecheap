package namecheap_provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// resourceNamecheapEmailForwarding manages a domain's entire email forwarding
// table via the namecheap.domains.dns.getEmailForwarding/setEmailForwarding
// API. Namecheap's setEmailForwarding is a full-table replace (like
// domain_records' SetHosts), so this resource owns the domain's complete set
// of forwarding rules: rules created outside Terraform surface as drift on
// refresh and are replaced on the next apply. Email forwarding only takes
// effect when the domain uses Namecheap BasicDNS/FreeDNS and its email_type is
// "FWD" (see namecheap_domain_records); a mismatch surfaces as an apply-time
// warning; SDKv2 cannot make this a plan-time diagnostic without an extra API
// call during plan (see #250's plan for the same limitation).
func resourceNamecheapEmailForwarding() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceEmailForwardingCreate,
		ReadContext:   resourceEmailForwardingRead,
		UpdateContext: resourceEmailForwardingUpdate,
		DeleteContext: resourceEmailForwardingDelete,

		Description: "Manages a domain's entire email forwarding table (mailbox alias -> destination address) via the Namecheap " +
			"domains.dns.getEmailForwarding/setEmailForwarding API. This resource owns the full table: rules created outside " +
			"Terraform surface as drift on refresh and are replaced on the next apply. Forwarding only takes effect when the " +
			"domain uses Namecheap BasicDNS/FreeDNS and its email_type is \"FWD\" (see namecheap_domain_records); a mismatch " +
			"surfaces as a warning at apply time.",

		Importer: &schema.ResourceImporter{
			StateContext: func(_ context.Context, data *schema.ResourceData, _ interface{}) ([]*schema.ResourceData, error) {
				domain := strings.ToLower(data.Id())
				if err := data.Set("domain", domain); err != nil {
					return nil, err
				}
				data.SetId(domain)
				return []*schema.ResourceData{data}, nil
			},
		},

		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "The registered root domain whose email forwarding is managed (e.g. \"example.com\"). Must be a root domain, not a subdomain.",
				ValidateFunc: validateDomainIsNotSubdomain,
			},
			"forwards": {
				Type:     schema.TypeMap,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Description: "Map of mailbox alias to destination email address, e.g. { info = \"me@example.com\" }. Use \"*\" as the " +
					"alias for a catch-all. This resource owns the domain's entire forwarding table: it must be non-empty (destroy " +
					"the resource to clear all forwarding) and any rule not listed here is removed on the next apply.",
				ValidateDiagFunc: validateForwards,
			},
		},
	}
}

// validateForwards enforces the forwards map's full-ownership contract
// (non-empty) and, per entry, that the mailbox alias is a bare lowercase local
// part (or "*" for a catch-all) and the destination looks like an email
// address. Validation is light-touch (no full RFC 5322 parsing) and reports
// attribute-scoped diagnostics per offending map key.
func validateForwards(v interface{}, path cty.Path) diag.Diagnostics {
	forwards := v.(map[string]interface{})

	if len(forwards) == 0 {
		return diag.Diagnostics{{
			Severity: diag.Error,
			Summary:  "forwards must not be empty",
			Detail: "namecheap_email_forwarding requires at least one forwarding rule, since it owns the domain's entire " +
				"forwarding table. To remove all email forwarding, destroy the resource instead of emptying this map.",
		}}
	}

	var diags diag.Diagnostics
	for mailbox, destRaw := range forwards {
		keyPath := append(path, cty.IndexStep{Key: cty.StringVal(mailbox)})

		if mailbox == "" || mailbox != strings.ToLower(mailbox) || strings.ContainsAny(mailbox, "@ \t\r\n") {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "Invalid forwards mailbox alias",
				Detail: fmt.Sprintf(
					"mailbox alias %q must be a non-empty, lowercase local alias (e.g. \"info\", or \"*\" for a catch-all) with no \"@\" or whitespace",
					mailbox,
				),
				AttributePath: keyPath,
			})
		}

		dest, _ := destRaw.(string)
		if !isPlausibleEmailAddress(dest) {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "Invalid forwards destination",
				Detail: fmt.Sprintf(
					"destination %q for mailbox %q must look like an email address (exactly one \"@\" with a non-empty local and domain part)",
					dest, mailbox,
				),
				AttributePath: keyPath,
			})
		}
	}

	return diags
}

// isPlausibleEmailAddress reports whether s has the shape of an email
// address: exactly one "@" with a non-empty local and domain part. This is
// deliberately light-touch, not full RFC 5322 validation.
func isPlausibleEmailAddress(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	return strings.Count(s, "@") == 1
}

// forwardsMapToSlice converts the schema's forwards map into the SDK's
// []EmailForward, sorted by mailbox key so the resulting mailboxN/ForwardToN
// request parameters are in deterministic order (stable mock assertions and
// API payloads).
func forwardsMapToSlice(forwards map[string]interface{}) []namecheap.EmailForward {
	mailboxes := make([]string, 0, len(forwards))
	for mailbox := range forwards {
		mailboxes = append(mailboxes, mailbox)
	}
	sort.Strings(mailboxes)

	result := make([]namecheap.EmailForward, 0, len(mailboxes))
	for _, mailbox := range mailboxes {
		result = append(result, namecheap.EmailForward{
			Mailbox:   mailbox,
			ForwardTo: forwards[mailbox].(string),
		})
	}
	return result
}

// forwardsSliceToMap converts the SDK's []EmailForward into the schema's
// forwards map. Mailbox keys are lowercased (dashboard-created rules may
// differ in case; aliases are case-insensitive) with the last entry winning
// on a post-lowercasing collision; ForwardTo is preserved verbatim.
func forwardsSliceToMap(forwards []namecheap.EmailForward) map[string]string {
	result := make(map[string]string, len(forwards))
	for _, fwd := range forwards {
		result[strings.ToLower(fwd.Mailbox)] = fwd.ForwardTo
	}
	return result
}

func resourceEmailForwardingCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return setEmailForwarding(ctx, data, meta, true)
}

func resourceEmailForwardingUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return setEmailForwarding(ctx, data, meta, false)
}

// setEmailForwarding backs both Create and Update: setEmailForwarding is the
// API's only write primitive (a full-table replace), so both operations issue
// the same call. After a successful set it runs the DNS-mode/email_type
// conflict check and returns its warning rather than dropping it.
func setEmailForwarding(ctx context.Context, data *schema.ResourceData, meta interface{}, isCreate bool) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))
	forwards := data.Get("forwards").(map[string]interface{})

	_, err := client.DomainsDNS.SetEmailForwardingWithContext(ctx, domain, forwardsMapToSlice(forwards))
	if err != nil {
		return diagFromClientError(err)
	}

	if isCreate {
		data.SetId(domain)
	}

	return checkEmailForwardingConflict(ctx, domain, client)
}

// checkEmailForwardingConflict issues one GetHostsWithContext call to detect
// the two ways email forwarding silently does nothing: the domain using
// custom DNS (GetHosts itself errors, mirroring resourceRecordRead's existing
// custom-DNS handling), or the domain's email_type not being "FWD". Both are
// apply-time warnings, never errors - the forwards are still stored
// account-side and take effect once the conflict is resolved.
func checkEmailForwardingConflict(ctx context.Context, domain string, client *namecheap.Client) diag.Diagnostics {
	resp, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return diag.Diagnostics{{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("Could not confirm %s uses Namecheap BasicDNS", domain),
			Detail: fmt.Sprintf(
				"Email forwarding requires the domain to use Namecheap BasicDNS (FreeDNS); %s appears to use custom DNS, so "+
					"the configured forwards will not take effect until it is switched back. Underlying error: %s",
				domain, err,
			),
		}}
	}

	if resp == nil || resp.DomainDNSGetHostsResult == nil {
		return nil
	}

	emailType := resp.DomainDNSGetHostsResult.EmailType
	if emailType != nil && *emailType != namecheap.EmailTypeForward {
		return diag.Diagnostics{{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("%s's email_type is not set to FWD", domain),
			Detail: fmt.Sprintf(
				"%s's email_type is %q, not %q. Set email_type = \"FWD\" on this domain's namecheap_domain_records resource "+
					"(or in the Namecheap dashboard) or mail will keep routing to the current MX targets instead of these forwards.",
				domain, *emailType, namecheap.EmailTypeForward,
			),
		}}
	}

	return nil
}

func resourceEmailForwardingRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	resp, err := client.DomainsDNS.GetEmailForwardingWithContext(ctx, domain)
	if err != nil {
		// A domain removed from the account is reported the same way as for
		// namecheap_domain_contacts (isDomainGoneError); treat it as gone.
		if isDomainGoneError(err) {
			data.SetId("")
			return nil
		}
		return diagFromClientError(err)
	}

	if resp == nil || resp.DomainDNSGetEmailForwardingResult == nil {
		data.SetId("")
		return nil
	}

	var forwards []namecheap.EmailForward
	if resp.DomainDNSGetEmailForwardingResult.Forwards != nil {
		forwards = *resp.DomainDNSGetEmailForwardingResult.Forwards
	}

	if err := data.Set("forwards", forwardsSliceToMap(forwards)); err != nil {
		return diag.FromErr(err)
	}
	if err := data.Set("domain", domain); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceEmailForwardingDelete(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	_, err := client.DomainsDNS.SetEmailForwardingWithContext(ctx, domain, []namecheap.EmailForward{})
	if err != nil {
		// Destroying against an already-gone domain is a no-op success, like
		// namecheap_domain_contacts' Read handling of the same error class.
		if isDomainGoneError(err) {
			return nil
		}
		return diagFromClientError(err)
	}

	return nil
}
