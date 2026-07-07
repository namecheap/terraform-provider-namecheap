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
- Go toolchain version is pinned in `go.mod` (currently `go 1.25.9`).
- No `vendor/` tree. Module resolution goes through `GOPROXY` with `go.sum`
  as the integrity authority; removing vendor kept the auditable chain
  (`go.mod` → `go.sum` → GOPROXY) while shedding 29 MB of committed churn
  and the unreviewable vendor diffs that every dep bump produced.

CI gates (in [`.github/workflows/ci.yml`](.github/workflows/ci.yml), `check` job):

- `go mod verify` — rehashes the module cache against `go.sum` (#163).
- `go mod tidy` + `git diff --exit-code` — fails on `go.mod` / `go.sum` drift
  (#174).

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
- [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) runs on
  every push in the `govulncheck` job as a Go-native gate complementary to
  Trivy (#233). It performs call-graph **reachability** analysis against the Go
  vulnerability database (<https://vuln.go.dev>) and the standard library for
  the pinned Go toolchain, and **fails the build when a vulnerable symbol is
  reachable from this module's code** — catching exploitable paths (including
  stdlib CVEs) that Trivy's version-range dependency scan does not cover.
  Imported-but-uncalled vulnerabilities are reported but do not block, so the
  gate stays low-noise. The binary is pinned (`@v1.3.0`) and bumped by hand
  (it's not in `go.mod`, so Dependabot doesn't track it); the advisory DB is
  fetched fresh on every run, so detection stays current regardless.

## Software Bill of Materials (SBOM)

- CycloneDX JSON SBOM generated per push, uploaded as a workflow artifact
  (`sbom-cyclonedx`, 90-day retention) (#166).
- CycloneDX JSON SBOM attached to every tagged GitHub Release as
  `terraform-provider-namecheap_<tag>_sbom.cdx.json` (#177).
- SBOM is produced by Trivy against the source tree, matching the CVE /
  license scans so the three views (scan report, workflow artifact,
  release asset) are consistent.

## PR security summary

- The `security-report` job in [`.github/workflows/ci.yml`](.github/workflows/ci.yml)
  consolidates every scan into one Markdown report and posts it as a **sticky
  PR comment** (one comment, updated in place per push), the run's **job
  summary**, and the **`security-summary`** workflow artifact (90-day
  retention) (#234).
- The report covers: Trivy vulnerabilities (by severity), IaC misconfig, secret
  and license findings; govulncheck reachable vs. imported-only counts; open
  CodeQL code-scanning alerts (best-effort, via the API); and a dependency
  overview (SBOM component count, `go.mod` direct/indirect split, license
  breakdown).
- It is **reporting only and never gates.** The per-scan pass/fail shown in the
  summary is taken verbatim from the `security` and `govulncheck` gate jobs
  (`needs.*.result`), so a green comment can never mask a red gate. The merge
  gates remain exactly as described above.
- Source data comes from non-gating JSON passes of the same scanners
  (`trivy-results.json`, `govulncheck.json`) plus the existing SBOM, uploaded as
  the `trivy-json` / `govulncheck-json` / `sbom-cyclonedx` artifacts. Secret
  findings list the rule and location only — never the matched value.
- The comment is posted with the default `GITHUB_TOKEN` (`pull-requests: write`,
  scoped to the `security-report` job). Dependabot runs skip the comment (their
  token is read-only) but still produce the job summary and artifact.

## CI / supply-chain pinning

- Every third-party GitHub Action is pinned by 40-char commit SHA with a
  trailing `# v<semver>` comment (#147, #161). Dependabot's `github-actions`
  ecosystem (#150) rotates the SHA + comment atomically when a new version
  releases.
- `namecheap/ec2-github-runner` is SHA-pinned for both the `start-runner`
  and `stop-runner` jobs at the same SHA (#149). Rotations are tracked
  as single-line PRs (#159, #164, #165).
- `hashicorp/setup-terraform` and `opentofu/setup-opentofu` (used by the
  fork-safe `acceptance_mock` job) are likewise SHA-pinned with a trailing
  `# v<semver>` comment.
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

- `go.mod` + `go.sum` make module resolution reproducible and integrity-
  checked against the cryptographic hashes in `go.sum`. Re-fetches through
  a different `GOPROXY` mirror produce identical bytes or the build fails.
- `GoReleaser` runs with `mod_timestamp: '{{ .CommitTimestamp }}'` and
  `-trimpath` in [`.goreleaser.yml`](.goreleaser.yml) for reproducible binary
  builds.
- Release artifacts are GPG-signed (`_SHA256SUMS.sig`) and carry a GitHub-
  issued build-provenance attestation via `actions/attest-build-provenance`.

## Self-hosted runner supply chain

The acceptance-test job runs on an EC2 instance launched by the self-hosted
runner action. Since #279 that instance is kept warm (stopped, not
terminated) between runs — see
[Warm-pool lifecycle, hygiene sweeps, and the EIP mutex](#warm-pool-lifecycle-hygiene-sweeps-and-the-eip-mutex-279)
below for the reuse/cleanup/mutex mechanics:

- Runner binary version is controlled via the SHA-pinned
  `namecheap/ec2-github-runner` action (currently bundles
  `actions/runner v2.335.1`, `externals/node24`, and writes outputs to
  `$GITHUB_OUTPUT` rather than the deprecated `::set-output`).
- Dependabot-triggered runs skip the EC2-backed jobs entirely because GitHub
  redacts `secrets.*` on `dependabot[bot]` events (#157). Maintainers
  re-trigger the acceptance pipeline manually per the flow in
  [`CONTRIBUTING.md`](CONTRIBUTING.md#dependabot-prs-maintainers).
- AMI comes from `DEVOPS/hardened-amazon-linux2023` (internal).
- Only one acceptance run may hold the single whitelisted Elastic IP at a
  time. Within this repo, `start-runner` serializes access with the
  SHA-pinned `ahmadnassri/action-workflow-queue` action (MIT), which queues a
  newer run behind older in-progress ones (FIFO, no cancellation). This
  replaces the former shared `concurrency` group, which silently cancelled
  pending runs when PRs were pushed close together. Across repos, the EIP
  mutex described below adds a second layer, since the queue only serializes
  runs within this repository.

### Warm-pool lifecycle, hygiene sweeps, and the EIP mutex (#279)

- **Warm pool.** `stop-runner` calls `namecheap/ec2-github-runner` with
  `reuse: stop` and `reuse-pool-tag: sandbox-acceptance` instead of
  terminating the instance, and `start-runner` reuses it on the next push.
  Most pushes therefore warm-start in seconds; a cold boot (~2-4 min) only
  happens after the nightly drain or the action's own `max-lifetime-minutes`
  (default 360) / `reuse-max-cycles` (default 20) limits, both left at their
  defaults. This is safe only because the sandbox pipeline is **push-only and
  never runs fork/PR code** — warm reuse means job N+1 runs on job N's disk,
  which the [fork-safe PR gating](#fork-safe-pull-request-ci) below makes a
  guarantee about trusted code only, not a coincidence. The
  `max-lifetime-minutes` TTL arms only on a cold boot and is **not** re-armed
  by a warm restart, so it is a first-session backstop, not the real
  daily-termination mechanism — that's the nightly drain below.
- **Per-job hygiene sweeps.** Only the Go/Terraform toolchain
  (`/opt/ci/toolcache`) and the Go build caches (`GOCACHE`/`GOMODCACHE` under
  `/opt/ci/cache`) persist across a stop/start cycle. Everything else — the
  checked-out workspace and the per-job `$HOME` at `/opt/ci/jobs/current` — is
  wiped by `scripts/hygiene-sweep.sh` both before ("pre", which also emits a
  `::warning::` and self-heals if it finds leftovers from a crashed or
  cancelled prior run that skipped its own cleanup) and after ("post",
  `if: always()`, the step that actually guarantees no code, env vars, or
  secrets remain on the stopped disk) every job. The initial workspace
  freshness guarantee at the very start of a run instead comes from
  `actions/checkout`'s own `clean: true`, since our script can't run before
  the repo it lives in has been checked out.
- **EIP mutex (`scripts/eip-mutex.sh`).** The whitelisted Elastic IP
  (`eipalloc-1796f61b`) is passed to the action's `eip-allocation-id` input
  again on every cold launch — that input is what actually associates the
  EIP, giving the instance connectivity during its own bootstrap (the fix
  for the CloudTrail-confirmed cold-boot failures). Immediately before that
  step, `start-runner` runs `scripts/eip-mutex.sh wait-until-free` as a
  precondition gate (not an acquire-and-hold): it blocks until the
  allocation is unassociated, or reaps it off a stopped instance that
  belongs to another repository, so the action's own association doesn't
  race a still-live holder elsewhere. The EIP is then deliberately left
  attached to the pool instance across every warm stop/start cycle rather
  than released per stop — the action's warm-restart path never
  re-associates the EIP itself, so a warm restart's connectivity depends on
  it already being there. After `mode: start` returns, `start-runner`'s
  "Verify EIP ownership" step (`scripts/eip-mutex.sh verify`) hard-asserts
  the EIP is still associated with this job's instance, covering both the
  residual cold-launch race and a warm-restart cross-repo steal, before
  `acceptance_test` is allowed to run `make testacc`. There is no explicit
  release step anywhere — release only ever happens as an automatic AWS
  side effect of `cleanup-ec2-runners.yml` terminating the pool instance,
  which is normally the nightly full drain but can occasionally be the
  leak-reaper pass instead, if the pool instance happens to sit stopped
  past its default `reaper-stopped-max-age` before the next drain runs
  (see below).
- **New IAM prerequisite.** The CI AWS identity now additionally requires
  `ec2:DisassociateAddress` and `ec2:DescribeAddresses`, on top of the
  already-granted `ec2:AssociateAddress` and `ec2:DescribeInstances`. This
  repository does not manage that IAM policy — **an AWS admin must grant both
  new actions to the CI role/user before this pipeline will work.**
- **Nightly drain and leak reaper.**
  [`cleanup-ec2-runners.yml`](.github/workflows/cleanup-ec2-runners.yml) runs
  `mode: cleanup` on two schedules: a nightly full drain at `37 2 * * *` UTC
  with a deliberately tiny `reaper-stopped-max-age` so the day's pool
  instance always ages past the threshold and is terminated, and a
  `7,37 * * * *` UTC leak-reaper pass at the action's own default threshold
  that catches crashed/cancelled leftovers without draining the warm pool
  during the day. Neither pass touches the EIP directly — it stays attached
  to the pool instance across every warm stop/start cycle and is only
  released as an automatic AWS side effect of whichever pass terminates
  that instance first. That's normally the nightly full drain, but the
  leak-reaper pass will do it instead if the pool instance is ever left
  stopped long enough to age past its own default threshold between
  drains (see the EIP mutex bullet above).
  `workflow_dispatch` exposes a `mode` choice and a `dry_run` input (default
  `true`) for safe manual runs.
- **Cross-repo hazard.** `eipalloc-1796f61b` is also used by
  `namecheap/mcp-server-namecheap`'s acceptance workflow, which as of this
  writing still hands the IP straight to the action's permissive
  `eip-allocation-id` input and can silently reassociate it away from a run
  in this repo. This repository's own cold-launch association now goes
  through that same permissive `eip-allocation-id` input — not an atomic
  `--no-allow-reassociation` test-and-set performed by our own script — so
  the association call itself is no longer what protects us. Protection
  instead comes from wrapping that call: `wait-until-free` blocks
  beforehand so the association doesn't race a still-live holder, and
  `verify` hard-fails afterward if the EIP turns out not to be associated
  with our own instance. This makes the repository a well-behaved actor —
  it will wait on, or loudly fail, contention rather than stealing — but it
  cannot force the other repository to do the same, and cannot prevent a
  steal, only detect one after the fact. Until then, an occasional
  `acceptance_test` failure at the "Verify sandbox EIP" step may be caused
  by a concurrent run in that other repo rather than a bug here.

  **Planned resolution.** Rather than have `mcp-server-namecheap` adopt the
  same wait/verify discipline, each repo is being moved to its **own**
  dedicated whitelisted EIP, which removes the contention at the source and
  lets this whole mutex (`scripts/eip-mutex.sh`, the `wait-until-free` /
  `verify` steps, and the `ec2:DisassociateAddress` grant) be deleted.
  Tracked under the "Dedicated per-repo sandbox EIP" milestone:
  namecheap/terraform-provider-namecheap#282 (this repo) and
  namecheap/mcp-server-namecheap#16 (the dedicated EIP for that repo).

## Fork-safe pull-request CI

CI runs on both `push` and `pull_request`, which lets PRs from forks run the
required checks and a credential-free acceptance suite without ever exposing
secrets:

- The trigger is `pull_request`, **never `pull_request_target`** — fork code runs
  only on GitHub-hosted runners with a read-only token and `secrets.*` redacted.
- The secret-bound EC2 sandbox jobs (`start-runner`, `acceptance_test`,
  `stop-runner`) are gated `github.event_name == 'push'`, so untrusted fork code
  never reaches the self-hosted runner, the whitelisted Elastic IP, or the AWS /
  Namecheap credentials. They keep running on every in-repo push, unchanged.
- The `acceptance_mock` job provides the fork-facing acceptance signal: it runs
  on `pull_request` on GitHub-hosted `ubuntu-latest`, references no `secrets.*`,
  and drives the in-process mock (see
  [`CONTRIBUTING.md`](CONTRIBUTING.md#running-mock-acceptance-tests)). The
  test-only `NAMECHEAP_API_URL` endpoint override it relies on is compiled only
  under the `testacc` build tag and is absent from released binaries.
- No `${{ github.event.* }}` value is interpolated into a `run:` shell block.

## What triggers a compliance failure

Any of the following fails the build and blocks merge:

- `go.mod` or `go.sum` not tidy.
- HIGH/CRITICAL CVE with an available fix in any dependency.
- Reachable Go vulnerability (dependency or stdlib) detected by govulncheck.
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
