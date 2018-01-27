# namecheap

Golang library for interacting with Namecheap's API. [GoDoc](https://godoc.org/github.com/adamdecaf/namecheap)

### Getting

```
$ go get github.com/adamdecaf/namecheap
```

### Usage

Generally callers would create a `namecheap.Client` and make calls off of that.

```go
import (
    "github.com/adamdecaf/namecheap"
)

// Reads environment variables
client, err := namecheap.New()

// Directly build client
client, err := namecheap.NewClient(username, apiuser string, token string, ip string, useSandbox)
```

Calling `namecheap.New()` reads the following environment variables:

- `NAMECHEAP_USERNAME`: Username: e.g. adamdecaf
- `NAMECHEAP_API_USER`: ApiUser: e.g. adamdecaf
- `NAMECHEAP_TOKEN`: From https://ap.www.namecheap.com/Profile/Tools/ApiAccess
- `NAMECHEAP_IP`: Your IP (must be whitelisted)
- `NAMECHEAP_USE_SANDBOX`: Use sandbox environment

### Contributing

I appreciate feedback, issues and Pull Requests. You can build the project with `make build` in the root and run tests with `make test`.

If you're looking to run tests yourself you can configure the environmental variables and override the test records in `client_test.go`. (To make live api calls) Otherwise only mockable tests will run.

The following are contributor oriented environmental variables:

- `DEBUG`: Log all responses
- `MOCKED`: Force disable `testClient`
