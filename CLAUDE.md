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

## DCO Sign-off

All git commits must include a `Signed-off-by` line for the [Developer Certificate of Origin](https://developercertificate.org/) (DCO) check to pass. The DCO bot on GitHub will block PRs that contain unsigned commits.

- Use `git commit --signoff` (or `-s`) to add the sign-off automatically.
- To sign off an entire branch retroactively: `git rebase HEAD~N --signoff` (replace N with the number of commits).
- The `Signed-off-by` identity must match the commit's author or committer name and email.

## Git privacy

Before creating git commits, check that `git config user.email` is set. If it is not configured, suggest the contributor set one. Do not override an already-configured email.

## Pull Requests

- All CI checks must pass before merge (unit tests, acceptance tests, CodeQL, DCO).
- PRs should include both unit tests and Terraform acceptance tests where applicable.
- Acceptance tests use `resource.Test()` with `TestStep` — see `namecheap/provider_test.go` for examples.

### Dependabot PRs

Workflow runs triggered by `dependabot[bot]` do **not** have access to `secrets.*` (GitHub redacts them by design). The `start-runner`, `acceptance_test`, and `stop-runner` jobs are gated with `if: ${{ github.actor != 'dependabot[bot]' }}` and appear as **skipped**, not failed, on Dependabot PRs — treat that as the expected state, not a regression.

When reviewing or preparing a Dependabot PR for merge:

- The `check` job (unit tests, lint, Codecov) must still be green.
- Skipped EC2 jobs are not a failure and do not need "re-running" as-is.
- Before approving merge, trigger acceptance tests manually under a maintainer identity so secrets resolve:
  ```shell
  gh workflow run CI --ref dependabot/go_modules/<branch-name>
  ```
  The resulting run is attributed to the maintainer, so `github.actor != 'dependabot[bot]'` is true and the full pipeline executes.

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
