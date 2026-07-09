---
page_title: "namecheap_domain_records Data Source - terraform-provider-namecheap"
subcategory: ""
description: |-
  A read-only view of a domain's live DNS records.
---

# namecheap_domain_records (Data Source)

Reads a domain's live DNS record set via `namecheap.domains.dns.getHosts`, along
with its nameservers (`namecheap.domains.dns.getList`) and email routing type.

The `records` object shape mirrors the
[`namecheap_domain_records`](../resources/domain_records.md) resource `record`
block attribute-for-attribute, so the output composes into resource inputs
without any field remapping.

## Example Usage

```terraform
data "namecheap_domain_records" "current" {
  domain = "example.com"
}

output "record_hostnames" {
  value = [for r in data.namecheap_domain_records.current.records : r.hostname]
}
```

Merge/augment an existing zone by feeding the live records into a resource
`dynamic` block:

```terraform
data "namecheap_domain_records" "current" {
  domain = "example.com"
}

resource "namecheap_domain_records" "managed" {
  domain = "example.com"
  mode   = "OVERWRITE"

  dynamic "record" {
    for_each = data.namecheap_domain_records.current.records
    content {
      hostname = record.value.hostname
      type     = record.value.type
      address  = record.value.address
      mx_pref  = record.value.mx_pref
      ttl      = record.value.ttl
    }
  }

  record {
    hostname = "new"
    type     = "A"
    address  = "10.0.0.9"
  }
}
```

## Argument Reference

- `domain` - (Required) The domain whose DNS records to read (e.g. `example.com`). Must be a registered root domain, not a subdomain.

## Attribute Reference

- `email_type` - The email routing type configured for the domain (e.g. `NONE`, `FWD`, `MXE`, `MX`, `OX`, `GMAIL`).
- `nameservers` - The custom nameservers configured for the domain; empty when the domain is using Namecheap's DNS.
- `records` - The live DNS host records for the domain. Each element has the following attributes:
  - `hostname` - Sub-domain/hostname of the record.
  - `type` - Record type (e.g. `A`, `AAAA`, `CNAME`, `MX`, `TXT`).
  - `address` - Record value (URL or IP address, depending on the record type).
  - `mx_pref` - MX preference for the host. Applicable to MX records only.
  - `ttl` - Time to live for the record, in seconds.
