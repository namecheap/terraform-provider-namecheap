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
  mode   = "MERGE"
}
```

Run `terraform plan` and Terraform reports what it would import and whether your
configuration matches. `terraform plan -generate-config-out=generated.tf` writes
a starting configuration for you, which is the fastest way to adopt a resource
whose current settings you do not already have written down.

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
| `namecheap_domain_records` | the domain name | `example.com` |
| `namecheap_domain_contacts` | the domain name | `example.com` |
| `namecheap_email_forwarding` | the domain name | `example.com` |
| `namecheap_personal_nameserver` | `<domain>/<nameserver>` | `example.com/ns1.example.com` |

Each resource's own page documents its ID format too; that page is the
authority if this table ever falls behind.

## Adopting a whole portfolio

The `namecheap_domains` data source lists every domain on the account, so a
portfolio can be adopted with one block per resource type rather than one per
domain. Terraform evaluates `import` blocks with `for_each`, and the data source
supplies the keys:

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
  mode   = "MERGE"
}
```

`MERGE` is deliberate here. In `OVERWRITE` mode this resource owns the entire
zone, so an apply against a configuration that does not yet list every existing
record will **delete** the records you did not write down — see the
[domain records guide](namecheap_domain_records_guide.md#overwrite). Adopt in
`MERGE`, confirm the plan is empty, and switch mode later if you want full
ownership.

-> A portfolio import reads the account listing plus one call per domain. On a
large portfolio that can brush against Namecheap's per-minute rate limit; the
[CI and automation environments guide](ci-environments.md) explains the
`requests_per_minute` and retry settings that pace it.

## After importing

1. Run `terraform plan`. An empty plan means the configuration matches reality
   and the adoption is complete.
2. A non-empty plan means your configuration differs from what exists. Read each
   proposed change and decide which side is right — sometimes the configuration
   is wrong, sometimes the live state has drifted and the plan is the fix.
3. For `namecheap_domain_records`, a common first-plan difference is records
   that exist at Namecheap but are absent from your configuration. In `MERGE`
   mode they are simply left alone; in `OVERWRITE` mode the provider warns and
   lists them, with paste-ready `record` blocks so you can adopt rather than
   lose them.
