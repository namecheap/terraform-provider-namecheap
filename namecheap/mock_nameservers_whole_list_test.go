//go:build testacc

package namecheap_provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccMockDomainRecordsNameserversWholeList covers custom nameservers as one
// setting of the domain, the same in MERGE and OVERWRITE mode: the configured
// list is the whole delegation, no nameservers means Namecheap DNS, and state
// always holds every live nameserver, so a plan lists each one an apply removes.
func TestAccMockDomainRecordsNameserversWholeList(t *testing.T) {
	const domain = hostRecordTestDomain
	const recordsResource = "namecheap_domain_records.dns"
	const setCustom = "namecheap.domains.dns.setCustom"
	const setDefault = "namecheap.domains.dns.setDefault"

	live := []string{"ns1.dns-host.net", "ns2.dns-host.net"}
	const wwwBlock = `
  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
`
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
	// importStep imports a delegated domain and checks that the import stored
	// both of its nameservers.
	importStep := func(id, cfg string) resource.TestStep {
		return resource.TestStep{
			Config:             cfg,
			ResourceName:       recordsResource,
			ImportState:        true,
			ImportStateId:      id,
			ImportStatePersist: true,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				if got := states[0].Attributes["nameservers.#"]; got != "2" {
					return fmt.Errorf("imported nameservers.# = %q, want 2", got)
				}
				return nil
			},
		}
	}
	writes := func(m *namecheapMock) int { return m.commandCount(setCustom) + m.commandCount(setDefault) }

	for _, mode := range []string{ncModeMerge, ncModeOverwrite} {
		for _, id := range []string{domain, domain + "/zone"} {
			// Both import IDs store the nameservers, so a configuration that
			// declares them plans no change.
			t.Run(mode+": import by "+id+" with the nameservers declared plans no change", func(t *testing.T) {
				m := newNamecheapMock(t)
				cfg := config(mode, live, "")
				resource.Test(t, resource.TestCase{
					PreCheck:          func() { mockPreCheck(t, m); m.seed(domain, nil, "", live) },
					ProviderFactories: mockProviderFactories(),
					Steps: []resource.TestStep{
						importStep(id, cfg),
						// MERGE is what an import stores, so only OVERWRITE has a
						// change of mode left to apply.
						{Config: cfg, PlanOnly: true, ExpectNonEmptyPlan: mode == ncModeOverwrite},
						{Config: cfg, Check: mockCheckNameservers(m, domain, live...)},
					},
				})
				if n := writes(m) - m.commandCount(setDefault); mode == ncModeMerge && n != 0 {
					t.Fatalf("%d setCustom call(s) before destroy, want 0: nothing changed", n)
				}
			})
		}

		// A configuration without nameservers means Namecheap DNS. After an
		// import the plan says so: it lists the nameservers as removed. Nothing
		// is written until that plan is applied.
		t.Run(mode+": import with no nameservers declared plans their removal", func(t *testing.T) {
			m := newNamecheapMock(t)
			cfg := config(mode, nil, "")
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m); m.seed(domain, nil, "", live) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					importStep(domain, cfg),
					{
						PreConfig: func() {
							if n := writes(m); n != 0 {
								t.Fatalf("%d setCustom/setDefault call(s) after the import, want 0", n)
							}
						},
						Config:             cfg,
						PlanOnly:           true,
						ExpectNonEmptyPlan: true,
					},
					{
						Config: cfg,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "0"),
							mockCheckNameserversDefault(m, domain),
						),
					},
				},
			})
		})

		// A nameserver added in the dashboard is drift: the refresh stores it,
		// the plan lists its removal and the apply removes it. MERGE used to
		// keep it out of state and out of every plan.
		t.Run(mode+": a nameserver added in the dashboard shows in the plan", func(t *testing.T) {
			m := newNamecheapMock(t)
			cfg := config(mode, live, "")
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m) },
				ProviderFactories: mockProviderFactories(),
				CheckDestroy:      mockCheckNameserversDefault(m, domain),
				Steps: []resource.TestStep{
					{Config: cfg, Check: mockCheckNameservers(m, domain, live...)},
					{
						PreConfig:          func() { m.seed(domain, nil, "", append([]string{"ns3.dashboard.net"}, live...)) },
						Config:             cfg,
						PlanOnly:           true,
						ExpectNonEmptyPlan: true,
					},
					{Config: cfg, Check: mockCheckNameservers(m, domain, live...)},
				},
			})
		})

		// Records in the configuration mean Namecheap DNS. When the domain is
		// delegated in the dashboard, the refresh stores its nameservers, the
		// plan lists them as removed, and the apply moves the domain back and
		// writes the records. Without the refresh storing them, the plan showed
		// the records alone.
		t.Run(mode+": records on a domain delegated in the dashboard plan the nameservers' removal", func(t *testing.T) {
			m := newNamecheapMock(t)
			cfg := config(mode, nil, wwwBlock)
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					{Config: cfg, Check: assertZoneHas(m, "www", "A", "203.0.113.10")},
					{
						PreConfig:          func() { m.seed(domain, nil, "NONE", live) },
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
						Check:              resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "2"),
					},
					{
						Config: cfg,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(recordsResource, "nameservers.#", "0"),
							mockCheckNameserversDefault(m, domain),
							assertZoneHas(m, "www", "A", "203.0.113.10"),
						),
					},
				},
			})
		})
	}

	// Replacing nameservers with records, and back, in MERGE as in OVERWRITE.
	t.Run("MERGE: nameservers to records and back", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameserversDefault(m, domain),
			Steps: []resource.TestStep{
				{Config: config(ncModeMerge, live, ""), Check: mockCheckNameservers(m, domain, live...)},
				{
					Config: config(ncModeMerge, nil, wwwBlock),
					Check: resource.ComposeTestCheckFunc(
						mockCheckNameserversDefault(m, domain),
						assertZoneHas(m, "www", "A", "203.0.113.10"),
					),
				},
				{Config: config(ncModeMerge, live, ""), Check: mockCheckNameservers(m, domain, live...)},
			},
		})
	})
}
