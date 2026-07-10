---
page_title: "namecheap_email_forwarding Resource - terraform-provider-namecheap"
subcategory: ""
description: |-
  Manages a domain's entire email forwarding table (mailbox alias -> destination address).
---

# namecheap_email_forwarding (Resource)

Manages a domain's email forwarding rules via the Namecheap
[`domains.dns.getEmailForwarding` / `domains.dns.setEmailForwarding`](https://www.namecheap.com/support/api/methods/domains-dns/set-email-forwarding/)
API.

~> **Full-ownership resource:** `setEmailForwarding` replaces the domain's **entire** forwarding table in one call, so this resource owns every rule for the domain. Forwarding rules created outside Terraform (e.g. through the dashboard) surface as drift on the next refresh and are **replaced** on the next apply. Destroying this resource clears the table entirely.

~> **Requires Namecheap BasicDNS/FreeDNS and `email_type = "FWD"`:** email forwarding only takes effect when the domain uses Namecheap's default DNS and its [`namecheap_domain_records`](./domain_records.md) resource (or the dashboard) has `email_type` set to `"FWD"`. If either condition isn't met, `apply` still succeeds and stores the rules, but emits a warning — the rules will not route mail until the mismatch is fixed.

## Example Usage

```terraform
resource "namecheap_domain_records" "example-com" {
  domain     = "example.com"
  email_type = "FWD"
}

resource "namecheap_email_forwarding" "example-com" {
  domain = "example.com"

  forwards = {
    info  = "me@example.com"
    sales = "sales-team@example.com"
    # "*" is the catch-all alias
    "*" = "catchall@example.com"
  }
}
```

## Argument Reference

- `domain` - (Required, Force New) The registered root domain whose email forwarding is managed (e.g. `example.com`). Must be a root domain, not a subdomain. Changing this forces a new resource.
- `forwards` - (Required) Map of mailbox alias to destination email address. Must be non-empty — destroy the resource instead of emptying this map to remove all forwarding. Each key must be a lowercase local alias with no `@` or whitespace (e.g. `info`), or `*` for a catch-all; each value must look like an email address.

## Import

Email forwarding can be imported by domain name, e.g.,

```terraform
terraform import namecheap_email_forwarding.example-com example.com
```
