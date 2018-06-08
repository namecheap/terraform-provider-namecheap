package main

import (
	"fmt"
	"strings"

	"github.com/adamdecaf/namecheap"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceNameCheapNS() *schema.Resource {
	return &schema.Resource{
		Create: resourceNameCheapNSCreate,
		Update: resourceNameCheapNSUpdate,
		Read:   resourceNameCheapNSRead,
		Delete: resourceNameCheapNSDelete,

		Schema: map[string]*schema.Schema{
			"domain": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"servers": &schema.Schema{
				Type:     schema.TypeList,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
		},
	}
}

func resourceNameCheapNSCreate(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	var servers []string
	for _, server := range d.Get("servers").([]interface{}) {
		servers = append(servers, server.(string))
	}

	_, err := client.SetNS(domain, servers)
	if err != nil {
		mutex.Unlock()
		return fmt.Errorf("Failed to set NS: %s", err)
	}
	d.SetId(strings.Join(servers, ","))
	mutex.Unlock()
	return resourceNameCheapNSRead(d, meta)
}

func resourceNameCheapNSUpdate(d *schema.ResourceData, meta interface{}) error {
	return resourceNameCheapNSCreate(d, meta)
}

func resourceNameCheapNSRead(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)

	servers, err := client.GetNS(domain)
	if err != nil {
		d.SetId("")
		mutex.Unlock()
		return fmt.Errorf("Failed to read servers: %s", err)
	}
	d.Set("servers", servers)
	mutex.Unlock()
	return nil
}

func resourceNameCheapNSDelete(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)

	err := client.ResetNS(domain)
	if err != nil {
		mutex.Unlock()
		return fmt.Errorf("Failed to reset ns: %s", err)
	}
	mutex.Unlock()
	return nil
}
