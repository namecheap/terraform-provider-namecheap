package main

import (
	//	"github.com/adamdecaf/terraform-provider-namecheap/namecheap"
	"github.com/hashicorp/terraform/plugin"
	"github.com/terraform-providers/terraform-provider-namecheap/namecheap"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: namecheap.Provider})
}
