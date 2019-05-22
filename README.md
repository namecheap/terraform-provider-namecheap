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

First you'll need to manually install this Terraform Provider for now until we get this into the official providers.

Note the command below will install the Linux binary, please check [releases](https://github.com/adamdecaf/terraform-provider-namecheap/releases) page for Windows and Mac builds.

| Terraform Version | terraform-provider-namecheap Version |
|----|----|
| 0.11 | [1.2.0](https://github.com/adamdecaf/terraform-provider-namecheap/releases/tag/1.2.0) |
| 0.12 | [1.3.0](https://github.com/adamdecaf/terraform-provider-namecheap/releases/tag/1.3.0) |

## Linux

```bash
# Download provider
# Terraform Docs: https://www.terraform.io/docs/configuration/providers.html#third-party-plugins

$ mkdir -p ~/.terraform.d/plugins/
$ wget -O ~/.terraform.d/plugins/terraform-provider-namecheap https://github.com/adamdecaf/terraform-provider-namecheap/releases/download/1.2.0/terraform-provider-namecheap-linux-amd64
```

## Mac

```bash
$ mkdir -p ~/.terraform.d/plugins/
$ curl https://github.com/adamdecaf/terraform-provider-namecheap/releases/download/1.2.0/terraform-provider-namecheap-osx-amd64 > ~/.terraform.d/plugins/terraform-provider-namecheap
$ chmod +x ~/.terraform.d/plugins/terraform-provider-namecheap
```

Then inside a Terraform file within your project (Ex. `providers.tf`):

```hcl
# For example, restrict namecheap version to 1.2.0
provider "namecheap" {
  version = "~> 1.2"
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

Clone repository to: `$GOPATH/src/github.com/adamdecaf/terraform-provider-namecheap`

```bash
$ mkdir -p $GOPATH/src/github.com/adamdecaf ; cd $GOPATH/src/github.com/adamdecaf
$ git clone git@github.com:adamdecaf/terraform-provider-namecheap
```

Enter the provider directory and build the provider

```bash
$ cd $GOPATH/src/github.com/adamdecaf/terraform-provider-namecheap
$ make build
```

Using the provider
----------------------

Make sure your API details are correct in the provider block.

```hcl
provider "namecheap" {
  username = "your_username"
  api_user = "your_username" # Same as username
  token = "your_token"
  ip = "your.ip.address.here"
  use_sandbox = false # Toggle for testing/sandbox mode
}
```

Developing the Provider
---------------------------

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (version 1.11+ is *required*). You'll also need to correctly setup a [GOPATH](http://golang.org/doc/code.html#GOPATH), as well as adding `$GOPATH/bin` to your `$PATH`.

This project uses [Go Modules](https://github.com/golang/go/wiki/Modules), added in Go 1.11.

To compile the provider, run `make build`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

```bash
$ make bin
...
$ $GOPATH/bin/terraform-provider-namecheap
...
```

In order to test the provider, you can simply run `make test`.

```bash
$ make test
```

In order to run the full suite of Acceptance tests, run `make testacc`.

*Note:* Acceptance tests create real resources, and often cost money to run.

```bash
$ make testacc
```

Another good way to test builds is to symlink the binary `terraform-provider-namecheap` that you are building into the `~/.terraform.d/plugins/` directory.
