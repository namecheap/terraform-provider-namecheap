//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMockDomainRecordsImportNameservers covers the two import IDs of
// namecheap_domain_records on a domain delegated to custom nameservers.
//
// In MERGE the nameservers in state are the ones the resource owns: an apply
// removes an owned nameserver the configuration does not declare, and destroy
// removes them all. An import by the plain domain name used to store every
// nameserver, so a configuration that declared none of them planned to move
// the domain back to Namecheap DNS.
func TestAccMockDomainRecordsImportNameservers(t *testing.T) {
	const domain = hostRecordTestDomain
	const recordsResource = "namecheap_domain_records.dns"
	const setCustom = "namecheap.domains.dns.setCustom"
	const setDefault = "namecheap.domains.dns.setDefault"

	declared := []string{"ns1.dns-host.net", "ns2.dns-host.net"}
	undeclared := []string{"ns3.manual.net", "ns4.manual.net"}
	all := append(append([]string{}, declared...), undeclared...)

	seed := func(m *namecheapMock, nameservers []string) { m.seed(domain, nil, "", nameservers) }

	config := func(mode string, nameservers []string, body string) string {
		line := ""
		if len(nameservers) > 0 {
			line = "  nameservers = [\"" + strings.Join(nameservers, `", "`) + "\"]\n"
		}
		return fmt.Sprintf(`
resource "namecheap_domain_records" "dns" {
  domain = %q
  mode   = %q
%s%s}
`, domain, mode, line, body)
	}
	importStep := func(id, cfg string) resource.TestStep {
		return resource.TestStep{
			Config:             cfg,
			ResourceName:       recordsResource,
			ImportState:        true,
			ImportStateId:      id,
			ImportStatePersist: true,
		}
	}
	// noDelegationChange fails when the domain's nameservers were written.
	noDelegationChange := func(t *testing.T, m *namecheapMock) {
		t.Helper()
		if n := m.commandCount(setCustom) + m.commandCount(setDefault); n != 0 {
			t.Fatalf("%d setCustom/setDefault call(s), want 0", n)
		}
	}

	// The portfolio case: a configuration with nothing but the domain. No plan,
	// apply or destroy of it may touch the domain's delegation.
	t.Run("plain ID with a config that declares no nameserver plans no change", func(t *testing.T) {
		m := newNamecheapMock(t)
		cfg := config(ncModeMerge, nil, "")
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameservers(m, domain, all...),
			Steps: []resource.TestStep{
				importStep(domain, cfg),
				{Config: cfg, PlanOnly: true},
				{
					Config: cfg,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "0"),
						mockCheckNameservers(m, domain, all...),
					),
				},
			},
		})
		noDelegationChange(t, m)
	})

	// Declared nameservers that are already live are adopted by the first apply.
	// The two nobody declared stay, through the apply and the destroy.
	t.Run("plain ID with a config that declares some nameservers adopts them", func(t *testing.T) {
		m := newNamecheapMock(t)
		cfg := config(ncModeMerge, declared, "")
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameservers(m, domain, undeclared...),
			Steps: []resource.TestStep{
				importStep(domain, cfg),
				{
					Config: cfg,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "2"),
						mockCheckNameservers(m, domain, all...),
					),
				},
				{Config: cfg, PlanOnly: true},
			},
		})
	})

	t.Run("plain ID with a config that declares every nameserver adopts them", func(t *testing.T) {
		m := newNamecheapMock(t)
		cfg := config(ncModeMerge, declared, "")
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, declared) },
			ProviderFactories: mockProviderFactories(),
			// The resource owns both now, so destroy returns the domain to
			// Namecheap DNS.
			CheckDestroy: mockCheckNameserversDefault(m, domain),
			Steps: []resource.TestStep{
				importStep(domain, cfg),
				{
					Config: cfg,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "2"),
						mockCheckNameservers(m, domain, declared...),
					),
				},
				{Config: cfg, PlanOnly: true},
			},
		})
	})

	// MERGE does not need an import: a create adopts the declared nameservers
	// that are already live, where it used to fail with "Duplicate nameserver".
	t.Run("a config applied without an import adopts live nameservers", func(t *testing.T) {
		m := newNamecheapMock(t)
		cfg := config(ncModeMerge, declared, "")
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameservers(m, domain, undeclared...),
			Steps: []resource.TestStep{
				{
					Config: cfg,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "2"),
						mockCheckNameservers(m, domain, all...),
					),
				},
				{Config: cfg, PlanOnly: true},
			},
		})
	})

	// Taking the nameservers out of a MERGE configuration releases the ones the
	// resource set and keeps the rest. It used to reset the whole delegation.
	t.Run("removing the nameservers from a config keeps the ones it did not set", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, undeclared) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameservers(m, domain, undeclared...),
			Steps: []resource.TestStep{
				{
					Config: config(ncModeMerge, declared, ""),
					Check:  mockCheckNameservers(m, domain, all...),
				},
				{
					Config: config(ncModeMerge, nil, ""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "0"),
						mockCheckNameservers(m, domain, undeclared...),
					),
				},
			},
		})
		if n := m.commandCount(setDefault); n != 0 {
			t.Fatalf("%d setDefault call(s), want 0: the domain stays delegated", n)
		}
	})

	// Records and email_type need Namecheap DNS. A MERGE resource does not move
	// a domain off nameservers it does not manage to write them: the plan fails,
	// where the apply used to reset the delegation with only the records in the
	// plan.
	const wwwBlock = `
  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
`
	delegatedElsewhere := regexp.MustCompile(`(?s)is\s+delegated\s+to\s+custom\s+nameservers\s+this\s+resource\s+does\s+not\s+manage.*` +
		`ns3\.manual\.net.*` + regexp.QuoteMeta(domain+"/zone"))
	for name, body := range map[string]string{"records": wwwBlock, "email_type": "  email_type = \"GMAIL\"\n"} {
		t.Run("adding "+name+" to a resource of a domain delegated elsewhere fails at plan", func(t *testing.T) {
			m := newNamecheapMock(t)
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
				ProviderFactories: mockProviderFactories(),
				CheckDestroy:      mockCheckNameservers(m, domain, all...),
				Steps: []resource.TestStep{
					importStep(domain, config(ncModeMerge, nil, "")),
					{Config: config(ncModeMerge, nil, "")},
					{
						Config:      config(ncModeMerge, nil, body),
						ExpectError: delegatedElsewhere,
					},
					{Config: config(ncModeMerge, nil, "")},
				},
			})
			noDelegationChange(t, m)
		})
	}

	// The resource set two of the four nameservers. Replacing them with records
	// would leave the domain on the other two, so that plan fails as well.
	t.Run("replacing its nameservers with records fails at plan while others remain", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, undeclared) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameservers(m, domain, undeclared...),
			Steps: []resource.TestStep{
				{Config: config(ncModeMerge, declared, "")},
				{
					Config:      config(ncModeMerge, nil, wwwBlock),
					ExpectError: delegatedElsewhere,
				},
				{
					Config: config(ncModeMerge, declared, ""),
					Check:  mockCheckNameservers(m, domain, all...),
				},
			},
		})
	})

	// When the resource manages every nameserver, the plan lists their removal,
	// and the apply returns the domain to Namecheap DNS and writes the records.
	t.Run("replacing its nameservers with records moves the domain to Namecheap DNS", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config(ncModeMerge, declared, ""),
					Check:  mockCheckNameservers(m, domain, declared...),
				},
				{
					Config: config(ncModeMerge, nil, wwwBlock),
					Check: resource.ComposeTestCheckFunc(
						mockCheckNameserversDefault(m, domain),
						assertZoneHas(m, "www", "A", "203.0.113.10"),
					),
				},
			},
		})
	})

	t.Run("/zone ID with a config that declares every nameserver plans no change", func(t *testing.T) {
		m := newNamecheapMock(t)
		cfg := config(ncModeMerge, all, "")
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				importStep(domain+"/zone", cfg),
				{Config: cfg, PlanOnly: true},
				{
					Config: cfg,
					Check:  resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "4"),
				},
			},
		})
	})

	// After a /zone import the resource owns every nameserver, and one the
	// configuration leaves out is removed: the plan lists it.
	t.Run("/zone ID with a config that declares some nameservers removes the rest", func(t *testing.T) {
		m := newNamecheapMock(t)
		cfg := config(ncModeMerge, declared, "")
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				importStep(domain+"/zone", cfg),
				{
					Config: cfg,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "2"),
						mockCheckNameservers(m, domain, declared...),
					),
				},
			},
		})
	})

	// OVERWRITE replaces the domain's nameservers with the configured ones. After
	// a plain import none is in state, so the plan could not list the two it
	// drops, and fails instead.
	t.Run("plain import with an OVERWRITE config that declares some nameservers fails at plan", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				importStep(domain, config(ncModeOverwrite, declared, "")),
				{
					Config: config(ncModeOverwrite, declared, ""),
					ExpectError: regexp.MustCompile(`(?s)would\s+remove\s+2\s+custom\s+nameserver\(s\).*ns3\.manual\.net.*ns4\.manual\.net.*` +
						regexp.QuoteMeta(domain+"/zone")),
				},
				{
					PreConfig: func() { noDelegationChange(t, m) },
					Config:    config(ncModeOverwrite, all, ""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "mode", ncModeOverwrite),
						mockCheckNameservers(m, domain, all...),
					),
				},
			},
		})
	})

	// An OVERWRITE configuration with records, or with nothing, moves a delegated
	// domain back to Namecheap DNS. The plan could not show that either.
	for name, body := range map[string]string{
		"records": `
  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
`,
		"nothing": "",
	} {
		t.Run("plain import of a delegated domain with an OVERWRITE config that declares "+name+" fails at plan", func(t *testing.T) {
			m := newNamecheapMock(t)
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
				ProviderFactories: mockProviderFactories(),
				// Destroy of a resource that owns nothing leaves the delegation.
				CheckDestroy: mockCheckNameservers(m, domain, all...),
				Steps: []resource.TestStep{
					importStep(domain, config(ncModeOverwrite, nil, body)),
					{
						Config:      config(ncModeOverwrite, nil, body),
						ExpectError: regexp.MustCompile(`(?s)would\s+remove\s+4\s+custom\s+nameserver\(s\).*ns1\.dns-host\.net`),
					},
				},
			})
			noDelegationChange(t, m)
		})
	}

	// The way to let OVERWRITE drop nameservers: after a /zone import they are
	// in state, and the plan lists the two it removes.
	t.Run("/zone import with an OVERWRITE config removes the nameservers the plan lists", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m, all) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				importStep(domain+"/zone", config(ncModeOverwrite, declared, "")),
				{
					Config: config(ncModeOverwrite, declared, ""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "mode", ncModeOverwrite),
						mockCheckNameservers(m, domain, declared...),
					),
				},
			},
		})
	})
}
