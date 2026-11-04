//go:build testacc

package namecheap_provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccMockDomainRecordsEmailTypeState covers email_type in state: every read,
// import included, stores the zone's mail mode, and leaving email_type out of
// the config keeps that mode without a diff.
func TestAccMockDomainRecordsEmailTypeState(t *testing.T) {
	const domain = "mock-example.com"
	const resourceName = "namecheap_domain_records.test"

	config := func(emailType string) string {
		line := ""
		if emailType != "" {
			line = fmt.Sprintf("email_type = %q", emailType)
		}
		return fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"
  %s

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
  }
}
`, domain, line)
	}

	// Import used to leave email_type out of state, so a config naming the
	// zone's own mode planned "+ email_type" right after the import.
	t.Run("import stores the zone's mail mode", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				mockPreCheck(t, m)
				// getHosts reports MXPref="10" on every row, MX or not.
				m.seed(domain, []hostEntry{{Name: "www", Type: "A", Address: "10.0.0.1", MXPref: 10, TTL: 1800}}, "GMAIL", nil)
			},
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckEmailType(m, domain, "GMAIL"),
			Steps: []resource.TestStep{
				{
					Config:             config("GMAIL"),
					ResourceName:       resourceName,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
					ImportStateCheck: func(states []*terraform.InstanceState) error {
						if got := states[0].Attributes["email_type"]; got != "GMAIL" {
							return fmt.Errorf("imported email_type = %q, want %q", got, "GMAIL")
						}
						return nil
					},
				},
				{
					Config:   config("GMAIL"),
					PlanOnly: true,
				},
				{
					Config:   config(""),
					PlanOnly: true,
				},
			},
		})
	})

	// Leaving email_type out keeps the zone's mode, so dropping the line from a
	// config must not plan a change either.
	t.Run("removing email_type from the config plans no change", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckEmailType(m, domain, "OX"),
			Steps: []resource.TestStep{
				{
					Config: config("OX"),
					Check: resource.ComposeTestCheckFunc(
						mockCheckEmailType(m, domain, "OX"),
						resource.TestCheckResourceAttr(resourceName, "email_type", "OX"),
					),
				},
				{
					Config:   config(""),
					PlanOnly: true,
				},
			},
		})
	})

	// A record-only config on a Google Workspace domain reads the preset into
	// state and leaves it on the zone through create, update and destroy.
	t.Run("record-only writes keep a GMAIL zone on GMAIL", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				mockPreCheck(t, m)
				m.seed(domain, nil, "GMAIL", nil)
			},
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckEmailType(m, domain, "GMAIL"),
			Steps: []resource.TestStep{
				{
					Config: config(""),
					Check:  mockCheckEmailType(m, domain, "GMAIL"),
				},
				{
					Config: config(""),
					Check: resource.ComposeTestCheckFunc(
						mockCheckEmailType(m, domain, "GMAIL"),
						resource.TestCheckResourceAttr(resourceName, "email_type", "GMAIL"),
					),
				},
				{
					Config: `
resource "namecheap_domain_records" "test" {
  domain = "mock-example.com"
  mode   = "MERGE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.2"
  }
}
`,
					Check: resource.ComposeTestCheckFunc(
						mockCheckEmailType(m, domain, "GMAIL"),
						mockCheckHostContains(m, domain, "www", "A", "10.0.0.2"),
					),
				},
			},
		})
	})
}
