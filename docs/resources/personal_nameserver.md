---
page_title: "namecheap_personal_nameserver Resource - terraform-provider-namecheap"
subcategory: "DNS"
description: |-
  Registers a personal (glue/vanity) nameserver such as ns1.example.com under a domain on your account, and manages the IP address it resolves to.
---

# namecheap_personal_nameserver (Resource)

Registers and manages a personal (glue/vanity) nameserver — such as `ns1.example.com` — for a domain on your Namecheap account, using the [Namecheap `domains.ns` API](https://www.namecheap.com/support/api/methods/domains-ns/create/).

A personal nameserver is a host you register under one of your domains and point at an IP address (a glue record), so the host can itself be used as a nameserver. This is different from assigning custom nameservers to a domain: to point a domain at existing nameservers, use the `nameservers` argument of the [`namecheap_domain_records`](./domain_records.md) resource, which calls `domains.dns.setCustom`.

## Example Usage

```terraform
resource "namecheap_personal_nameserver" "ns1" {
  domain     = "example.com"
  nameserver = "ns1.example.com"
  ip         = "93.184.216.34"
}

resource "namecheap_personal_nameserver" "ns2" {
  domain     = "example.com"
  nameserver = "ns2.example.com"
  ip         = "93.184.216.35"
}
```

~> `ip` must be a real, routable **public** IP address (the glue record for the nameserver host). The Namecheap API rejects reserved ranges — RFC 1918 private (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), RFC 5737 documentation (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`), loopback, etc. — with `Parameter value policy error. IP address ... is reserved. (3024278)`. The addresses above are placeholders; use your nameserver host's actual public IP.

## Argument Reference

- `domain` - (Required, Force New) The registered root domain the personal nameserver belongs to (e.g. `example.com`). Must be a root domain present on the account, not a subdomain. Changing this forces a new resource.
- `nameserver` - (Required, Force New) The fully qualified hostname of the personal nameserver to register (e.g. `ns1.example.com`). Changing this forces a new resource.
- `ip` - (Required) The IP address the personal nameserver resolves to (the glue record's address). This value can be changed in place, which issues a `domains.ns.update`.

~> It is strongly recommended to set `domain` and `nameserver` in lower case to prevent undefined behavior.

## Import

Personal nameservers can be imported using the composite ID `<domain>/<nameserver>`, e.g.,

```shell
terraform import namecheap_personal_nameserver.ns1 example.com/ns1.example.com
```
