//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMockRegression pins the fixes for four historical bugs so they cannot
// silently regress. All four issues are closed/fixed; these are guardrails, run
// fast and credential-free against the stateful mock.
func TestAccMockRegression(t *testing.T) {
	const domain = "mock-example.com"
	const resourceName = "namecheap_domain_records.test"

	// #71: a FreeDNS subdomain (a domain value that parses to a non-empty
	// third-level part) was silently written to the root zone. It is now rejected
	// at validation time by validateDomainIsNotSubdomain.
	t.Run("regression_71_subdomain_rejected", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: `
resource "namecheap_domain_records" "test" {
  domain = "sub.mock-example.com"
  mode   = "MERGE"

  record {
    hostname = "@"
    type     = "A"
    address  = "1.2.3.4"
  }
}
`,
					ExpectError: regexp.MustCompile(`contains a subdomain`),
				},
			},
		})
	})

	// #37: creating an MX record failed. It now succeeds; the provider appends a
	// trailing dot to the MX address on the server side, and the record is
	// idempotent on replan (this exercises the MERGE path, complementing the
	// OVERWRITE MX case in the scenario matrix).
	t.Run("regression_37_mx_create", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckHostsCleared(m, domain),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain     = "%s"
  mode       = "MERGE"
  email_type = "MX"

  record {
    hostname = "@"
    type     = "MX"
    address  = "mail.example.com"
    mx_pref  = 10
  }
}
`, domain),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						resource.TestCheckResourceAttr(resourceName, "email_type", "MX"),
						mockCheckEmailType(m, domain, "MX"),
						mockCheckHostContains(m, domain, "@", "MX", "mail.example.com."),
					),
				},
			},
		})
	})

	// #68: `terraform import` set an internal mode=IMPORT that the schema's mode
	// validator rejected ("expected mode to be one of [MERGE OVERWRITE]"). Import
	// now succeeds. mode is import-only and differs from the configured value, so
	// it is excluded from ImportStateVerify.
	t.Run("regression_68_import", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameserversDefault(m, domain),
			Steps: []resource.TestStep{
				{
					Config: mockNameserversConfig("OVERWRITE",
						"ns-1467.awsdns-55.org", "ns-1076.awsdns-06.org"),
					Check: mockCheckNameservers(m, domain,
						"ns-1467.awsdns-55.org", "ns-1076.awsdns-06.org"),
				},
				{
					ResourceName:            resourceName,
					ImportState:             true,
					ImportStateId:           domain,
					ImportStateVerify:       true,
					ImportStateVerifyIgnore: []string{"mode"},
				},
			},
		})
	})

	// #73: a pre-existing CAA iodef record (with a quoted value) broke all record
	// creation in MERGE mode, because MERGE re-sent the existing record through
	// the address fixer. The fixer now round-trips a quoted 3-part iodef value, so
	// an unrelated MERGE add succeeds and the CAA record is preserved.
	t.Run("regression_73_caa_iodef_preserved", func(t *testing.T) {
		m := newNamecheapMock(t)
		const caaAddress = `0 iodef "mailto:support@mock-example.com"`
		m.seed(domain, []hostEntry{
			{Name: "@", Type: "CAA", Address: caaAddress, MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy: resource.ComposeTestCheckFunc(
				mockCheckHostCount(m, domain, 1),
				mockCheckHostContains(m, domain, "@", "CAA", caaAddress),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "test"
    type     = "A"
    address  = "1.1.1.1"
    ttl      = 1800
  }
}
`, domain),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						mockCheckHostCount(m, domain, 2),
						mockCheckHostContains(m, domain, "test", "A", "1.1.1.1"),
						mockCheckHostContains(m, domain, "@", "CAA", caaAddress),
					),
				},
			},
		})
	})
}
