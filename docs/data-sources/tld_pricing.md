---
page_title: "namecheap_tld_pricing Data Source - terraform-provider-namecheap"
subcategory: ""
description: |-
  The published Namecheap price for one TLD, action and term.
---

# namecheap_tld_pricing (Data Source)

Looks up what Namecheap charges for one TLD, one action (register, renew or
transfer) and one term, via the `namecheap.users.getPricing` API command. The
request is narrowed server-side, so each data source instance is exactly one API
call rather than a walk of the full price sheet.

## Example Usage

```terraform
data "namecheap_tld_pricing" "com" {
  tld    = "com"
  action = "REGISTER"
  years  = 1
}

output "com_price" {
  value = "${data.namecheap_tld_pricing.com.price} ${data.namecheap_tld_pricing.com.currency}"
}
```

## Comparing TLDs

Use `for_each` to price several TLDs, then pick with ordinary HCL. Compare with
`tonumber()`, never as strings — `"8.88" < "10.50"` is false lexicographically:

```terraform
data "namecheap_tld_pricing" "candidates" {
  for_each = toset(["com", "net", "org"])

  tld    = each.key
  action = "REGISTER"
  years  = 1
}

output "cheapest_tld" {
  value = one([
    for tld, p in data.namecheap_tld_pricing.candidates : tld
    if tonumber(p.price) == min([for q in data.namecheap_tld_pricing.candidates : tonumber(q.price)]...)
  ])
}
```

Each element of a `for_each` is its own read, so pricing three TLDs is three API
calls.

## Money is exported as strings

Every monetary attribute is a **string** holding the exact decimal Namecheap
returned (`"8.88"`), never a number, for the same reason as
[`namecheap_account_balance`](account_balance.md): Terraform numbers are
IEEE-754 floats and a price must not change value by round-tripping through one.
Convert with `tonumber()` at the point of comparison.

## Which attribute is "the price"?

- `price` is what you would actually be charged. It resolves the precedence
  Namecheap documents: the server's final price (which already reflects any
  promotion or special), then your account price, then the regular price.
- `regular_price` is the public list price — use it to show a discount.
- `promo_price` identifies a promotion when the API reports one. It does **not**
  add to `price`; an active promotion is already reflected there.

## Argument Reference

- `tld` - (Required) The top-level domain to price, written without a leading dot (`com`, `co.uk`). Validated at plan time.
- `action` - (Optional) The action to price: `REGISTER` (default), `RENEW` or `TRANSFER`.
- `years` - (Optional) The term length in years to price, 1-10. Defaults to `1`.

## Attribute Reference

- `price` - The price actually charged for this tier, as an exact decimal string (see above).
- `regular_price` - The public list price for this tier, as an exact decimal string.
- `your_price` - The account-specific price for this tier, as an exact decimal string. Empty when the API does not return one.
- `promo_price` - The promotional price for this tier, as an exact decimal string, or empty when the API reported no promotion. Namecheap does not document what a zero promotional price means, so `"0.00"` is passed through as reported rather than being treated as "no promotion".
- `currency` - The currency the prices are denominated in (e.g. `USD`). Empty when the API omits it for this tier; `namecheap_account_balance` always reports the account currency.
- `duration_type` - The unit the term is expressed in, as returned by the API (always `YEAR` for the tiers this data source matches).
- `id` - `pricing:<tld>:<action>:<years>`.

## Notes

- Only domain pricing is exposed. SSL and WhoisGuard products live under
  different product types in the same API command and would need a different
  attribute shape.
- A TLD Namecheap does not sell, or a term it does not publish a tier for, is an
  error naming the TLD, action and term — not a silent zero price.
- Prices change. The data source is re-read on every plan, so a long-lived plan
  file can hold a stale price; nothing here reserves a price for a later apply.
