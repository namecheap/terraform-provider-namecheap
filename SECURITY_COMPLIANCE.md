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

- [Trivy](https://trivy.dev) runs on every code-touching push and PR in the
  `security` job with `scanners: vuln,misconfig,secret,license` (#166, #176).
  Changes gated away as non-code — root-level markdown, `LICENSE`, the
  release-please manifest — skip the scan jobs (see [CI.md](CI.md)); nothing
  in that set can alter the module graph or the shipped artifacts.
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
  every code-touching push and PR in the `govulncheck` job as a Go-native gate complementary to
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

The acceptance-test job runs on a fresh EC2 instance launched by the
self-hosted runner action for every run and terminated by `stop-runner`
afterwards. (The #279 warm pool — stop/start reuse between runs — was removed
under DEVOPS-22119: on this repo's baked AMI a warm restart measured no faster
than a cold launch, while parking the instance added a stop-wait to every
run.) See
[Runner lifecycle, hygiene sweeps, and the sandbox EIP](#runner-lifecycle-hygiene-sweeps-and-the-sandbox-eip)
below for the lifecycle/cleanup/EIP mechanics:

- Runner binary version is controlled via the SHA-pinned
  `namecheap/ec2-github-runner` action (currently bundles
  `actions/runner v2.335.1`, `externals/node24`, and writes outputs to
  `$GITHUB_OUTPUT` rather than the deprecated `::set-output`).
- Dependabot-triggered runs skip the EC2-backed jobs entirely because GitHub
  redacts `secrets.*` on `dependabot[bot]` events (#157). Maintainers
  re-trigger the acceptance pipeline manually per the flow in
  [`CONTRIBUTING.md`](CONTRIBUTING.md#dependabot-prs-maintainers).
- AMI comes from `DEVOPS/hardened-amazon-linux2023` (internal).
- Only one acceptance run may hold this repo's whitelisted Elastic IP at a
  time. `start-runner` serializes access with the SHA-pinned
  `ahmadnassri/action-workflow-queue` action (MIT), which queues a newer run
  behind older in-progress ones (FIFO, no cancellation). This replaces the
  former shared `concurrency` group, which silently cancelled pending runs
  when PRs were pushed close together. The EIP is dedicated to this repository
  (see the dedicated sandbox EIP bullet below), so this within-repo queue is
  the only serialization needed — there is no cross-repo mutex.

### Runner lifecycle, hygiene sweeps, and the sandbox EIP

- **Per-run lifecycle.** `start-runner` launches a fresh instance for every
  acceptance run and `stop-runner` terminates it afterwards (the action's
  default `reuse: terminate`). The #279 warm pool (`reuse: stop` +
  `reuse-pool-tag: sandbox-acceptance`) was removed under DEVOPS-22119: on
  this repo's baked AMI a warm restart registered in the same ~70s as a cold
  launch, while parking the instance added a 22-152s stop-wait to every run
  and an EIP-drift failure class. A fresh disk per run also removes the
  job-N+1-runs-on-job-N's-disk state carry-over the warm pool had to
  document and sweep around; the sandbox pipeline remains **push-only and
  never runs fork/PR code** (see
  [fork-safe PR gating](#fork-safe-pull-request-ci) below). The action's
  `max-lifetime-minutes` TTL (default 360) still arms on every launch as a
  self-destruct backstop for instances whose stop-runner never ran.
- **Per-job hygiene sweeps.** Defense in depth kept from the warm-pool era —
  cheap, and self-diagnosing even on a fresh instance. The checked-out
  workspace and the per-job `$HOME` at `/opt/ci/jobs/current` are wiped by
  `scripts/hygiene-sweep.sh` both before ("pre", which also emits a
  `::warning::` and self-heals if it finds leftovers from a crashed or
  cancelled prior run that skipped its own cleanup) and after ("post",
  `if: always()`, the step that guarantees no code, env vars, or secrets
  remain on the disk) every job. The initial workspace freshness guarantee
  at the very start of a run instead comes from `actions/checkout`'s own
  `clean: true`, since our script can't run before the repo it lives in has
  been checked out.
- **Dedicated sandbox EIP.** The whitelisted Elastic IP
  (`eipalloc-1796f61b`) is passed to the action's `eip-allocation-id` input,
  which associates it during every launch — that is what gives the instance
  connectivity during its own bootstrap (the fix for the CloudTrail-confirmed
  cold-boot failures) and is also the IP the Namecheap sandbox API allows.
  Associating *after* `mode: start` is deliberately avoided: it would change
  an already-registered runner's public IP and sever its connection to
  GitHub, hanging the job. **This repository is currently the
  sole user of this allocation** — `mcp-server-namecheap`'s Acceptance (EC2)
  workflow is *disabled* pending its own dedicated EIP (#282 /
  `mcp-server-namecheap#16`, both still open), so nothing else associates the
  IP today. This is an interim stopgap enforced by that disabled workflow, not
  yet a permanent architectural per-repo split; re-enabling that workflow
  before it has its own EIP would reintroduce the contention. On that basis
  there is no cross-repo contention to arbitrate and no lock script:
  `start-runner`'s "Resolve sandbox EIP public IP" step publishes the
  allocation's public IP, and `acceptance_test`'s
  credential-free "Verify sandbox EIP" step compares the runner's actual
  public IP against it — refusing to run `make testacc` unless the runner
  really holds the whitelisted IP, which also confirms the association
  succeeded. Concurrent runs *within this repo* are still serialized by
  `ahmadnassri/action-workflow-queue` (a single EIP can't serve two runners at
  once); there is no explicit release step anywhere — release happens as an
  automatic AWS side effect of `stop-runner` terminating the instance at the
  end of every run (or of the leak reaper terminating a leaked one). Because
  that release only lands when the old instance reaches `terminated`,
  `start-runner`'s "Wait for the sandbox EIP to be free" step polls
  `DescribeAddresses` before launching, so a back-to-back run doesn't race
  the previous instance's shutdown for the association.
- **IAM prerequisite.** The CI AWS identity (`sys_github_runner_provisioner`)
  needs the full set below for the EIP + diagnostics model; an
  admin granting less will hit `UnauthorizedOperation` mid-cycle (see the
  policy-diff comment on #281 for the ready-to-apply statements):
  - **Instance lifecycle:** `ec2:RunInstances`,
    `ec2:TerminateInstances`, `ec2:CreateTags`, `ec2:DescribeImages`,
    `ec2:DescribeInstances`, `ec2:DescribeInstanceStatus`.
    (`ec2:StopInstances`, `ec2:StartInstances` and
    `ec2:ModifyInstanceAttribute` were needed only by the removed warm pool
    and can be revoked.)
  - **EIP:** `ec2:AssociateAddress` (the action attaches the EIP during
    launch) and `ec2:DescribeAddresses` (the
    "Resolve sandbox EIP public IP" step). `ec2:DisassociateAddress` is
    **not** required — the cross-repo EIP reaper that used it was removed with
    the mutex.
  - **Bootstrap diagnostics:** `ec2:GetConsoleOutput` and `ec2:DescribeTags`,
    for the action's fast-fail/console-capture on a failed registration
    (without them, failures degrade to timeout-only detection with no console
    output — the exact symptom seen earlier in this PR).
- **Leak reaper.**
  [`cleanup-ec2-runners.yml`](.github/workflows/cleanup-ec2-runners.yml) runs
  `mode: cleanup` every 30 minutes (`7,37 * * * *` UTC, the action's default
  thresholds) to terminate crashed/cancelled leftovers whose stop-runner
  never ran. (The former nightly full-drain pass existed only to empty the
  warm pool and was removed with it.) The reaper never touches the EIP
  directly — it is released as an automatic AWS side effect of instance
  termination. `workflow_dispatch` exposes a `dry_run` input (default
  `true`) for safe manual runs.
- **One EIP per repo (no cross-repo mutex).** `eipalloc-1796f61b` was
  previously shared with `namecheap/mcp-server-namecheap`'s acceptance
  workflow, which could silently reassociate ("steal") it mid-run. That is
  why this pipeline once carried a cross-repo lock (`scripts/eip-mutex.sh`
  with `wait-until-free` reaping + `verify`). That lock — its script, its two
  workflow steps, and the `ec2:DisassociateAddress` grant — has been removed.
  The migration to a genuine per-repo split is **in progress, not complete**:
  `mcp-server-namecheap`'s acceptance workflow is disabled as an interim
  stopgap so it no longer touches this IP, and giving it its own dedicated EIP
  (after which the disable is lifted) is still open work. Tracked under the
  "Dedicated per-repo sandbox EIP" milestone:
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
