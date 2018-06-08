package namecheap

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/adamdecaf/namecheap"
	"github.com/hashicorp/terraform/helper/schema"
)

// We need a mutex here because of the underlying api
var mutex = &sync.Mutex{}

const ncDefaultTTL int = 1800
const ncDefaultMXPref int = 10

func resourceNameCheapRecord() *schema.Resource {
	return &schema.Resource{
		Create: resourceNameCheapRecordCreate,
		Update: resourceNameCheapRecordUpdate,
		Read:   resourceNameCheapRecordRead,
		Delete: resourceNameCheapRecordDelete,

		Schema: map[string]*schema.Schema{
			"domain": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"type": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"address": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"mx_pref": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  10,
			},
			"ttl": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  1800,
			},
			"hostname": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceNameCheapRecordCreate(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	defer mutex.Unlock()

	client := meta.(*namecheap.Client)
	record := namecheap.Record{
		Name:       d.Get("name").(string),
		RecordType: d.Get("type").(string),
		Address:    d.Get("address").(string),
		MXPref:     d.Get("mx_pref").(int),
		TTL:        d.Get("ttl").(int),
	}

	_, err := client.AddRecord(d.Get("domain").(string), &record)

	if err != nil {
		return fmt.Errorf("Failed to create namecheap Record: %s", err)
	}
	hashId := client.CreateHash(&record)
	d.SetId(strconv.Itoa(hashId))

	return resourceNameCheapRecordRead(d, meta)
}

func resourceNameCheapRecordUpdate(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	mutex.Unlock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	hashId, err := strconv.Atoi(d.Id())
	if err != nil {
		return fmt.Errorf("Failed to parse id: %s", err)
	}
	record := namecheap.Record{
		Name:       d.Get("name").(string),
		RecordType: d.Get("type").(string),
		Address:    d.Get("address").(string),
		MXPref:     d.Get("mx_pref").(int),
		TTL:        d.Get("ttl").(int),
	}
	err = client.UpdateRecord(domain, hashId, &record)
	if err != nil {
		return fmt.Errorf("Failed to update namecheap record: %s", err)
	}
	newHashId := client.CreateHash(&record)
	d.SetId(strconv.Itoa(newHashId))
	return resourceNameCheapRecordRead(d, meta)
}

func resourceNameCheapRecordRead(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	defer mutex.Unlock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	hashId, err := strconv.Atoi(d.Id())
	if err != nil {
		return fmt.Errorf("Failed to parse id: %s", err)
	}

	record, err := client.ReadRecord(domain, hashId)
	if err != nil {
		return fmt.Errorf("Couldn't find namecheap record: %s", err)
	}
	d.Set("name", record.Name)
	d.Set("type", record.RecordType)
	d.Set("address", record.Address)
	d.Set("mx_pref", record.MXPref)
	d.Set("ttl", record.TTL)

	if record.Name == "" {
		d.Set("hostname", d.Get("domain").(string))
	} else {
		d.Set("hostname", fmt.Sprintf("%s.%s", record.Name, d.Get("domain").(string)))
	}

	return nil
}

func resourceNameCheapRecordDelete(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	defer mutex.Unlock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	hashId, err := strconv.Atoi(d.Id())
	if err != nil {
		return fmt.Errorf("Failed to parse id: %s", err)
	}
	err = client.DeleteRecord(domain, hashId)

	if err != nil {
		return fmt.Errorf("Failed to delete namecheap record: %s", err)
	}

	return nil
}
