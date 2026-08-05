# Namecheap Terraform Provider

[![CI](https://github.com/namecheap/terraform-provider-namecheap/actions/workflows/ci.yml/badge.svg)](https://github.com/namecheap/terraform-provider-namecheap/actions/workflows/ci.yml)
[![CodeQL](https://github.com/namecheap/terraform-provider-namecheap/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/namecheap/terraform-provider-namecheap/actions/workflows/github-code-scanning/codeql)
[![Release](https://img.shields.io/github/v/release/namecheap/terraform-provider-namecheap)](https://github.com/namecheap/terraform-provider-namecheap/releases)
[![Terraform Registry](https://img.shields.io/badge/terraform-registry-blueviolet)](https://registry.terraform.io/providers/namecheap/namecheap/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/namecheap/terraform-provider-namecheap)](https://github.com/namecheap/terraform-provider-namecheap/blob/master/go.mod)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/namecheap/terraform-provider-namecheap/graph/badge.svg)](https://codecov.io/gh/namecheap/terraform-provider-namecheap)

A Terraform Provider for Namecheap domain DNS configuration.

- [Namecheap Provider Documentation](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs)
- [Guide: Migration to v2.0.0 new major release](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/guides/namecheap_provider_migration_v2.0.0)
- [Guide: Namecheap domain records](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/guides/namecheap_domain_records_guide)
- [Guide: Running in CI and automation environments](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/guides/ci-environments)
- [Guide: Importing an existing portfolio](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/guides/importing)

## Prerequisites

First you'll need to apply for API access to Namecheap. You can do that on
this [API admin page](https://ap.www.namecheap.com/settings/tools/apiaccess/).

Next, find out your IP address and add that IP (or any other IPs accessing this API) to
this [whitelist admin page](https://ap.www.namecheap.com/settings/tools/apiaccess/whitelisted-ips) on Namecheap.

Once you've done that, make note of the API key, your IP address, and your username to fill into our `provider` block.

`client_ip` is optional: when it is left unset the provider auto-detects the caller's public IP over HTTPS. Set it explicitly (or via `NAMECHEAP_CLIENT_IP`) when running on hosts with restricted egress or split routing. For running from CI runners, containers, and other automation, see the [Running in CI and automation environments](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/guides/ci-environments) guide.

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

  # Optional client resilience tuning (defaults shown):
  # requests_per_minute = 20      # client-side rate limit (1-20)
  # max_retries         = 4       # attempts per call (>= 0; 0 means SDK default of 4)
  # retry_max_elapsed   = "2m"    # max total retry time (Go duration)
  # request_timeout     = "30s"   # per-request HTTP timeout (Go duration)
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

`namecheap_domain_records` owns a domain's whole zone. To manage records
individually — one resource per record, so different owners can share a domain —
use [`namecheap_domain_host_record`](https://registry.terraform.io/providers/namecheap/namecheap/latest/docs/resources/domain_host_record)
instead. The two are mutually exclusive for a given domain.

```terraform
resource "namecheap_domain_host_record" "www" {
  domain   = "your-domain.com"
  hostname = "www"
  type     = "A"
  address  = "203.0.113.10"
}
```

### Testing

Every pull request runs a fast, credential-free mock-backed acceptance suite
(`make testacc-mock`) on GitHub-hosted runners across a Terraform + OpenTofu
version matrix:

| Engine | Versions tested in CI |
| --- | --- |
| Terraform | 1.5.7 (minimum supported), 1.15.5 (latest series) |
| OpenTofu | 1.12.3 |

A live-API sandbox suite (`make testacc-sandbox`, aliased as `make testacc`)
additionally runs on pushes to this repository. See
[CONTRIBUTING.md](CONTRIBUTING.md#tests) for how to run each locally.

### Contributing

To contribute, please read our [contributing](CONTRIBUTING.md) docs.

### Compliance & security

See [`SECURITY_COMPLIANCE.md`](SECURITY_COMPLIANCE.md) for the full set of
controls (dependency pinning, vulnerability and license scanning, SBOM
publication, supply-chain pinning) and for how to report a vulnerability.

