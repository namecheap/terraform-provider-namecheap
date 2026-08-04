package namecheap_provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

const (
	// hostRecordIDSeparator joins the four components of a namecheap_domain_host_record
	// import ID: domain/type/hostname/address.
	hostRecordIDSeparator = "/"

	// hostRecordRetryAttempts is how many times a mutation re-runs the SDK's
	// read-modify-write-verify cycle when it loses a race with another writer.
	// Namecheap has no per-record API — every change rewrites the whole zone —
	// so two applies touching one domain concurrently will collide, and the
	// retry is what makes for_each over a domain's records usable at all.
	hostRecordRetryAttempts = 3

	// hostRecordFixedMXPref is the preference Namecheap reports for every record
	// type that does not have one — which is all of them but MX. It is both the
	// schema default and the value sent for those types; see
	// hostRecordEffectiveMXPref for why sending anything else cannot work.
	hostRecordFixedMXPref = 10
)

// resourceNamecheapDomainHostRecord manages a single DNS host record, leaving every
// other record on the domain untouched.
//
// This is the per-record counterpart to namecheap_domain_records, which owns a
// domain's whole zone. The two are mutually exclusive per domain: pointing both
// at the same domain makes each fight to impose its own view of the zone.
//
// How a change is applied — the mechanics the resource page deliberately does not
// carry, because none of it is actionable for a user beyond "change a domain from
// one place at a time":
//
// The underlying API has no per-record operation — domains.dns.setHosts replaces
// the entire record set — so every change here is a read-modify-write of the whole
// zone, guarded two ways. The provider serializes changes to one domain through
// ncMutexKV within a single Terraform run. Across runs, the SDK re-reads the zone
// after writing and retries when the result is not what it wrote, which catches a
// concurrent writer in most cases but cannot catch all of them: the comparison is
// against the set computed from this run's own read, so a foreign write landing
// between that read and the setHosts is inside the replaced set — overwritten,
// verified as correct, and lost. Narrowed, not closed; see the warning on the
// resource page.
func resourceNamecheapDomainHostRecord() *schema.Resource {
	return &schema.Resource{
		Description: "Manages a single DNS host record on a domain, leaving all other records untouched. Mutually exclusive with namecheap_domain_records for the same domain.",

		CreateContext: resourceNamecheapDomainHostRecordCreate,
		ReadContext:   resourceNamecheapDomainHostRecordRead,
		UpdateContext: resourceNamecheapDomainHostRecordUpdate,
		DeleteContext: resourceNamecheapDomainHostRecordDelete,
		Importer: &schema.ResourceImporter{
			StateContext: resourceNamecheapDomainHostRecordImport,
		},

		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "The registered root domain the record belongs to (e.g. `example.com`). Must be a root domain on the account, not a subdomain. Changing this forces a new resource.",
				ValidateFunc: validateDomainIsNotSubdomain,
			},
			"hostname": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				// Required does not reject an empty string. SDK normalization would
				// read it as the apex, but the ID rendered for it is one the importer
				// refuses to parse — so the record could never be imported. Write `@`.
				ValidateFunc: validation.StringIsNotEmpty,
				Description:  "The sub-domain the record answers for, or `@` for the domain itself (e.g. `www`). Changing this forces a new resource, because the host is part of the record's identity.",
			},
			"type": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice(namecheap.AllowedRecordTypeValues, false),
				Description: fmt.Sprintf("The record type: %s. Changing this forces a new resource. MX and MXE records additionally require the domain's email routing to be set accordingly — see the resource documentation.",
					strings.Join(namecheap.AllowedRecordTypeValues, ", ")),
			},
			"address": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
				Description: "The record's value, whose meaning depends on `type`: an IP address for A/AAAA, a hostname for CNAME/MX/NS, arbitrary text for TXT, a URL for URL/URL301/FRAME. " +
					"Changing this does not force a new resource: the existing record is edited, in one write that swaps the value, so the name never stops resolving. The resource id is re-rendered, since the address is part of it.",
			},
			"mx_pref": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      hostRecordFixedMXPref,
				ValidateFunc: validation.IntBetween(0, 255),
				Description: fmt.Sprintf("The MX preference, lower being preferred, between 0 and 255. Applies to MX records only — Namecheap stores a fixed %d for every other type, so the value is ignored there rather than fought over. "+
					"For MX records it is part of what identifies the record, so a primary and a backup mail server can name the same host.", hostRecordFixedMXPref),
			},
			"ttl": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      1800,
				ValidateFunc: validation.IntBetween(namecheap.MinTTL, namecheap.MaxTTL),
				Description:  fmt.Sprintf("Time to live in seconds, between %d and %d. Changing this edits the existing record rather than forcing a new resource.", namecheap.MinTTL, namecheap.MaxTTL),
			},
		},
	}
}

// hostRecordFromData builds the SDK record described by the configuration.
func hostRecordFromData(data *schema.ResourceData) namecheap.DomainsDNSHostRecord {
	recordType := data.Get("type").(string)
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(strings.ToLower(data.Get("hostname").(string))),
		RecordType: namecheap.String(strings.ToUpper(recordType)),
		Address:    namecheap.String(data.Get("address").(string)),
		MXPref:     namecheap.UInt8(hostRecordEffectiveMXPref(recordType, data.Get("mx_pref").(int))),
		TTL:        namecheap.Int(data.Get("ttl").(int)),
	}
}

// hostRecordEffectiveMXPref returns the preference to send for a record of this
// type. Only MX has one; for every other type Namecheap stores a fixed 10 whatever
// it is sent, so the configured value is ignored rather than fought over.
//
// Sending it anyway does not merely leave a diff: the SDK verifies a write by
// re-reading the zone and comparing it to what it meant to write, and the
// preference is part of that comparison. A 20 sent for an A record comes back as
// 10, the write looks like a race it lost, and the apply fails with a concurrent
// modification error after three pointless rewrites of the whole zone.
func hostRecordEffectiveMXPref(recordType string, mxPref int) uint8 {
	if hostRecordMXPrefIsIdentity(recordType) {
		return uint8(mxPref)
	}
	return hostRecordFixedMXPref
}

// hostRecordBeforeChange builds the SDK record as the zone still holds it: the
// pre-change value of every attribute an update is able to move. That is what
// identifies the record during an update, both for the ambiguity check and for
// the selector — the new values are not in the zone yet.
func hostRecordBeforeChange(data *schema.ResourceData) namecheap.DomainsDNSHostRecord {
	oldAddress, _ := data.GetChange("address")
	oldTTL, _ := data.GetChange("ttl")
	oldMXPref, _ := data.GetChange("mx_pref")
	recordType := data.Get("type").(string)
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(strings.ToLower(data.Get("hostname").(string))),
		RecordType: namecheap.String(strings.ToUpper(recordType)),
		Address:    namecheap.String(oldAddress.(string)),
		MXPref:     namecheap.UInt8(hostRecordEffectiveMXPref(recordType, oldMXPref.(int))),
		TTL:        namecheap.Int(oldTTL.(int)),
	}
}

// hostRecordRestoreBeforeChange puts the pre-change values of the mutable
// attributes back into state. SDKv2 persists the *planned* values when an update
// returns an error, which would leave state describing a record the zone does not
// hold — and orphan the record it does hold, since no later plan would mention it
// again.
func hostRecordRestoreBeforeChange(data *schema.ResourceData) {
	for _, key := range []string{"address", "ttl", "mx_pref"} {
		before, _ := data.GetChange(key)
		_ = data.Set(key, before)
	}
}

// hostRecordSelector identifies record within the zone. Host, type and address
// together are a record's identity — the same triple the import ID uses — plus
// the MX preference for MX records, where it is what tells a primary mail server
// from its backup (see hostRecordMXPrefIsIdentity).
//
// Records the selector cannot tell apart are rejected before any write, by
// hostRecordLookup, because the SDK applies a change to *every* matching record.
func hostRecordSelector(record namecheap.DomainsDNSHostRecord) namecheap.RecordSelector {
	selector := namecheap.RecordSelector{
		HostName:   namecheap.String(strings.ToLower(derefString(record.HostName))),
		RecordType: namecheap.String(strings.ToUpper(derefString(record.RecordType))),
		Address:    namecheap.String(derefString(record.Address)),
	}
	if hostRecordMXPrefIsIdentity(derefString(record.RecordType)) {
		selector.MXPref = record.MXPref
	}
	return selector
}

// hostRecordMXPrefIsIdentity reports whether mx_pref is part of what distinguishes
// one record from another. Two MX records naming the same mail host at different
// preferences are an ordinary primary/backup pair, so for MX the preference is
// part of the record's identity; for every other type the API reports a fixed 10
// that means nothing.
func hostRecordMXPrefIsIdentity(recordType string) bool {
	return strings.ToUpper(strings.TrimSpace(recordType)) == namecheap.RecordTypeMX
}

// hostRecordIdentityMatches reports whether live is the record want describes, by
// the same fields hostRecordSelector matches on: host, type and address, plus the
// MX preference for MX records.
//
// The arguments are not interchangeable. A nil MXPref on want means "not known" —
// the import ID does not carry one — and matches any preference the live record
// has; a nil on the live side never matches a want that specifies one. Everything
// else is compared in the SDK's normalized form, so the API's own spelling of a
// host or address still matches the configuration's.
func hostRecordIdentityMatches(live, want namecheap.DomainsDNSHostRecord) bool {
	liveRecord, wanted := namecheap.NormalizeRecord(live), namecheap.NormalizeRecord(want)
	if derefString(liveRecord.HostName) != derefString(wanted.HostName) ||
		derefString(liveRecord.RecordType) != derefString(wanted.RecordType) ||
		derefString(liveRecord.Address) != derefString(wanted.Address) {
		return false
	}
	if !hostRecordMXPrefIsIdentity(derefString(wanted.RecordType)) || wanted.MXPref == nil {
		return true
	}
	return liveRecord.MXPref != nil && *liveRecord.MXPref == *wanted.MXPref
}

// hostRecordExistsError reports that the record a configuration asks for is already
// in the zone, and points at import — the only way to take ownership of it without
// creating a second, indistinguishable copy.
//
// Both create and update need this: create refuses to add a record that exists,
// and update refuses to *move* a record onto one that exists, since the SDK's
// upsert appends without a collision check.
func hostRecordExistsError(domain string, data *schema.ResourceData, existing namecheap.DomainsDNSHostRecordDetailed) diag.Diagnostics {
	return diag.Diagnostics{{
		Severity: diag.Error,
		Summary:  fmt.Sprintf("DNS record already exists on %s", domain),
		// The ID is rendered from the configuration, not from the API's echo of
		// the record: they denote the same record either way, and importing with
		// the spelling the configuration uses is what leaves the first plan empty.
		Detail: fmt.Sprintf("A %s record for %q pointing at %q already exists (TTL %d). Import it instead of creating it:\n\n"+
			"  terraform import <resource address> %s",
			derefString(existing.Type), derefString(existing.Name), derefString(existing.Address), derefInt(existing.TTL),
			hostRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string))),
	}}
}

// hostRecordID renders the resource ID, which doubles as the import ID.
func hostRecordID(domain, recordType, hostname, address string) string {
	return strings.Join([]string{
		strings.ToLower(domain),
		strings.ToUpper(recordType),
		strings.ToLower(hostname),
		address,
	}, hostRecordIDSeparator)
}

func resourceNamecheapDomainHostRecordCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	ncMutexKV.Lock(domain)
	defer ncMutexKV.Unlock(domain)

	record := hostRecordFromData(data)

	// A record with the same identity already present would be created twice by
	// setHosts, leaving a duplicate the selector can no longer tell apart.
	exists, existing, diags := hostRecordLookup(ctx, client, domain, record)
	if diags.HasError() {
		return diags
	}
	if exists {
		return hostRecordExistsError(domain, data, existing)
	}

	_, err := client.DomainsDNS.AddRecordsWithContext(ctx, domain,
		[]namecheap.DomainsDNSHostRecord{record},
		namecheap.WithRetryOnConflict(hostRecordRetryAttempts))
	if err != nil {
		return hostRecordWriteError(domain, "create", err)
	}

	data.SetId(hostRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string)))
	return resourceNamecheapDomainHostRecordRead(ctx, data, meta)
}

func resourceNamecheapDomainHostRecordRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	record := hostRecordFromData(data)
	found, live, diags := hostRecordLookup(ctx, client, domain, record)
	if diags.HasError() {
		return diags
	}
	if !found {
		// Deleted outside Terraform: drop it from state so the next plan offers
		// to recreate it, rather than failing.
		data.SetId("")
		return nil
	}

	// Only the attributes are read back. domain, hostname, type and address are
	// what the record was just *found* by — hostRecordMatches compares them
	// normalized — so state already describes this record, and overwriting them
	// with the API's own spelling is precisely what must not happen: the API
	// lower-cases a host and adds a trailing dot to a CNAME target, neither of
	// which a configuration writes. Storing that spelling puts state permanently at
	// odds with the config, which for the ForceNew fields plans a destroy and
	// recreate of a live record on every single apply.
	_ = data.Set("ttl", derefInt(live.TTL))

	// The preference is only read back for the one type that has one. Elsewhere the
	// API answers with its fixed 10, and storing that over a configured value would
	// leave a diff no apply can settle — see hostRecordEffectiveMXPref, which is why
	// the configured value never reached the API in the first place.
	if hostRecordMXPrefIsIdentity(data.Get("type").(string)) {
		_ = data.Set("mx_pref", derefInt(live.MXPref))
	}

	data.SetId(hostRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string)))
	return nil
}

func resourceNamecheapDomainHostRecordUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	ncMutexKV.Lock(domain)
	defer ncMutexKV.Unlock(domain)

	// Every failure funnels through here, because SDKv2 persists the planned values
	// whenever an update returns an error — whichever step failed. Restoring them in
	// one place is what stops a refused or failed update from orphaning the record
	// this resource still owns. See hostRecordRestoreBeforeChange.
	if diags := hostRecordApplyUpdate(ctx, client, data, domain); diags.HasError() {
		hostRecordRestoreBeforeChange(data)
		return diags
	}

	data.SetId(hostRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string)))
	return resourceNamecheapDomainHostRecordRead(ctx, data, meta)
}

// hostRecordApplyUpdate checks the zone and performs the update's write. It is
// separate from resourceNamecheapDomainHostRecordUpdate only so that every way it can
// fail lands on that function's single state-restoring error path.
func hostRecordApplyUpdate(ctx context.Context, client *namecheap.Client, data *schema.ResourceData, domain string) diag.Diagnostics {
	// domain, hostname and type are ForceNew, so only address, ttl and mx_pref can
	// reach here — and the record to change is still the pre-change one, which is
	// what both the ambiguity check and the selector have to describe.
	before := hostRecordBeforeChange(data)
	target := hostRecordFromData(data)

	// One read serves both checks below; an update is expensive enough already
	// (the SDK's own cycle is another read, a write and a verifying read).
	zone, diags := hostRecordZone(ctx, client, domain)
	if diags.HasError() {
		return diags
	}

	// Refuse to write when the record to be changed cannot be picked out of the
	// zone: the selector would carry the change to every record it matches.
	if _, _, diags := hostRecordResolve(domain, zone, before); diags.HasError() {
		return diags
	}

	// A change to an identity field can land on a record that already exists. The
	// SDK's upsert is a filter-then-append with no collision check, so that write
	// would add a second record indistinguishable from the first — and the read
	// straight after would refuse to touch either, leaving the resource wedged with
	// no way out but the dashboard. Refuse it the same way create does.
	//
	// Only when the identity actually moved: a lone TTL change leaves it alone, and
	// the lookup would find this resource's own record and call it a collision.
	if !hostRecordIdentityMatches(before, target) {
		exists, existing, diags := hostRecordResolve(domain, zone, target)
		if diags.HasError() {
			return diags
		}
		if exists {
			return hostRecordExistsError(domain, data, existing)
		}
	}

	_, err := client.DomainsDNS.UpsertRecordsWithContext(ctx, domain, hostRecordSelector(before),
		[]namecheap.DomainsDNSHostRecord{target},
		namecheap.WithRetryOnConflict(hostRecordRetryAttempts))
	if err != nil {
		return hostRecordWriteError(domain, "update", err)
	}
	return nil
}

func resourceNamecheapDomainHostRecordDelete(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	ncMutexKV.Lock(domain)
	defer ncMutexKV.Unlock(domain)

	record := hostRecordFromData(data)

	// Refuse to delete what cannot be picked out of the zone unambiguously, and
	// skip the write entirely when the record is already gone — rewriting a whole
	// zone to remove nothing is a race waiting to happen.
	found, _, diags := hostRecordLookup(ctx, client, domain, record)
	if diags.HasError() {
		return diags
	}
	if !found {
		data.SetId("")
		return nil
	}

	_, err := client.DomainsDNS.DeleteRecordsWithContext(ctx, domain, hostRecordSelector(record),
		namecheap.WithRetryOnConflict(hostRecordRetryAttempts))
	if err != nil {
		return hostRecordWriteError(domain, "delete", err)
	}

	data.SetId("")
	return nil
}

func resourceNamecheapDomainHostRecordImport(ctx context.Context, data *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	parts := strings.Split(data.Id(), hostRecordIDSeparator)
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid import ID %q: expected %q, e.g. %q",
			data.Id(), "<domain>/<type>/<hostname>/<address>", "example.com/A/www/10.0.0.1")
	}

	// The address may itself contain the separator (a URL record, say), so only
	// the first three components are split off and the rest is the address.
	domain, recordType, hostname := parts[0], parts[1], parts[2]
	address := strings.Join(parts[3:], hostRecordIDSeparator)

	for name, value := range map[string]string{"domain": domain, "type": recordType, "hostname": hostname, "address": address} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("invalid import ID %q: the %s component is empty", data.Id(), name)
		}
	}

	// The MX preference is not part of the import ID, so it is left unset here:
	// hostRecordMatches then ignores it, and two MX records differing only in
	// preference come back as an ambiguous match rather than a coin flip.
	want := namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(hostname),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
	}

	client := meta.(*namecheap.Client)
	found, live, diags := hostRecordLookup(ctx, client, strings.ToLower(domain), want)
	if diags.HasError() {
		return nil, hostRecordImportError(domain, diags)
	}
	if !found {
		return nil, fmt.Errorf("no %s record for %q pointing at %q exists on %s; "+
			"check the value matches the zone exactly (the API normalizes some addresses, e.g. a trailing dot on CNAME targets)",
			strings.ToUpper(recordType), hostname, address, domain)
	}

	// The address is taken as the ID spelled it, not as the API echoes it: an
	// imported record should land in the same state a configuration written the same
	// way would produce, and no configuration writes the trailing dot the API adds
	// to a CNAME/ALIAS/NS/MX target. Host and type are canonicalized instead —
	// lower and upper case respectively — because that is the spelling the ID
	// itself renders, and the case of a DNS name carries no meaning. TTL and the MX
	// preference are not in the ID at all, so they can only come from the API.
	_ = data.Set("domain", strings.ToLower(domain))
	_ = data.Set("hostname", strings.ToLower(hostname))
	_ = data.Set("type", derefString(live.Type))
	_ = data.Set("address", address)
	_ = data.Set("ttl", derefInt(live.TTL))
	_ = data.Set("mx_pref", derefInt(live.MXPref))
	data.SetId(hostRecordID(domain, derefString(live.Type), hostname, address))

	return []*schema.ResourceData{data}, nil
}

// hostRecordLookup finds the one record matching want in domain's live zone. It
// reports whether that record exists and returns the live copy, so callers read
// back the API's own view of it (its TTL and MX preference) rather than the
// configuration's.
//
// Several records matching want is an error, not a pick-the-first: the SDK applies
// a change to every record a selector matches, so continuing would update or
// delete all of them.
func hostRecordLookup(ctx context.Context, client *namecheap.Client, domain string, want namecheap.DomainsDNSHostRecord) (bool, namecheap.DomainsDNSHostRecordDetailed, diag.Diagnostics) {
	zone, diags := hostRecordZone(ctx, client, domain)
	if diags.HasError() {
		return false, namecheap.DomainsDNSHostRecordDetailed{}, diags
	}
	return hostRecordResolve(domain, zone, want)
}

// hostRecordZone reads domain's live host records. It is separate from the matching
// below so a caller that has two identities to check — update, checking both the
// record it is moving and where it is moving to — can pay for one read.
func hostRecordZone(ctx context.Context, client *namecheap.Client, domain string) ([]namecheap.DomainsDNSHostRecordDetailed, diag.Diagnostics) {
	resp, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return nil, dataSourceDomainReadError(domain, err)
	}
	if resp == nil || resp.DomainDNSGetHostsResult == nil || resp.DomainDNSGetHostsResult.Hosts == nil {
		return nil, nil
	}
	return *resp.DomainDNSGetHostsResult.Hosts, nil
}

// hostRecordResolve picks the one record in zone matching want, applying the
// same all-or-nothing rule as hostRecordLookup.
func hostRecordResolve(domain string, zone []namecheap.DomainsDNSHostRecordDetailed, want namecheap.DomainsDNSHostRecord) (bool, namecheap.DomainsDNSHostRecordDetailed, diag.Diagnostics) {
	var zero namecheap.DomainsDNSHostRecordDetailed

	matches := hostRecordMatches(zone, want)
	switch len(matches) {
	case 0:
		return false, zero, nil
	case 1:
		return true, matches[0], nil
	default:
		return false, zero, hostRecordAmbiguousError(domain, want, matches)
	}
}

// hostRecordMatches returns every record in zone that shares want's identity.
//
// Identity is host, type and address — plus the MX preference for MX records, and
// then only when want carries one (import does not know it until the record is
// found). TTL is never part of it: matching on it would make a TTL changed in the
// dashboard look like a deletion. Normalization is the SDK's, so the API's own
// spelling of an address — a trailing dot on a CNAME target, say — still matches
// the configuration's.
func hostRecordMatches(zone []namecheap.DomainsDNSHostRecordDetailed, want namecheap.DomainsDNSHostRecord) []namecheap.DomainsDNSHostRecordDetailed {
	var matches []namecheap.DomainsDNSHostRecordDetailed
	for _, host := range zone {
		if hostRecordIdentityMatches(namecheap.RecordFromDetailed(host), want) {
			matches = append(matches, host)
		}
	}
	return matches
}

// hostRecordAmbiguousError reports a zone holding several records this resource
// cannot tell apart, listing what distinguishes them so the user can go and look.
func hostRecordAmbiguousError(domain string, want namecheap.DomainsDNSHostRecord, matches []namecheap.DomainsDNSHostRecordDetailed) diag.Diagnostics {
	candidates := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := fmt.Sprintf("  - TTL %d", derefInt(match.TTL))
		if hostRecordMXPrefIsIdentity(derefString(match.Type)) {
			candidate += fmt.Sprintf(", MX preference %d", derefInt(match.MXPref))
		}
		candidates = append(candidates, candidate)
	}

	return diag.Diagnostics{{
		Severity: diag.Error,
		Summary:  fmt.Sprintf("Ambiguous DNS record on %s", domain),
		Detail: fmt.Sprintf("%d records on %s share this one's identity — type %s, host %q, address %q — and "+
			"differ only in ways this resource cannot select on:\n\n%s\n\n"+
			"Namecheap has no per-record API, so a change is applied by matching records within the zone — and a "+
			"match that hits several would change or delete all of them. Remove the duplicate in the Namecheap "+
			"dashboard, or manage this domain's records as one set with namecheap_domain_records.",
			len(matches), domain, derefString(want.RecordType), derefString(want.HostName), derefString(want.Address),
			strings.Join(candidates, "\n")),
	}}
}

// hostRecordImportError flattens diagnostics into the single error the importer
// interface allows, keeping the detail: for an ambiguous match that detail is the
// list of candidates, which is the whole value of the message.
func hostRecordImportError(domain string, diags diag.Diagnostics) error {
	if diags[0].Detail == "" {
		return fmt.Errorf("reading %s during import: %s", domain, diags[0].Summary)
	}
	return fmt.Errorf("reading %s during import: %s: %s", domain, diags[0].Summary, diags[0].Detail)
}

// hostRecordWriteError turns an SDK write failure into diagnostics that name the
// domain and say what to do about it. A lost race and a mail-routing rejection
// are the two failures a user of this resource will actually hit, and neither is
// self-explanatory from the SDK's message alone.
func hostRecordWriteError(domain, verb string, err error) diag.Diagnostics {
	if errors.Is(err, namecheap.ErrConcurrentModification) {
		return diag.Diagnostics{{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("Concurrent change to %s while trying to %s a record", domain, verb),
			Detail: "Namecheap replaces a domain's entire record set on every change, so this resource reads the zone, " +
				"modifies it and writes it back. Another writer changed the zone during that cycle, and the change was " +
				"abandoned rather than overwriting theirs.\n\n" +
				"Re-run the apply. If it keeps happening, something outside this configuration is writing to the domain — " +
				"another Terraform run, a script, or the Namecheap dashboard.",
		}}
	}

	var invalid *namecheap.InvalidArgumentsError
	if errors.As(err, &invalid) {
		return diag.Diagnostics{{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("Namecheap rejected the record set for %s", domain),
			Detail: fmt.Sprintf("%s\n\nMX and MXE records are tied to the domain's email routing, which this resource does "+
				"not manage: use namecheap_domain_records' email_type for those, or set it in the Namecheap dashboard.", invalid.Error()),
		}}
	}

	diags := diagFromClientError(err)
	for i := range diags {
		diags[i].Summary = fmt.Sprintf("%s (domain %q)", diags[i].Summary, domain)
	}
	return diags
}
