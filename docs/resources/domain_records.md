---
page_title: "namecheap_domain_records Resource - terraform-provider-namecheap"
subcategory: ""
description: |-
  
---

# namecheap_domain_records (Resource)

Follow [Namecheap domain records guide](../guides/namecheap_domain_records_guide.md) to get detailed information about
each argument and usage examples.

## Example Usage

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "NONE"

  record {
    hostname = "blog"
    type = "A"
    address = "10.11.12.13"
  }

  record {
    hostname = "@"
    type = "ALIAS"
    address = "www.testdomain.com"
  }
}

resource "namecheap_domain_records" "my-domain2-com" {
  domain = "my-domain2.com"
  mode = "OVERWRITE" // Warning: this will remove all manually set records

  nameservers = [
    "ns1.some-domain.com",
    "ns2.some-domain.com"
  ]
}
```

## Argument Reference

- `domain` - (Required) Purchased available domain name on your account
- `mode` - (Optional) Possible values: `MERGE` (default), `OVERWRITE` - removes all manually set records & sets only ones that were specified in TF config
- `email_type` - (Optional) Possible values: NONE, FWD, MXE, MX, OX, GMAIL. Conflicts with `nameservers`
- `record` - (Optional) (see [below for nested schema](#nestedblock--record)) Might contain one or more `record`
  records. Conflicts with `nameservers`
- `nameservers` - (Optional) List of nameservers. Conflicts with `email_type` and `record`

<a id="nestedblock--record"></a>

### Nested Schema for `record`

- `address` - (Required) Possible values are URL or IP address. The value for this parameter is based on record type
- `hostname` - (Required) Sub-domain/hostname to create the record for
- `type` - (Required) Possible values: A, AAAA, ALIAS, CAA, CNAME, MX, MXE, NS, TXT, URL, URL301, FRAME
- `mx_pref` - (Optional) MX preference for host. Applicable for MX records only
- `ttl` - (Optional) Time to live for all record types. Possible values: any value between 60 to 60000

~> It is strongly recommended to set `address`, `hostname`, `nameservers` in lower case to prevent undefined behavior!  

## Import

Domain records can be imported using by domain name, e.g.,

```terraform
terraform import namecheap_domain_records.main example.com
```