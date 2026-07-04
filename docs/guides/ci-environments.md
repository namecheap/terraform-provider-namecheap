---
page_title: Running in CI and automation environments
---

# Running in CI and automation environments

This guide collects the recurring answers for running the Namecheap provider
from CI pipelines, container runners, and other automation where there is no
interactive operator. It covers IP whitelisting, when to set `client_ip`
explicitly, tuning the client so parallel jobs do not collide with Namecheap's
rate limit, and enabling structured debug logs.

For the full list of provider arguments and their defaults, see the
[provider configuration reference](../index.md#argument-reference).

## Whitelist the runner's egress IP

Every Namecheap API call is authorized against the **public IP address the
Namecheap API sees as the caller**, and that IP must be whitelisted for your
account at the
[API access whitelisted IPs page](https://ap.www.namecheap.com/settings/tools/apiaccess/whitelisted-ips).

In CI this is the runner's **egress** IP (the address traffic leaves from),
which is not necessarily the runner's own interface address:

- **Self-hosted runners** behind a NAT gateway share the gateway's public IP.
  Whitelist that gateway IP, and pin egress to it (for example a NAT Gateway or
  a fixed egress node) so it does not change.
- **Cloud-hosted / ephemeral runners** (e.g. hosted CI, autoscaling fleets)
  often draw from large, changing IP pools. Route their outbound traffic
  through a fixed egress (NAT gateway, proxy, or a small static-IP bastion) and
  whitelist that single address, rather than trying to whitelist an entire pool.

~> **Important:** If the calling IP is not whitelisted, the Namecheap API rejects
requests regardless of whether the credentials are correct. When a pipeline that
worked yesterday starts failing on auth, an IP change is the most common cause.

## Setting `client_ip` explicitly vs. auto-detection

When `client_ip` (or `NAMECHEAP_CLIENT_IP`) is left unset, the provider
auto-detects the caller's public IP via an outbound HTTPS request to
`api.ipify.org` with a 5 second timeout. If that request fails, provider
configuration fails with an error telling you to set `client_ip` explicitly.

**Rely on auto-detection when** the runner has straightforward outbound HTTPS
access and its egress IP is already whitelisted. This keeps the configuration
free of a hard-coded address.

**Set `client_ip` explicitly when** any of the following apply:

- The runner has **no outbound access to `api.ipify.org`** (locked-down egress,
  no general internet, or `api.ipify.org` is blocked). Auto-detection cannot
  succeed and configuration will fail.
- Traffic to Namecheap leaves through a **different address** than a generic
  detection request would report (for example split egress, or Namecheap
  traffic is forced through a specific proxy/NAT). Auto-detection could return
  an IP that is not the one Namecheap sees, and calls would fail whitelisting.
- You want deterministic, auditable configuration that does not depend on a
  third-party detection endpoint being reachable.

```terraform
provider "namecheap" {
  # Pin the whitelisted egress IP so configuration never depends on api.ipify.org.
  client_ip = "203.0.113.10"
}
```

-> You can supply the value without touching HCL by exporting
`NAMECHEAP_CLIENT_IP` in the job environment. An explicitly set value is always
honored unchanged; the provider never overrides it with a detected address.

## Avoiding rate-limit collisions

Namecheap enforces a documented primary quota (per-minute request limit) at the
account level. When several CI jobs — or several `terraform apply` runs — hit
the API for the **same account** concurrently, their requests are counted
together and can trip the limit. Four provider arguments control how the client
paces and recovers from this:

- `requests_per_minute` (default `20`, valid range `1`–`20`) — the client-side
  rate limit, in requests per minute. If you run **N** jobs against one account
  in parallel, lower this so the combined rate stays within quota (roughly
  `20 / N` per job).
- `max_retries` (default `4`, must be `>= 0`) — total attempts, including the
  first, before giving up. Note that `0` falls back to the SDK default of `4`
  rather than disabling retries.
- `retry_max_elapsed` (default `"2m"`) — the maximum total wall-clock time spent
  retrying a single call, as a [Go duration string](https://pkg.go.dev/time#ParseDuration).
  Keep this comfortably under your CI step timeout so a retry storm does not
  cause the job to be killed mid-call.
- `request_timeout` (default `"30s"`) — the per-request HTTP timeout, as a Go
  duration string.

```terraform
# Example: four parallel pipelines sharing one Namecheap account.
provider "namecheap" {
  requests_per_minute = 5     # 20 / 4 jobs
  max_retries         = 6     # ride out brief bursts from sibling jobs
  retry_max_elapsed   = "3m"  # keep below the CI step timeout
  request_timeout     = "30s"
}
```

~> Prefer **serializing** Terraform runs for the same account where you can
(for example a job concurrency group), and use `requests_per_minute` as a
safety net rather than the only defense.

Each argument also has a `NAMECHEAP_*` environment variable
(`NAMECHEAP_REQUESTS_PER_MINUTE`, `NAMECHEAP_MAX_RETRIES`,
`NAMECHEAP_RETRY_MAX_ELAPSED`, `NAMECHEAP_REQUEST_TIMEOUT`), which is often more
convenient to set per pipeline.

## Debug logging

Set the `TF_LOG_PROVIDER_NAMECHEAP` environment variable to `DEBUG` to emit
structured, per-API-call log entries. Each entry includes the API command, the
attempt number, the call duration, the HTTP status, and the Namecheap error
code:

```sh
export TF_LOG_PROVIDER_NAMECHEAP=DEBUG
terraform apply
```

This is the fastest way to diagnose the failures most common in automation —
missing/incorrect credentials, an un-whitelisted `client_ip`, and rate-limit
throttling (visible as retries and error codes). The provider forwards only the
SDK's structured events; secret parameters are redacted by the SDK before
logging, so no credentials are added to the output.

-> `TF_LOG_PROVIDER_NAMECHEAP` scopes verbose logging to just this provider. Use
the broader `TF_LOG=DEBUG` only when you also need core Terraform and other
providers' logs, since it is far noisier.
