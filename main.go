package main

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	namecheap "github.com/namecheap/terraform-provider-namecheap/namecheap"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: namecheap.Provider,
	})
}
