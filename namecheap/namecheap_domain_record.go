package namecheap_provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

const (
	ncModeMerge     = "MERGE"
	ncModeOverwrite = "OVERWRITE"

	// recordsImportScopeZone is the import ID suffix that asks for the whole
	// zone: "example.com/zone". The plain "example.com" imports no records.
	recordsImportScopeZone = "zone"
)

func resourceNamecheapDomainRecords() *schema.Resource {
	return &schema.Resource{
		Description:   "Manages the DNS host records of a domain, or delegates the domain to custom nameservers. In MERGE mode it manages only the records it declares; in OVERWRITE mode it owns the whole zone and deletes anything absent from the configuration.",
		CreateContext: resourceRecordCreate,
		UpdateContext: resourceRecordUpdate,
		ReadContext:   resourceRecordRead,
		DeleteContext: resourceRecordDelete,

		CustomizeDiff: customizeRecordsDiff,

		Importer: &schema.ResourceImporter{
			StateContext: importRecordsState,
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
				// Computed: every read stores the zone's mail mode, import
				// included, and leaving email_type out keeps that mode.
				Computed:     true,
				ValidateFunc: validation.StringInSlice(namecheap.AllowedEmailTypeValues, false),
				Description:  fmt.Sprintf("Possible values: %s. Leave it out to keep the domain's current mail mode, which is then read into state.", strings.TrimSpace(strings.Join(namecheap.AllowedEmailTypeValues, ", "))),
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

// parseRecordsImportID splits an import ID into the domain and the import
// scope: "example.com" imports the domain's settings and no records,
// "example.com/zone" imports every record too.
func parseRecordsImportID(id string) (domain string, wholeZone bool, err error) {
	domain, scope, hasScope := strings.Cut(id, "/")
	if hasScope && scope != recordsImportScopeZone {
		return "", false, fmt.Errorf(
			"unexpected import ID %q: use %q to import the domain's settings without its records, "+
				"or %q to import every record of the zone as well",
			id, "example.com", "example.com/"+recordsImportScopeZone)
	}
	if _, errs := validateDomainIsNotSubdomain(domain, "domain"); len(errs) > 0 {
		return "", false, fmt.Errorf("unexpected import ID %q: %w", id, errs[0])
	}
	return domain, hasScope, nil
}

// importRecordsState imports a domain by one of two IDs. Read cannot see the
// configuration, so it cannot tell the records a configuration declares from
// the rest of the zone; the ID says which of the two the import is for.
//
// "example.com" stores no records. In MERGE the records in state are the ones
// the resource owns, and the next apply deletes an owned record that the
// configuration does not declare, so an import must not hand it records nobody
// declared. A declared record that is already live is adopted by the first
// apply (mergeRecords).
//
// "example.com/zone" stores every record, for a configuration that declares
// the whole zone or is generated with -generate-config-out. The resource then
// owns them all: one the configuration leaves out is planned for removal, as
// for any record block removed from a MERGE configuration.
//
// Custom nameservers are one setting of the domain, not rows of a zone, and are
// stored by both forms. The refresh Terraform runs after the import stores
// email_type.
func importRecordsState(ctx context.Context, data *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	domain, wholeZone, err := parseRecordsImportID(data.Id())
	if err != nil {
		return nil, err
	}
	client := meta.(*namecheap.Client)
	apiDomain := strings.ToLower(domain)

	data.SetId(apiDomain)
	if err := data.Set("domain", domain); err != nil {
		return nil, err
	}
	if err := data.Set("mode", ncModeMerge); err != nil {
		return nil, err
	}

	nsResponse, err := client.DomainsDNS.GetListWithContext(ctx, apiDomain)
	if err != nil {
		return nil, err
	}
	if err := validateGetListResponse(nsResponse); err != nil {
		return nil, fmt.Errorf("unable to read DNS state for domain %s: %w", apiDomain, err)
	}

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		nameservers, diags := readNameserversOverwrite(nsResponse)
		if err := firstDiagError(diags); err != nil {
			return nil, err
		}
		if err := data.Set("nameservers", *nameservers); err != nil {
			return nil, err
		}
		return []*schema.ResourceData{data}, nil
	}

	if wholeZone {
		records, _, _, diags := readRecordsOverwrite(ctx, apiDomain, nil, client)
		if err := firstDiagError(diags); err != nil {
			return nil, err
		}
		if err := data.Set("record", *records); err != nil {
			return nil, err
		}
	}

	return []*schema.ResourceData{data}, nil
}

// firstDiagError returns the first error in diags as an error, for callers
// that report through an error value, or nil when diags holds none.
func firstDiagError(diags diag.Diagnostics) error {
	for _, d := range diags {
		if d.Severity != diag.Error {
			continue
		}
		if d.Detail != "" {
			return fmt.Errorf("%s: %s", d.Summary, d.Detail)
		}
		return fmt.Errorf("%s", d.Summary)
	}
	return nil
}

// customizeRecordsDiff fails the plan when email_type = "MXE" would leave the
// zone with no MXE record. Namecheap accepts that write and puts the zone on
// MXE with nothing to publish, so the domain is left without a mail server.
//
// In OVERWRITE the configured records are the whole zone. In MERGE an MXE row
// another resource manages counts too, so when the configuration has none the
// live zone is read, once per plan that writes; a failed read skips the check
// and leaves it to the apply.
func customizeRecordsDiff(ctx context.Context, diff *schema.ResourceDiff, meta interface{}) error {
	config := diff.GetRawConfig()
	if emailType := configuredEmailType(config); emailType == nil || *emailType != namecheap.EmailTypeMXE {
		return nil
	}
	hasMXE, known := configHasMXERecord(config.GetAttr("record"))
	if hasMXE || !known {
		return nil
	}
	// CustomizeDiff runs on plans with no changes and again during apply, so the
	// check, and the live read it may need, is limited to plans that write.
	if !diff.HasChange("record") && !diff.HasChange("email_type") {
		return nil
	}
	// An unknown mode must not fall into the lenient MERGE branch below and then
	// apply as OVERWRITE.
	if !diff.NewValueKnown("mode") {
		return nil
	}

	if strings.ToUpper(diff.Get("mode").(string)) == ncModeOverwrite {
		return fmt.Errorf(`email_type = "MXE" needs at least one record with type = "MXE": ` +
			`without one Namecheap puts the domain on MXE with no mail server. ` +
			`Add the MXE record for your mail server's IPv4 address, or set another email_type`)
	}

	if !diff.NewValueKnown("domain") {
		return nil
	}
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(diff.Get("domain").(string))
	response, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil || validateGetHostsResponse(response) != nil {
		return nil
	}
	priorRecords, _ := diff.GetChange("record")
	keeps, err := zoneKeepsMXERecord(response.DomainDNSGetHostsResult.Hosts, priorRecords.(*schema.Set).List())
	if err != nil || keeps {
		return nil
	}
	return fmt.Errorf(`email_type = "MXE" needs at least one record with type = "MXE", `+
		`and neither this resource nor the rest of %s's zone has one: `+
		`without one Namecheap puts the domain on MXE with no mail server. `+
		`Add the MXE record for your mail server's IPv4 address to this resource, or set another email_type`, domain)
}

// configHasMXERecord reports whether the configured record blocks hold an MXE
// record. known is false when a record type is not known until apply, so the
// answer cannot be given at plan time.
func configHasMXERecord(records cty.Value) (hasMXE bool, known bool) {
	if !records.IsKnown() {
		return false, false
	}
	if records.IsNull() {
		return false, true
	}
	known = true
	for it := records.ElementIterator(); it.Next(); {
		_, record := it.Element()
		if !record.IsKnown() {
			known = false
			continue
		}
		recordType := record.GetAttr("type")
		if !recordType.IsKnown() {
			known = false
			continue
		}
		if !recordType.IsNull() && recordType.AsString() == namecheap.RecordTypeMXE {
			return true, true
		}
	}
	return false, known
}

// zoneKeepsMXERecord reports whether a MERGE write keeps an MXE row this
// resource does not manage: a live MXE row absent from priorRecords, the
// records the resource wrote last, matched as the MERGE write matches them
// (isPriorRecord).
func zoneKeepsMXERecord(hosts *[]namecheap.DomainsDNSHostRecordDetailed, priorRecords []interface{}) (bool, error) {
	if hosts == nil {
		return false, nil
	}
	prior := convertRecordTypeSetToDomainRecords(&priorRecords)
	for _, host := range *hosts {
		if derefStr(host.Type) != namecheap.RecordTypeMXE {
			continue
		}
		owned, err := isPriorRecord(host, *prior)
		if err != nil {
			return false, err
		}
		if !owned {
			return true, nil
		}
	}
	return false, nil
}

// configuredEmailType returns the email_type a configuration sets, or nil when
// it leaves it out or the value is not known yet. Writes take it from the
// configuration because email_type is Computed: state holds the mode the last
// refresh read, and nil makes a write keep the mode the zone has when it runs.
func configuredEmailType(config cty.Value) *string {
	if config.IsNull() || !config.IsKnown() {
		return nil
	}
	v := config.GetAttr("email_type")
	if v.IsNull() || !v.IsKnown() {
		return nil
	}
	emailType := v.AsString()
	return &emailType
}

func resourceRecordCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	domain := strings.ToLower(data.Get("domain").(string))
	mode := strings.ToUpper(data.Get("mode").(string))
	emailType := configuredEmailType(data.GetRawConfig())

	var records []interface{}
	var nameservers []interface{}

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

	var records []interface{}
	var nameservers []interface{}

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
			realNameservers, nsDiags := readNameserversMerge(nsResponse, convertInterfacesToString(nameservers))
			if nsDiags.HasError() {
				return nsDiags
			}
			diags = append(diags, nsDiags...)
			_ = data.Set("nameservers", *realNameservers)
		}

		if mode == ncModeOverwrite {
			realNameservers, nsDiags := readNameserversOverwrite(nsResponse)
			if nsDiags.HasError() {
				return nsDiags
			}
			diags = append(diags, nsDiags...)
			_ = data.Set("nameservers", *realNameservers)
		}

		_ = data.Set("record", []interface{}{})
		// Custom nameservers leave no Namecheap mail mode in effect, so clear one
		// stored while the domain used Namecheap DNS.
		if _, ok := data.GetOk("email_type"); ok {
			_ = data.Set("email_type", "")
		}
	} else {
		if mode == ncModeMerge {
			realRecords, realEmailType, recordDiags := readRecordsMerge(ctx, domain, records, client)
			if recordDiags.HasError() {
				return recordDiags
			}
			diags = append(diags, recordDiags...)
			_ = data.Set("record", *realRecords)

			if realEmailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}
		}

		if mode == ncModeOverwrite {
			realRecords, realEmailType, unmanagedRecords, recordDiags := readRecordsOverwrite(ctx, domain, records, client)
			if recordDiags.HasError() {
				return recordDiags
			}
			diags = append(diags, recordDiags...)
			_ = data.Set("record", *realRecords)
			if realEmailType != nil {
				_ = data.Set("email_type", *realEmailType)
			}

			if len(unmanagedRecords) > 0 {
				diags = append(diags, buildUnmanagedDeletionWarning(domain, unmanagedRecords, "will delete"))
			}
		}

		if nameservers != nil {
			_ = data.Set("nameservers", []string{})
		}
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

	emailType := configuredEmailType(data.GetRawConfig())

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

	// OVERWRITE with no records, no nameservers and no email_type: resend the
	// zone's current mail mode and wipe the records this resource no longer
	// configures.
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
