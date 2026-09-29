//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccMockRegression pins the fixes for historical bugs so they cannot
// silently regress. These are guardrails, run fast and credential-free against
// the stateful mock.
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
	// it is excluded from ImportStateVerify, as is the adopted marker (#355).
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
					ImportStateVerifyIgnore: []string{"mode", "adopted"},
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
	// #355: after `terraform import` in MERGE mode, state held every live record,
	// so the first apply (and a destroy before any apply) treated undeclared
	// records as managed and deleted them at Namecheap. Import now marks the
	// records as adopted; the first MERGE apply releases the undeclared ones from
	// state without touching the zone, and a MERGE destroy before any apply
	// deletes nothing.
	t.Run("regression_355_merge_import_apply_keeps_undeclared", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
}
`, domain)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			// After a real apply the resource owns only www, so destroy removes
			// www and leaves the undeclared home record alone.
			CheckDestroy: resource.ComposeTestCheckFunc(
				mockCheckHostCount(m, domain, 1),
				mockCheckHostContains(m, domain, "home", "A", "203.0.113.20"),
			),
			Steps: []resource.TestStep{
				{
					Config:             config,
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
					ImportStateCheck: func(states []*terraform.InstanceState) error {
						if len(states) != 1 {
							return fmt.Errorf("expected 1 imported state, got %d", len(states))
						}
						if got := states[0].Attributes["adopted"]; got != "true" {
							return fmt.Errorf("adopted after import = %q, want \"true\"", got)
						}
						if got := states[0].Attributes["record.#"]; got != "2" {
							return fmt.Errorf("record.# after import = %q, want \"2\"", got)
						}
						return nil
					},
				},
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						mockCheckHostCount(m, domain, 2),
						mockCheckHostContains(m, domain, "www", "A", "203.0.113.10"),
						mockCheckHostContains(m, domain, "home", "A", "203.0.113.20"),
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
						// Every declared record is already live as declared, so
						// releasing home from state needs no zone rewrite.
						mockCheckCommandCount(m, "namecheap.domains.dns.setHosts", 0),
					),
				},
			},
		})
	})

	t.Run("regression_355_merge_import_destroy_keeps_undeclared", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			// Nothing was ever applied, so destroy must not touch the zone.
			CheckDestroy: mockCheckHostCount(m, domain, 2),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
}
`, domain),
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
			},
		})
	})

	// Portfolio adoption from the importing guide: a MERGE resource with no
	// record blocks. The first apply must settle ownership without writing to
	// the zone at all, and every live record stays.
	t.Run("regression_355_merge_import_no_records_writes_nothing", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"
}
`, domain)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckHostCount(m, domain, 2),
			Steps: []resource.TestStep{
				{
					Config:             config,
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						mockCheckHostCount(m, domain, 2),
						resource.TestCheckResourceAttr(resourceName, "record.#", "0"),
						resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
						func(*terraform.State) error {
							if got := m.commandCount("namecheap.domains.dns.setHosts"); got != 0 {
								return fmt.Errorf("setHosts called %d time(s) while settling an import with no declared records, want 0", got)
							}
							return nil
						},
					),
				},
			},
		})
	})

	// Configuration that already matches the imported zone exactly: the plan
	// is still non-empty (adopted is recomputed) so that one apply settles
	// ownership, but no write is needed for it.
	t.Run("regression_355_merge_import_matching_config_settles_without_write", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
}
`, domain)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckHostsCleared(m, domain),
			Steps: []resource.TestStep{
				{
					Config:             config,
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						mockCheckHostCount(m, domain, 1),
						resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
						func(*terraform.State) error {
							if got := m.commandCount("namecheap.domains.dns.setHosts"); got != 0 {
								return fmt.Errorf("setHosts called %d time(s) while settling an import that matches config, want 0", got)
							}
							return nil
						},
					),
				},
			},
		})
	})

	// OVERWRITE keeps its documented semantics after import: the first apply
	// owns the whole zone and removes undeclared records (with the #250 warning
	// at apply time). Guards that the adoption marker does not weaken OVERWRITE.
	t.Run("regression_355_overwrite_import_apply_still_owns_zone", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
}
`, domain)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckHostsCleared(m, domain),
			Steps: []resource.TestStep{
				{
					Config:             config,
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						mockCheckHostCount(m, domain, 1),
						mockCheckHostContains(m, domain, "www", "A", "203.0.113.10"),
						resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
					),
				},
			},
		})
	})

	// An externally delegated domain (custom nameservers) adopted with the
	// portfolio config declares nothing, so settling ownership must not touch
	// the delegation: the pre-existing "custom DNS but no nameservers declared"
	// reset would otherwise switch the domain to Namecheap default DNS.
	t.Run("regression_355_merge_import_custom_ns_keeps_delegation", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, nil, "NONE", []string{"ns1.example-dns.net", "ns2.example-dns.net"})

		config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"
}
`, domain)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameservers(m, domain, "ns1.example-dns.net", "ns2.example-dns.net"),
			Steps: []resource.TestStep{
				{
					Config:             config,
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						mockCheckNameservers(m, domain, "ns1.example-dns.net", "ns2.example-dns.net"),
						mockCheckCommandCount(m, "namecheap.domains.dns.setDefault", 0),
						mockCheckCommandCount(m, "namecheap.domains.dns.setCustom", 0),
						resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
						resource.TestCheckResourceAttr(resourceName, "nameservers.#", "0"),
					),
				},
			},
		})
	})

	// A declared email_type is part of the configuration the settle apply
	// reconciles: it must reach Namecheap on that apply, both when the declared
	// records already match the zone exactly (so no record differs) and when no
	// records are declared at all.
	t.Run("regression_355_merge_import_applies_email_type", func(t *testing.T) {
		mx := hostEntry{Name: "@", Type: "MX", Address: "mail.example.com.", MXPref: 10, TTL: 1800}
		home := hostEntry{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800}
		for _, tc := range []struct {
			name   string
			hosts  []hostEntry
			record string
		}{
			{name: "with_matching_record", hosts: []hostEntry{mx}, record: `
  record {
    hostname = "@"
    type     = "MX"
    address  = "mail.example.com."
    mx_pref  = 10
  }
`},
			{name: "without_records", hosts: []hostEntry{mx, home}, record: ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := newNamecheapMock(t)
				m.seed(domain, tc.hosts, "NONE", nil)

				config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain     = "%s"
  mode       = "MERGE"
  email_type = "MX"
%s}
`, domain, tc.record)

				resource.Test(t, resource.TestCase{
					PreCheck:          func() { mockPreCheck(t, m) },
					ProviderFactories: mockProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config:             config,
							ResourceName:       resourceName,
							ImportState:        true,
							ImportStateId:      domain,
							ImportStatePersist: true,
						},
						{
							Config: config,
							Check: resource.ComposeTestCheckFunc(
								mockCheckEmailType(m, domain, "MX"),
								mockCheckHostCount(m, domain, len(tc.hosts)),
								mockCheckHostContains(m, domain, "@", "MX", "mail.example.com."),
								resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
							),
						},
					},
				})
			})
		}
	})

	// A declared record that is live under the same hostname/type/address but
	// with a different ttl is not yet "as declared": the settle apply must still
	// write it. Guards the write decision against matching on identity alone.
	t.Run("regression_355_merge_import_settles_declared_ttl", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
    ttl      = 300
  }
}
`, domain)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config:             config,
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						mockCheckHostCount(m, domain, 2),
						mockCheckHostContains(m, domain, "home", "A", "203.0.113.20"),
						func(*terraform.State) error {
							for _, h := range m.state(domain).hosts {
								if h.Name == "www" && h.Type == "A" {
									if h.TTL != 300 {
										return fmt.Errorf("www A ttl after settle = %d, want 300", h.TTL)
									}
									return nil
								}
							}
							return fmt.Errorf("www A record missing after settle")
						},
						resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
					),
				},
			},
		})
	})

	// MERGE identifies a record by hostname, type and address, and adoption never
	// removes a live record. A declared record whose address differs from the
	// live one is therefore added alongside it; the live one is released from
	// state (and named in the warning), not replaced. Pins the documented
	// behaviour so a change to it is deliberate.
	t.Run("regression_355_merge_import_differing_address_adds_alongside", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.11", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
}
`, domain)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			// Destroy removes only the record the resource applied.
			CheckDestroy: resource.ComposeTestCheckFunc(
				mockCheckHostCount(m, domain, 2),
				mockCheckHostContains(m, domain, "www", "A", "203.0.113.11"),
				mockCheckHostContains(m, domain, "home", "A", "203.0.113.20"),
			),
			Steps: []resource.TestStep{
				{
					Config:             config,
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
				{
					Config: config,
					Check: resource.ComposeTestCheckFunc(
						mockCheckHostCount(m, domain, 3),
						mockCheckHostContains(m, domain, "www", "A", "203.0.113.10"),
						mockCheckHostContains(m, domain, "www", "A", "203.0.113.11"),
						mockCheckHostContains(m, domain, "home", "A", "203.0.113.20"),
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						resource.TestCheckResourceAttr(resourceName, "adopted", "false"),
					),
				},
			},
		})
	})

	// Import stores mode = MERGE regardless of the configured mode, and Delete
	// reads the mode from state, so a destroy before the first apply is the
	// MERGE no-op even when the configuration says OVERWRITE: nothing was ever
	// applied, so nothing is deleted.
	t.Run("regression_355_overwrite_import_destroy_before_apply_keeps_zone", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy: resource.ComposeTestCheckFunc(
				mockCheckHostCount(m, domain, 2),
				mockCheckCommandCount(m, "namecheap.domains.dns.setHosts", 0),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
}
`, domain),
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
					ImportStateCheck: func(states []*terraform.InstanceState) error {
						if got := states[0].Attributes["mode"]; got != "MERGE" {
							return fmt.Errorf("mode after import = %q, want MERGE", got)
						}
						return nil
					},
				},
			},
		})
	})
}
