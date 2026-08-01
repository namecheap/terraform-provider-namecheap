---
page_title: "namecheap_account_balance Data Source - terraform-provider-namecheap"
subcategory: ""
description: |-
  The funds available in the Namecheap account the provider is authenticated as.
---

# namecheap_account_balance (Data Source)

Reads the funds in the account the provider is authenticated as, via the
Namecheap `namecheap.users.getBalances` API command. It takes no arguments — the
account is the one the provider credentials belong to.

Its purpose is to make money visible to a plan: registrations, renewals and
transfers all draw on this balance, and Terraform has no other way to check
affordability before an apply starts spending.

## Example Usage

```terraform
data "namecheap_account_balance" "current" {}

output "funds" {
  value = "${data.namecheap_account_balance.current.available_balance} ${data.namecheap_account_balance.current.currency}"
}
```

## Cost-aware applies

Pair the balance with a `precondition` so a charge-bearing apply fails at plan
time — with your message — instead of part-way through a batch:

```terraform
data "namecheap_account_balance" "current" {}

data "namecheap_tld_pricing" "com" {
  tld    = "com"
  action = "REGISTER"
}

output "planned_registration" {
  value = data.namecheap_tld_pricing.com.price

  precondition {
    condition     = tonumber(data.namecheap_account_balance.current.available_balance) >= tonumber(data.namecheap_tld_pricing.com.price)
    error_message = "Insufficient Namecheap balance for registration."
  }
}
```

The same `precondition` block works inside a `lifecycle` block on a
charge-bearing resource, which is where you usually want it.

## Money is exported as strings

Every monetary attribute is a **string** holding the exact decimal Namecheap
returned (`"123.45"`), never a number. Terraform numbers are IEEE-754 floats, and
a balance or price that round-trips through a float can silently change value —
not acceptable for a figure that gates a charge. Convert with `tonumber()` at the
point of comparison, as above, so the rounding is explicit and local.

## Argument Reference

This data source takes no arguments.

## Attribute Reference

- `currency` - The currency the amounts are listed in (e.g. `USD`), as reported by the API.
- `available_balance` - Total amount available in the account, as an exact decimal string. This is the figure to gate a charge-bearing apply on.
- `account_balance` - Total amount in the account, as an exact decimal string. Per the Namecheap API this is the same figure as `available_balance`.
- `earned_amount` - Amount earned from Marketplace sales, as an exact decimal string.
- `withdrawable_amount` - Amount available for withdrawal, as an exact decimal string.
- `funds_required_for_auto_renew` - Amount required to auto-renew the domains in the account, as an exact decimal string.
- `id` - Always `account_balance`. The underlying command takes no parameters, so there is nothing to key the ID on.

## Notes

- The read is a single API call and is refreshed on every plan, so the value is
  current as of the plan — but nothing reserves those funds. A concurrent
  purchase elsewhere can still leave a later apply short.
- Adding funds is deliberately not supported by this provider: payment flows do
  not belong in `terraform apply`.
