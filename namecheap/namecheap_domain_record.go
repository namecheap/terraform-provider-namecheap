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

// recordsMode is how a namecheap_domain_records resource relates to the zone.
type recordsMode string

const (
	// ncModeMerge manages only the records and nameservers the resource
	// declares; several resources may share one domain.
	ncModeMerge recordsMode = "MERGE"
	// ncModeOverwrite owns the whole zone: anything undeclared is removed.
	ncModeOverwrite recordsMode = "OVERWRITE"
	// ncModeImport is set by the importer for the single read that adopts a
	// zone. That read stores MERGE with adopted = true (#68, #355).
	ncModeImport recordsMode = "IMPORT"
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
				if err := data.Set("mode", string(ncModeImport)); err != nil {
					return nil, err
				}

				return []*schema.ResourceData{data}, nil
			},
		},

		// While records adopted by `terraform import` are still unreconciled,
		// force a non-empty plan so Update runs once and settles ownership
		// even when the configuration already matches the imported zone (#355).
		CustomizeDiff: func(ctx context.Context, diff *schema.ResourceDiff, meta interface{}) error {
			if diff.Id() != "" && diff.Get("adopted").(bool) {
				return diff.SetNewComputed("adopted")
			}
			return nil
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
				Default:      string(ncModeMerge),
				ValidateFunc: validation.StringInSlice([]string{string(ncModeMerge), string(ncModeOverwrite)}, true),
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
			"adopted": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Set by the provider after `terraform import`: true while the imported records and nameservers have not yet been reconciled with the configuration. The first apply clears it. In MERGE mode that apply releases undeclared records from state without deleting them at Namecheap. A destroy before that apply deletes nothing, whatever mode the configuration declares.",
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

// recordsIntent is what one CRUD call works from, read once from the
// ResourceData: the declared records and nameservers and, on Update, the ones
// in the prior state (elsewhere prior equals declared or is empty).
type recordsIntent struct {
	domain           string
	mode             recordsMode
	adopted          bool
	emailType        *string
	records          []interface{}
	nameservers      []string
	priorRecords     []interface{}
	priorNameservers []string
}

func newRecordsIntent(data *schema.ResourceData) recordsIntent {
	priorRecords, _ := data.GetChange("record")
	priorNameservers, _ := data.GetChange("nameservers")

	intent := recordsIntent{
		domain:           strings.ToLower(data.Get("domain").(string)),
		mode:             recordsMode(strings.ToUpper(data.Get("mode").(string))),
		adopted:          data.Get("adopted").(bool),
		records:          data.Get("record").(*schema.Set).List(),
		nameservers:      convertInterfacesToString(data.Get("nameservers").(*schema.Set).List()),
		priorRecords:     priorRecords.(*schema.Set).List(),
		priorNameservers: convertInterfacesToString(priorNameservers.(*schema.Set).List()),
	}
	if emailType, ok := data.GetOk("email_type"); ok {
		intent.emailType = namecheap.String(emailType.(string))
	}
	return intent
}

func (i recordsIntent) hasRecords() bool     { return len(i.records) != 0 }
func (i recordsIntent) hasNameservers() bool { return len(i.nameservers) != 0 }
func (i recordsIntent) hadRecords() bool     { return len(i.priorRecords) != 0 }
func (i recordsIntent) hadNameservers() bool { return len(i.priorNameservers) != 0 }

// onlyEmailTypeInScope reports a configuration with neither records nor
// nameservers, now or before, which leaves the email type as the only thing
// to manage.
func (i recordsIntent) onlyEmailTypeInScope() bool {
	return !i.hasRecords() && !i.hadRecords() && !i.hasNameservers() && !i.hadNameservers()
}

// lockIfMerge serialises MERGE calls on one domain, since several resources
// may manage it. The returned function releases the lock; for the other modes
// both are no-ops.
func lockIfMerge(intent recordsIntent) func() {
	if intent.mode != ncModeMerge {
		return func() {}
	}
	ncMutexKV.Lock(intent.domain)
	return func() { ncMutexKV.Unlock(intent.domain) }
}

// domainUsesNamecheapDNS reports whether the domain is on Namecheap DNS, where
// host records apply, or delegated to custom nameservers, where they cannot
// even be read.
func domainUsesNamecheapDNS(ctx context.Context, domain string, client *namecheap.Client) (bool, diag.Diagnostics) {
	nsResponse, err := client.DomainsDNS.GetListWithContext(ctx, domain)
	if err != nil {
		return false, diagFromClientError(err)
	}
	if nsResponse == nil || nsResponse.DomainDNSGetListResult == nil || nsResponse.DomainDNSGetListResult.IsUsingOurDNS == nil {
		return false, diag.Errorf("Unable to read DNS state for domain %s: the domain may not exist or may have been removed from the account", domain)
	}
	return *nsResponse.DomainDNSGetListResult.IsUsingOurDNS, nil
}

func resourceRecordCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	intent := newRecordsIntent(data)
	unlock := lockIfMerge(intent)
	defer unlock()

	var diags diag.Diagnostics
	switch intent.mode {
	case ncModeMerge:
		diags = createMerge(ctx, intent, client)
	case ncModeOverwrite:
		diags = createOverwrite(ctx, intent, client)
	}
	if diags.HasError() {
		return diags
	}

	data.SetId(intent.domain)
	_ = data.Set("adopted", false)

	return diags
}

// createMerge adds the declared records and nameservers to whatever the zone
// already has.
func createMerge(ctx context.Context, intent recordsIntent, client *namecheap.Client) diag.Diagnostics {
	var diags diag.Diagnostics

	if intent.hasRecords() {
		diags = append(diags, createRecordsMerge(ctx, intent.domain, intent.emailType, intent.records, client)...)
		if diags.HasError() {
			return diags
		}
	}

	if intent.hasNameservers() {
		diags = append(diags, createNameserversMerge(ctx, intent.domain, intent.nameservers, client)...)
	}

	return diags
}

// createOverwrite replaces the zone with the declared records and nameservers.
func createOverwrite(ctx context.Context, intent recordsIntent, client *namecheap.Client) diag.Diagnostics {
	var diags diag.Diagnostics

	if intent.hasRecords() {
		diags = append(diags, createRecordsOverwrite(ctx, intent.domain, intent.emailType, intent.records, nil, client)...)
		if diags.HasError() {
			return diags
		}
	}

	if intent.hasNameservers() {
		diags = append(diags, createNameserversOverwrite(ctx, intent.domain, intent.nameservers, client)...)
	}

	return diags
}

func resourceRecordRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	intent := newRecordsIntent(data)
	unlock := lockIfMerge(intent)
	defer unlock()

	// Nameserver status comes first: on custom nameservers Namecheap does not
	// serve the zone, so reading host records would fail.
	usingOurDNS, diags := domainUsesNamecheapDNS(ctx, intent.domain, client)
	if diags.HasError() {
		return diags
	}

	if usingOurDNS {
		diags = append(diags, readZone(ctx, data, intent, client)...)
	} else {
		diags = append(diags, readDelegation(ctx, data, intent, client)...)
	}
	if diags.HasError() {
		return diags
	}

	// The importer's read adopts whatever it found: the resource goes on in
	// MERGE mode with the marker set for the first apply to settle (#68, #355).
	if intent.mode == ncModeImport {
		_ = data.Set("mode", string(ncModeMerge))
		_ = data.Set("adopted", true)
	}

	return diags
}

// readZone refreshes a domain on Namecheap DNS. MERGE keeps only the records
// it declares, OVERWRITE and IMPORT take the whole zone; there are no custom
// nameservers to keep.
func readZone(ctx context.Context, data *schema.ResourceData, intent recordsIntent, client *namecheap.Client) diag.Diagnostics {
	var (
		records   *[]map[string]interface{}
		emailType *string
		diags     diag.Diagnostics
	)

	switch intent.mode {
	case ncModeMerge:
		records, emailType, diags = readRecordsMerge(ctx, intent.domain, intent.records, client)
	case ncModeOverwrite, ncModeImport:
		var unmanaged []namecheap.DomainsDNSHostRecordDetailed
		records, emailType, unmanaged, diags = readRecordsOverwrite(ctx, intent.domain, intent.records, client)
		// Refresh-time warning for OVERWRITE only: IMPORT is adopting these
		// records, not about to delete them (#65, #250).
		if !diags.HasError() && intent.mode == ncModeOverwrite && len(unmanaged) > 0 {
			diags = append(diags, buildUnmanagedDeletionWarning(intent.domain, unmanaged, "will delete"))
		}
	}
	if diags.HasError() || records == nil {
		return diags
	}

	_ = data.Set("record", *records)
	if intent.emailType != nil {
		_ = data.Set("email_type", *emailType)
	}
	if intent.hasNameservers() {
		_ = data.Set("nameservers", []string{})
	}

	return diags
}

// readDelegation refreshes a domain on custom nameservers. MERGE keeps only the
// nameservers it declares, OVERWRITE and IMPORT take them all; there are no
// records to read.
func readDelegation(ctx context.Context, data *schema.ResourceData, intent recordsIntent, client *namecheap.Client) diag.Diagnostics {
	var (
		nameservers *[]string
		diags       diag.Diagnostics
	)

	switch intent.mode {
	case ncModeMerge:
		nameservers, diags = readNameserversMerge(ctx, intent.domain, intent.nameservers, client)
	case ncModeOverwrite, ncModeImport:
		nameservers, diags = readNameserversOverwrite(ctx, intent.domain, client)
	}
	if diags.HasError() {
		return diags
	}

	if nameservers != nil {
		_ = data.Set("nameservers", *nameservers)
	}
	_ = data.Set("record", []interface{}{})

	return diags
}

func resourceRecordUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	intent := newRecordsIntent(data)
	unlock := lockIfMerge(intent)
	defer unlock()

	usingOurDNS, diags := domainUsesNamecheapDNS(ctx, intent.domain, client)
	if diags.HasError() {
		return diags
	}

	switch {
	case intent.mode == ncModeMerge && intent.adopted:
		diags = append(diags, settleMergeImport(ctx, data, intent, usingOurDNS, client)...)
	case intent.mode == ncModeMerge:
		diags = append(diags, updateMerge(ctx, intent, usingOurDNS, client)...)
	case intent.mode == ncModeOverwrite:
		diags = append(diags, updateOverwrite(ctx, intent, usingOurDNS, client)...)
	}
	if diags.HasError() {
		return diags
	}

	// Ownership is settled once the first apply after import succeeds.
	_ = data.Set("adopted", false)

	return diags
}

// updateMerge brings the declared records and nameservers in line with the
// configuration and leaves everything else in the zone alone.
func updateMerge(ctx context.Context, intent recordsIntent, usingOurDNS bool, client *namecheap.Client) diag.Diagnostics {
	var diags diag.Diagnostics

	// Records can only be hosted on Namecheap DNS: a domain switched to custom
	// nameservers by hand goes back to default DNS before records are written.
	if !usingOurDNS && !intent.hasNameservers() {
		if _, err := client.DomainsDNS.SetDefaultWithContext(ctx, intent.domain); err != nil {
			return diagFromClientError(err)
		}
	}

	// Nameservers removed from the configuration are released before the
	// records are written, since SetHosts fails on a delegated domain.
	if intent.hadNameservers() && !intent.hasNameservers() {
		diags = append(diags, updateNameserversMerge(ctx, intent.domain, intent.priorNameservers, nil, client)...)
		if diags.HasError() {
			return diags
		}
	}

	if intent.hasRecords() || intent.hadRecords() {
		diags = append(diags, updateRecordsMerge(ctx, intent.domain, intent.emailType, intent.priorRecords, intent.records, client)...)
		if diags.HasError() {
			return diags
		}
	}

	if intent.hasNameservers() {
		diags = append(diags, updateNameserversMerge(ctx, intent.domain, intent.priorNameservers, intent.nameservers, client)...)
		if diags.HasError() {
			return diags
		}
	}

	if intent.emailType != nil && intent.onlyEmailTypeInScope() {
		diags = append(diags, createRecordsMerge(ctx, intent.domain, intent.emailType, []interface{}{}, client)...)
	}

	return diags
}

// updateOverwrite replaces the zone with the declared records and nameservers.
func updateOverwrite(ctx context.Context, intent recordsIntent, usingOurDNS bool, client *namecheap.Client) diag.Diagnostics {
	var diags diag.Diagnostics

	// Nameservers removed from the configuration, or enabled by hand, are
	// reset before records are written, since SetHosts fails on a delegated
	// domain.
	if !intent.hasNameservers() && (intent.hadNameservers() || !usingOurDNS) {
		if _, err := client.DomainsDNS.SetDefaultWithContext(ctx, intent.domain); err != nil {
			return diagFromClientError(err)
		}
	}

	if intent.hasRecords() || intent.hadRecords() {
		// Prior records count as consented removals for the deletion warning:
		// a record just taken out of the configuration is not a surprise.
		// Adopted records were never consented to (#250, #355).
		consented := intent.priorRecords
		if intent.adopted {
			consented = nil
		}
		diags = append(diags, createRecordsOverwrite(ctx, intent.domain, intent.emailType, intent.records, consented, client)...)
		if diags.HasError() {
			return diags
		}
	}

	if intent.hasNameservers() {
		diags = append(diags, createNameserversOverwrite(ctx, intent.domain, intent.nameservers, client)...)
		if diags.HasError() {
			return diags
		}
	}

	// With neither records nor nameservers in scope the zone is emptied and
	// the email type set as declared, or reset to NONE when undeclared.
	if intent.onlyEmailTypeInScope() {
		diags = append(diags, createRecordsOverwrite(ctx, intent.domain, intent.emailType, []interface{}{}, nil, client)...)
	}

	return diags
}

// settleMergeImport is the first MERGE apply after `terraform import` (#355).
// The imported records and nameservers were found, not applied, so nothing
// undeclared is owned: it leaves state with a warning and stays as it is at
// Namecheap. Only what the configuration declares and the zone does not
// already have is written.
func settleMergeImport(ctx context.Context, data *schema.ResourceData, intent recordsIntent, usingOurDNS bool, client *namecheap.Client) diag.Diagnostics {
	var diags diag.Diagnostics

	released, err := releasedRecords(*convertRecordTypeSetToDomainRecords(&intent.priorRecords), *convertRecordTypeSetToDomainRecords(&intent.records))
	if err != nil {
		return diagFromClientError(err)
	}
	if len(released) > 0 {
		diags = append(diags, buildReleasedRecordsWarning(intent.domain, released))
	}
	if releasedNS := releasedNameservers(intent.priorNameservers, intent.nameservers); len(releasedNS) > 0 {
		diags = append(diags, buildReleasedNameserversWarning(intent.domain, releasedNS))
	}

	// State was refreshed against the zone, so a declared record absent from
	// it is not live exactly as declared. Whole set elements are compared so
	// a ttl or mx_pref change counts too.
	priorRecordSet, declaredRecordSet := data.GetChange("record")
	recordsNeedWrite := declaredRecordSet.(*schema.Set).Difference(priorRecordSet.(*schema.Set)).Len() > 0
	emailTypeNeedsWrite := intent.emailType != nil && data.HasChange("email_type")

	switch {
	case intent.hasRecords() && (recordsNeedWrite || emailTypeNeedsWrite):
		// Hosting records needs Namecheap DNS, so a domain found on custom
		// nameservers with none declared is reset first, as any MERGE apply does.
		if !usingOurDNS && !intent.hasNameservers() {
			if _, err := client.DomainsDNS.SetDefaultWithContext(ctx, intent.domain); err != nil {
				return diagFromClientError(err)
			}
		}
		// Passing the declared records as both previous and current re-asserts
		// them: a live record matching a declared one is replaced by the
		// declared version, everything else in the zone stays.
		diags = append(diags, updateRecordsMerge(ctx, intent.domain, intent.emailType, intent.records, intent.records, client)...)
		if diags.HasError() {
			return diags
		}

	case !intent.hasRecords() && emailTypeNeedsWrite && !intent.hadNameservers() && !intent.hasNameservers():
		// Only the email type changes; the adopted records stay in the zone.
		diags = append(diags, updateRecordsMerge(ctx, intent.domain, intent.emailType, nil, nil, client)...)
		if diags.HasError() {
			return diags
		}
	}

	// Same rule for nameservers: write only when a declared one is not live.
	if len(releasedNameservers(intent.nameservers, intent.priorNameservers)) > 0 {
		diags = append(diags, updateNameserversMerge(ctx, intent.domain, intent.nameservers, intent.nameservers, client)...)
	}

	return diags
}

func resourceRecordDelete(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	intent := newRecordsIntent(data)
	unlock := lockIfMerge(intent)
	defer unlock()

	switch intent.mode {
	case ncModeMerge:
		// Adopted by import and never applied: nothing is owned, so nothing
		// is deleted. Import always stores MERGE, so this is the only path an
		// un-applied import can take (#355).
		if intent.adopted {
			return nil
		}
		return deleteMerge(ctx, intent, client)
	case ncModeOverwrite:
		return deleteOverwrite(ctx, intent, client)
	}

	return nil
}

// deleteMerge removes only what the resource declared and leaves the rest of
// the zone alone.
func deleteMerge(ctx context.Context, intent recordsIntent, client *namecheap.Client) diag.Diagnostics {
	if intent.hasRecords() {
		return deleteRecordsMerge(ctx, intent.domain, intent.records, client)
	}
	if intent.hasNameservers() {
		return deleteNameserversMerge(ctx, intent.domain, intent.nameservers, client)
	}
	return nil
}

// deleteOverwrite clears the zone or the delegation the resource owned.
func deleteOverwrite(ctx context.Context, intent recordsIntent, client *namecheap.Client) diag.Diagnostics {
	if intent.hasRecords() {
		return deleteRecordsOverwrite(ctx, intent.domain, intent.records, client)
	}
	if intent.hasNameservers() {
		return deleteNameserversOverwrite(ctx, intent.domain, client)
	}
	return nil
}
