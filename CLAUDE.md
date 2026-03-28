# CLAUDE.md

## Verification Steps

Run these after making changes:

```bash
make build          # Build binary
make format         # go fmt ./...
make check          # go vet ./...
make lint           # golangci-lint run
make test           # Unit tests with coverage
```

Run a single test: `go test -v ./namecheap/... -run TestFunctionName -count=1`

## Architecture

Terraform provider (terraform-plugin-sdk/v2) managing Namecheap domain DNS via go-namecheap-sdk/v2. Single resource: `namecheap_domain_records`. All logic in `namecheap/` package (package name: `namecheap_provider`).

### MERGE vs OVERWRITE Mode

Central design pattern — every CRUD operation has paired implementations:

- **MERGE** (default): Multiple Terraform configs manage different records on the same domain. Uses `ncMutexKV` for domain-level locking.
- **OVERWRITE**: Single config owns all records for a domain.

Naming convention: `{operation}Records{Mode}()`, `{operation}Nameservers{Mode}()`.

## go-namecheap-sdk/v2

Internal SDK owned by the same team. Source: `github.com/namecheap/go-namecheap-sdk/v2`. Vendored in this repo.

**Input/Output struct asymmetry**: The SDK uses different structs for reading vs writing DNS records:
- `DomainsDNSHostRecord` — input struct for `SetHosts()` (fields: HostName, RecordType, Address, MXPref `*uint8`, TTL)
- `DomainsDNSHostRecordDetailed` — output struct from `GetHosts()` (fields: HostId, Name, Type, Address, MXPref `*int`, TTL, IsActive, etc.)

### SDK Gotchas

- **Pointer-heavy**: All struct fields are pointers. Use helpers: `namecheap.String()`, `namecheap.Int()`, `namecheap.Bool()`, `namecheap.UInt8()`.
- **MXPref type mismatch**: Follows from the struct asymmetry above — `GetHosts` returns `*int`, `SetHosts` expects `*uint8`. Bridge with `namecheap.UInt8(uint8(*remoteRecord.MXPref))`.
- **SetCustom()** requires minimum 2 nameservers.
- **Default parking records** (CNAME www→parkingpage.namecheap.com, URL @→http://www.domain) are returned by the API and must be filtered.
- **Inconsistent error checking**: `GetHosts` uses `len(response.Errors) > 0`, all other methods use `response.Errors != nil && len(*response.Errors) > 0`.
