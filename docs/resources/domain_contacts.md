---
page_title: "namecheap_domain_contacts Resource - terraform-provider-namecheap"
subcategory: "Domains"
description: |-
  Manages a domain's WHOIS contact information (registrant, tech, admin and auxiliary billing contacts).
---

# namecheap_domain_contacts (Resource)

Manages the WHOIS contact information of a domain in your Namecheap account: the `registrant`, `tech`, `admin` and `aux_billing` contact blocks. It maps onto the Namecheap `namecheap.domains.getContacts` / `namecheap.domains.setContacts` API.

This is a standalone resource, well suited to domains whose registration is not otherwise managed by Terraform (imported portfolios), and to fleet-wide contact updates via `for_each`.

## Example Usage

```terraform
resource "namecheap_domain_contacts" "main" {
  domain = "example.com"

  registrant {
    first_name     = "Jane"
    last_name      = "Doe"
    organization   = "Example Corp"
    address1       = "1 Main St"
    city           = "Lisbon"
    state_province = "Lisboa"
    postal_code    = "1000-001"
    country        = "PT"
    phone          = "+351.123456789"
    email_address  = "hostmaster@example.com"
  }

  # tech, admin and aux_billing are optional. When omitted they default to the
  # registrant values; the defaulting is shown in the plan (it is not hidden
  # server-side). Provide a block to override a specific contact:
  #
  # tech {
  #   first_name     = "Tim"
  #   ...
  # }
}
```

### Fleet update (`for_each`)

Apply the same contact to many domains at once:

```terraform
locals {
  managed_domains = ["example.com", "example.net", "example.org"]
}

resource "namecheap_domain_contacts" "fleet" {
  for_each = toset(local.managed_domains)
  domain   = each.value

  registrant {
    first_name     = "Jane"
    last_name      = "Doe"
    organization   = "Example Corp"
    address1       = "1 Main St"
    city           = "Lisbon"
    state_province = "Lisboa"
    postal_code    = "1000-001"
    country        = "PT"
    phone          = "+351.123456789"
    email_address  = "hostmaster@example.com"
  }
}
```

## Argument Reference

- `domain` - (Required, ForceNew) Purchased available domain name on your account. Must be a registered root domain (e.g. `example.com`), not a subdomain.
- `registrant` - (Required) The registrant contact (see [nested schema](#nested-schema-for-contact-blocks)).
- `tech` - (Optional) The tech contact. Defaults to `registrant` when omitted.
- `admin` - (Optional) The admin contact. Defaults to `registrant` when omitted.
- `aux_billing` - (Optional) The auxiliary billing contact. Defaults to `registrant` when omitted.

### Nested Schema for contact blocks

Each of `registrant`, `tech`, `admin` and `aux_billing` accepts the same fields.

Required:

- `first_name` - (Required) The contact's first name.
- `last_name` - (Required) The contact's last name.
- `address1` - (Required) The primary street address.
- `city` - (Required) The contact's city.
- `state_province` - (Required) The state or province.
- `postal_code` - (Required) The postal/ZIP code.
- `country` - (Required) The two-letter ISO 3166-1 alpha-2 country code (e.g. `US`, `PT`).
- `phone` - (Required) The phone number in `+NNN.NNNNNNNNNN` format (e.g. `+1.6613102107`).
- `email_address` - (Required) The contact e-mail address.

Optional:

- `organization` - (Optional) The contact's organization.
- `job_title` - (Optional) The contact's job title.
- `address2` - (Optional) The secondary street address.

~> **Plan-time validation.** `phone`, `country` and `email_address` are validated at plan time (format only) to catch obvious mistakes before the slower server-side rejection. Namecheap enforces additional per-country and ICANN-required-field rules server-side; a valid-looking value can still be rejected on apply.

## Default-to-registrant

Omitted `tech`, `admin` and `aux_billing` blocks default to the `registrant` values. The Namecheap `setContacts` API requires all four contact blocks, so the provider fills the omitted ones with the registrant. This defaulting is applied during planning, so the resolved values appear in the plan and in state rather than being applied invisibly.

## WHOIS privacy interplay

This resource manages the *underlying* registrant data on the domain. When WHOIS privacy (Domain Privacy / WhoisGuard) is enabled, the registry-visible contact is the privacy service's proxy, not the values managed here — those remain the domain's real contact data behind the privacy service. Toggling WHOIS privacy is out of scope for this resource.

## Mutual exclusion

Manage a given domain's contacts in exactly one place. Do not point two `namecheap_domain_contacts` resources at the same domain, and do not combine this resource with any other mechanism that also sets the domain's contacts — the last apply wins and the resources will fight on every plan.

## Import

Domain contacts can be imported by domain name, e.g.,

```shell
terraform import namecheap_domain_contacts.main example.com
```

The import seeds state from `getContacts`.

## Destroy semantics

The Namecheap API has no operation to delete a domain's contacts. Destroying this resource removes it from Terraform state (and emits a warning) but leaves the last-applied contact values on the domain — analogous to abandoning, rather than deleting, the managed data. To change the contacts after destroy, manage them again with this resource or edit them in the Namecheap dashboard.
