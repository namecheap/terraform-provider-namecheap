Namecheap Terraform Provider
==================

Usage
---------------------

```
# Download provider
# Terraform Docs: https://www.terraform.io/docs/configuration/providers.html#third-party-plugins

$ mkdir -p ~/.terraform.d/plugins/
$ wget -O ~/.terraform.d/plugins/terraform-provider-namecheap https://github.com/adamdecaf/terraform-provider-namecheap/releases/download/v1.1.1/terraform-provider-namecheap-linux-amd64
```

Then inside a file (e.g. `example.com.tf`):

```hcl
provider "namecheap" {}

resource "namecheap_record" "www-example-com" {
  name = "www"
  domain = "example.com"
  address = "127.0.0.1"
  mx_pref = 10
  type = "A"
}
```

Setup terraform and view the plan output.

```hcl
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

Clone repository to: `$GOPATH/src/github.com/terraform-providers/terraform-provider-$PROVIDER_NAME`

```sh
$ mkdir -p $GOPATH/src/github.com/terraform-providers; cd $GOPATH/src/github.com/terraform-providers
$ git clone git@github.com:terraform-providers/terraform-provider-$PROVIDER_NAME
```

Enter the provider directory and build the provider

```sh
$ cd $GOPATH/src/github.com/terraform-providers/terraform-provider-$PROVIDER_NAME
$ make build
```

Developing the Provider
---------------------------

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (version 1.11+ is *required*). You'll also need to correctly setup a [GOPATH](http://golang.org/doc/code.html#GOPATH), as well as adding `$GOPATH/bin` to your `$PATH`.

This project uses [Go Modules](https://github.com/golang/go/wiki/Modules), added in Go 1.11.

To compile the provider, run `make build`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

```sh
$ make bin
...
$ $GOPATH/bin/terraform-provider-$PROVIDER_NAME
...
```

In order to test the provider, you can simply run `make test`.

```sh
$ make test
```

In order to run the full suite of Acceptance tests, run `make testacc`.

*Note:* Acceptance tests create real resources, and often cost money to run.

```sh
$ make testacc
```
