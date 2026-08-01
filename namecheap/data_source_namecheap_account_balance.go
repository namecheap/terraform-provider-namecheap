package namecheap_provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// accountBalanceID is the synthetic ID of the account-balance data source. The
// underlying command (namecheap.users.getBalances) takes no parameters and
// describes the account the provider is configured with, so there is nothing to
// key the ID on; a constant keeps it stable across refreshes.
const accountBalanceID = "account_balance"

// dataSourceNamecheapAccountBalance exposes the funds in the account the
// provider is authenticated as, so a configuration can gate charge-bearing
// operations (registration, renewal, transfer) on affordability before spending.
//
// Every monetary attribute is exported as a *string* holding the exact decimal
// the API returned, never a number: Terraform numbers are IEEE-754 floats, and
// a price or balance that round-trips through a float can silently change. Use
// tonumber() at the comparison site when a numeric check is needed, and accept
// that rounding is then yours to reason about.
func dataSourceNamecheapAccountBalance() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceNamecheapAccountBalanceRead,
		Schema: map[string]*schema.Schema{
			"currency": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The currency the amounts are listed in (e.g. USD).",
			},
			"available_balance": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Total amount available in the account, as an exact decimal string (e.g. \"123.45\"). This is the figure to gate a charge-bearing apply on.",
			},
			"account_balance": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Total amount in the account, as an exact decimal string. Per the Namecheap API this is the same figure as available_balance.",
			},
			"earned_amount": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Amount earned from Marketplace sales, as an exact decimal string.",
			},
			"withdrawable_amount": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Amount available for withdrawal, as an exact decimal string.",
			},
			"funds_required_for_auto_renew": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Amount required to auto-renew the domains in the account, as an exact decimal string.",
			},
		},
	}
}

func dataSourceNamecheapAccountBalanceRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	resp, err := client.Users.GetBalancesWithContext(ctx)
	if err != nil {
		return diagFromClientError(err)
	}
	if resp == nil || resp.UserGetBalancesResult == nil {
		return diag.Errorf("Namecheap returned no balance information for this account")
	}

	balances := resp.UserGetBalancesResult
	_ = data.Set("currency", balances.Currency)
	_ = data.Set("available_balance", balances.AvailableBalance.String())
	_ = data.Set("account_balance", balances.AccountBalance.String())
	_ = data.Set("earned_amount", balances.EarnedAmount.String())
	_ = data.Set("withdrawable_amount", balances.WithdrawableAmount.String())
	_ = data.Set("funds_required_for_auto_renew", balances.FundsRequiredForAutoRenew.String())

	data.SetId(accountBalanceID)
	return nil
}
