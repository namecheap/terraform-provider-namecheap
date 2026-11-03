//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var mxeNeedsRecordError = regexp.MustCompile(`email_type = "MXE" needs at least one record with type = "MXE"`)

// TestAccMockDomainRecordsMXENeedsMXERecord covers the plan-time check that
// email_type = "MXE" leaves the zone with at least one MXE record. Namecheap
// accepts MXE with none and the domain then has no mail server, so the check
// must fail the plan before any setHosts is sent.
func TestAccMockDomainRecordsMXENeedsMXERecord(t *testing.T) {
	const domain = "mock-example.com"

	config := func(mode string, withMXE bool) string {
		mxe := ""
		if withMXE {
			mxe = `
  record {
    hostname = "@"
    type     = "MXE"
    address  = "10.0.0.25"
  }`
		}
		return fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain     = "%s"
  mode       = "%s"
  email_type = "MXE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
  }%s
}
`, domain, mode, mxe)
	}

	for _, mode := range []string{ncModeOverwrite, ncModeMerge} {
		t.Run(mode+" without an MXE record fails at plan", func(t *testing.T) {
			m := newNamecheapMock(t)
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      config(mode, false),
						ExpectError: mxeNeedsRecordError,
					},
				},
			})
			if n := m.commandCount("namecheap.domains.dns.setHosts"); n != 0 {
				t.Errorf("setHosts calls = %d, want 0: the check must fail before any write", n)
			}
		})

		t.Run(mode+" dropping the last MXE record fails at plan", func(t *testing.T) {
			m := newNamecheapMock(t)
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config(mode, true),
						Check:  mockCheckHostContains(m, domain, "@", "MXE", "10.0.0.25"),
					},
					{
						Config:      config(mode, false),
						ExpectError: mxeNeedsRecordError,
					},
					{
						// The rejected plan wrote nothing: only the first step's write
						// reached setHosts (checked here, before destroy writes again),
						// and the first configuration plans clean. A PlanOnly step runs
						// no Check, so the count is asserted in PreConfig.
						PreConfig: func() {
							if n := m.commandCount("namecheap.domains.dns.setHosts"); n != 1 {
								t.Errorf("setHosts calls = %d, want 1: the rejected plan must not write", n)
							}
						},
						Config:   config(mode, true),
						PlanOnly: true,
					},
				},
			})
		})
	}

	t.Run("MERGE passes when another resource's MXE record is on the zone", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(domain, []hostEntry{
			{Name: "@", Type: "MXE", Address: "10.0.0.26", MXPref: 10, TTL: 1800},
		}, "MXE", nil)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config(ncModeMerge, false),
					Check: resource.ComposeTestCheckFunc(
						mockCheckEmailType(m, domain, "MXE"),
						mockCheckHostContains(m, domain, "@", "MXE", "10.0.0.26"),
						mockCheckHostContains(m, domain, "www", "A", "10.0.0.1"),
					),
				},
			},
		})
	})
}
