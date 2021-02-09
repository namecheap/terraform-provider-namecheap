Namecheap Terraform Provider
==================

[![Go Report Card](https://goreportcard.com/badge/github.com/adamdecaf/terraform-provider-namecheap)](https://goreportcard.com/report/github.com/adamdecaf/terraform-provider-namecheap)
[![Support via Flattr](https://button.flattr.com/flattr-badge-large.png)](https://flattr.com/@adamdecaf)

A Terraform Provider for Namecheap domain dns configuration.

Prerequisites
---------------------

First you'll need to apply for API access to Namecheap. You can do that on this [API admin page](https://ap.www.namecheap.com/settings/tools/apiaccess/).

Next, find out your IP address and add that IP (or any other IPs accessing this API) to this [whitelist admin page](https://ap.www.namecheap.com/settings/tools/apiaccess/whitelisted-ips) on Namecheap.

Once you've done that, make note of the API token, your IP address, and your username to fill into our `provider` block.

Usage
---------------------

Then inside a Terraform file within your project (Ex. `providers.tf`):

Using the provider
----------------------

Make sure your API details are correct in the provider block.

```hcl
terraform {
  required_providers {
    namecheap = {
      source  = "robgmills/namecheap"
      version = "1.5.1"
    }
  }
}

provider "namecheap" {
  username = "your_username" # Also set by env variable `NAMECHEAP_USERNAME`
  api_user = "your_username" # Same as username; also set by env variable `NAMECHEAP_API_USER`
  token = "your_token" # Also set by env variable `NAMECHEAP_TOKEN`
  ip = "your.ip.address.here" # Also set by env variable `NAMECHEAP_IP`
  use_sandbox = false # Toggle for testing/sandbox mode; Also set by env variable `NAMECHEAP_USE_SANDBOX`
}

# Create a DNS A Record for a domain you own
resource "namecheap_record" "www-example-com" {
  name = "www"
  domain = "example.com"
  address = "127.0.0.1"
  mx_pref = 10
  type = "A"
}
```

Setup terraform and view the plan output.

```bash
$ terraform init
Terraform has been successfully initialized!

$ terraform plan
Terraform will perform the following actions:

  + namecheap_record.www-example.com
      id:       <computed>
      address:  "127.0.0.1"
      domain:   "example.com"
      hostname: <computed>
      mx_pref:  "10"
      name:     "www"
      ttl:      "60"
      type:     "A"


Plan: 1 to add, 0 to change, 0 to destroy.

$ terraform apply
...
Apply complete! Resources: 1 added, 0 changed, 0 destroyed.
```

Building The Provider
---------------------

```bash
$ go get github.com/adamdecaf/terraform-provider-namecheap
$ cd $GOPATH/src/github.com/adamdecaf/terraform-provider-namecheap
$ make build
```

Developing the Provider
---------------------------

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (version 1.15+ is recommended). You'll also need to correctly setup a [GOPATH](http://golang.org/doc/code.html#GOPATH), as well as adding `$GOPATH/bin` to your `$PATH`.

This project uses [Go Modules](https://github.com/golang/go/wiki/Modules), added in Go 1.11.

To compile the provider, run `make build`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

```bash
$ make build
$ ls $GOPATH/bin/terraform-provider-namecheap
...
```

In order to test the provider, you can simply run `make test`.

```bash
$ make test
```

In order to run the full suite of Acceptance tests, run `make testacc`.

*Note:* Acceptance tests create real resources, and often cost money to run.  They are also dependent on environment variables to configure the test instance of the provider.

```bash
$ make testacc
```

To contribute changes, please open a PR by forking the repository, adding the fork to your local copy of the git repository, create a branch, commit your changes, and open a PR:

```bash
$ git remote add fork git@github.com/youruser/terraform-provider-namechep
$ git checkout -b your-new-feature
$ git add .
$ git commit -m "Add a new feature"
$ git push -u fork your-new-feature
...
```

Troubleshooting the Provider
---------------------------

Problem: `Error: Failed to create namecheap Record: Could not find the record with hash`
Solution: Double check your IP did not change and make sure it is whitelisted with Namecheaps API. Also ensure the domain names you have in your terraform config are still associated with your account (in cases like where you let one expire). In these rare edge-cases, you may have to delete the bad domain records by running `terraform state rm namecheap_record.the_tf_name_of_your_record`.
