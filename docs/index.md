---
page_title: "Namecheap provider"
subcategory: ""
---

# Namecheap Provider

The Namecheap Provider can be used to configure domain records. Before moving forward, make sure you have enabled API
access for your account and whitelisted your static IP address where the terraform will be running.

**Recommended resources:**

- [Namecheap API documentation](https://www.namecheap.com/support/api/intro/)
- [Namecheap domain records guide](guides/namecheap_domain_records_guide.md)

## Example Usage

```tf
terraform {
  required_providers {
    namecheap = {
      source = "namecheap/namecheap"
      version = "2.0.0"
    }
  }
}

# Namecheap API credentials
provider "namecheap" {
  user_name = "user"
  api_user = "user"
  api_key = "key"
  client_ip = "123.123.123.123"
  use_sandbox = false
}

resource "namecheap_domain_records" "domain-com" {
  #...
}
```

## Argument Reference

- `user_name` (`NAMECHEAP_USER_NAME`) - (Required) A registered user name for Namecheap.
- `api_user` (`NAMECHEAP_API_USER`) - (Required) A registered api user for Namecheap
- `api_key` (`NAMECHEAP_API_KEY`) - (Required) The Namecheap API key
- `client_ip` (`NAMECHEAP_CLIENT_IP`) - (Required) IP address of the machine running terraform that is whitelisted
- `use_sandbox` (`NAMECHEAP_USE_SANDBOX`) - (Optional) Use sandbox API endpoints. If `true`, all API requests will be
  made through `sandbox.namecheap.com` endpoint. You can [sign up](https://www.sandbox.namecheap.com/myaccount/signup/)
  a free sandbox account

-> You can set up arguments via environment variables `NAMECHEAP_*`
