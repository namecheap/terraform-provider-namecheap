package namecheap_provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"strings"
)

const (
	ncModeMerge     = "MERGE"
	ncModeOverwrite = "OVERWRITE"

	ncEmailTypeNONE = "NONE"
	ncEmailTypeFWD  = "FWD"
	ncEmailTypeMXE  = "MXE"
	ncEmailTypeMX   = "MX"
	ncEmailTypeOX   = "OX"
)

func resourceNamecheapDomainRecords() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceRecordCreate,
		UpdateContext: resourceRecordUpdate,
		ReadContext:   resourceRecordRead,
		DeleteContext: resourceRecordDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
				Description:  "Purchased available domain name on your account",
			},
			"email_type": {
				ConflictsWith: []string{"nameservers"},
				Type:          schema.TypeString,
				Optional:      true,
				ValidateFunc:  validation.StringInSlice([]string{ncEmailTypeNONE, ncEmailTypeFWD, ncEmailTypeMXE, ncEmailTypeMX, ncEmailTypeOX}, false),
				Description:   fmt.Sprintf("Possible values: %s, %s, %s, %s, %s", ncEmailTypeNONE, ncEmailTypeFWD, ncEmailTypeMXE, ncEmailTypeMX, ncEmailTypeOX),
			},
			"mode": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      ncModeMerge,
				ValidateFunc: validation.StringInSlice([]string{ncModeMerge, ncModeOverwrite}, true),
				Description:  fmt.Sprintf("Possible values: %s (default), %s", ncModeMerge, ncModeOverwrite),
			},
			"record": {
				ConflictsWith: []string{"nameservers"},
				Type:          schema.TypeSet,
				Optional:      true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"hostname": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Sub-domain/hostname to create the record for",
						},
						"type": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice([]string{"A", "AAAA", "ALIAS", "CAA", "CNAME", "MX", "MXE", "NS", "TXT", "URL", "URL301", "FRAME"}, false),
							Description:  "Possible values: A, AAAA, ALIAS, CAA, CNAME, MX, MXE, NS, TXT, URL, URL301, FRAME",
						},
						"address": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Possible values are URL or IP address. The value for this parameter is based on record type",
						},
						"mx_pref": {
							Type:        schema.TypeInt,
							Optional:    true,
							Default:     10,
							Description: "MX preference for host. Applicable for MX records only",
						},
						"ttl": {
							Type:        schema.TypeInt,
							Optional:    true,
							Default:     1799,
							Description: "Time to live for all record types. Possible values: any value between 60 to 60000",
						},
					},
				},
			},
			"nameservers": {
				ConflictsWith: []string{"email_type", "record"},
				Type:          schema.TypeList,
				Optional:      true,
				MinItems:      1,
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					ValidateFunc: validation.StringIsNotEmpty,
				},
			},
		},
	}
}

func resourceRecordCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := data.Get("domain").(string)
	mode := data.Get("mode").(string)
	data.SetId(domain)

	var emailType *string
	var records []interface{}
	var nameservers []interface{}

	if emailTypeRaw, ok := data.GetOk("email_type"); ok {
		emailTypeString := emailTypeRaw.(string)
		emailType = &emailTypeString
	}

	if recordsRaw, ok := data.GetOk("record"); ok {
		records = recordsRaw.(*schema.Set).List()
	}

	if nameserversRaw, ok := data.GetOk("nameservers"); ok {
		nameservers = nameserversRaw.([]interface{})
	}

	if strings.EqualFold(mode, ncModeMerge) && records != nil {
		err := createRecordsMerge(domain, emailType, records, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeOverwrite) && records != nil {
		err := createRecordsOverwrite(domain, emailType, records, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeMerge) && nameservers != nil {
		err := createNameserversMerge(domain, convertInterfacesToString(nameservers), client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeOverwrite) && nameservers != nil {
		err := createNameserversOverwrite(domain, convertInterfacesToString(nameservers), client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	return nil
}

func resourceRecordRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := data.Get("domain").(string)
	mode := data.Get("mode").(string)

	var emailType *string
	var records []interface{}
	var nameservers []interface{}

	if emailTypeRaw, ok := data.GetOk("email_type"); ok {
		emailTypeString := emailTypeRaw.(string)
		emailType = &emailTypeString
	}

	if recordsRaw, ok := data.GetOk("record"); ok {
		records = recordsRaw.(*schema.Set).List()
	}

	if nameserversRaw, ok := data.GetOk("nameservers"); ok {
		nameservers = nameserversRaw.([]interface{})
	}

	// We must read nameservers status before hosts.
	// If you're using custom nameservers, then the reading records process will fail since Namecheap doesn't control
	// the domain behaviour.
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		if strings.EqualFold(mode, ncModeMerge) {
			realNameservers, err := readNameserversMerge(domain, convertInterfacesToString(nameservers), client)
			if err != nil {
				return diag.FromErr(err)
			}
			_ = data.Set("nameservers", *realNameservers)
		}

		if strings.EqualFold(mode, ncModeOverwrite) {
			realNameservers, err := readNameserversOverwrite(domain, client)
			if err != nil {
				return diag.FromErr(err)
			}
			_ = data.Set("nameservers", *realNameservers)
		}
		return nil
	} else {
		if strings.EqualFold(mode, ncModeMerge) {
			realRecords, realEmailType, err := readRecordsMerge(domain, records, client)
			if err != nil {
				return diag.FromErr(err)
			}
			_ = data.Set("record", *realRecords)

			if emailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}
		}

		if strings.EqualFold(mode, ncModeOverwrite) {
			realRecords, realEmailType, err := readRecordsOverwrite(domain, records, client)
			if err != nil {
				return diag.FromErr(err)
			}
			_ = data.Set("record", *realRecords)
			if emailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}
		}
	}

	return nil
}

func resourceRecordUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := data.Get("domain").(string)
	mode := data.Get("mode").(string)

	oldRecordsRaw, newRecordsRaw := data.GetChange("record")
	oldNameserversRaw, newNameserversRaw := data.GetChange("nameservers")

	oldRecords := oldRecordsRaw.(*schema.Set).List()
	newRecords := newRecordsRaw.(*schema.Set).List()

	oldNameservers := oldNameserversRaw.([]interface{})
	newNameservers := newNameserversRaw.([]interface{})

	oldRecordsLen := len(oldRecords)
	newRecordsLen := len(newRecords)

	oldNameserversLen := len(oldNameservers)
	newNameserversLen := len(newNameservers)

	var emailType *string

	if emailTypeRaw, ok := data.GetOk("email_type"); ok {
		emailTypeString := emailTypeRaw.(string)
		emailType = &emailTypeString
	}

	// If the previous state contains nameservers, but the new one does not contain,
	// then reset nameservers before applying records.
	// This case is possible when user removed nameservers lines and pasted records, so before applying records,
	// we must reset nameservers to defaults, otherwise we will face API exception
	if strings.EqualFold(mode, ncModeOverwrite) && oldNameserversLen != 0 && newNameserversLen == 0 {
		_, err := client.DomainsDNS.SetDefault(domain)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeMerge) && oldNameserversLen != 0 && newNameserversLen == 0 {
		err := updateNameserversMerge(domain, convertInterfacesToString(oldNameservers), convertInterfacesToString(newNameservers), client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeMerge) && (newRecordsLen != 0 || oldRecordsLen != 0) {
		err := updateRecordsMerge(domain, emailType, oldRecords, newRecords, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeOverwrite) && (newRecordsLen != 0 || oldRecordsLen != 0) {
		err := createRecordsOverwrite(domain, emailType, newRecords, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeOverwrite) && newNameserversLen != 0 {
		err := createNameserversOverwrite(domain, convertInterfacesToString(newNameservers), client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeMerge) && newNameserversLen != 0 {
		err := updateNameserversMerge(domain, convertInterfacesToString(oldNameservers), convertInterfacesToString(newNameservers), client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	// If user wants to control email type only while records & nameservers are absent,
	// then we have to update just an email status
	if emailType != nil && oldNameserversLen == 0 && newNameserversLen == 0 && oldRecordsLen == 0 && newRecordsLen == 0 {
		if strings.EqualFold(mode, ncModeOverwrite) {
			err := createRecordsOverwrite(domain, emailType, []interface{}{}, client)
			if err != nil {
				return diag.FromErr(err)
			}
		}
		if strings.EqualFold(mode, ncModeMerge) {
			err := createRecordsMerge(domain, emailType, []interface{}{}, client)
			if err != nil {
				return diag.FromErr(err)
			}
		}
	}

	// For overwrite mode, when no nameservers and records, and email type is not set, then we have to reset it to NONE
	if emailType == nil && strings.EqualFold(mode, ncModeOverwrite) && oldNameserversLen == 0 && newNameserversLen == 0 && oldRecordsLen == 0 && newRecordsLen == 0 {
		err := createRecordsOverwrite(domain, nil, []interface{}{}, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	return nil
}

func resourceRecordDelete(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := data.Get("domain").(string)
	mode := data.Get("mode").(string)

	var records []interface{}
	var nameservers []interface{}

	if recordsRaw, ok := data.GetOk("record"); ok {
		records = recordsRaw.(*schema.Set).List()
	}

	if nameserversRaw, ok := data.GetOk("nameservers"); ok {
		nameservers = nameserversRaw.([]interface{})
	}

	recordsLen := len(records)
	nameserversLen := len(nameservers)

	if strings.EqualFold(mode, ncModeMerge) && recordsLen != 0 {
		err := deleteRecordsMerge(domain, records, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeOverwrite) && recordsLen != 0 {
		err := deleteRecordsOverwrite(domain, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeMerge) && nameserversLen != 0 {
		err := deleteNameserversMerge(domain, convertInterfacesToString(nameservers), client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if strings.EqualFold(mode, ncModeOverwrite) && nameserversLen != 0 {
		err := deleteNameserversOverwrite(domain, client)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	return nil
}
