---
page_title: "namecheap_domains Data Source - terraform-provider-namecheap"
subcategory: ""
description: |-
  The account's domain portfolio, with optional filtering.
---

# namecheap_domains (Data Source)

Lists the account's domain portfolio via the Namecheap `namecheap.domains.getList`
API command. The data source **auto-paginates** across all result pages, so the
`domains` attribute always reflects the complete result set for the given
filters.

## Example Usage

```terraform
data "namecheap_domains" "all" {
  search_term = ""    # optional keyword filter
  list_type   = "ALL" # ALL | EXPIRING | EXPIRED
}

output "domain_names" {
  value = [for d in data.namecheap_domains.all.domains : d.name]
}
```

## Portfolio-wide composition pattern

The portfolio pairs naturally with `for_each` to apply uniform policy (SPF,
DMARC, verification records, ...) across every domain in the account in a few
lines:

```terraform
data "namecheap_domains" "all" {
  list_type = "ALL"
}

resource "namecheap_domain_records" "spf" {
  for_each = { for d in data.namecheap_domains.all.domains : d.name => d }

  domain = each.value.name
  mode   = "MERGE"

  record {
    hostname = "@"
    type     = "TXT"
    address  = "v=spf1 include:spf.example.com -all"
  }
}
```

## Argument Reference

- `search_term` - (Optional) Keyword to filter the returned domains. Maps to the getList `SearchTerm` parameter.
- `list_type` - (Optional) Which subset of the account's domains to return. Possible values: `ALL` (default), `EXPIRING`, `EXPIRED`. Maps to the getList `ListType` parameter.

## Attribute Reference

- `domains` - The list of domains matching the filters. Each element has the following attributes:
  - `id` - Namecheap internal domain identifier.
  - `name` - The domain name (e.g. `example.com`).
  - `user` - The account user the domain belongs to.
  - `created` - Registration date as an RFC3339 timestamp (UTC).
  - `expires` - Expiration date as an RFC3339 timestamp (UTC).
  - `expires_in_days` - Whole calendar days until the domain expires (negative if already expired).
  - `is_expired` - Whether the domain has expired.
  - `is_locked` - Whether the registrar lock is enabled.
  - `auto_renew` - Whether auto-renew is enabled.
  - `whois_guard` - WhoisGuard status (e.g. `ENABLED`, `DISABLED`, `NOTPRESENT`).
  - `is_premium` - Whether the domain is a premium domain.
  - `is_our_dns` - Whether the domain is using Namecheap's DNS.
