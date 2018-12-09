#!/usr/bin/env bash
make build
mv /Users/ogp/go-workspace/bin/terraform-provider-namecheap ~/.terraform.d/plugins/terraform-provider-namecheap
chmod +x ~/.terraform.d/plugins/terraform-provider-namecheap
