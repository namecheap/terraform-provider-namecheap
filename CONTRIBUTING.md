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

## Release

We'll publish a new tagged release once significant changes accumulated. A new version will be available on the registry
within a few minutes after tagging release. If you're expecting to get a new release with mandatory fixes for you, feel
free to contact us.
