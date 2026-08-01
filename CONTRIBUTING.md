# Contributing to Terraform - Namecheap Provider

You're welcome to start a discussion about required features, file an issue or submit a work in progress (WIP) pull
request. Feel free to ask us for help. We'll do our best to guide you and help you to get on it.

## Tests

### Running unit tests

To run unit tests, execute the following command:

```shell
$ make test
```

### Running acceptance tests

Before going forward, you must set up the following environment variables:

```dotenv
NAMECHEAP_USER_NAME=user_name
NAMECHEAP_API_USER=user_name
NAMECHEAP_API_KEY=api_key
NAMECHEAP_CLIENT_IP=your.whitelisted.ip # optional
NAMECHEAP_TEST_DOMAIN=my-domain.com
NAMECHEAP_USE_SANDBOX=true # optional
```

To simplify testing, you can sign up a free account on
our [Sandbox](https://www.namecheap.com/support/knowledgebase/article.aspx/763/63/what-is-sandbox/) environment,
purchase (for free) the fake domain and use the credentials from there for testing environment described below.

**NOTE:** Do not forget to set up `NAMECHEAP_USE_SANDBOX=true` for sandbox account!

**NOTE:** Make sure you have whitelisted your public IP address! Follow
our [API Documentation](https://www.namecheap.com/support/api/intro/) to get info about whitelisting IP.

Run acceptance tests:

```shell
$ make testacc
```

### Running mock acceptance tests

The provider also ships a **mock-backed** acceptance suite that runs the same
`resource.Test` lifecycles against an in-process, stateful mock of the Namecheap
API — no credentials, no network, no whitelisted IP:

```shell
$ make testacc-mock
```

This suite is compiled only under the `testacc` build tag (which enables a
test-only `NAMECHEAP_API_URL` endpoint override; see
[`SECURITY_COMPLIANCE.md`](SECURITY_COMPLIANCE.md#fork-safe-pull-request-ci)),
so it never affects `make test` or released binaries. It still drives a real
`terraform` binary; `make testacc-mock` uses the one on your `PATH` (set
`TF_ACC_TERRAFORM_PATH` to target a specific `terraform`/`tofu`). It is a fast,
credential-free **complement** to — not a replacement for — the live sandbox
suite, which remains the source of truth for API-contract behavior.

If the Namecheap SDK ever ships a reusable mock package (SDK issue #121), the
in-repo mock in `namecheap/mock_server_test.go` can be swapped out behind the
same test helpers without touching individual test bodies.

## Git Email Privacy

To keep your personal email out of the public git history, consider using a GitHub noreply address.
You can enable this in [GitHub Settings → Emails](https://github.com/settings/emails) by checking **"Keep my email addresses private"**. Your noreply address follows the format `<id>+<username>@users.noreply.github.com` and is shown on that page.

To use it for this repo:

```shell
$ git config user.email "YOUR_ID+YOUR_USERNAME@users.noreply.github.com"
```

This is optional — use whichever email you prefer.

## Commits and DCO

This project enforces the [Developer Certificate of Origin](https://developercertificate.org/) (DCO) on all pull
requests. The DCO bot will block merging if any commit is missing a sign-off.

Every commit message **must** include a `Signed-off-by` line matching the commit author's name and email:

```
Signed-off-by: Author Name <authoremail@example.com>
```

Use the `-s` flag to add it automatically:

```shell
$ git commit -s -m "your commit message"
```

If you forgot to sign off, you can fix all commits on your branch at once:

```shell
$ git rebase HEAD~N --signoff   # replace N with the number of commits
$ git push --force-with-lease
```

## Pull Requests

- Ensure all CI checks pass: unit tests, acceptance tests, CodeQL analysis, and DCO.
- Include both unit tests and [Terraform acceptance tests](https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests)
  where applicable. Acceptance tests should use `resource.Test()` with `TestStep`.
- Keep PRs focused — one logical change per PR.
- Adding a resource or data source? See [Definition of done](#definition-of-done-for-a-new-resource-or-data-source) — the registry page is generated, so documentation is part of the change, not a follow-up.
- Familiarize yourself with [`SECURITY_COMPLIANCE.md`](SECURITY_COMPLIANCE.md) for the compliance gates your PR will be checked against (dependency drift, vulnerability and license scans, SBOM publication, etc.).

### Definition of done for a new resource or data source

Registry documentation is generated from the provider schema by
[tfplugindocs](https://github.com/hashicorp/terraform-plugin-docs), which is
pinned as a Go tool dependency — no separate install. A new surface is finished
when all of the following are true, and CI's `docs` job enforces every one:

1. **Every attribute has a `Description`.** The published page is generated from
   these, so an empty one ships as a blank cell. `TestSchemaDescriptionsArePresent`
   fails on any attribute without one — nested blocks included — and
   `TestSchemaDescriptionsAreUsable` rejects text that merely restates the
   attribute name. Put the nuance in the description itself: whether a change
   forces replacement, what an empty value means, which other attribute it
   conflicts with.
2. **There is an example.** Add `examples/resources/<name>/resource.tf` (or
   `examples/data-sources/<name>/data-source.tf`) and reference it from the
   template. Every `.tf` under `examples/` is type-checked against the provider
   built from your branch, so an example that no longer compiles fails CI.
3. **Importable resources ship an `import.sh`** showing the ID format, and the
   page has an `## Import` section. Add the ID to the table in the
   [importing guide](docs/guides/importing.md).
4. **There is a template** at `templates/resources/<name>.md.tmpl`, and
   `make docs` has been run and the regenerated `docs/` committed.

Locally:

```shell
make docs               # regenerate docs/ from templates + schema
make docs-check         # fail if the committed tree is stale, then validate it
make examples-validate  # type-check every example against this provider
```

Edit `templates/`, never `docs/` — `docs/` is generated output and any hand-edit
is reverted by the next `make docs` (and caught by `make docs-check` before
that).

### What runs on your PR

Every pull request — **including PRs from forks** — runs, on GitHub-hosted
runners with no secrets:

- the `check` job (unit tests, lint, coverage),
- the `docs` job (documentation is current, examples type-check), and
- `acceptance_mock`: the credential-free mock acceptance suite (`make
  testacc-mock`) across a Terraform + OpenTofu version matrix.

The live-API sandbox suite (`acceptance_test`, on the self-hosted EC2 runner) is
**push-only** — it runs on branch pushes within this repository, not on
`pull_request` events, because it needs secrets that GitHub does not expose to
fork PRs. So for a fork contribution, the mock suite is the acceptance signal;
a maintainer validates against the live sandbox before merge.

Since #279, the self-hosted EC2 runner behind `acceptance_test` is kept in a
warm pool: it's stopped (not terminated) after each push and reused by the
next one, so most pushes warm-start in seconds instead of paying the cold-boot
tax every time. A full ~2-4 minute cold boot still happens on the first push
after the nightly drain, or once the runner's 360-minute/20-cycle warm-reuse
limit is hit. This is only possible because the sandbox pipeline is
push-only and never runs fork/PR code, as above — that's what makes it safe
for one job's disk to carry over to the next; fork PRs keep going through
`acceptance_mock`, unchanged either way. Per-job disk (the checked-out
workspace, `$HOME`, dotfiles) is wiped both before and after every run — only
the Go/Terraform toolchain and build caches persist across a stop/start
cycle. To force a clean cold boot (e.g. while debugging a toolchain issue),
trigger [`cleanup-ec2-runners.yml`](.github/workflows/cleanup-ec2-runners.yml)
via `workflow_dispatch` with `mode: nightly-drain`; leave `dry_run: true`
(the default) to preview what it would do first.

Definition of done for a change to provider behavior: unit tests **and** a
mock-backed `TestAccMock*` case cover it, and both pass locally
(`make test && make testacc-mock`).

### Dependabot PRs (maintainers)

Dependabot-triggered workflow runs don't have access to repository secrets — GitHub redacts `secrets.*` for `dependabot[bot]` by design, so any job that reaches AWS, the self-hosted EC2 runner, or the Namecheap sandbox credentials cannot complete. For that reason the `start-runner`, `acceptance_test`, and `stop-runner` jobs are gated with `if: ${{ github.event_name == 'push' && github.actor != 'dependabot[bot]' }}` and will show as **skipped** (not failed) on Dependabot PRs. The `check` job and the credential-free `acceptance_mock` job still run and must pass.

Before merging a Dependabot PR, a maintainer must trigger acceptance tests manually under their own identity. Secrets resolve for maintainer-initiated runs, so the full pipeline executes. Use the exact branch name shown on the PR — Dependabot prefixes branches with the ecosystem (`go_modules`, `github_actions`, etc.), and the package segment that follows varies per update:

```shell
# copy the branch from the PR page, e.g. dependabot/go_modules/github.com/hashicorp/go-cty-1.5.0
gh workflow run CI --ref <dependabot-branch-from-the-PR>
# or: Actions tab → CI → Run workflow → select the Dependabot branch
```

Once the manual run is green, merge as usual. If the dependency touches code paths exercised only by acceptance tests, do not merge on the `check` job alone.

## Release

We'll publish a new tagged release once significant changes have accumulated. A new version will be available on the registry
within a few minutes after tagging release. If you're expecting to get a new release with mandatory fixes for you, feel
free to contact us.
