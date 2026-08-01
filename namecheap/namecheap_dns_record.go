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
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     10,
				Description: "The MX preference, lower being preferred. Applies to MX records only; the API returns 10 for every other type, so leaving it at the default keeps plans empty.",
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

// dnsRecordSelector identifies this resource's record within the zone. Host,
// type and address together are the record's identity — the same triple the
// import ID uses — so an update that changes only the address selects on the
// address the record had before the change.
func dnsRecordSelector(domain, hostname, recordType, address string) namecheap.RecordSelector {
	return namecheap.RecordSelector{
		HostName:   namecheap.String(strings.ToLower(hostname)),
		RecordType: namecheap.String(strings.ToUpper(recordType)),
		Address:    namecheap.String(address),
	}
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
			Detail: fmt.Sprintf("A %s record for %q pointing at %q already exists (TTL %d). Import it instead of creating it:\n\n"+
				"  terraform import <resource address> %s",
				derefString(existing.Type), derefString(existing.Name), derefString(existing.Address), derefInt(existing.TTL),
				dnsRecordID(domain, derefString(existing.Type), derefString(existing.Name), derefString(existing.Address))),
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

	_ = data.Set("domain", domain)
	_ = data.Set("hostname", derefString(live.Name))
	_ = data.Set("type", derefString(live.Type))
	_ = data.Set("address", derefString(live.Address))
	_ = data.Set("ttl", derefInt(live.TTL))
	_ = data.Set("mx_pref", derefInt(live.MXPref))

	data.SetId(dnsRecordID(domain, derefString(live.Type), derefString(live.Name), derefString(live.Address)))
	return nil
}

func resourceNamecheapDNSRecordUpdate(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	ncMutexKV.Lock(domain)
	defer ncMutexKV.Unlock(domain)

	// domain, hostname and type are ForceNew, so only address, ttl and mx_pref
	// can reach here. The selector has to use the *old* address, since that is
	// what identifies the record still in the zone.
	oldAddress, _ := data.GetChange("address")
	selector := dnsRecordSelector(domain, data.Get("hostname").(string), data.Get("type").(string), oldAddress.(string))

	_, err := client.DomainsDNS.UpsertRecordsWithContext(ctx, domain, selector,
		[]namecheap.DomainsDNSHostRecord{dnsRecordFromData(data)},
		namecheap.WithRetryOnConflict(dnsRecordRetryAttempts))
	if err != nil {
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

	selector := dnsRecordSelector(domain, data.Get("hostname").(string), data.Get("type").(string), data.Get("address").(string))

	_, err := client.DomainsDNS.DeleteRecordsWithContext(ctx, domain, selector,
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

	_ = data.Set("domain", strings.ToLower(domain))
	_ = data.Set("type", strings.ToUpper(recordType))
	_ = data.Set("hostname", strings.ToLower(hostname))
	_ = data.Set("address", address)
	data.SetId(dnsRecordID(domain, recordType, hostname, address))

	client := meta.(*namecheap.Client)
	found, live, diags := dnsRecordLookup(ctx, client, strings.ToLower(domain), dnsRecordFromData(data))
	if diags.HasError() {
		return nil, fmt.Errorf("reading %s during import: %s", domain, diags[0].Summary)
	}
	if !found {
		return nil, fmt.Errorf("no %s record for %q pointing at %q exists on %s; "+
			"check the value matches the zone exactly (the API normalizes some addresses, e.g. a trailing dot on CNAME targets)",
			strings.ToUpper(recordType), hostname, address, domain)
	}

	// Adopt the API's own normalization so the first plan after import is empty.
	_ = data.Set("hostname", derefString(live.Name))
	_ = data.Set("type", derefString(live.Type))
	_ = data.Set("address", derefString(live.Address))
	_ = data.Set("ttl", derefInt(live.TTL))
	_ = data.Set("mx_pref", derefInt(live.MXPref))
	data.SetId(dnsRecordID(domain, derefString(live.Type), derefString(live.Name), derefString(live.Address)))

	return []*schema.ResourceData{data}, nil
}

// dnsRecordLookup finds the record matching want in domain's live zone. It
// reports whether exactly that record exists and returns the live copy, so
// callers read back the API's own normalization rather than the configuration's
// spelling of it.
func dnsRecordLookup(ctx context.Context, client *namecheap.Client, domain string, want namecheap.DomainsDNSHostRecord) (bool, namecheap.DomainsDNSHostRecordDetailed, diag.Diagnostics) {
	var zero namecheap.DomainsDNSHostRecordDetailed

	resp, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return false, zero, dataSourceDomainReadError(domain, err)
	}
	if resp == nil || resp.DomainDNSGetHostsResult == nil || resp.DomainDNSGetHostsResult.Hosts == nil {
		return false, zero, nil
	}

	// Match on identity only — host, type and address. TTL and MX preference are
	// attributes of a record, not part of what makes it that record: matching on
	// them would make a TTL changed in the dashboard look like a deletion, and
	// would fail during import, where they are not known until the record is
	// found. Normalization is the SDK's, so the API's own spelling of an address
	// (a trailing dot on a CNAME target, say) still matches the configuration's.
	target := namecheap.NormalizeRecord(want)
	for _, host := range *resp.DomainDNSGetHostsResult.Hosts {
		live := namecheap.NormalizeRecord(namecheap.RecordFromDetailed(host))
		if derefString(live.HostName) == derefString(target.HostName) &&
			derefString(live.RecordType) == derefString(target.RecordType) &&
			derefString(live.Address) == derefString(target.Address) {
			return true, host, nil
		}
	}
	return false, zero, nil
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
