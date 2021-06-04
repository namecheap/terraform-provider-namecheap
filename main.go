package main

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	"github.com/namecheap/terraform-provider-namecheap/namecheap"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: namecheap.Provider,
	})
}
