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
	// dnsRecordIDSeparator joins the four components of a namecheap_dns_record
	// import ID: domain/type/hostname/address.
	dnsRecordIDSeparator = "/"

	// dnsRecordRetryAttempts is how many times a mutation re-runs the SDK's
	// read-modify-write-verify cycle when it loses a race with another writer.
	// Namecheap has no per-record API — every change rewrites the whole zone —
	// so two applies touching one domain concurrently will collide, and the
	// retry is what makes for_each over a domain's records usable at all.
	dnsRecordRetryAttempts = 3
)

// resourceNamecheapDNSRecord manages a single DNS host record, leaving every
// other record on the domain untouched.
//
// This is the per-record counterpart to namecheap_domain_records, which owns a
// domain's whole zone. The two are mutually exclusive per domain: pointing both
// at the same domain makes each fight to impose its own view of the zone.
//
// The underlying API has no per-record operation — domains.dns.setHosts replaces
// the entire record set — so every change here is a read-modify-write of the
// whole zone, guarded two ways: the provider serializes changes to one domain
// through ncMutexKV within a single Terraform run, and the SDK verifies the zone
// after writing so a change lost to a writer outside this run surfaces as an
// error instead of silently disappearing.
func resourceNamecheapDNSRecord() *schema.Resource {
	return &schema.Resource{
		Description: "Manages a single DNS host record on a domain, leaving all other records untouched. Mutually exclusive with namecheap_domain_records for the same domain.",

		CreateContext: resourceNamecheapDNSRecordCreate,
		ReadContext:   resourceNamecheapDNSRecordRead,
		UpdateContext: resourceNamecheapDNSRecordUpdate,
		DeleteContext: resourceNamecheapDNSRecordDelete,
		Importer: &schema.ResourceImporter{
			StateContext: resourceNamecheapDNSRecordImport,
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
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The sub-domain the record answers for, or `@` for the domain itself (e.g. `www`). Changing this forces a new resource, because the host is part of the record's identity.",
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
				Type:        schema.TypeString,
				Required:    true,
				Description: "The record's value, whose meaning depends on `type`: an IP address for A/AAAA, a hostname for CNAME/MX/NS, arbitrary text for TXT, a URL for URL/URL301/FRAME. Changed in place.",
			},
			"mx_pref": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      10,
				ValidateFunc: validation.IntBetween(0, 255),
				Description: "The MX preference, lower being preferred, between 0 and 255. Applies to MX records only; the API returns 10 for every other type, so leaving it at the default keeps plans empty. " +
					"For MX records it is part of what identifies the record, so a primary and a backup mail server can name the same host.",
			},
			"ttl": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      1800,
				ValidateFunc: validation.IntBetween(namecheap.MinTTL, namecheap.MaxTTL),
				Description:  fmt.Sprintf("Time to live in seconds, between %d and %d. Changed in place.", namecheap.MinTTL, namecheap.MaxTTL),
			},
		},
	}
}

// dnsRecordFromData builds the SDK record described by the configuration.
func dnsRecordFromData(data *schema.ResourceData) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(strings.ToLower(data.Get("hostname").(string))),
		RecordType: namecheap.String(strings.ToUpper(data.Get("type").(string))),
		Address:    namecheap.String(data.Get("address").(string)),
		MXPref:     namecheap.UInt8(uint8(data.Get("mx_pref").(int))),
		TTL:        namecheap.Int(data.Get("ttl").(int)),
	}
}

// dnsRecordBeforeChange builds the SDK record as the zone still holds it: the
// pre-change value of every attribute an update is able to move. That is what
// identifies the record during an update, both for the ambiguity check and for
// the selector — the new values are not in the zone yet.
func dnsRecordBeforeChange(data *schema.ResourceData) namecheap.DomainsDNSHostRecord {
	oldAddress, _ := data.GetChange("address")
	oldTTL, _ := data.GetChange("ttl")
	oldMXPref, _ := data.GetChange("mx_pref")
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(strings.ToLower(data.Get("hostname").(string))),
		RecordType: namecheap.String(strings.ToUpper(data.Get("type").(string))),
		Address:    namecheap.String(oldAddress.(string)),
		MXPref:     namecheap.UInt8(uint8(oldMXPref.(int))),
		TTL:        namecheap.Int(oldTTL.(int)),
	}
}

// dnsRecordRestoreBeforeChange puts the pre-change values of the mutable
// attributes back into state. SDKv2 persists the *planned* values when an update
// returns an error, which would leave state describing a record the zone does not
// hold — and orphan the record it does hold, since no later plan would mention it
// again.
func dnsRecordRestoreBeforeChange(data *schema.ResourceData) {
	for _, key := range []string{"address", "ttl", "mx_pref"} {
		before, _ := data.GetChange(key)
		_ = data.Set(key, before)
	}
}

// dnsRecordSelector identifies record within the zone. Host, type and address
// together are a record's identity — the same triple the import ID uses — plus
// the MX preference for MX records, where it is what tells a primary mail server
// from its backup (see dnsRecordMXPrefIsIdentity).
//
// Records the selector cannot tell apart are rejected before any write, by
// dnsRecordLookup, because the SDK applies a change to *every* matching record.
func dnsRecordSelector(record namecheap.DomainsDNSHostRecord) namecheap.RecordSelector {
	selector := namecheap.RecordSelector{
		HostName:   namecheap.String(strings.ToLower(derefString(record.HostName))),
		RecordType: namecheap.String(strings.ToUpper(derefString(record.RecordType))),
		Address:    namecheap.String(derefString(record.Address)),
	}
	if dnsRecordMXPrefIsIdentity(derefString(record.RecordType)) {
		selector.MXPref = record.MXPref
	}
	return selector
}

// dnsRecordMXPrefIsIdentity reports whether mx_pref is part of what distinguishes
// one record from another. Two MX records naming the same mail host at different
// preferences are an ordinary primary/backup pair, so for MX the preference is
// part of the record's identity; for every other type the API reports a fixed 10
// that means nothing.
func dnsRecordMXPrefIsIdentity(recordType string) bool {
	return strings.ToUpper(strings.TrimSpace(recordType)) == namecheap.RecordTypeMX
}

// dnsRecordID renders the resource ID, which doubles as the import ID.
func dnsRecordID(domain, recordType, hostname, address string) string {
	return strings.Join([]string{
		strings.ToLower(domain),
		strings.ToUpper(recordType),
		strings.ToLower(hostname),
		address,
	}, dnsRecordIDSeparator)
}

func resourceNamecheapDNSRecordCreate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	ncMutexKV.Lock(domain)
	defer ncMutexKV.Unlock(domain)

	record := dnsRecordFromData(data)

	// A record with the same identity already present would be created twice by
	// setHosts, leaving a duplicate the selector can no longer tell apart.
	exists, existing, diags := dnsRecordLookup(ctx, client, domain, record)
	if diags.HasError() {
		return diags
	}
	if exists {
		return diag.Diagnostics{{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("DNS record already exists on %s", domain),
			// The ID is rendered from the configuration, not from the API's echo of
			// the record: they denote the same record either way, and importing with
			// the spelling the configuration uses is what leaves the first plan empty.
			Detail: fmt.Sprintf("A %s record for %q pointing at %q already exists (TTL %d). Import it instead of creating it:\n\n"+
				"  terraform import <resource address> %s",
				derefString(existing.Type), derefString(existing.Name), derefString(existing.Address), derefInt(existing.TTL),
				dnsRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string))),
		}}
	}

	_, err := client.DomainsDNS.AddRecordsWithContext(ctx, domain,
		[]namecheap.DomainsDNSHostRecord{record},
		namecheap.WithRetryOnConflict(dnsRecordRetryAttempts))
	if err != nil {
		return dnsRecordWriteError(domain, "create", err)
	}

	data.SetId(dnsRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string)))
	return resourceNamecheapDNSRecordRead(ctx, data, meta)
}

func resourceNamecheapDNSRecordRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	record := dnsRecordFromData(data)
	found, live, diags := dnsRecordLookup(ctx, client, domain, record)
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
	// what the record was just *found* by — dnsRecordMatches compares them
	// normalized — so state already describes this record, and overwriting them
	// with the API's own spelling is precisely what must not happen: the API
	// lower-cases a host and adds a trailing dot to a CNAME target, neither of
	// which a configuration writes. Storing that spelling puts state permanently at
	// odds with the config, which for the ForceNew fields plans a destroy and
	// recreate of a live record on every single apply.
	_ = data.Set("ttl", derefInt(live.TTL))
	_ = data.Set("mx_pref", derefInt(live.MXPref))

	data.SetId(dnsRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string)))
	return nil
}

func resourceNamecheapDNSRecordUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	ncMutexKV.Lock(domain)
	defer ncMutexKV.Unlock(domain)

	// domain, hostname and type are ForceNew, so only address, ttl and mx_pref can
	// reach here — and the record to change is still the pre-change one, which is
	// what both the ambiguity check and the selector have to describe.
	before := dnsRecordBeforeChange(data)
	if _, _, diags := dnsRecordLookup(ctx, client, domain, before); diags.HasError() {
		return diags
	}

	_, err := client.DomainsDNS.UpsertRecordsWithContext(ctx, domain, dnsRecordSelector(before),
		[]namecheap.DomainsDNSHostRecord{dnsRecordFromData(data)},
		namecheap.WithRetryOnConflict(dnsRecordRetryAttempts))
	if err != nil {
		// The write did not happen, so state must keep describing the record that
		// is still in the zone. See dnsRecordRestoreBeforeChange.
		dnsRecordRestoreBeforeChange(data)
		return dnsRecordWriteError(domain, "update", err)
	}

	data.SetId(dnsRecordID(domain, data.Get("type").(string), data.Get("hostname").(string), data.Get("address").(string)))
	return resourceNamecheapDNSRecordRead(ctx, data, meta)
}

func resourceNamecheapDNSRecordDelete(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	ncMutexKV.Lock(domain)
	defer ncMutexKV.Unlock(domain)

	record := dnsRecordFromData(data)

	// Refuse to delete what cannot be picked out of the zone unambiguously, and
	// skip the write entirely when the record is already gone — rewriting a whole
	// zone to remove nothing is a race waiting to happen.
	found, _, diags := dnsRecordLookup(ctx, client, domain, record)
	if diags.HasError() {
		return diags
	}
	if !found {
		data.SetId("")
		return nil
	}

	_, err := client.DomainsDNS.DeleteRecordsWithContext(ctx, domain, dnsRecordSelector(record),
		namecheap.WithRetryOnConflict(dnsRecordRetryAttempts))
	if err != nil {
		return dnsRecordWriteError(domain, "delete", err)
	}

	data.SetId("")
	return nil
}

func resourceNamecheapDNSRecordImport(ctx context.Context, data *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	parts := strings.Split(data.Id(), dnsRecordIDSeparator)
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid import ID %q: expected %q, e.g. %q",
			data.Id(), "<domain>/<type>/<hostname>/<address>", "example.com/A/www/10.0.0.1")
	}

	// The address may itself contain the separator (a URL record, say), so only
	// the first three components are split off and the rest is the address.
	domain, recordType, hostname := parts[0], parts[1], parts[2]
	address := strings.Join(parts[3:], dnsRecordIDSeparator)

	for name, value := range map[string]string{"domain": domain, "type": recordType, "hostname": hostname, "address": address} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("invalid import ID %q: the %s component is empty", data.Id(), name)
		}
	}

	// The MX preference is not part of the import ID, so it is left unset here:
	// dnsRecordMatches then ignores it, and two MX records differing only in
	// preference come back as an ambiguous match rather than a coin flip.
	want := namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(hostname),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
	}

	client := meta.(*namecheap.Client)
	found, live, diags := dnsRecordLookup(ctx, client, strings.ToLower(domain), want)
	if diags.HasError() {
		return nil, dnsRecordImportError(domain, diags)
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
	data.SetId(dnsRecordID(domain, derefString(live.Type), hostname, address))

	return []*schema.ResourceData{data}, nil
}

// dnsRecordLookup finds the one record matching want in domain's live zone. It
// reports whether that record exists and returns the live copy, so callers read
// back the API's own view of it (its TTL and MX preference) rather than the
// configuration's.
//
// Several records matching want is an error, not a pick-the-first: the SDK applies
// a change to every record a selector matches, so continuing would update or
// delete all of them.
func dnsRecordLookup(ctx context.Context, client *namecheap.Client, domain string, want namecheap.DomainsDNSHostRecord) (bool, namecheap.DomainsDNSHostRecordDetailed, diag.Diagnostics) {
	var zero namecheap.DomainsDNSHostRecordDetailed

	matches, diags := dnsRecordMatches(ctx, client, domain, want)
	if diags.HasError() {
		return false, zero, diags
	}
	switch len(matches) {
	case 0:
		return false, zero, nil
	case 1:
		return true, matches[0], nil
	default:
		return false, zero, dnsRecordAmbiguousError(domain, want, matches)
	}
}

// dnsRecordMatches returns every record in domain's live zone that shares want's
// identity.
//
// Identity is host, type and address — plus the MX preference for MX records, and
// then only when want carries one (import does not know it until the record is
// found). TTL is never part of it: matching on it would make a TTL changed in the
// dashboard look like a deletion. Normalization is the SDK's, so the API's own
// spelling of an address — a trailing dot on a CNAME target, say — still matches
// the configuration's.
func dnsRecordMatches(ctx context.Context, client *namecheap.Client, domain string, want namecheap.DomainsDNSHostRecord) ([]namecheap.DomainsDNSHostRecordDetailed, diag.Diagnostics) {
	resp, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return nil, dataSourceDomainReadError(domain, err)
	}
	if resp == nil || resp.DomainDNSGetHostsResult == nil || resp.DomainDNSGetHostsResult.Hosts == nil {
		return nil, nil
	}

	target := namecheap.NormalizeRecord(want)
	matchMXPref := dnsRecordMXPrefIsIdentity(derefString(target.RecordType)) && target.MXPref != nil

	var matches []namecheap.DomainsDNSHostRecordDetailed
	for _, host := range *resp.DomainDNSGetHostsResult.Hosts {
		live := namecheap.NormalizeRecord(namecheap.RecordFromDetailed(host))
		if derefString(live.HostName) != derefString(target.HostName) ||
			derefString(live.RecordType) != derefString(target.RecordType) ||
			derefString(live.Address) != derefString(target.Address) {
			continue
		}
		if matchMXPref && (live.MXPref == nil || *live.MXPref != *target.MXPref) {
			continue
		}
		matches = append(matches, host)
	}
	return matches, nil
}

// dnsRecordAmbiguousError reports a zone holding several records this resource
// cannot tell apart, listing what distinguishes them so the user can go and look.
func dnsRecordAmbiguousError(domain string, want namecheap.DomainsDNSHostRecord, matches []namecheap.DomainsDNSHostRecordDetailed) diag.Diagnostics {
	candidates := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := fmt.Sprintf("  - TTL %d", derefInt(match.TTL))
		if dnsRecordMXPrefIsIdentity(derefString(match.Type)) {
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

// dnsRecordImportError flattens diagnostics into the single error the importer
// interface allows, keeping the detail: for an ambiguous match that detail is the
// list of candidates, which is the whole value of the message.
func dnsRecordImportError(domain string, diags diag.Diagnostics) error {
	if diags[0].Detail == "" {
		return fmt.Errorf("reading %s during import: %s", domain, diags[0].Summary)
	}
	return fmt.Errorf("reading %s during import: %s: %s", domain, diags[0].Summary, diags[0].Detail)
}

// dnsRecordWriteError turns an SDK write failure into diagnostics that name the
// domain and say what to do about it. A lost race and a mail-routing rejection
// are the two failures a user of this resource will actually hit, and neither is
// self-explanatory from the SDK's message alone.
func dnsRecordWriteError(domain, verb string, err error) diag.Diagnostics {
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
