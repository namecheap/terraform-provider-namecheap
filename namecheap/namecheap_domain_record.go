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
		Description:   "Manages the DNS host records of a domain, or delegates the domain to custom nameservers. In MERGE mode it manages only the records it declares; in OVERWRITE mode it owns the whole zone and deletes anything absent from the configuration.",
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
				Description:  "Purchased available domain name on your account",
				ValidateFunc: validateDomainIsNotSubdomain,
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
				Description:   "One or more DNS host records for the domain. Conflicts with `nameservers`, since a domain either uses Namecheap DNS with these records or delegates to custom nameservers.",
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
							Default:     1800,
							Description: fmt.Sprintf("Time to live for all record types. Possible values: any value between %d to %d", namecheap.MinTTL, namecheap.MaxTTL),
						},
					},
				},
			},
			"nameservers": {
				ConflictsWith: []string{"email_type", "record"},
				Type:          schema.TypeSet,
				Optional:      true,
				Description:   "Custom nameservers to delegate the domain to. Conflicts with `email_type` and `record`, which only apply while the domain uses Namecheap DNS.",
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					ValidateFunc: validation.StringIsNotEmpty,
				},
			},
		},
	}
}

func validateDomainIsNotSubdomain(val interface{}, key string) (warns []string, errs []error) {
	v := val.(string)
	if v == "" {
		errs = append(errs, fmt.Errorf("%q must not be empty", key))
		return
	}
	parsed, err := namecheap.ParseDomain(v)
	if err != nil {
		errs = append(errs, fmt.Errorf("%q is not a valid domain: %s", key, err))
		return
	}
	if parsed.TRD != "" {
		errs = append(errs, fmt.Errorf(
			"%q contains a subdomain (%q). The Namecheap API does not support managing "+
				"FreeDNS subdomain zones directly. Use the root domain with hostname records "+
				"instead (e.g., domain = %q with hostname = %q)",
			key, v, parsed.SLD+"."+parsed.TLD, parsed.TRD,
		))
	}
	return
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

	var diags diag.Diagnostics

	if mode == ncModeMerge && records != nil {
		recordDiags := createRecordsMerge(ctx, domain, emailType, records, client)
		if recordDiags.HasError() {
			return recordDiags
		}
		diags = append(diags, recordDiags...)
	}

	if mode == ncModeOverwrite && records != nil {
		recordDiags := createRecordsOverwrite(ctx, domain, emailType, records, nil, client)
		if recordDiags.HasError() {
			return recordDiags
		}
		diags = append(diags, recordDiags...)
	}

	if mode == ncModeMerge && nameservers != nil {
		nsDiags := createNameserversMerge(ctx, domain, convertInterfacesToString(nameservers), client)
		if nsDiags.HasError() {
			return nsDiags
		}
		diags = append(diags, nsDiags...)
	}

	if mode == ncModeOverwrite && nameservers != nil {
		nsDiags := createNameserversOverwrite(ctx, domain, convertInterfacesToString(nameservers), client)
		if nsDiags.HasError() {
			return nsDiags
		}
		diags = append(diags, nsDiags...)
	}

	data.SetId(domain)

	return diags
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

	var diags diag.Diagnostics

	// We must read nameservers status before hosts.
	// If you're using custom nameservers, then the reading records process will fail since Namecheap doesn't control
	// the domain behaviour.
	nsResponse, err := client.DomainsDNS.GetListWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if nsResponse == nil || nsResponse.DomainDNSGetListResult == nil || nsResponse.DomainDNSGetListResult.IsUsingOurDNS == nil {
		return diag.Errorf("Unable to read DNS state for domain %s: the domain may not exist or may have been removed from the account", domain)
	}

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		if mode == ncModeMerge {
			realNameservers, nsDiags := readNameserversMerge(ctx, domain, convertInterfacesToString(nameservers), client)
			if nsDiags.HasError() {
				return nsDiags
			}
			diags = append(diags, nsDiags...)
			_ = data.Set("nameservers", *realNameservers)
		}

		if mode == ncModeOverwrite || mode == ncModeImport {
			realNameservers, nsDiags := readNameserversOverwrite(ctx, domain, client)
			if nsDiags.HasError() {
				return nsDiags
			}
			diags = append(diags, nsDiags...)
			_ = data.Set("nameservers", *realNameservers)
		}

		_ = data.Set("record", []interface{}{})
	} else {
		if mode == ncModeMerge {
			realRecords, realEmailType, recordDiags := readRecordsMerge(ctx, domain, records, client)
			if recordDiags.HasError() {
				return recordDiags
			}
			diags = append(diags, recordDiags...)
			_ = data.Set("record", *realRecords)

			if emailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}
		}

		if mode == ncModeOverwrite || mode == ncModeImport {
			realRecords, realEmailType, unmanagedRecords, recordDiags := readRecordsOverwrite(ctx, domain, records, client)
			if recordDiags.HasError() {
				return recordDiags
			}
			diags = append(diags, recordDiags...)
			_ = data.Set("record", *realRecords)
			if emailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}

			// Refresh-time warning only for state mode - IMPORT is adopting
			// these records, not about to delete them (#65, #250).
			if mode == ncModeOverwrite && len(unmanagedRecords) > 0 {
				diags = append(diags, buildUnmanagedDeletionWarning(domain, unmanagedRecords, "will delete"))
			}
		}

		if nameservers != nil {
			_ = data.Set("nameservers", []string{})
		}
	}

	if mode == ncModeImport {
		_ = data.Set("mode", ncModeMerge)
	}

	return diags
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

	var diags diag.Diagnostics

	nsResponse, err := client.DomainsDNS.GetListWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if nsResponse == nil || nsResponse.DomainDNSGetListResult == nil || nsResponse.DomainDNSGetListResult.IsUsingOurDNS == nil {
		return diag.Errorf("Unable to read DNS state for domain %s: the domain may not exist or may have been removed from the account", domain)
	}

	// If the previous state contains nameservers, but the new one does not contain,
	// then reset nameservers before applying records.
	// This case is possible when user removed nameservers lines and pasted records, so before applying records,
	// we must reset nameservers to defaults, otherwise we will face API exception
	if (mode == ncModeOverwrite && oldNameserversLen != 0 && newNameserversLen == 0) ||
		// This condition resolves the issue if a user set up records on TF file, but in fact, manually enabled custom DNS.
		// Before applying records, we have to set default DNS
		(!*nsResponse.DomainDNSGetListResult.IsUsingOurDNS && newNameserversLen == 0) {
		_, err := client.DomainsDNS.SetDefaultWithContext(ctx, domain)
		if err != nil {
			return diagFromClientError(err)
		}
	}

	if mode == ncModeMerge && oldNameserversLen != 0 && newNameserversLen == 0 {
		nsDiags := updateNameserversMerge(ctx, domain, convertInterfacesToString(oldNameservers), convertInterfacesToString(newNameservers), client)
		if nsDiags.HasError() {
			return nsDiags
		}
		diags = append(diags, nsDiags...)
	}

	if mode == ncModeMerge && (newRecordsLen != 0 || oldRecordsLen != 0) {
		recordDiags := updateRecordsMerge(ctx, domain, emailType, oldRecords, newRecords, client)
		if recordDiags.HasError() {
			return recordDiags
		}
		diags = append(diags, recordDiags...)
	}

	if mode == ncModeOverwrite && (newRecordsLen != 0 || oldRecordsLen != 0) {
		// oldRecords is passed as the pre-flight's prior-state reference so a
		// record the user just deliberately removed from config (still live
		// at pre-flight time, since SetHosts hasn't run yet) is treated as a
		// consented removal rather than a surprise deletion warning.
		recordDiags := createRecordsOverwrite(ctx, domain, emailType, newRecords, oldRecords, client)
		if recordDiags.HasError() {
			return recordDiags
		}
		diags = append(diags, recordDiags...)
	}

	if mode == ncModeOverwrite && newNameserversLen != 0 {
		nsDiags := createNameserversOverwrite(ctx, domain, convertInterfacesToString(newNameservers), client)
		if nsDiags.HasError() {
			return nsDiags
		}
		diags = append(diags, nsDiags...)
	}

	if mode == ncModeMerge && newNameserversLen != 0 {
		nsDiags := updateNameserversMerge(ctx, domain, convertInterfacesToString(oldNameservers), convertInterfacesToString(newNameservers), client)
		if nsDiags.HasError() {
			return nsDiags
		}
		diags = append(diags, nsDiags...)
	}

	// If user wants to control email type only while records & nameservers are absent,
	// then we have to update just an email status
	if emailType != nil && oldNameserversLen == 0 && newNameserversLen == 0 && oldRecordsLen == 0 && newRecordsLen == 0 {
		if mode == ncModeOverwrite {
			recordDiags := createRecordsOverwrite(ctx, domain, emailType, []interface{}{}, nil, client)
			if recordDiags.HasError() {
				return recordDiags
			}
			diags = append(diags, recordDiags...)
		}
		if mode == ncModeMerge {
			recordDiags := createRecordsMerge(ctx, domain, emailType, []interface{}{}, client)
			if recordDiags.HasError() {
				return recordDiags
			}
			diags = append(diags, recordDiags...)
		}
	}

	// For overwrite mode, when no nameservers and records, and email type is not set, then we have to reset it to NONE
	if emailType == nil && mode == ncModeOverwrite && oldNameserversLen == 0 && newNameserversLen == 0 && oldRecordsLen == 0 && newRecordsLen == 0 {
		recordDiags := createRecordsOverwrite(ctx, domain, nil, []interface{}{}, nil, client)
		if recordDiags.HasError() {
			return recordDiags
		}
		diags = append(diags, recordDiags...)
	}

	return diags
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
		return deleteRecordsMerge(ctx, domain, records, client)
	}

	if mode == ncModeOverwrite && recordsLen != 0 {
		return deleteRecordsOverwrite(ctx, domain, records, client)
	}

	if mode == ncModeMerge && nameserversLen != 0 {
		return deleteNameserversMerge(ctx, domain, convertInterfacesToString(nameservers), client)
	}

	if mode == ncModeOverwrite && nameserversLen != 0 {
		return deleteNameserversOverwrite(ctx, domain, client)
	}

	return nil
}
