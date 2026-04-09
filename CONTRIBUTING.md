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
NAMECHEAP_CLIENT_IP=your.whitelisted.ip
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
- Include both unit tests and [Terraform acceptance tests](https://developer.hashicorp.com/terraform/plugin/sdkv2/testing/acceptance-tests)
  where applicable. Acceptance tests should use `resource.Test()` with `TestStep`.
- Keep PRs focused — one logical change per PR.

## Release

We'll publish a new tagged release once significant changes have accumulated. A new version will be available on the registry
within a few minutes after tagging release. If you're expecting to get a new release with mandatory fixes for you, feel
free to contact us.
