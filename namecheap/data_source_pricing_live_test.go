package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Live-sandbox acceptance tests for the two money data sources. Like the other
// TestAcc* tests they run only under `make testacc` (TF_ACC=1) and skip via
// testAccPreCheck when the NAMECHEAP_* credentials are absent, so they never
// touch the real API without explicit opt-in.
//
// Both are strictly read-only — getBalances and getPricing charge nothing and
// change nothing — so unlike the resource suites there is no state to restore
// and nothing to clean up.

// TestAccDataSourceAccountBalance reads the sandbox account's funds against the
// live API. Sandbox balances are arbitrary, so the assertions are on shape, not
// value: the currency must be a three-letter code and every amount must be a
// decimal string the API actually sent — which is what proves the exact-decimal
// contract survives a real response rather than only a crafted one.
func TestAccDataSourceAccountBalance(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "namecheap_account_balance" "current" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("data.namecheap_account_balance.current", "currency", regexp.MustCompile(`^[A-Z]{3}$`)),
					testAccCheckIsDecimalString("data.namecheap_account_balance.current", "available_balance"),
					testAccCheckIsDecimalString("data.namecheap_account_balance.current", "account_balance"),
					testAccCheckIsDecimalString("data.namecheap_account_balance.current", "earned_amount"),
					testAccCheckIsDecimalString("data.namecheap_account_balance.current", "withdrawable_amount"),
					testAccCheckIsDecimalString("data.namecheap_account_balance.current", "funds_required_for_auto_renew"),
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "id", accountBalanceID),
				),
			},
		},
	})
}

// TestAccDataSourceTldPricing prices .com against the live API. It is the only
// test in the suite that observes the real getPricing response shape, so it is
// what confirms the SDK's attribute names still match the server: a renamed or
// dropped Price attribute shows up here as an empty exported value, not as a
// passing test.
//
// It deliberately reads a single tier. The sandbox rate-limits the whole
// package's run (the DNS suites answer HTTP 405 once the rolling window is
// exhausted), so every live call added here is taken from a shared budget;
// action and TLD coverage therefore lives in the mock suite, where it is free.
func TestAccDataSourceTldPricing(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_tld_pricing" "register" {
  tld    = "com"
  action = "REGISTER"
  years  = 1
}
`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckIsDecimalString("data.namecheap_tld_pricing.register", "price"),
					testAccCheckIsDecimalString("data.namecheap_tld_pricing.register", "regular_price"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.register", "duration_type", "YEAR"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.register", "id", "pricing:com:REGISTER:1"),
					// Currency is optional per tier, so its absence is not a failure —
					// but when the server does send it, it must be a currency code.
					testAccCheckOptionalPattern("data.namecheap_tld_pricing.register", "currency", regexp.MustCompile(`^[A-Z]{3}$`)),
					// promo_price is empty unless the server quoted a positive
					// promotion; .com is normally un-promoted (the API says so with
					// "0.0"), and anything non-empty must still be a bare decimal.
					testAccCheckOptionalPattern("data.namecheap_tld_pricing.register", "promo_price", regexp.MustCompile(`^\d+(\.\d+)?$`)),
					// Log what the live API actually returned for the optional
					// attributes, so a sandbox run leaves evidence of the response
					// shape rather than only a pass/fail.
					testAccLogAttrs("data.namecheap_tld_pricing.register", "price", "regular_price", "your_price", "promo_price", "currency"),
				),
			},
		},
	})
}

// testAccCheckIsDecimalString asserts an attribute is present and is a bare
// decimal number as a string — no currency symbol, no thousands separator, no
// scientific notation — which is the exported-money contract both data sources
// promise.
func testAccCheckIsDecimalString(name, attr string) resource.TestCheckFunc {
	decimal := regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("data source %s not found in state", name)
		}
		value, ok := rs.Primary.Attributes[attr]
		if !ok {
			return fmt.Errorf("%s.%s not set", name, attr)
		}
		if !decimal.MatchString(value) {
			return fmt.Errorf("%s.%s = %q, want a bare decimal string", name, attr, value)
		}
		return nil
	}
}

// testAccCheckOptionalPattern asserts an attribute matches pattern when it is
// non-empty, and passes when it is empty. It is for attributes the API may
// legitimately omit, where asserting presence would make the test fail on a
// server that is behaving correctly.
func testAccCheckOptionalPattern(name, attr string, pattern *regexp.Regexp) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("data source %s not found in state", name)
		}
		value := rs.Primary.Attributes[attr]
		if value == "" {
			return nil
		}
		if !pattern.MatchString(value) {
			return fmt.Errorf("%s.%s = %q, want empty or matching %s", name, attr, value, pattern)
		}
		return nil
	}
}

// testAccLogAttrs prints the given attributes to the test log. It never fails;
// its purpose is to record what the live API returned, so a sandbox run is
// evidence about the response shape and not just a green tick.
func testAccLogAttrs(name string, attrs ...string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("data source %s not found in state", name)
		}
		for _, attr := range attrs {
			fmt.Printf("live %s.%s = %q\n", name, attr, rs.Primary.Attributes[attr])
		}
		return nil
	}
}
