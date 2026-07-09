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
// rather than from getInfo. They are a subset of domainPortfolioElemSchema so
// the single-domain and portfolio shapes stay consistent.
var domainLifecycleAttrs = []string{
	"created", "expires", "expires_in_days",
	"is_expired", "is_locked", "auto_renew", "whois_guard",
}

// setDomainLifecycleFromList fetches domain from the account portfolio listing
// and copies the lifecycle attributes (see domainLifecycleAttrs) onto data.
//
// getList's SearchTerm is a substring keyword filter, not an exact lookup, so
// the exact domain is matched client-side (case-insensitively) among the
// returned rows. A transport/API error is surfaced (named with the domain); a
// clean response that simply does not contain the domain is treated as "no
// lifecycle data available" and leaves the fields at their zero values, because
// the caller has already confirmed the domain exists via getInfo.
func setDomainLifecycleFromList(ctx context.Context, client *namecheap.Client, data *schema.ResourceData, domain string) diag.Diagnostics {
	resp, err := client.Domains.GetListWithContext(ctx, &namecheap.DomainsGetListArgs{
		SearchTerm: namecheap.String(domain),
		Page:       namecheap.Int(1),
		PageSize:   namecheap.Int(domainsPageSize),
	})
	if err != nil {
		return dataSourceDomainReadError(domain, err)
	}
	if resp == nil || resp.Domains == nil {
		return nil
	}

	now := time.Now().UTC()
	for i := range *resp.Domains {
		d := &(*resp.Domains)[i]
		if d.Name == nil || !strings.EqualFold(*d.Name, domain) {
			continue
		}
		flat := flattenPortfolioDomain(d, now)
		for _, attr := range domainLifecycleAttrs {
			_ = data.Set(attr, flat[attr])
		}
		break
	}
	return nil
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
// A nil pointer renders as an empty string.
func formatDateTime(dt *namecheap.DateTime) string {
	if dt == nil {
		return ""
	}
	return dt.Time.UTC().Format(time.RFC3339)
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
// described by domainRecordElemSchema.
func flattenHostRecord(h *namecheap.DomainsDNSHostRecordDetailed) map[string]interface{} {
	return map[string]interface{}{
		"hostname": derefString(h.Name),
		"type":     derefString(h.Type),
		"address":  derefString(h.Address),
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
