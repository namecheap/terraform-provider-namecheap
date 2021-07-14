---
page_title: Migration guide for v2.0.0 release
---

# Migration guide for v2.0.0 release

The Namecheap Provider v2.0.0 is our new major release with much improved and stabilised the general workflow. This
guide will explain how to upgrade your old terraform files regarding new v2 format requirements.

Most justification of a new approach has been described
in [github issue](https://github.com/namecheap/terraform-provider-namecheap/issues/46).

Primarily, the guide will consist of examples with old format of specific record and new one with a few notes. You have
to change the terraform file format and apply a new state. Since we have completely changed approach to create records,
there's no way for automatic migration.

## API credentials migration

### Old format

```terraform
provider "namecheap" {
  username = "your_username"
  api_user = "your_username"
  token = "your_token"
  ip = "your.ip.address.here"
  use_sandbox = false
}
```

### New format

```terraform
provider "namecheap" {
  user_name = "your_username"
  api_user = "your_username"
  api_key = "your_token"
  client_ip = "your.ip.address.here"
  use_sandbox = false
}
```

The main justification of such renaming is to be consistent with
official [Namecheap API documentation](https://www.namecheap.com/support/api/intro/).

#### Environment variables (Old -> New)

- `NAMECHEAP_USERNAME`      -> `NAMECHEAP_USER_NAME`
- `NAMECHEAP_API_USER`      -> `NAMECHEAP_API_USER`
- `NAMECHEAP_TOKEN`         -> `NAMECHEAP_API_KEY`
- `NAMECHEAP_IP`            -> `NAMECHEAP_CLIENT_IP`
- `NAMECHEAP_USE_SANDBOX`   -> `NAMECHEAP_USE_SANDBOX`

## Nameserver resource migration (old: namecheap_ns)

Now nameservers should be set via `namecheap_domain_records` resource.

### Old format

```terraform
resource "namecheap_ns" "domain-com" {
  domain = "my-domain.com"
  servers = ["ns-1.domain-47.com", "ns-2.domain-48.com"]
}
```

### New format

```terraform
resource "namecheap_domain_records" "my-domain2-com" {
  domain = "my-domain.com"
  mode = "OVERWRITE"

  nameservers = ["ns-1.domain-47.com", "ns-2.domain-48.com"]
}
```

Mode `OVERWRITE` means that the new `nameservers` list will overwrite existing settings (for example, if you set some
servers manually or via other terraform instance). This was the default behavior for our previous version of terraform
provider and recommended for this migration guide.

## Domain records migration (old: namecheap_record)

Now domain records should be set via `namecheap_domain_records` resource.

### Old format


```terraform
resource "namecheap_record" "blog-my-domain-com" {
  domain = "my-domain.com"
  name = "blog"
  type = "A"
  address = "10.11.12.13"
  ttl = 1800
}

resource "namecheap_record" "app-my-domain-com" {
  domain = "my-domain.com"
  name = "app"
  type = "A"
  address = "10.11.12.14"
  ttl = 1800
}
```

### New format

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  mode = "OVERWRITE"

  record {
    hostname = "blog"
    type = "A"
    address = "10.11.12.13"
    ttl = 1800
  }
  
  record {
    hostname = "app"
    type = "A"
    address = "10.11.12.14"
    ttl = 1800
  }
}
```

Previously, each record was a separate resource. Now we have a combined `namecheap_domain_records` resource where you
can collect all records that should be added for domain.

~> Previous version had a bug with applying MX records since you hadn't an ability to set email settings.
