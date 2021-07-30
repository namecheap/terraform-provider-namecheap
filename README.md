# Namecheap Terraform Provider

A Terraform Provider for Namecheap domain DNS configuration.

- [Namecheap Provider Documentation](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs)
- [Guide: Migration to v2.0.0 new major release](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/guides/namecheap_provider_migration_v2.0.0)
- [Guide: Namecheap domain records](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/guides/namecheap_domain_records_guide)

## Prerequisites

First you'll need to apply for API access to Namecheap. You can do that on
this [API admin page](https://ap.www.namecheap.com/settings/tools/apiaccess/).

Next, find out your IP address and add that IP (or any other IPs accessing this API) to
this [whitelist admin page](https://ap.www.namecheap.com/settings/tools/apiaccess/whitelisted-ips) on Namecheap.

Once you've done that, make note of the API key, your IP address, and your username to fill into our `provider` block.

## Usage Example

Make sure your API details are correct in the provider block.

Terraform 0.13 and later:

```hcl
terraform {
  required_providers {
    namecheap = {
      source = "namecheap/namecheap"
      version = ">= 2.0.0"
    }
  }
}

provider "namecheap" {
  user_name = "your_username"
  api_user = "your_username"
  api_key = "your_api_key"
  client_ip = "your.ip.address.here"
  use_sandbox = false
}


resource "namecheap_domain_records" "domain-com" {
  domain = "domain.com"
  mode = "OVERWRITE"

  record {
    hostname = "dev"
    type = "A"
    address = "10.12.14.19"
  }
}

resource "namecheap_domain_records" "domain2-com" {
  domain = "domain2.com"
  mode = "OVERWRITE"

  nameservers = [
    "ns1.random-domain.org",
    "ns2.random-domain.org",
  ]
}
```

### Contributing

To contribute, please read our [contributing](CONTRIBUTING.md) docs.  
