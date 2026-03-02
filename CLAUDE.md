# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make build          # Build binary: terraform-provider-namecheap
make format         # go fmt ./...
make check          # go vet ./...
make lint           # golangci-lint run (requires local install)
make test           # Unit tests: go test -v ./namecheap/... -count=1 -cover
make testacc        # Acceptance tests (requires env vars, see below)
make docs           # Generate docs with tfplugindocs
make vendor         # go mod vendor
```

Run a single test: `go test -v ./namecheap/... -run TestFunctionName -count=1`

### Acceptance Tests

Require environment variables: `NAMECHEAP_USER_NAME`, `NAMECHEAP_API_USER`, `NAMECHEAP_API_KEY`, `NAMECHEAP_TEST_DOMAIN`. Set `NAMECHEAP_USE_SANDBOX=true` for sandbox testing.

## Architecture

This is a Terraform provider built with **terraform-plugin-sdk/v2** that manages Namecheap domain DNS configuration through the **go-namecheap-sdk/v2**.

### Single Resource Provider

The provider exposes one resource: `namecheap_domain_records`. All provider logic lives in the `namecheap/` package (package name: `namecheap_provider`).

**Key files:**
- `main.go` — Plugin entry point, serves the provider
- `namecheap/provider.go` — Provider schema, config, and API client setup
- `namecheap/namecheap_domain_record.go` — Resource CRUD schema and dispatch
- `namecheap/namecheap_domain_record_functions.go` — Core business logic (~670 lines)
- `namecheap/internal/mutexkv/` — Domain-level mutex for concurrent access

### MERGE vs OVERWRITE Mode

The central design pattern. Every CRUD operation has paired implementations:

- **MERGE mode** (default): Multiple Terraform configs can manage different records on the same domain. Uses `ncMutexKV` (defined in provider.go) for domain-level locking to prevent race conditions.
- **OVERWRITE mode**: Single Terraform config owns all records for a domain. No locking needed.

Functions follow the naming convention: `{operation}Records{Mode}()` and `{operation}Nameservers{Mode}()` (e.g., `createRecordsMerge`, `readRecordsOverwrite`).

### Address Normalization

DNS records require address fixup before API calls:
- `getFixedAddressOfRecord()` — Routes to type-specific fixers
- `fixAddressEndWithDot()` — Appends trailing dot for CNAME, ALIAS, NS, MX records
- `fixCAAAddressValue()` — Formats CAA record values with flags, tag, and quoted value
- `filterDefaultParkingRecords()` — Strips Namecheap default parking records

### Error Handling

Uses `diag.Diagnostics` throughout for Terraform-native error reporting. API errors are wrapped with `diag.FromErr()`.

## CI Pipeline

Runs on push (`.github/workflows/ci.yml`):
1. `go vet` + golangci-lint v1.54 + unit tests (ubuntu-latest)
2. Acceptance tests on self-hosted EC2 runner (AL2023) against Namecheap sandbox

## go-namecheap-sdk/v2 (Core Dependency)

The provider is entirely built on `github.com/namecheap/go-namecheap-sdk/v2`. Understanding SDK patterns is critical.

### SDK Client Structure

The `*namecheap.Client` (stored as `meta interface{}` in provider) exposes three services:
- `client.Domains` — `GetInfo()`, `GetList()`
- `client.DomainsDNS` — `GetHosts()`, `SetHosts()`, `GetList()`, `SetCustom()`, `SetDefault()`
- `client.DomainsNS` — `Create()`, `Delete()`, `GetInfo()`, `Update()`

DNS methods accept full domain strings and parse internally. NS methods take pre-split `sld`/`tld` parameters.

### Pointer-Heavy Design

All SDK struct fields are pointers (`*string`, `*int`, `*bool`). Use the SDK helper constructors: `namecheap.String()`, `namecheap.Int()`, `namecheap.Bool()`, `namecheap.UInt8()`. Nil fields mean absent/unset values, not zero values.

### MXPref Type Mismatch

`GetHosts` returns `MXPref` as `*int`, but `SetHosts` expects `*uint8`. The provider bridges this with `namecheap.UInt8(uint8(*remoteRecord.MXPref))`.

### Retry Logic

The SDK retries on HTTP 405 (Namecheap's rate-limit response) with progressive delays: 1s, 5s, 15s, 30s, 50s. Retries are mutex-serialized. Total max wait: 101 seconds.

### SetHosts Validation

The SDK validates client-side before API calls:
- Record type must be in `AllowedRecordTypeValues` (A, AAAA, ALIAS, CAA, CNAME, MX, MXE, NS, TXT, URL, URL301, FRAME)
- TTL must be 60–60000
- MX records require `MXPref` and `EmailType == "MX"`; MXE requires `EmailType == "MXE"` and exactly 1 record
- URL/URL301/FRAME records require protocol prefix; CAA iodef requires `http://` or `mailto:`
- Email type must be in: NONE, MXE, MX, FWD, OX, GMAIL

### SDK Gotchas

- `SetCustom()` requires minimum 2 nameservers — the provider enforces this in merge logic too
- `DomainsDNS.GetList()` silently falls back to `Domains.GetInfo()` on error 2019166 (FreeDNS domains)
- `ParseDomain()` handles compound TLDs (`co.uk`, `gov.ua`) via `publicsuffix-go`
- Default parking records (CNAME www→parkingpage.namecheap.com, URL @→http://www.domain) are returned by the API and must be filtered
- `GetHosts` error checking uses `len(response.Errors) > 0` while all other methods use `response.Errors != nil && len(*response.Errors) > 0`

## Key Dependencies

- Go 1.21.5
- `github.com/hashicorp/terraform-plugin-sdk/v2` v2.31.0
- `github.com/namecheap/go-namecheap-sdk/v2` v2.4.0
- `github.com/stretchr/testify` v1.8.4 (tests)
