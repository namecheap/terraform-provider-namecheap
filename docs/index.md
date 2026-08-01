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
- [Running in CI and automation environments guide](guides/ci-environments.md)

## Example Usage

Terraform 0.13 and later:

```tf
terraform {
  required_providers {
    namecheap = {
      source = "namecheap/namecheap"
      version = ">= 2.0.0"
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

  # Optional client resilience tuning (defaults shown):
  # requests_per_minute = 20
  # max_retries         = 4
  # retry_max_elapsed   = "2m"
  # retry_base_delay    = "500ms"
  # retry_max_delay     = "30s"
  # request_timeout     = "30s"
}

resource "namecheap_domain_records" "domain-com" {
  #...
}
```

## Argument Reference

Every argument can be provided inline in the `provider` block or via its
`NAMECHEAP_*` environment variable. When both are set, the inline value takes
precedence.

### Credentials

- `user_name` (`NAMECHEAP_USER_NAME`) - (Required, Sensitive) A registered user name for Namecheap. Must be supplied inline or via the environment variable.
- `api_user` (`NAMECHEAP_API_USER`) - (Required, Sensitive) A registered API user for Namecheap. Must be supplied inline or via the environment variable.
- `api_key` (`NAMECHEAP_API_KEY`) - (Required, Sensitive) The Namecheap API key. Must be supplied inline or via the environment variable.
- `client_ip` (`NAMECHEAP_CLIENT_IP`) - (Optional, String) The public IP address the Namecheap API sees as the caller. It must be whitelisted at the [API access whitelisted IPs page](https://ap.www.namecheap.com/settings/tools/apiaccess/whitelisted-ips). When left unset, the provider auto-detects this machine's public IP via an outbound HTTPS request to `api.ipify.org` (5 second timeout). If detection fails (for example on a host with no outbound network access), provider configuration fails with guidance to set `client_ip` explicitly. An explicitly set value is always honored unchanged. See the [CI and automation environments guide](guides/ci-environments.md) for guidance on when to set this explicitly.
- `use_sandbox` (`NAMECHEAP_USE_SANDBOX`) - (Optional, Bool) Use sandbox API endpoints. Defaults to `false`. If `true`, all API requests are
  made through the `sandbox.namecheap.com` endpoint. You can [sign up](https://www.sandbox.namecheap.com/myaccount/signup/)
  for a free sandbox account.

### Client behavior and resilience

- `requests_per_minute` (`NAMECHEAP_REQUESTS_PER_MINUTE`) - (Optional, Int) Client-side rate limit applied to the Namecheap API, in requests per minute. Must be between `1` and `20` (Namecheap's documented primary quota). Defaults to `20`.
- `max_retries` (`NAMECHEAP_MAX_RETRIES`) - (Optional, Int) Total number of attempts (including the first) for a single API call before giving up. Must be `>= 0`. Defaults to `4`. Note: the underlying SDK treats a zero value as "unset", so setting this to `0` falls back to the SDK default of `4` attempts rather than disabling retries.
- `retry_max_elapsed` (`NAMECHEAP_RETRY_MAX_ELAPSED`) - (Optional, String) Maximum total wall-clock time to spend retrying a single API call, as a [Go duration string](https://pkg.go.dev/time#ParseDuration) (e.g. `"2m"`, `"90s"`). Must parse and be greater than zero. Defaults to `"2m"`.
- `retry_base_delay` (`NAMECHEAP_RETRY_BASE_DELAY`) - (Optional, String) First backoff delay before a retried API call, as a [Go duration string](https://pkg.go.dev/time#ParseDuration) (e.g. `"500ms"`, `"10s"`). Subsequent delays double up to `retry_max_delay`, and each is then jittered to between 50% and 100% of that value. Must parse, be greater than zero, and not exceed `retry_max_delay`. Defaults to `"500ms"`.
- `retry_max_delay` (`NAMECHEAP_RETRY_MAX_DELAY`) - (Optional, String) Cap on any single backoff delay between retried API calls, as a [Go duration string](https://pkg.go.dev/time#ParseDuration) (e.g. `"30s"`, `"1m"`). Must parse, be greater than zero, and be at least `retry_base_delay`. Defaults to `"30s"`.
- `request_timeout` (`NAMECHEAP_REQUEST_TIMEOUT`) - (Optional, String) Timeout applied to the underlying HTTP client for a single request to the Namecheap API, as a [Go duration string](https://pkg.go.dev/time#ParseDuration) (e.g. `"30s"`, `"1m"`). Must parse and be greater than zero. Defaults to `"30s"`.

-> You can set up arguments via environment variables `NAMECHEAP_*`

-> **Debug logging:** set `TF_LOG_PROVIDER_NAMECHEAP=DEBUG` to emit structured, per-API-call log entries (command, attempt, duration, status, and error code). This is the recommended way to diagnose credential, whitelisting, and rate-limit issues. See the [CI and automation environments guide](guides/ci-environments.md#debug-logging).

~> **Important:** The `namecheap_domain_records` resource supports two modes: `MERGE` (default) and `OVERWRITE`. Be careful with `OVERWRITE` mode — it replaces the entire DNS zone and **permanently deletes** all records not in the Terraform configuration. Since v2.5.0 the provider warns at both `plan` and `apply` before any such deletion, with a paste-ready suggestion for adopting the record instead. See the [domain records guide](guides/namecheap_domain_records_guide.md#overwrite) for details.
