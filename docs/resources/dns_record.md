---
page_title: "namecheap_dns_record Resource - terraform-provider-namecheap"
subcategory: "DNS"
description: |-
  Manages a single DNS host record on a domain, leaving all other records untouched. Mutually exclusive with namecheap_domain_records for the same domain.
---

# namecheap_dns_record (Resource)

Manages a **single** DNS host record, leaving every other record on the domain
untouched. This is the per-record counterpart to
[`namecheap_domain_records`](./domain_records.md), which owns a domain's whole
zone, and it is the shape most people expect from a DNS provider: one resource
per record, `for_each` over a map, per-record import.

## Example Usage

```terraform
# Each record is managed independently; everything else in the zone is left alone.
resource "namecheap_dns_record" "www" {
  domain   = "example.com"
  hostname = "www"
  type     = "A"
  address  = "203.0.113.10"
  ttl      = 1800
}

resource "namecheap_dns_record" "spf" {
  domain   = "example.com"
  hostname = "@"
  type     = "TXT"
  address  = "v=spf1 include:_spf.example.net -all"
}
```

### One record per entry

```terraform
# A record per entry, driven by a map. Each instance owns exactly its own record,
# so adding or removing one key does not touch the others.
locals {
  hosts = {
    api = "203.0.113.20"
    cdn = "203.0.113.21"
  }
}

resource "namecheap_dns_record" "fleet" {
  for_each = local.hosts

  domain   = "example.com"
  hostname = each.key
  type     = "A"
  address  = each.value
}
```

## Choosing between this and `namecheap_domain_records`

~> **Do not point both at the same domain.** `namecheap_domain_records` manages a
domain's entire record set; this resource manages one record within it. Used
together on one domain they fight — the zone-level resource will delete records
this one created (in `OVERWRITE` mode) or fight over ownership (in `MERGE`),
because neither knows about the other.

- **Use `namecheap_dns_record`** when different records are owned by different
  people, modules or workspaces, or when you want `for_each` over a map of hosts.
- **Use `namecheap_domain_records`** when one configuration should own the whole
  zone, zone-file style — including the guarantee that nothing else exists.

## How a change is applied

Namecheap has no per-record API: `domains.dns.setHosts` replaces the entire
record set. Every change here is therefore a read-modify-write of the whole zone,
protected two ways:

1. Within a single Terraform run, changes to one domain are serialized, so
   parallel instances of this resource do not interleave.
2. After writing, the zone is re-read and compared against what the write
   intended. A mismatch is retried, so a change of *yours* is not silently lost.

~> **This narrows the race with writers outside the run; it does not close it.**
The comparison is against the record set this run computed from its own read, so a
foreign write that lands between that read and the `setHosts` is inside the
replaced set — it is overwritten, the verification still matches, and the apply
succeeds. Concurrent writers *can* cost you a record. Serialize changes to a
domain: one Terraform run at a time, and no dashboard edits while it runs.

## Records this resource cannot tell apart

A record is identified by its host, type and address — plus the preference for
`MX`, so a primary and a backup mail server naming the same host stay distinct.

Two records that agree on all of those and differ only in TTL cannot be selected
individually through the API, so any operation on them is refused with an error
listing the candidates, rather than being applied to both. Remove the duplicate,
or manage the domain's records as one set with
[`namecheap_domain_records`](./domain_records.md).

The import ID carries no preference, so an `MX` record sharing its host and target
with another at a different preference is ambiguous on import for the same reason.

## Mail records

MX and MXE records are tied to the domain's email routing (`EmailType`), which
this resource does not manage. Set it with
[`namecheap_domain_records`](./domain_records.md)' `email_type`, or in the
Namecheap dashboard, before creating MX records here; otherwise the API rejects
the write and the provider surfaces the reason.

!> **Domains using Private Email or Google Workspace cannot be managed with this
resource yet.** Those zones hold their mail provider's MX records while the zone
type stays `OX`/`GMAIL`, a combination the underlying SDK's validation rejects —
so *any* change through this resource fails, including one to an unrelated A
record. Use [`namecheap_domain_records`](./domain_records.md) for those domains.
Tracked in [go-namecheap-sdk#162](https://github.com/namecheap/go-namecheap-sdk/issues/162).

## Argument Reference

- `domain` - (Required, Force New) The registered root domain the record belongs to (e.g. `example.com`). Must be a root domain on the account, not a subdomain.
- `hostname` - (Required, Force New) The sub-domain the record answers for, or `@` for the domain itself (e.g. `www`).
- `type` - (Required, Force New) The record type: `A`, `AAAA`, `ALIAS`, `CAA`, `CNAME`, `MX`, `MXE`, `NS`, `TXT`, `URL`, `URL301`, `FRAME`.
- `address` - (Required) The record's value, whose meaning depends on `type`: an IP address for `A`/`AAAA`, a hostname for `CNAME`/`MX`/`NS`, arbitrary text for `TXT`, a URL for `URL`/`URL301`/`FRAME`. Changed in place.
- `ttl` - (Optional) Time to live in seconds, between `60` and `60000`. Defaults to `1800`. Changed in place.
- `mx_pref` - (Optional) MX preference, lower being preferred, between `0` and `255`. Applies to `MX` records only. Defaults to `10`, which is what the API reports for every other type.

`domain`, `hostname` and `type` are the record's identity, so changing any of
them replaces the record rather than editing it. `address`, `ttl` and `mx_pref`
are changed in place; if such a change is rejected by the API, the record is left
as it was and the next plan proposes the same change again.

-> The case of `domain` and `hostname` is kept as you write it, as is an address
the API only re-spells (the trailing dot it adds to a `CNAME`, `ALIAS`, `NS` or
`MX` target). Only a genuine change out-of-band shows up as drift.

## Attribute Reference

- `id` - `<domain>/<type>/<hostname>/<address>`, normalized to lower-case domain, upper-case type and lower-case hostname.

## Import

Records are imported by that same composite ID:

```shell
# The ID is <domain>/<type>/<hostname>/<address>. The address may itself contain
# slashes (a URL record, for instance) — only the first three components are split.
terraform import namecheap_dns_record.www example.com/A/www/203.0.113.10
```

The address may itself contain `/` — a `URL` record's target, for instance —
because only the first three components are split off.

-> The address must match what the zone actually holds. Namecheap normalizes some
values (a `CNAME` target gains a trailing dot, `CAA` values are quoted), so if an
import reports the record does not exist, read the current value from the
[`namecheap_domain_records` data source](../data-sources/domain_records.md) and
use that spelling.
