//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMockDomainRecordsModeSwitch covers the plan-time check on a change of
// mode from MERGE to OVERWRITE.
//
// OVERWRITE deletes every record the configuration does not declare, and a plan
// lists a removal only for a record in state. A MERGE state holds the records
// the resource declared, so without the check the plan shows a change of mode
// and the apply deletes the rest of the zone. An OVERWRITE configuration gets
// that plan after an import by the plain domain name, which stores no records.
func TestAccMockDomainRecordsModeSwitch(t *testing.T) {
	const domain = hostRecordTestDomain
	const recordsResource = "namecheap_domain_records.zone"
	const setHosts = "namecheap.domains.dns.setHosts"

	// The error is wrapped by Terraform, so words are matched across line breaks.
	wouldDeleteHome := regexp.MustCompile(`(?s)would\s+delete\s+1\s+record\(s\).*hostname = "home".*` +
		regexp.QuoteMeta(domain+"/zone"))

	seed := func(m *namecheapMock) {
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "198.51.100.7", MXPref: 10, TTL: 60},
		}, "GMAIL", nil)
	}

	const wwwBlock = `
  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
`
	const homeBlock = `
  record {
    hostname = "home"
    type     = "A"
    address  = "198.51.100.7"
    ttl      = 60
  }
`
	config := func(mode, body string) string {
		return fmt.Sprintf(`
resource "namecheap_domain_records" "zone" {
  domain = %q
  mode   = %q
%s}
`, domain, mode, body)
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
	// noWriteSoFar fails when the zone was written before this step: the step
	// before it must have been stopped at the plan.
	noWriteSoFar := func(t *testing.T, m *namecheapMock) func() {
		return func() {
			if n := m.commandCount(setHosts); n != 0 {
				t.Fatalf("%d setHosts call(s) before this step, want 0: a refused plan must not write", n)
			}
		}
	}

	for name, body := range map[string]string{"some records": wwwBlock, "no record": ""} {
		t.Run("plain import with an OVERWRITE config that declares "+name+" fails at plan", func(t *testing.T) {
			m := newNamecheapMock(t)
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m); seed(m) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					importStep(domain, config(ncModeOverwrite, body)),
					{
						Config:      config(ncModeOverwrite, body),
						ExpectError: regexp.MustCompile(`(?s)would\s+delete\s+\d\s+record\(s\).*hostname = "home"`),
					},
					{
						// Declaring every record is the first way out the error
						// gives: the same switch of mode then deletes nothing.
						PreConfig: noWriteSoFar(t, m),
						Config:    config(ncModeOverwrite, wwwBlock+homeBlock),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(recordsResource, "mode", ncModeOverwrite),
							resource.TestCheckResourceAttr(recordsResource, "record.#", "2"),
							assertZoneHas(m, "www", "A", "203.0.113.10"),
							assertZoneHas(m, "home", "A", "198.51.100.7"),
							mockCheckHostCount(m, domain, 2),
						),
					},
				},
			})
		})
	}

	// The second way out: after a /zone import every record is in state, so the
	// plan lists the removal of "home" and the check has nothing to report.
	t.Run("/zone import with an OVERWRITE config deletes the records the plan lists", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				importStep(domain+"/zone", config(ncModeOverwrite, wwwBlock)),
				{
					Config: config(ncModeOverwrite, wwwBlock),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "mode", ncModeOverwrite),
						resource.TestCheckResourceAttr(recordsResource, "record.#", "1"),
						assertZoneHas(m, "www", "A", "203.0.113.10"),
						assertZoneLacks(m, "home", "A", "198.51.100.7"),
						mockCheckHostCount(m, domain, 1),
					),
				},
			},
		})
	})

	// No import involved: a MERGE resource that manages "www" in a zone where
	// another tool keeps "home" is switched to OVERWRITE.
	t.Run("a MERGE resource switched to OVERWRITE fails at plan while the zone holds other records", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				mockPreCheck(t, m)
				m.seed(domain, []hostEntry{{Name: "home", Type: "A", Address: "198.51.100.7", MXPref: 10, TTL: 60}}, "GMAIL", nil)
			},
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config(ncModeMerge, wwwBlock),
					Check:  mockCheckHostCount(m, domain, 2),
				},
				{
					Config:      config(ncModeOverwrite, wwwBlock),
					ExpectError: wouldDeleteHome,
				},
				{
					Config: config(ncModeMerge, wwwBlock),
					Check: resource.ComposeTestCheckFunc(
						assertZoneHas(m, "home", "A", "198.51.100.7"),
						mockCheckHostCount(m, domain, 2),
					),
				},
			},
		})
	})

	// A MERGE resource that already manages the whole zone has nothing to lose.
	t.Run("a MERGE resource that manages the whole zone switches to OVERWRITE", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{Config: config(ncModeMerge, wwwBlock+homeBlock)},
				{
					Config: config(ncModeOverwrite, wwwBlock+homeBlock),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "mode", ncModeOverwrite),
						mockCheckHostCount(m, domain, 2),
					),
				},
			},
		})
	})

	// The default parking records are not records anyone declared or would miss:
	// every OVERWRITE write replaces them without a warning.
	t.Run("default parking records do not fail the plan", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				mockPreCheck(t, m)
				m.seed(domain, []hostEntry{
					{Name: "www", Type: "CNAME", Address: "parkingpage.namecheap.com.", MXPref: 10, TTL: 1800},
					{Name: "@", Type: "URL", Address: "http://www." + domain + "/?from=@", MXPref: 10, TTL: 1800},
				}, "NONE", nil)
			},
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				importStep(domain, config(ncModeOverwrite, homeBlock)),
				{
					Config: config(ncModeOverwrite, homeBlock),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "mode", ncModeOverwrite),
						assertZoneHas(m, "home", "A", "198.51.100.7"),
						mockCheckHostCount(m, domain, 1),
					),
				},
			},
		})
	})

	// With nameservers the apply delegates the domain and writes no records, so
	// there is nothing to refuse.
	t.Run("an OVERWRITE config with nameservers is not checked", func(t *testing.T) {
		m := newNamecheapMock(t)
		const nameservers = `
  nameservers = ["ns1.example.net", "ns2.example.net"]
`
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				importStep(domain, config(ncModeOverwrite, nameservers)),
				{
					Config: config(ncModeOverwrite, nameservers),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "mode", ncModeOverwrite),
						mockCheckNameservers(m, domain, "ns1.example.net", "ns2.example.net"),
					),
				},
			},
		})
		if n := m.commandCount(setHosts); n != 0 {
			t.Fatalf("%d setHosts call(s), want 0: delegating a domain writes no records", n)
		}
	})
}
