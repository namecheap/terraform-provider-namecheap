---
page_title: "Importing an existing portfolio"
subcategory: ""
description: |-
  Bringing domains you already own at Namecheap under Terraform management, one resource or a whole portfolio at a time.
---

# Importing an existing portfolio

Nothing in this provider registers your domains for you — they already exist at
Namecheap. Adopting them means telling Terraform about resources that are
already there, which is what `import` does. This guide covers the ID format for
each resource, the two ways to run an import, and how to adopt a whole portfolio
without hand-writing a block per domain.

~> Import **reads**; it never creates, changes or deletes anything at Namecheap.
The risky step is the `terraform plan` immediately afterwards: if your
configuration does not match what was imported, the plan will propose changes.
Always read that first plan before applying it.

## Import blocks (Terraform 1.5+)

An `import` block lives in your configuration, so the adoption is reviewable in
a pull request like any other change:

```terraform
import {
  to = namecheap_domain_records.example
  id = "example.com"
}

resource "namecheap_domain_records" "example" {
  domain = "example.com"
}
```

Run `terraform plan` and Terraform reports what it would import and whether your
configuration matches. `terraform plan -generate-config-out=generated.tf` writes
a starting configuration for you, which is the fastest way to adopt a resource
whose current settings you do not already have written down. For
`namecheap_domain_records` use the `example.com/zone` ID with it, so the
generated configuration lists the records; see
[two import IDs](#namecheap_domain_records-two-import-ids).

Once applied, the `import` block has done its job and can be deleted.

## The `terraform import` command

The older, imperative form. It mutates state immediately and leaves no record in
the configuration, so prefer import blocks where you can:

```shell
terraform import namecheap_domain_records.example example.com
```

## ID formats

| Resource | ID | Example |
|---|---|---|
| `namecheap_domain_records` | the domain name: settings, no records | `example.com` |
| `namecheap_domain_records` | `<domain>/zone`: settings and every record | `example.com/zone` |
| `namecheap_domain_contacts` | the domain name | `example.com` |
| `namecheap_email_forwarding` | the domain name | `example.com` |
| `namecheap_personal_nameserver` | `<domain>/<nameserver>` | `example.com/ns1.example.com` |
| `namecheap_domain_host_record` | `<domain>/<type>/<hostname>/<address>` | `example.com/A/www/203.0.113.10` |

Each resource's own page documents its ID format too; that page is the
authority if this table ever falls behind.

## `namecheap_domain_records`: two import IDs

An import cannot see your configuration, so it cannot know which of a zone's
records you are going to declare. The ID tells it.

| ID | Stored in state | Use it when |
|---|---|---|
| `example.com` | `email_type` and custom `nameservers`; no records | the configuration declares no records, or only some of the zone's records |
| `example.com/zone` | the above and every record of the zone | the configuration declares the whole zone |

This matters because of how `MERGE` mode (the default) works: the records in
state are the ones the resource owns, and an owned record that is missing from
the configuration is deleted on the next apply.

- After **`example.com`** the resource owns no records. A record you declare
  that already exists in the zone is adopted by the first apply — the plan shows
  it as added, and nothing is written twice. Records you do not declare never
  enter state, so no plan, apply or destroy of this resource can delete them.

  A record is the same record when hostname, type and address match. The plan
  cannot show how a declared record differs from the live one: the `ttl` and
  `mx_pref` of the configuration replace the live values, and a record declared
  with a different address is added next to the live one. Set `ttl` explicitly
  if the live value matters.
- After **`example.com/zone`** the resource owns every record. Declare all of
  them, or generate the configuration:

  ```terraform
  import {
    to = namecheap_domain_records.example
    id = "example.com/zone"
  }
  ```

  ```shell
  terraform plan -generate-config-out=generated.tf
  ```

  A record left out of the configuration shows in the plan as removed, and
  applying that plan deletes it from the zone.

-> **`MERGE` mode does not need an import.** Applying a `MERGE` configuration to
a domain that was never imported gives the same result as importing by the plain
domain name first: declared records that already exist are adopted, everything
else is left alone. Import first when the domain uses custom nameservers, or
when you want the plan to show an `email_type` change before it is made.

~> **`OVERWRITE` mode: import with `/zone`.** An `OVERWRITE` configuration
deletes every record it does not declare. After a `/zone` import the plan lists
those records as removed. After an import by the plain domain name the state
holds no records, so the plan could not list them as removed. The provider
fails that plan instead and names every record the apply would have deleted,
with paste-ready `record` blocks: declare them, or import again with `/zone`.

### Releasing records from state

A state imported with `/zone`, or with provider 2.9.11 or earlier, where every
import stored the whole zone, owns records you may not want this resource to
manage. A plan that lists them as removed is the sign. To keep them in the zone
and out of this resource, import it again by the plain domain name:

```shell
terraform state rm namecheap_domain_records.example
terraform import namecheap_domain_records.example example.com
```

Records still declared in the configuration are adopted again by the next apply.

`OVERWRITE` mode is different: there the configuration is the whole zone
whatever the state holds, so records cannot be released this way. After an
import by the plain domain name, the plan of an `OVERWRITE` configuration fails
while the zone holds a record the configuration does not declare.

### Managing only some of a zone's records

Give each record you own its own
[`namecheap_domain_host_record`](../resources/domain_host_record.md), and keep
`namecheap_domain_records` for the mail mode only. A `for_each` import block
(Terraform 1.7+) adopts the records in one go:

```terraform
locals {
  a_records = {
    www = "203.0.113.10"
    api = "203.0.113.11"
  }
}

import {
  to = namecheap_domain_records.mail
  id = "example.com"
}

resource "namecheap_domain_records" "mail" {
  domain     = "example.com"
  email_type = "GMAIL"
}

import {
  for_each = local.a_records
  to       = namecheap_domain_host_record.a[each.key]
  id       = "example.com/A/${each.key}/${each.value}"
}

resource "namecheap_domain_host_record" "a" {
  for_each = local.a_records

  domain   = "example.com"
  hostname = each.key
  type     = "A"
  address  = each.value
}
```

Everything else in the zone — a record a dynamic DNS client updates, one another
team owns — stays out of state and is written back unchanged whenever one of
your records changes. `MX` and `MXE` records are the exception: they are declared
in `namecheap_domain_records` together with `email_type`.

### Moving records from `record` blocks to `namecheap_domain_host_record`

Removing a `record` block always means "delete this record", so a record cannot
be handed to a `namecheap_domain_host_record` by deleting its block: the apply
either deletes the record or fails because it already exists. The
`namecheap_domain_records` resource has to release its records first, which
changes state only and writes nothing to the zone.

With Terraform 1.7 or later the whole move is configuration. Here `www` moves
and `api` stays in a `record` block for now. The `removed` block needs the
`namecheap_domain_records` resource to take a new name (`all` becomes
`settings`):

```terraform
# Forget the old resource without deleting its records.
removed {
  from = namecheap_domain_records.all

  lifecycle {
    destroy = false
  }
}

# The same domain under a new name, imported with no records. The record it
# still declares is adopted by this apply.
import {
  to = namecheap_domain_records.settings
  id = "example.com"
}

resource "namecheap_domain_records" "settings" {
  domain     = "example.com"
  email_type = "GMAIL"

  record {
    hostname = "api"
    type     = "A"
    address  = "203.0.113.11"
  }
}

# The record that moves.
import {
  to = namecheap_domain_host_record.www
  id = "example.com/A/www/203.0.113.10"
}

resource "namecheap_domain_host_record" "www" {
  domain   = "example.com"
  hostname = "www"
  type     = "A"
  address  = "203.0.113.10"
}
```

One apply moves the records and leaves the zone as it was. Delete the `removed`
and `import` blocks afterwards.

On older Terraform, or to keep the resource name, release the records from the
command line and then apply the same `resource` blocks:

```shell
terraform state rm namecheap_domain_records.all
terraform import namecheap_domain_records.all example.com
terraform import namecheap_domain_host_record.www example.com/A/www/203.0.113.10
```

Every later batch needs the release again, because the records still declared in
`record` blocks belong to the resource once more. Moving all records in one go
takes it once.

## Adopting a whole portfolio (Terraform 1.7+)

The `namecheap_domains` data source lists every domain on the account, so a
portfolio can be adopted with one block per resource type rather than one per
domain. Terraform evaluates `import` blocks with `for_each`, and the data source
supplies the keys.

~> `for_each` inside an `import` block requires **Terraform 1.7 or later**. On
1.5 and 1.6 an `import` block accepts only a literal `to`/`id` pair, so adopt a
portfolio there by generating one block per domain (or by looping
`terraform import` in a shell script).

```terraform
data "namecheap_domains" "all" {
  list_type = "ALL"
}

locals {
  # Filter to what you actually want under management — expired domains and
  # ones handled by another team usually do not belong in this state file.
  managed = {
    for d in data.namecheap_domains.all.domains : d.name => d
    if !d.is_expired
  }
}

import {
  for_each = local.managed
  to       = namecheap_domain_records.portfolio[each.key]
  id       = each.key
}

resource "namecheap_domain_records" "portfolio" {
  for_each = local.managed

  domain = each.key
}
```

The plain domain name is deliberate here: it stores each domain's settings and
none of its records, so the plan after the import is empty and no record is at
risk. Add records afterwards, per domain, in `record` blocks or as
`namecheap_domain_host_record` resources. Do not adopt a portfolio with
`mode = "OVERWRITE"` in the configuration: that mode owns the entire zone, so
the plan after the import fails for every domain whose existing records the
configuration does not list in full — see the
[domain records guide](namecheap_domain_records_guide.md#overwrite).

-> A portfolio import reads the account listing, then makes three API calls per
domain while it imports (four with `/zone`) and two per domain on every later
plan. On a large portfolio that can reach Namecheap's rate limits; the
[CI and automation environments guide](ci-environments.md) explains the
`requests_per_minute` and retry settings that pace it.

## After importing

1. Run `terraform plan`. An empty plan means the configuration matches reality
   and the adoption is complete.
2. A non-empty plan means your configuration differs from what exists. Read each
   proposed change and decide which side is right — sometimes the configuration
   is wrong, sometimes the live state has drifted and the plan is the fix.
3. For `namecheap_domain_records` imported by the plain domain name, records
   that exist at Namecheap but are absent from your configuration are not in
   state and are left alone; records you do declare show as added and are
   adopted, not duplicated. After a `/zone` import a record absent from the
   configuration shows as removed — add it to the configuration unless you mean
   to delete it. With `mode = "OVERWRITE"` in the configuration, the plan after
   an import by the plain domain name fails while the zone holds such records,
   and lists them with paste-ready `record` blocks.
