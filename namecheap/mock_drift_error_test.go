//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMockDriftRefresh covers the drift-detection dimension: when the backend
// changes out-of-band, a refresh must surface a non-empty plan (rather than the
// provider silently ignoring the divergence).
func TestAccMockDriftRefresh(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"
	const resourceName = "namecheap_domain_records.test"

	config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
    ttl      = 1800
  }
}
`, domain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckHostsCleared(m, domain),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
					mockCheckHostContains(m, domain, "www", "A", "10.0.0.1"),
				),
			},
			{
				// Simulate an external change to the record's address, then plan
				// the unchanged config: the refresh must observe the drift and
				// produce a non-empty plan that would correct it back.
				PreConfig: func() {
					m.seed(domain, []hostEntry{
						{Name: "www", Type: "A", Address: "10.9.9.9", MXPref: 10, TTL: 1800},
					}, "NONE", nil)
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccMockErrorSurface covers the error-surface dimension: an API error must
// propagate as a Terraform error with the actionable, code-mapped diagnostic
// (diagnostics.go), not a silent success. Here the mock fails the GetHosts read
// during create with code 2019166, which maps to the "Domain not found"
// diagnostic.
func TestAccMockErrorSurface(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"
	m.failOn("namecheap.domains.dns.getHosts", "2019166", "Domain not found")

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "sub1"
    type     = "A"
    address  = "1.2.3.4"
  }
}
`, domain),
				ExpectError: regexp.MustCompile(`Domain not found`),
			},
		},
	})
}
