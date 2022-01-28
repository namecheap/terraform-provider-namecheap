package namecheap_provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

const (
	ncModeMerge     = "MERGE"
	ncModeOverwrite = "OVERWRITE"
	ncModeImport    = "IMPORT"
)

func resourceNamecheapDomainRecords() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceRecordCreate,
		UpdateContext: resourceRecordUpdate,
		ReadContext:   resourceRecordRead,
		DeleteContext: resourceRecordDelete,

		Importer: &schema.ResourceImporter{
			StateContext: func(ctx context.Context, data *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
				if err := data.Set("domain", data.Id()); err != nil {
					return nil, err
				}
				if err := data.Set("mode", ncModeImport); err != nil {
					return nil, err
				}

				return []*schema.ResourceData{data}, nil
			},
		},

		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringIsNotEmpty,
				Description:  "Purchased available domain name on your account",
			},
			"email_type": {
				ConflictsWith: []string{"nameservers"},
				Type:          schema.TypeString,
				Optional:      true,
				ValidateFunc:  validation.StringInSlice(namecheap.AllowedEmailTypeValues, false),
				Description:   fmt.Sprintf("Possible values: %s", strings.TrimSpace(strings.Join(namecheap.AllowedEmailTypeValues, ", "))),
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
							ValidateFunc: validation.StringInSlice(namecheap.AllowedRecordTypeValues, false),
							Description:  fmt.Sprintf("Possible values: %s", strings.TrimSpace(strings.Join(namecheap.AllowedRecordTypeValues, ", "))),
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
							Description: fmt.Sprintf("Time to live for all record types. Possible values: any value between %d to %d", namecheap.MinTTL, namecheap.MaxTTL),
						},
					},
				},
			},
			"nameservers": {
				ConflictsWith: []string{"email_type", "record"},
				Type:          schema.TypeSet,
				Optional:      true,
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

	domain := strings.ToLower(data.Get("domain").(string))
	mode := strings.ToUpper(data.Get("mode").(string))

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
		nameservers = nameserversRaw.(*schema.Set).List()
	}

	if mode == ncModeMerge {
		ncMutexKV.Lock(domain)
		defer ncMutexKV.Unlock(domain)
	}

	if mode == ncModeMerge && records != nil {
		diags := createRecordsMerge(domain, emailType, records, client)
		if diags.HasError() {
			return diags
		}
	}

	if mode == ncModeOverwrite && records != nil {
		diags := createRecordsOverwrite(domain, emailType, records, client)
		if diags.HasError() {
			return diags
		}
	}

	if mode == ncModeMerge && nameservers != nil {
		diags := createNameserversMerge(domain, convertInterfacesToString(nameservers), client)
		if diags.HasError() {
			return diags
		}
	}

	if mode == ncModeOverwrite && nameservers != nil {
		diags := createNameserversOverwrite(domain, convertInterfacesToString(nameservers), client)
		if diags.HasError() {
			return diags
		}
	}

	data.SetId(domain)

	return nil
}

func resourceRecordRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	mode := strings.ToUpper(data.Get("mode").(string))

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
		nameservers = nameserversRaw.(*schema.Set).List()
	}

	if mode == ncModeMerge {
		ncMutexKV.Lock(domain)
		defer ncMutexKV.Unlock(domain)
	}

	// We must read nameservers status before hosts.
	// If you're using custom nameservers, then the reading records process will fail since Namecheap doesn't control
	// the domain behaviour.
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		if mode == ncModeMerge {
			realNameservers, diags := readNameserversMerge(domain, convertInterfacesToString(nameservers), client)
			if diags.HasError() {
				return diags
			}
			_ = data.Set("nameservers", *realNameservers)
		}

		if mode == ncModeOverwrite || mode == ncModeImport {
			realNameservers, diags := readNameserversOverwrite(domain, client)
			if diags.HasError() {
				return diags
			}
			_ = data.Set("nameservers", *realNameservers)
		}

		_ = data.Set("record", []interface{}{})
	} else {
		if mode == ncModeMerge {
			realRecords, realEmailType, diags := readRecordsMerge(domain, records, client)
			if diags.HasError() {
				return diags
			}
			_ = data.Set("record", *realRecords)

			if emailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}
		}

		if mode == ncModeOverwrite || mode == ncModeImport {
			realRecords, realEmailType, diags := readRecordsOverwrite(domain, records, client)
			if diags.HasError() {
				return diags
			}
			_ = data.Set("record", *realRecords)
			if emailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}
		}

		if nameservers != nil {
			_ = data.Set("nameservers", []string{})
		}
		if mode == ncModeImport {
			_ = data.Set("mode", ncModeMerge)
		}
	}

	return nil
}

func resourceRecordUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	mode := strings.ToUpper(data.Get("mode").(string))

	oldRecordsRaw, newRecordsRaw := data.GetChange("record")
	oldNameserversRaw, newNameserversRaw := data.GetChange("nameservers")

	oldRecords := oldRecordsRaw.(*schema.Set).List()
	newRecords := newRecordsRaw.(*schema.Set).List()

	oldNameservers := oldNameserversRaw.(*schema.Set).List()
	newNameservers := newNameserversRaw.(*schema.Set).List()

	oldRecordsLen := len(oldRecords)
	newRecordsLen := len(newRecords)

	oldNameserversLen := len(oldNameservers)
	newNameserversLen := len(newNameservers)

	var emailType *string

	if emailTypeRaw, ok := data.GetOk("email_type"); ok {
		emailTypeString := emailTypeRaw.(string)
		emailType = &emailTypeString
	}

	if mode == ncModeMerge {
		ncMutexKV.Lock(domain)
		defer ncMutexKV.Unlock(domain)
	}

	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	// If the previous state contains nameservers, but the new one does not contain,
	// then reset nameservers before applying records.
	// This case is possible when user removed nameservers lines and pasted records, so before applying records,
	// we must reset nameservers to defaults, otherwise we will face API exception
	if (mode == ncModeOverwrite && oldNameserversLen != 0 && newNameserversLen == 0) ||
		// This condition resolves the issue if a user set up records on TF file, but in fact, manually enabled custom DNS.
		// Before applying records, we have to set default DNS
		(!*nsResponse.DomainDNSGetListResult.IsUsingOurDNS && newNameserversLen == 0) {
		_, err := client.DomainsDNS.SetDefault(domain)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if mode == ncModeMerge && oldNameserversLen != 0 && newNameserversLen == 0 {
		diags := updateNameserversMerge(domain, convertInterfacesToString(oldNameservers), convertInterfacesToString(newNameservers), client)
		if diags.HasError() {
			return diags
		}
	}

	if mode == ncModeMerge && (newRecordsLen != 0 || oldRecordsLen != 0) {
		diags := updateRecordsMerge(domain, emailType, oldRecords, newRecords, client)
		if diags.HasError() {
			return diags
		}
	}

	if mode == ncModeOverwrite && (newRecordsLen != 0 || oldRecordsLen != 0) {
		diags := createRecordsOverwrite(domain, emailType, newRecords, client)
		if diags.HasError() {
			return diags
		}
	}

	if mode == ncModeOverwrite && newNameserversLen != 0 {
		diags := createNameserversOverwrite(domain, convertInterfacesToString(newNameservers), client)
		if diags.HasError() {
			return diags
		}
	}

	if mode == ncModeMerge && newNameserversLen != 0 {
		diags := updateNameserversMerge(domain, convertInterfacesToString(oldNameservers), convertInterfacesToString(newNameservers), client)
		if diags.HasError() {
			return diags
		}
	}

	// If user wants to control email type only while records & nameservers are absent,
	// then we have to update just an email status
	if emailType != nil && oldNameserversLen == 0 && newNameserversLen == 0 && oldRecordsLen == 0 && newRecordsLen == 0 {
		if mode == ncModeOverwrite {
			diags := createRecordsOverwrite(domain, emailType, []interface{}{}, client)
			if diags.HasError() {
				return diags
			}
		}
		if mode == ncModeMerge {
			diags := createRecordsMerge(domain, emailType, []interface{}{}, client)
			if diags.HasError() {
				return diags
			}
		}
	}

	// For overwrite mode, when no nameservers and records, and email type is not set, then we have to reset it to NONE
	if emailType == nil && mode == ncModeOverwrite && oldNameserversLen == 0 && newNameserversLen == 0 && oldRecordsLen == 0 && newRecordsLen == 0 {
		diags := createRecordsOverwrite(domain, nil, []interface{}{}, client)
		if diags.HasError() {
			return diags
		}
	}

	return nil
}

func resourceRecordDelete(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	mode := strings.ToUpper(data.Get("mode").(string))

	var records []interface{}
	var nameservers []interface{}

	if recordsRaw, ok := data.GetOk("record"); ok {
		records = recordsRaw.(*schema.Set).List()
	}

	if nameserversRaw, ok := data.GetOk("nameservers"); ok {
		nameservers = nameserversRaw.(*schema.Set).List()
	}

	recordsLen := len(records)
	nameserversLen := len(nameservers)

	if mode == ncModeMerge {
		ncMutexKV.Lock(domain)
		defer ncMutexKV.Unlock(domain)
	}

	if mode == ncModeMerge && recordsLen != 0 {
		return deleteRecordsMerge(domain, records, client)
	}

	if mode == ncModeOverwrite && recordsLen != 0 {
		return deleteRecordsOverwrite(domain, client)
	}

	if mode == ncModeMerge && nameserversLen != 0 {
		return deleteNameserversMerge(domain, convertInterfacesToString(nameservers), client)
	}

	if mode == ncModeOverwrite && nameserversLen != 0 {
		return deleteNameserversOverwrite(domain, client)
	}

	return nil
}
