## DCO Sign-off

All git commits must include a `Signed-off-by` line for the [Developer Certificate of Origin](https://developercertificate.org/) (DCO) check to pass. The DCO bot on GitHub will block PRs that contain unsigned commits.

- Use `git commit --signoff` (or `-s`) to add the sign-off automatically.
- To sign off an entire branch retroactively: `git rebase HEAD~N --signoff` (replace N with the number of commits).
- The `Signed-off-by` identity must match the commit's author or committer name and email.

## Pull Requests

- All CI checks must pass before merge (unit tests, acceptance tests, CodeQL, DCO).
- PRs should include both unit tests and Terraform acceptance tests where applicable.
- Acceptance tests use `resource.Test()` with `TestStep` — see `namecheap/provider_test.go` for examples.
