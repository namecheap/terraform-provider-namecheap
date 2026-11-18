package namecheap_provider

import (
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

func TestDiagFromClientError(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		wantSummary    string
		wantDetailPart string
	}{
		{
			name:           "domain not found",
			err:            &namecheap.APIError{Number: 2019166, Message: "Domain not found", Command: "namecheap.domains.dns.getHosts"},
			wantSummary:    "Domain not found",
			wantDetailPart: "Verify the domain name is spelled correctly and is present in the Namecheap account",
		},
		{
			name:           "domain not associated with account",
			err:            &namecheap.APIError{Number: 2016166, Message: "Domain is not associated with your account", Command: "namecheap.domains.dns.getHosts"},
			wantSummary:    "Domain not associated with this account",
			wantDetailPart: "Confirm you are using the API credentials for the account that owns this domain",
		},
		{
			name:           "domain not associated with Enom",
			err:            &namecheap.APIError{Number: 3016166, Message: "Domain is not associated with Enom", Command: "namecheap.domains.dns.setHosts"},
			wantSummary:    "Domain not associated with this account",
			wantDetailPart: "Confirm you are using the API credentials for the account that owns this domain",
		},
		{
			name:           "auth failure or IP not whitelisted",
			err:            &namecheap.APIError{Number: 1011102, Message: "API Key is invalid or API access has not been enabled", Command: "namecheap.domains.dns.getHosts"},
			wantSummary:    "Namecheap API authentication failed (invalid credentials or IP not whitelisted)",
			wantDetailPart: "whitelisted-ips",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diags := diagFromClientError(c.err)

			assert.True(t, diags.HasError())
			assert.Len(t, diags, 1)
			assert.Equal(t, c.wantSummary, diags[0].Summary)
			assert.Contains(t, diags[0].Detail, c.wantDetailPart)
		})
	}
}

// TestDiagFromClientError_Fallthrough locks in the non-breaking guarantee:
// any error that is not a mapped *namecheap.APIError code - including an
// unmapped APIError code and a plain error - must produce exactly the same
// diagnostics as diag.FromErr(err) did before this mapping existed.
func TestDiagFromClientError_Fallthrough(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "plain unmapped error",
			err:  errors.New("boom"),
		},
		{
			name: "unmapped APIError code",
			err:  &namecheap.APIError{Number: 9999999, Message: "Some other failure", Command: "namecheap.domains.dns.setHosts"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diags := diagFromClientError(c.err)

			assert.True(t, diags.HasError())
			assert.Len(t, diags, 1)
			assert.Equal(t, c.err.Error(), diags[0].Summary)
		})
	}
}

// TestImportDiagError covers the one place that turns diagnostics into the
// single error an importer returns, for every resource that imports.
func TestImportDiagError(t *testing.T) {
	t.Run("keeps the detail", func(t *testing.T) {
		// For an ambiguous record match the detail is the list of candidates,
		// which is the whole message.
		err := importDiagError("example.com", diag.Diagnostics{{
			Severity: diag.Error,
			Summary:  "Ambiguous DNS record on example.com",
			Detail:   "  - TTL 300\n  - TTL 7200",
		}})
		assert.EqualError(t, err, "reading example.com during import: Ambiguous DNS record on example.com:   - TTL 300\n  - TTL 7200")
	})

	t.Run("no detail", func(t *testing.T) {
		err := importDiagError("example.com", diag.Diagnostics{{Severity: diag.Error, Summary: "Domain not found"}})
		assert.EqualError(t, err, "reading example.com during import: Domain not found")
	})

	t.Run("skips a warning that comes before the error", func(t *testing.T) {
		err := importDiagError("example.com", diag.Diagnostics{
			{Severity: diag.Warning, Summary: "Unmanaged records"},
			{Severity: diag.Error, Summary: "Domain not found"},
		})
		assert.EqualError(t, err, "reading example.com during import: Domain not found")
	})

	t.Run("no error", func(t *testing.T) {
		assert.NoError(t, importDiagError("example.com", nil))
		assert.NoError(t, importDiagError("example.com", diag.Diagnostics{{Severity: diag.Warning, Summary: "Unmanaged records"}}))
	})
}
