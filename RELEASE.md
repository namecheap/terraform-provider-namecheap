# Release Process

This project uses a **semi-automated, maintainer-gated** release flow built on
[release-please](https://github.com/googleapis/release-please). Changes merged
to `master` do not ship immediately — they accumulate in a long-lived "Release
PR", and a binary is published only when a maintainer merges that PR.

Three workflows participate:

| Workflow | Trigger | Role |
|---|---|---|
| [`ci.yml`](.github/workflows/ci.yml) | PRs, push to any branch | Lint, vet, unit tests, acceptance tests |
| [`versioning.yml`](.github/workflows/versioning.yml) | `ci.yml` success on `master` | Runs release-please to open/update the Release PR |
| [`release.yml`](.github/workflows/release.yml) | push of a `v*` tag | GoReleaser build + GPG sign + SBOM + attestation |

[`pr-title.yml`](.github/workflows/pr-title.yml) enforces
[Conventional Commits](https://www.conventionalcommits.org/) on PR titles, which
release-please parses to compute the version bump.

## Flow

1. **Merge PRs to `master`.** PRs are squash-merged, so the (conventional) PR
   title becomes the commit subject release-please classifies. Nothing is
   released yet.
2. **release-please maintains the Release PR.** After CI passes on `master`,
   `versioning.yml` runs release-please, which computes the next SemVer bump
   (`fix:` → patch, `feat:` → minor, `feat!:`/`BREAKING CHANGE:` → major),
   updates `CHANGELOG.md` and `.release-please-manifest.json`, and opens or
   updates a PR titled `chore(master): release X.Y.Z`.
3. **Maintainer merges the Release PR** when the accumulated changes are worth
   shipping. This is the only manual gate. Merging tags the commit `vX.Y.Z`.
4. **`release.yml` publishes** the binaries for the new tag, and the Terraform
   Registry ingests the GitHub Release automatically (usually within minutes).

> release-please authenticates with a GitHub App (`vars.APP_CLIENT_ID` /
> `secrets.APP_PRIVATE_KEY`, the same App used by `ci.yml`), not the default
> `GITHUB_TOKEN` — only App-authored events re-trigger `release.yml`. The App
> must grant `contents: write` and `pull-requests: write`.

## Manual / emergency release

If release-please is unavailable, bump `.release-please-manifest.json` and
`CHANGELOG.md`, commit to `master`, then tag and push:
`git tag -a vX.Y.Z -m "Release vX.Y.Z" && git push origin vX.Y.Z`. `release.yml`
runs on the tag; the next release-please run reconciles its state.
