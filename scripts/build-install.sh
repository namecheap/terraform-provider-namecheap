#!/usr/bin/env bash
make build
mv $GOPATH/bin/terraform-provider-namecheap ~/.terraform.d/plugins/terraform-provider-namecheap
chmod +x ~/.terraform.d/plugins/terraform-provider-namecheap
