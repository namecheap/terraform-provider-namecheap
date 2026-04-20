# Security & Compliance

This document is the single entry point for the security and compliance controls
enforced in this repository. It is a reference for auditors, maintainers, and
contributors — kept short, current, and linked to the PR or issue that
introduced each control. If you are looking for "how do I report a
vulnerability", see [Reporting a vulnerability](#reporting-a-vulnerability) at
the bottom.

## Dependency declaration and pinning

- Every direct and indirect Go dependency is declared in [`go.mod`](go.mod)
  with an exact version or pseudo-version.
- [`go.sum`](go.sum) records a cryptographic hash for each module; re-fetches
  that don't match the hash fail the build.
- [`vendor/`](vendor/) is committed and kept in sync with `go.mod`. Vendoring
  is not a soft policy — CI fails if `go mod vendor` would produce a diff.
- Go toolchain version is pinned in `go.mod` (currently `go 1.25.9`).

CI gates (in [`.github/workflows/ci.yml`](.github/workflows/ci.yml), `check` job):

- `go mod verify` — rehashes the module cache against `go.sum` (#163).
- `go mod tidy` + `git diff --exit-code` — fails on `go.mod` / `go.sum` drift
  (#174).
- `go mod vendor` + `git diff --exit-code` — fails on `vendor/` drift (#175).

## Vulnerability, misconfig, secret, and license scanning

- [Trivy](https://trivy.dev) runs on every push in the `security` job with
  `scanners: vuln,misconfig,secret,license` (#166, #176).
- Gate: `CRITICAL,HIGH` with `ignore-unfixed: true` — unfixable advisories
  surface in the run log but don't block merges.
- License policy lives in [`trivy.yaml`](trivy.yaml) at repo root. Denylist
  (not allowlist) — six copyleft / source-available licenses are rejected:
  `GPL-2.0`, `GPL-3.0`, `AGPL-1.0`, `AGPL-3.0`, `LGPL-3.0`, `SSPL-1.0`.
  Every other license is accepted automatically, no review needed.
- Exception workflow: open a PR adding a scoped `ignored-licenses:` entry
  (package + license combination) to `trivy.yaml`, with a justification and
  a reviewer from outside the requesting team.

## Software Bill of Materials (SBOM)

- CycloneDX JSON SBOM generated per push, uploaded as a workflow artifact
  (`sbom-cyclonedx`, 90-day retention) (#166).
- CycloneDX JSON SBOM attached to every tagged GitHub Release as
  `terraform-provider-namecheap_<tag>_sbom.cdx.json` (#177).
- SBOM is produced by Trivy against the source tree, matching the CVE /
  license scans so the three views (scan report, workflow artifact,
  release asset) are consistent.

## CI / supply-chain pinning

- Every third-party GitHub Action is pinned by 40-char commit SHA with a
  trailing `# v<semver>` comment (#147, #161). Dependabot's `github-actions`
  ecosystem (#150) rotates the SHA + comment atomically when a new version
  releases.
- `namecheap/ec2-github-runner` is SHA-pinned for both the `start-runner`
  and `stop-runner` jobs at the same SHA (#149). Rotations are tracked
  as single-line PRs (#159, #164, #165).
- Dependabot covers `gomod` and `github-actions` with a weekly cadence
  and a 5-PR cap per ecosystem (#150).

## Binary download integrity

On the self-hosted acceptance-test runner, two binaries are fetched from
vendor CDNs. Both are checksum-verified before use (#160):

- Go toolchain tarball → verified against `dl.google.com`'s `<tarball>.sha256`.
- Terraform CLI zip → verified against HashiCorp's `terraform_<ver>_SHA256SUMS`.

Either a transport-layer tamper or an origin compromise fails the step
before `tar -xzf` / `unzip` runs.

## Reproducibility

- `go.mod` + `go.sum` + the `vendor/` tree make module resolution reproducible
  on any Go-enabled machine without proxy access.
- `GoReleaser` runs with `mod_timestamp: '{{ .CommitTimestamp }}'` and
  `-trimpath` in [`.goreleaser.yml`](.goreleaser.yml) for reproducible binary
  builds.
- Release artifacts are GPG-signed (`_SHA256SUMS.sig`) and carry a GitHub-
  issued build-provenance attestation via `actions/attest-build-provenance`.

## Self-hosted runner supply chain

The acceptance-test job runs on an ephemeral EC2 instance launched by the
self-hosted runner action:

- Runner binary version is controlled via the SHA-pinned
  `namecheap/ec2-github-runner` action (currently bundles
  `actions/runner v2.333.1`, `externals/node24`, and writes outputs to
  `$GITHUB_OUTPUT` rather than the deprecated `::set-output`).
- Dependabot-triggered runs skip the EC2-backed jobs entirely because GitHub
  redacts `secrets.*` on `dependabot[bot]` events (#157). Maintainers
  re-trigger the acceptance pipeline manually per the flow in
  [`CONTRIBUTING.md`](CONTRIBUTING.md#dependabot-prs-maintainers).
- AMI comes from `DEVOPS/hardened-amazon-linux2023` (internal).

## What triggers a compliance failure

Any of the following fails the build and blocks merge:

- `go.mod` or `go.sum` not tidy.
- `vendor/` tree does not match `go.mod`.
- HIGH/CRITICAL CVE with an available fix in any dependency.
- Denied license in any dependency.
- Trivy secret scan matches a credential-shaped string.
- Go or Terraform tarball SHA-256 mismatch on the self-hosted runner.
- Stale module hashes (`go mod verify`).
- Pre-commit hook failures (DCO sign-off missing, etc.).

## Reporting a vulnerability

Please use GitHub's "Report a vulnerability" flow under the repository's
**Security** tab, or open a private security advisory directly at
<https://github.com/namecheap/terraform-provider-namecheap/security/advisories/new>.
Do not open a public issue for security reports.
