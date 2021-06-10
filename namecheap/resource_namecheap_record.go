package namecheap

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk"
)

// We need a mutex here because of the underlying api
var mutex = &sync.Mutex{}

func resourceNameCheapRecord() *schema.Resource {
	return &schema.Resource{
		Create: resourceNameCheapRecordCreate,
		Update: resourceNameCheapRecordUpdate,
		Read:   resourceNameCheapRecordRead,
		Delete: resourceNameCheapRecordDelete,

		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(ncDefaultTimeout * time.Second),
			Update: schema.DefaultTimeout(ncDefaultTimeout * time.Second),
			Read:   schema.DefaultTimeout(ncDefaultTimeout * time.Second),
			Delete: schema.DefaultTimeout(ncDefaultTimeout * time.Second),
		},

		Schema: map[string]*schema.Schema{
			"domain": {
				Type:     schema.TypeString,
				Required: true,
			},
			"name": {
				Type:     schema.TypeString,
				Required: true,
			},
			"type": {
				Type:     schema.TypeString,
				Required: true,
			},
			"address": {
				Type:     schema.TypeString,
				Required: true,
			},
			"mx_pref": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  ncDefaultMXPref,
			},
			"ttl": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  ncDefaultTTL,
			},
			"hostname": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceNameCheapRecordCreate(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()

	client := meta.(*namecheap.Client)
	record := namecheap.Record{
		Name:       d.Get("name").(string),
		RecordType: d.Get("type").(string),
		Address:    d.Get("address").(string),
		MXPref:     d.Get("mx_pref").(int),
		TTL:        d.Get("ttl").(int),
	}

	err := retryAPICall(func() error {
		_, err := client.AddRecord(d.Get("domain").(string), &record)
		return err
	})
	if err != nil {
		return err
	}

	hashID := client.CreateHash(&record)
	d.SetId(strconv.Itoa(hashID))

	mutex.Unlock()
	return resourceNameCheapRecordRead(d, meta)
}

func resourceNameCheapRecordUpdate(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	hashID, err := strconv.Atoi(d.Id())
	if err != nil {
		mutex.Unlock()
		return fmt.Errorf("Failed to parse id=%q: %s", d.Id(), err)
	}
	record := namecheap.Record{
		Name:       d.Get("name").(string),
		RecordType: d.Get("type").(string),
		Address:    d.Get("address").(string),
		MXPref:     d.Get("mx_pref").(int),
		TTL:        d.Get("ttl").(int),
	}

	err = retryAPICall(func() error {
		return client.UpdateRecord(domain, hashID, &record)
	})
	if err != nil {
		return err
	}

	newHashID := client.CreateHash(&record)
	d.SetId(strconv.Itoa(newHashID))

	mutex.Unlock()
	return resourceNameCheapRecordRead(d, meta)
}

func resourceNameCheapRecordRead(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	defer mutex.Unlock()

	var record *namecheap.Record

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	hashID, err := strconv.Atoi(d.Id())
	if err != nil {
		// Attempt to import, assume d.Id is in the following formats:
		// - '@.domain.tld/A/127.0.0.1'
		// - 'test.domain.tld/A/127.0.0.1'
		parts := strings.Split(d.Id(), "/")
		if len(parts) != 3 {
			return fmt.Errorf("read: failed to parse %q as import ID", d.Id())
		}
		domainParts := strings.Split(parts[0], ".")
		if len(domainParts) < 2 {
			return fmt.Errorf("read: invalid domain %q to import", parts[0])
		}

		// Get 'domain.tld' hosts
		records, err := client.GetHosts(domainParts[len(domainParts)-2] + "." + domainParts[len(domainParts)-1])
		if err != nil {
			return fmt.Errorf("read: failed to GetHosts domain=%q: %s", parts[0], err)
		}
		hash := client.CreateHash(&namecheap.Record{
			Name:       domainParts[0], // '@' or 'tld'
			RecordType: parts[1],
			Address:    parts[2],
		})

		rec, err := client.FindRecordByHash(hash, records)

		if err != nil {
			return fmt.Errorf("read: problem finding record for %s/%s/%s: %v", parts[0], parts[1], parts[2], err)
		}

		// Mutate global state and set 'id' to our computed hash
		record = rec
		d.SetId(fmt.Sprintf("%d", hash))
		_ = d.Set("domain", domainParts[len(domainParts)-2]+"."+domainParts[len(domainParts)-1])
		_ = d.Set("hostname", parts[0])
	}

	err = retryAPICall(func() error {
		if record != nil {
			return nil // already found via 'terraform import'
		}

		rec, err := client.ReadRecord(domain, hashID)
		if err == nil {
			record = rec
		}
		return err
	})
	if err != nil {
		return err
	}

	_ = d.Set("name", record.Name)
	_ = d.Set("type", record.RecordType)
	_ = d.Set("address", record.Address)
	_ = d.Set("mx_pref", record.MXPref)
	_ = d.Set("ttl", record.TTL)

	if record.Name == "" {
		_ = d.Set("hostname", d.Get("domain").(string))
	} else {
		_ = d.Set("hostname", fmt.Sprintf("%s.%s", record.Name, d.Get("domain").(string)))
	}

	return nil
}

func resourceNameCheapRecordDelete(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()
	defer mutex.Unlock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	hashID, err := strconv.Atoi(d.Id())
	if err != nil {
		return fmt.Errorf("Failed to parse id=%q: %s", d.Id(), err)
	}

	return retryAPICall(func() error {
		return client.DeleteRecord(domain, hashID)
	})
}
