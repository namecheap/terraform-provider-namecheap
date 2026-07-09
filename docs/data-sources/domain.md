---
page_title: "namecheap_domain Data Source - terraform-provider-namecheap"
subcategory: ""
description: |-
  Read-only information about a single domain in the account.
---

# namecheap_domain (Data Source)

Looks up read-only information about a single domain via the Namecheap
`namecheap.domains.getInfo` API command. Use it to reference an existing
(non-Terraform-managed) domain's DNS provider or nameservers in other resources,
or to gate logic on whether a domain uses Namecheap DNS.

~> `getInfo` returns DNS-oriented information only. Domain lifecycle fields
(`created`, `expires`, `is_expired`, `is_locked`, `auto_renew`, `whois_guard`)
are **not** returned by `getInfo` — they are available per-domain from the
[`namecheap_domains`](domains.md) portfolio data source instead.

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
```

## Argument Reference

- `domain` - (Required) The domain name to look up (e.g. `example.com`). Must be a registered root domain, not a subdomain.

## Attribute Reference

- `dns_provider_type` - The DNS provider currently serving the domain (e.g. `NAMECHEAP`, `FreeDNS`, `Custom`).
- `is_our_dns` - Whether the domain is using Namecheap's DNS.
- `is_premium` - Whether the domain is a premium domain.
- `is_premium_dns` - Whether the domain has an active PremiumDNS subscription.
- `nameservers` - The nameservers currently configured for the domain.
