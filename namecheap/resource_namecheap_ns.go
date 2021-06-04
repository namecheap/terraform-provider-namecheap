package namecheap

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk"
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

	err := retryAPICall(func() error {
		_, err := client.SetNS(domain, servers)
		return err
	})
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
	defer mutex.Unlock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)

	var servers []string
	err := retryAPICall(func() error {
		svcs, err := client.GetNS(domain)
		if err != nil {
			return err
		}
		servers = svcs
		return nil
	})
	if err != nil {
		d.SetId("")
		return fmt.Errorf("Failed to read servers: %s", err)
	}
	d.Set("servers", servers)
	return nil
}

func resourceNameCheapNSDelete(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	defer mutex.Unlock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)

	err := retryAPICall(func() error {
		return client.ResetNS(domain)
	})
	if err != nil {
		return fmt.Errorf("Failed to reset ns: %s", err)
	}
	return nil
}
