---
page_title: "namecheap_domain Data Source - terraform-provider-namecheap"
subcategory: "Domains"
description: |-
  Read-only information about a single domain in the account.
---

# namecheap_domain (Data Source)

Looks up read-only information about a single domain. Use it to reference an existing (non-Terraform-managed) domain's DNS provider, nameservers or expiry in other resources, or to gate logic on domain properties (is it expiring? locked? which DNS type is active?).

~> This data source performs **two** Namecheap API calls per read: `namecheap.domains.getInfo` supplies the DNS-oriented fields (`dns_provider_type`, `is_our_dns`, `is_premium`, `is_premium_dns`, `nameservers`), and `namecheap.domains.getList` supplies the lifecycle fields (`created`, `expires`, `is_expired`, `is_locked`, `auto_renew`, `whois_guard`) that `getInfo` does not return. If you only need lifecycle data for many domains at once, prefer the [`namecheap_domains`](domains.md) portfolio data source, which returns all of it in one paginated listing.

## Example Usage

```terraform
data "namecheap_domain" "main" {
  domain = "example.com"
}

output "uses_namecheap_dns" {
  value = data.namecheap_domain.main.is_our_dns
}

output "nameservers" {
  value = data.namecheap_domain.main.nameservers
}

output "expires" {
  value = data.namecheap_domain.main.expires
}
```

## Argument Reference

- `domain` - (Required) The domain name to look up (e.g. `example.com`). Must be a registered root domain, not a subdomain.

## Attribute Reference

- `dns_provider_type` - The DNS provider currently serving the domain (e.g. `NAMECHEAP`, `FreeDNS`, `Custom`).
- `is_our_dns` - Whether the domain is using Namecheap's DNS.
- `is_premium` - Whether the domain is a premium domain.
- `is_premium_dns` - Whether the domain has an active PremiumDNS subscription.
- `nameservers` - The nameservers currently configured for the domain.
- `created` - Registration date as an RFC3339 timestamp (UTC).
- `expires` - Expiration date as an RFC3339 timestamp (UTC).
- `expires_in_days` - Whole calendar days until the domain expires (negative if already expired). Derived from the wall clock at read time; prefer `expires` where a stable value is needed.
- `is_expired` - Whether the domain has expired.
- `is_locked` - Whether the registrar lock is enabled.
- `auto_renew` - Whether auto-renew is enabled.
- `whois_guard` - WhoisGuard status (e.g. `ENABLED`, `DISABLED`, `NOTPRESENT`).
