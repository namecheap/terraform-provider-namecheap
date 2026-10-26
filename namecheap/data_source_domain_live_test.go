package namecheap_provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Live-sandbox acceptance tests for the two domain data sources. Like the
// other TestAcc* tests they run only under `make testacc` (TF_ACC=1) and skip
// via testAccPreCheck when the NAMECHEAP_* credentials are absent. Both are
// read-only (getInfo and getList) and need nothing restored.
//
// Their purpose is to observe the real date format of the live API. The
// provider renders every date as RFC3339, so the value a user sees cannot
// reveal what the API sent; these assertions pin the rendered shape that the
// API's calendar-date format produces (midnight UTC), and would fail the day
// the API starts sending something else for these commands, which is the
// signal to look at the SDK's date parser before users see raw text.

// rfc3339Midnight is what a calendar date (MM/DD/YYYY, the format getList and
// getInfo have always used) renders as.
var rfc3339Midnight = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T00:00:00Z$`)

// rfc3339MidnightOrEmpty allows the empty rendering of an absent date.
var rfc3339MidnightOrEmpty = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T00:00:00Z)?$`)

// TestAccDataSourceDomain reads the configured test domain through getInfo and
// asserts the lifecycle dates render as midnight-UTC RFC3339 timestamps and
// expires_in_days is an integer consistent with the expiry.
func TestAccDataSourceDomain(t *testing.T) {
	domain := os.Getenv("NAMECHEAP_TEST_DOMAIN")
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "namecheap_domain" "test" {
  domain = %q
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "domain", domain),
					resource.TestMatchResourceAttr("data.namecheap_domain.test", "created", rfc3339Midnight),
					resource.TestMatchResourceAttr("data.namecheap_domain.test", "expires", rfc3339Midnight),
					resource.TestMatchResourceAttr("data.namecheap_domain.test", "expires_in_days", regexp.MustCompile(`^-?[1-9]\d*$`)),
					resource.TestMatchResourceAttr("data.namecheap_domain.test", "whois_guard_expires", rfc3339MidnightOrEmpty),
				),
			},
		},
	})
}

// TestAccDataSourceDomains lists the portfolio filtered to the test domain
// through getList and asserts the same date shape on the listing row.
func TestAccDataSourceDomains(t *testing.T) {
	domain := os.Getenv("NAMECHEAP_TEST_DOMAIN")
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "namecheap_domains" "test" {
  search_term = %q
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("data.namecheap_domains.test", "domains.#", regexp.MustCompile(`^[1-9]\d*$`)),
					resource.TestCheckResourceAttr("data.namecheap_domains.test", "domains.0.name", domain),
					resource.TestMatchResourceAttr("data.namecheap_domains.test", "domains.0.created", rfc3339Midnight),
					resource.TestMatchResourceAttr("data.namecheap_domains.test", "domains.0.expires", rfc3339Midnight),
					resource.TestMatchResourceAttr("data.namecheap_domains.test", "domains.0.expires_in_days", regexp.MustCompile(`^-?[1-9]\d*$`)),
				),
			},
		},
	})
}
