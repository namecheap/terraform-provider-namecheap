package namecheap_provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// domainLifecycleAttrs are the attributes exported by the namecheap_domain data
// source that originate from the portfolio listing (namecheap.domains.getList)
// rather than from getInfo. Since SDK v2.10.1 maps getInfo's full response,
// only the registrar-lock and domain auto-renew booleans (which getInfo does
// not carry at all) and is_expired still come from the listing. is_expired is
// listed here so the API's own flag — which accounts for renewal grace periods
// and matches the namecheap_domains data source — overwrites the fallback value
// derived from getInfo's expiry date and status.
var domainLifecycleAttrs = []string{
	"is_expired", "is_locked", "auto_renew",
}

// fetchAllDomains pages through namecheap.domains.getList for the given filters,
// returning every matching domain across all pages. It is shared by the
// namecheap_domains portfolio read and by setDomainLifecycleFromList so both walk
// the whole result set rather than a single page. Page/PageSize are managed here
// (PageSize is the documented maximum); listType/searchTerm map to the getList
// ListType/SearchTerm params (searchTerm is omitted when empty).
func fetchAllDomains(ctx context.Context, client *namecheap.Client, listType, searchTerm string) ([]namecheap.Domain, error) {
	var all []namecheap.Domain
	for page := 1; ; page++ {
		args := &namecheap.DomainsGetListArgs{
			ListType: namecheap.String(listType),
			Page:     namecheap.Int(page),
			PageSize: namecheap.Int(domainsPageSize),
		}
		if searchTerm != "" {
			args.SearchTerm = namecheap.String(searchTerm)
		}

		resp, err := client.Domains.GetListWithContext(ctx, args)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("empty response from Namecheap while listing domains (page %d)", page)
		}
		if resp.Domains != nil {
			all = append(all, *resp.Domains...)
		}

		// Stop when the paging block indicates every item has been fetched. When
		// paging is absent or degenerate, stop after the current page so a
		// malformed response cannot spin an unbounded loop.
		if resp.Paging == nil || resp.Paging.TotalItems == nil || resp.Paging.PageSize == nil || *resp.Paging.PageSize <= 0 {
			break
		}
		if page*(*resp.Paging.PageSize) >= *resp.Paging.TotalItems {
			break
		}
	}
	return all, nil
}

// setDomainLifecycleFromList fetches domain from the account portfolio listing
// and copies the lifecycle attributes (see domainLifecycleAttrs) onto data.
//
// getList's SearchTerm is a substring keyword filter, not an exact lookup, so it
// pages through the entire filtered result set and matches the exact domain
// client-side (case-insensitively) — a match must never be missed just because
// it landed on a later page. A transport/API error is surfaced (named with the
// domain). When the domain is genuinely absent from the listing (despite getInfo
// having confirmed it exists), the lifecycle fields are left at their zero values
// and a warning is emitted so the gap is visible rather than silent.
func setDomainLifecycleFromList(ctx context.Context, client *namecheap.Client, data *schema.ResourceData, domain string) diag.Diagnostics {
	domains, err := fetchAllDomains(ctx, client, domainsListTypeAll, domain)
	if err != nil {
		return dataSourceDomainReadError(domain, err)
	}

	now := time.Now().UTC()
	for i := range domains {
		d := &domains[i]
		if d.Name == nil || !strings.EqualFold(*d.Name, domain) {
			continue
		}
		flat := flattenPortfolioDomain(d, now)
		for _, attr := range domainLifecycleAttrs {
			_ = data.Set(attr, flat[attr])
		}
		return nil
	}

	return diag.Diagnostics{{
		Severity: diag.Warning,
		Summary:  fmt.Sprintf("Registrar lock and auto-renew unavailable for domain %q", domain),
		Detail: "getInfo confirmed the domain exists, but it did not appear in the " +
			"namecheap.domains.getList portfolio listing, so is_locked and auto_renew " +
			"were left at their zero values and is_expired was derived from the expiry " +
			"date and status instead of the listing's own flag. " +
			"This is unexpected; please report it if it persists.",
	}}
}

// dataSourceDomainReadError converts an SDK error from a domain-scoped read into
// diagnostics that name the domain. It reuses diagFromClientError so that known
// Namecheap error codes (e.g. 2019166 "Domain not found") keep their remediation
// text, while the summary is prefixed with the domain so a failed lookup clearly
// identifies which domain could not be read.
func dataSourceDomainReadError(domain string, err error) diag.Diagnostics {
	diags := diagFromClientError(err)
	for i := range diags {
		diags[i].Summary = fmt.Sprintf("%s (domain %q)", diags[i].Summary, domain)
	}
	return diags
}

// formatDateTime renders a Namecheap DateTime as an RFC3339 string. Namecheap
// returns lifecycle dates as calendar dates (the SDK parses "MM/DD/YYYY" with
// time.Parse, yielding midnight UTC), so the RFC3339 output is timezone-stable.
// A nil pointer renders as an empty string, as does the zero time — since SDK
// v2.10.1 an empty date element decodes to the zero value instead of erroring.
func formatDateTime(dt *namecheap.DateTime) string {
	if dt == nil || dt.IsZero() {
		return ""
	}
	return dt.Time.UTC().Format(time.RFC3339)
}

// formatPremiumDNSDate renders a PremiumDnsSubscription date — an ISO string
// without a zone designator, e.g. "2027-06-02T00:00:00" — as RFC3339 (UTC), so
// premium_dns_expires matches the format of every other date attribute. The
// zero time ("0001-01-01T00:00:00", the API's "never" sentinel, which can
// appear even on a live subscription) renders as an empty string. A value that
// fails to parse is passed through verbatim rather than dropped.
func formatPremiumDNSDate(s string) string {
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return s
	}
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// daysUntil returns the whole number of calendar days from now until target,
// negative when target is in the past. Both instants are reduced to their UTC
// calendar date before subtracting, so the result is independent of the wall
// clock time of day and of the local timezone. now is passed explicitly so the
// boundary behaviour is deterministically testable.
func daysUntil(target, now time.Time) int {
	targetDay := time.Date(target.UTC().Year(), target.UTC().Month(), target.UTC().Day(), 0, 0, 0, 0, time.UTC)
	nowDay := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return int(targetDay.Sub(nowDay).Hours() / 24)
}

// derefString returns the dereferenced value of a *string, or "" when nil.
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// derefBool returns the dereferenced value of a *bool, or false when nil.
func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

// derefInt returns the dereferenced value of a *int, or 0 when nil.
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// domainRecordElemSchema returns the schema for a single DNS host record as
// exported by the namecheap_domain_records data source. The attribute names and
// types intentionally mirror the record block of the namecheap_domain_records
// resource (see resourceNamecheapDomainRecords), so a data-source record object
// feeds straight into a resource record block without field remapping. This
// parity is asserted by TestDomainRecordFieldParity.
func domainRecordElemSchema() map[string]*schema.Schema {
	return map[string]*schema.Schema{
		"hostname": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Sub-domain/hostname of the record.",
		},
		"type": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Record type (e.g. A, AAAA, CNAME, MX, TXT).",
		},
		"address": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Record value (URL or IP address, depending on the record type).",
		},
		"mx_pref": {
			Type:        schema.TypeInt,
			Computed:    true,
			Description: "MX preference for the host. Applicable to MX records only.",
		},
		"ttl": {
			Type:        schema.TypeInt,
			Computed:    true,
			Description: "Time to live for the record, in seconds.",
		},
	}
}

// flattenHostRecord converts an SDK detailed host record into the map shape
// described by domainRecordElemSchema. The address is normalized the same way
// the namecheap_domain_records resource normalizes on read (via
// getFixedAddressOfRecord: a trailing dot for CNAME/ALIAS/NS/MX, quotes for CAA),
// so a record exported here composes into that resource without plan drift on
// those types. On a malformed value (e.g. a bad CAA) or a nil field it falls
// back to the raw address rather than failing the read.
func flattenHostRecord(h *namecheap.DomainsDNSHostRecordDetailed) map[string]interface{} {
	address := derefString(h.Address)
	if h.Type != nil && h.Address != nil {
		if fixed, err := getFixedAddressOfRecord(&namecheap.DomainsDNSHostRecord{
			HostName:   h.Name,
			RecordType: h.Type,
			Address:    h.Address,
		}); err == nil && fixed != nil {
			address = *fixed
		}
	}
	return map[string]interface{}{
		"hostname": derefString(h.Name),
		"type":     derefString(h.Type),
		"address":  address,
		"mx_pref":  derefInt(h.MXPref),
		"ttl":      derefInt(h.TTL),
	}
}

// domainPortfolioElemSchema returns the schema for a single domain as exported
// by the namecheap_domains portfolio data source.
func domainPortfolioElemSchema() map[string]*schema.Schema {
	return map[string]*schema.Schema{
		"id": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Namecheap internal domain identifier.",
		},
		"name": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "The domain name (e.g. example.com).",
		},
		"user": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "The account user the domain belongs to.",
		},
		"created": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Registration date as an RFC3339 timestamp (UTC).",
		},
		"expires": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Expiration date as an RFC3339 timestamp (UTC).",
		},
		"expires_in_days": {
			Type:        schema.TypeInt,
			Computed:    true,
			Description: "Whole calendar days until the domain expires (negative if already expired).",
		},
		"is_expired": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether the domain has expired.",
		},
		"is_locked": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether the registrar lock is enabled.",
		},
		"auto_renew": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether auto-renew is enabled.",
		},
		"whois_guard": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "WhoisGuard status (e.g. ENABLED, DISABLED, NOTPRESENT).",
		},
		"is_premium": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether the domain is a premium domain.",
		},
		"is_our_dns": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether the domain is using Namecheap's DNS.",
		},
	}
}

// flattenPortfolioDomain converts an SDK Domain (from getList) into the map
// shape described by domainPortfolioElemSchema. now is used to compute
// expires_in_days.
func flattenPortfolioDomain(d *namecheap.Domain, now time.Time) map[string]interface{} {
	m := map[string]interface{}{
		"id":              derefString(d.ID),
		"name":            derefString(d.Name),
		"user":            derefString(d.User),
		"created":         formatDateTime(d.Created),
		"expires":         formatDateTime(d.Expires),
		"expires_in_days": 0,
		"is_expired":      derefBool(d.IsExpired),
		"is_locked":       derefBool(d.IsLocked),
		"auto_renew":      derefBool(d.AutoRenew),
		"whois_guard":     derefString(d.WhoisGuard),
		"is_premium":      derefBool(d.IsPremium),
		"is_our_dns":      derefBool(d.IsOurDNS),
	}
	if d.Expires != nil {
		m["expires_in_days"] = daysUntil(d.Expires.Time, now)
	}
	return m
}
