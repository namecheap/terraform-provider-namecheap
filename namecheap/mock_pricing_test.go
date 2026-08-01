//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Mock-backed acceptance coverage for the two money data sources. These run the
// real Terraform binary against the stateful mock, so they cover the plan/apply
// path — schema wiring, validation diagnostics and the cost-aware precondition
// pattern the registry docs recommend — which the direct ReadContext unit tests
// in data_source_pricing_read_test.go cannot reach.

// TestAccMockDataSourceAccountBalance reads the account funds and asserts every
// exported attribute, including that amounts keep the server's exact decimal
// string rather than being reformatted through a float.
func TestAccMockDataSourceAccountBalance(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedBalances(mockAccountBalance{
		Currency:                  "USD",
		AvailableBalance:          "123.45",
		AccountBalance:            "123.45",
		EarnedAmount:              "15.00",
		WithdrawableAmount:        "100.10",
		FundsRequiredForAutoRenew: "42.50",
	})

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_account_balance" "current" {}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "currency", "USD"),
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "available_balance", "123.45"),
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "account_balance", "123.45"),
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "earned_amount", "15.00"),
					// A trailing zero surviving proves the value is passed through as
					// the server's decimal string, not parsed and re-rendered.
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "withdrawable_amount", "100.10"),
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "funds_required_for_auto_renew", "42.50"),
					resource.TestCheckResourceAttr("data.namecheap_account_balance.current", "id", accountBalanceID),
				),
			},
		},
	})
}

// TestAccMockDataSourceAccountBalanceError asserts an API failure on the balance
// read surfaces as a diagnostic instead of an empty, apparently-broke account.
func TestAccMockDataSourceAccountBalanceError(t *testing.T) {
	m := newNamecheapMock(t) // no seedBalances: the mock answers with an API error

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_account_balance" "current" {}
`,
				ExpectError: regexp.MustCompile("balances are not available"),
			},
		},
	})
}

// TestAccMockDataSourceTldPricing reads a single TLD price and asserts the
// exported attributes plus the absence of any collateral API traffic — the read
// must be one narrowed getPricing call, not a portfolio walk.
func TestAccMockDataSourceTldPricing(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedPricing("REGISTER", "com",
		mockPriceTier{Duration: 1, DurationType: "YEAR", Price: "8.88", RegularPrice: "10.87", YourPrice: "9.99", Currency: "USD"},
		mockPriceTier{Duration: 2, DurationType: "YEAR", Price: "17.76", RegularPrice: "21.74", YourPrice: "19.98", Currency: "USD"},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_tld_pricing" "com" {
  tld    = "com"
  action = "REGISTER"
  years  = 1
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.com", "price", "8.88"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.com", "regular_price", "10.87"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.com", "your_price", "9.99"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.com", "promo_price", ""),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.com", "currency", "USD"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.com", "duration_type", "YEAR"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.com", "id", "pricing:com:REGISTER:1"),
					assertNoCommands(m, "namecheap.domains.getList", "namecheap.domains.getInfo", "namecheap.domains.dns.getHosts"),
					assertCommandCalled(m, "namecheap.users.getPricing"),
				),
			},
		},
	})
}

// TestAccMockDataSourceTldPricingPromotion covers a promotional tier end to end:
// promo_price reports the discount while price still reports what is
// actually charged.
func TestAccMockDataSourceTldPricingPromotion(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedPricing("REGISTER", "shop",
		mockPriceTier{Duration: 1, DurationType: "YEAR", Price: "1.16", RegularPrice: "19.99", YourPrice: "1.16", Currency: "EUR", Promotion: "1.16"},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_tld_pricing" "shop" {
  tld = "shop"
}
`,
				Check: resource.ComposeTestCheckFunc(
					// action defaults to REGISTER and years to 1.
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.shop", "action", "REGISTER"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.shop", "years", "1"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.shop", "price", "1.16"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.shop", "promo_price", "1.16"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.shop", "regular_price", "19.99"),
					resource.TestCheckResourceAttr("data.namecheap_tld_pricing.shop", "currency", "EUR"),
				),
			},
		},
	})
}

// TestAccMockDataSourceTldPricingUnknownTld asserts that a TLD Namecheap does
// not price produces a diagnostic naming it, rather than a silent zero price.
func TestAccMockDataSourceTldPricingUnknownTld(t *testing.T) {
	m := newNamecheapMock(t) // nothing seeded: every lookup returns an empty sheet

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_tld_pricing" "nope" {
  tld = "notatld"
}
`,
				ExpectError: regexp.MustCompile(`No REGISTER price published for \.notatld`),
			},
		},
	})
}

// TestAccMockDataSourceTldPricingInvalidInput asserts the plan-time validators
// reject bad input before any API call is made.
func TestAccMockDataSourceTldPricingInvalidInput(t *testing.T) {
	m := newNamecheapMock(t)

	for name, tc := range map[string]struct{ config, wantErr string }{
		"leading dot": {
			config: `
data "namecheap_tld_pricing" "t" {
  tld = ".com"
}
`,
			wantErr: `without a leading dot`,
		},
		"unknown action": {
			config: `
data "namecheap_tld_pricing" "t" {
  tld    = "com"
  action = "BUY"
}
`,
			wantErr: `expected action to be one of`,
		},
		"years out of range": {
			config: `
data "namecheap_tld_pricing" "t" {
  tld   = "com"
  years = 99
}
`,
			wantErr: `expected years to be in the range`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      tc.config,
						ExpectError: regexp.MustCompile(tc.wantErr),
					},
				},
			})
		})
	}
}

// TestAccMockCostAwarePreconditionBlocks is the pattern the registry docs
// recommend: gate a charge-bearing apply on the account balance. With funds
// below the threshold the plan fails with the operator's own message, before
// anything is spent.
func TestAccMockCostAwarePreconditionBlocks(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedBalances(mockAccountBalance{Currency: "USD", AvailableBalance: "3.20", AccountBalance: "3.20"})
	m.seedPricing("REGISTER", "com",
		mockPriceTier{Duration: 1, DurationType: "YEAR", Price: "8.88", RegularPrice: "10.87", YourPrice: "9.99", Currency: "USD"},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      costAwareConfig,
				ExpectError: regexp.MustCompile("Insufficient Namecheap balance"),
			},
		},
	})
}

// TestAccMockCostAwarePreconditionPasses is the same configuration with enough
// funds: the precondition holds and the composed outputs resolve, proving the
// two data sources compose as documented rather than only failing convincingly.
func TestAccMockCostAwarePreconditionPasses(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedBalances(mockAccountBalance{Currency: "USD", AvailableBalance: "250.00", AccountBalance: "250.00"})
	m.seedPricing("REGISTER", "com",
		mockPriceTier{Duration: 1, DurationType: "YEAR", Price: "8.88", RegularPrice: "10.87", YourPrice: "9.99", Currency: "USD"},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: costAwareConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckOutput("registration_cost", "8.88"),
					resource.TestCheckOutput("balance", "250.00"),
				),
			},
		},
	})
}

// costAwareConfig is the documented cost-aware pattern: read the price, read the
// balance, and refuse to proceed unless the funds cover the purchase. tonumber()
// is applied only at the comparison, keeping the exported values exact decimals.
const costAwareConfig = `
data "namecheap_account_balance" "current" {}

data "namecheap_tld_pricing" "com" {
  tld    = "com"
  action = "REGISTER"
  years  = 1
}

output "registration_cost" {
  value = data.namecheap_tld_pricing.com.price
}

output "balance" {
  value = data.namecheap_account_balance.current.available_balance

  precondition {
    condition = tonumber(data.namecheap_account_balance.current.available_balance) >= tonumber(data.namecheap_tld_pricing.com.price)
    error_message = "Insufficient Namecheap balance for registration."
  }
}
`

// assertCommandCalled returns a check asserting the mock served at least one
// request for command.
func assertCommandCalled(m *namecheapMock, command string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := m.commandCount(command); got == 0 {
			return fmt.Errorf("commandCount(%q) = 0, want at least 1", command)
		}
		return nil
	}
}

// assertNoCommands returns a check asserting the mock never served any of the
// given commands, pinning that a pricing read does not fan out into unrelated
// API traffic.
func assertNoCommands(m *namecheapMock, commands ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, command := range commands {
			if got := m.commandCount(command); got != 0 {
				return fmt.Errorf("commandCount(%q) = %d, want 0", command, got)
			}
		}
		return nil
	}
}
