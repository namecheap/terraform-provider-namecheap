package namecheap_provider

import (
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// diagFromClientError converts an error returned by a go-namecheap-sdk call
// into diag.Diagnostics.
//
// When err wraps a *namecheap.APIError whose Number matches one of the
// codes below, it returns a targeted diagnostic with remediation text so a
// Terraform user can tell auth/permission/not-found failures apart instead
// of reading a raw SDK error string.
//
// Every other error - including any *namecheap.APIError code that is not
// explicitly mapped - falls through unchanged to diag.FromErr(err), which is
// exactly what every call site did before this mapping existed. This keeps
// the change strictly additive: no caller can regress by adopting it.
//
// Mapped codes are sourced two ways: the per-method "Error Codes" tables of
// docs/namecheap-api-v2.md in namecheap/go-namecheap-sdk (2019166, 2016166,
// 3016166), and Namecheap's global auth response code 1011102 ("API Key is
// invalid or API access has not been enabled"), the single most common failure
// for CI users, which covers a wrong key, disabled API access, a
// sandbox/production credential mismatch, or a calling IP that is not
// whitelisted. Rate limiting is intentionally NOT mapped here: the API
// surfaces it as a pre-request HTTP 405 that go-namecheap-sdk absorbs in its
// transport retry layer, so it never reaches this function as an *APIError
// (set the requests_per_minute provider option to avoid it).
func diagFromClientError(err error) diag.Diagnostics {
	var apiErr *namecheap.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Number {
		case 1011102:
			// Namecheap global response code 1011102: "API Key is invalid or
			// API access has not been enabled" - deliberately ambiguous on
			// Namecheap's side, so list every remediation.
			return diag.Diagnostics{{
				Severity: diag.Error,
				Summary:  "Namecheap API authentication failed (invalid credentials or IP not whitelisted)",
				Detail: fmt.Sprintf(
					"Namecheap API error %d: %s. This one error covers several causes - check each: "+
						"(1) api_user/api_key are correct for the target environment (sandbox credentials differ from production; set use_sandbox to match); "+
						"(2) API access is enabled for the account; "+
						"(3) the public IP this provider calls from is whitelisted at "+
						"https://ap.www.namecheap.com/settings/tools/apiaccess/whitelisted-ips - the most common cause in CI, where the runner's egress IP changes (set client_ip to that egress IP).",
					apiErr.Number, apiErr.Message,
				),
			}}
		case 2019166:
			// docs/namecheap-api-v2.md § Error Codes: "Domain not found".
			return diag.Diagnostics{{
				Severity: diag.Error,
				Summary:  "Domain not found",
				Detail: fmt.Sprintf(
					"Namecheap API error %d: %s. Verify the domain name is spelled correctly and is present in the Namecheap account associated with the configured ApiUser.",
					apiErr.Number, apiErr.Message,
				),
			}}
		case 2016166, 3016166:
			// docs/namecheap-api-v2.md § Error Codes: "Domain is not associated
			// with your account" (2016166) / "...with Enom" (3016166).
			return diag.Diagnostics{{
				Severity: diag.Error,
				Summary:  "Domain not associated with this account",
				Detail: fmt.Sprintf(
					"Namecheap API error %d: %s. The domain is not associated with the Namecheap account used to authenticate (ApiUser/ApiKey). Confirm you are using the API credentials for the account that owns this domain.",
					apiErr.Number, apiErr.Message,
				),
			}}
		}
	}

	return diag.FromErr(err)
}
