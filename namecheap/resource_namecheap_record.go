package namecheap

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adamdecaf/namecheap"
	"github.com/hashicorp/terraform/helper/schema"
)

// We need a mutex here because of the underlying api
var mutex = &sync.Mutex{}

// This is the "Auto" TTL setting in Namecheap
const ncDefaultTTL int = 1799
const ncDefaultMXPref int = 10
const ncDefaultTimeout time.Duration = 30
const ncMaxThrottleRetry int = 60
const ncBackoffMultiplier int = 2

func resourceNameCheapRecord() *schema.Resource {
	return &schema.Resource{
		Create: resourceNameCheapRecordCreate,
		Update: resourceNameCheapRecordUpdate,
		Read:   resourceNameCheapRecordRead,
		Delete: resourceNameCheapRecordDelete,

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

	apiThrottleBackoffTime := 2

	for {
		_, err := client.AddRecord(d.Get("domain").(string), &record)
		if err != nil {
			log.Printf("[INFO] Err: %v", err.Error())

			if strings.Contains(err.Error(), "expected element type <ApiResponse> but have <html>") {
				log.Printf("[WARN] Bad Namecheap API response received, backing off for %d seconds...",
					apiThrottleBackoffTime)
				time.Sleep(time.Duration(apiThrottleBackoffTime) * time.Second)
				apiThrottleBackoffTime = apiThrottleBackoffTime * ncBackoffMultiplier
				if apiThrottleBackoffTime > ncMaxThrottleRetry {
					log.Printf("[ERROR] API Retry Limit Reached. Couldn't find namecheap record: %v", err)
					break
				}
				continue
			}
			return fmt.Errorf("Failed to create namecheap Record: %s", err)
		}
		hashId := client.CreateHash(&record)
		d.SetId(strconv.Itoa(hashId))
		break
	}

	mutex.Unlock()
	return resourceNameCheapRecordRead(d, meta)
}

func resourceNameCheapRecordUpdate(d *schema.ResourceData, meta interface{}) error {
	mutex.Lock()

	client := meta.(*namecheap.Client)
	domain := d.Get("domain").(string)
	hashId, err := strconv.Atoi(d.Id())
	if err != nil {
		mutex.Unlock()
		return fmt.Errorf("Failed to parse id: %s", err)
	}
	record := namecheap.Record{
		Name:       d.Get("name").(string),
		RecordType: d.Get("type").(string),
		Address:    d.Get("address").(string),
		MXPref:     d.Get("mx_pref").(int),
		TTL:        d.Get("ttl").(int),
	}

	apiThrottleBackoffTime := 2

	for {
		err = client.UpdateRecord(domain, hashId, &record)
		if err != nil {
			log.Printf("[INFO] Err: %v", err.Error())

			if strings.Contains(err.Error(), "expected element type <ApiResponse> but have <html>") {
				log.Printf("[WARN] Bad Namecheap API response received, backing off for %v seconds...",
					apiThrottleBackoffTime)
				time.Sleep(time.Duration(apiThrottleBackoffTime) * time.Second)
				apiThrottleBackoffTime = apiThrottleBackoffTime * ncBackoffMultiplier
				if apiThrottleBackoffTime > ncMaxThrottleRetry {
					log.Printf("[ERROR] API Retry Limit Reached. Couldn't find namecheap record: %v", err)
					break
				}
				continue
			}
			log.Printf("[ERROR] Failed to update namecheap record: %s", err)
			break
		}
		newHashId := client.CreateHash(&record)
		d.SetId(strconv.Itoa(newHashId))
		break
	}

	mutex.Unlock()
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

	apiThrottleBackoffTime := 2

	for {
		record, err := client.ReadRecord(domain, hashId)
		if err != nil {
			log.Printf("[TRACE] Record: %v", record)
			log.Printf("[INFO] Err: %v", err.Error())
			if strings.Contains(err.Error(), "expected element type <ApiResponse> but have <html>") {
				log.Printf("[WARN] Bad Namecheap API response received, backing off for %v seconds...",
					apiThrottleBackoffTime)
				time.Sleep(time.Duration(apiThrottleBackoffTime) * time.Second)
				apiThrottleBackoffTime = apiThrottleBackoffTime * ncBackoffMultiplier
				if apiThrottleBackoffTime > ncMaxThrottleRetry {
					log.Printf("[ERROR] API Retry Limit Reached. Couldn't find namecheap record: %v", err)
					break
				}
				continue
			}
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
		break
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

	apiThrottleBackoffTime := 2

	for {
		err = client.DeleteRecord(domain, hashId)
		if err != nil {
			log.Printf("[TRACE] Err: %v", err.Error())

			if strings.Contains(err.Error(), "expected element type <ApiResponse> but have <html>") {
				log.Printf("[WARN] Bad Namecheap API response received, backing off for %d seconds...",
					apiThrottleBackoffTime)
				time.Sleep(time.Duration(apiThrottleBackoffTime) * time.Second)
				apiThrottleBackoffTime = apiThrottleBackoffTime * ncBackoffMultiplier
				if apiThrottleBackoffTime > ncMaxThrottleRetry {
					log.Printf("[ERROR] API Retry Limit Reached. Bailing: %v", err)
					break
				}
				continue
			}
			return fmt.Errorf("Failed to delete namecheap record: %s", err)
		}
		break
	}

	return nil
}
