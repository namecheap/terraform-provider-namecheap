data "namecheap_tld_pricing" "candidates" {
  for_each = toset(["com", "net", "org"])

  tld    = each.key
  action = "REGISTER"
  years  = 1
}

locals {
  prices         = { for tld, p in data.namecheap_tld_pricing.candidates : tld => tonumber(p.price) }
  cheapest_price = min(values(local.prices)...)

  # sort() makes the tie-break deterministic: promotions routinely price several
  # TLDs identically, and picking "the" cheapest one would otherwise fail.
  cheapest_tlds = sort([for tld, price in local.prices : tld if price == local.cheapest_price])
}

output "cheapest_tld" {
  value = local.cheapest_tlds[0]
}
