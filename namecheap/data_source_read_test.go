package namecheap_provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the data-source ReadContext functions directly against an
// in-process httptest server, driving the real go-namecheap-sdk client. Unlike
// the testacc mock suite (which the coverage upload does not observe because it
// is gated behind the `testacc` build tag), these are ordinary unit tests, so
// they exercise — and count coverage for — the read/pagination/error paths.

// newDataSourceTestClient builds an SDK client whose requests are directed at
// baseURL, with client-side rate limiting disabled so a test issuing several
// calls does not stall on the limiter.
func newDataSourceTestClient(baseURL string) *namecheap.Client {
	client := namecheap.NewClient(&namecheap.ClientOptions{
		UserName:  "unit-user",
		ApiUser:   "unit-user",
		ApiKey:    "unit-key",
		ClientIp:  "127.0.0.1",
		RateLimit: &namecheap.RateLimitOptions{Disabled: true},
	})
	client.BaseURL = baseURL
	return client
}

// startDataSourceServer starts an httptest server routing on the request's
// Command form value to handler, returns an SDK client bound to it, and
// registers server shutdown with t.Cleanup.
func startDataSourceServer(t *testing.T, handler func(command string, r *http.Request) string) *namecheap.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, handler(r.FormValue("Command"), r))
	}))
	t.Cleanup(srv.Close)
	return newDataSourceTestClient(srv.URL)
}

// --- XML builders (mirror the real API envelopes the SDK parses) ------------

func xmlGetInfo(domain string, isPremium, isPremiumDNS bool, providerType string, isOurDNS bool, nameservers []string) string {
	var ns []string
	for _, n := range nameservers {
		ns = append(ns, fmt.Sprintf(`<Nameserver>%s</Nameserver>`, n))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.domains.getInfo">
    <DomainGetInfoResult DomainName="%s" IsPremium="%t">
      <PremiumDnsSubscription><IsActive>%t</IsActive></PremiumDnsSubscription>
      <DnsDetails ProviderType="%s" IsUsingOurDNS="%t">
        %s
      </DnsDetails>
    </DomainGetInfoResult>
  </CommandResponse>
</ApiResponse>`, domain, isPremium, isPremiumDNS, providerType, isOurDNS, strings.Join(ns, "\n        "))
}

// dsDomainRow is a single portfolio row for building a getList response.
type dsDomainRow struct {
	ID, Name, User, Created, Expires, WhoisGuard        string
	IsExpired, IsLocked, AutoRenew, IsPremium, IsOurDNS bool
}

func xmlGetListPage(rows []dsDomainRow, total, page, pageSize int) string {
	var lines []string
	for _, d := range rows {
		lines = append(lines, fmt.Sprintf(
			`<Domain ID="%s" Name="%s" User="%s" Created="%s" Expires="%s" IsExpired="%t" IsLocked="%t" AutoRenew="%t" WhoisGuard="%s" IsPremium="%t" IsOurDNS="%t" />`,
			d.ID, d.Name, d.User, d.Created, d.Expires, d.IsExpired, d.IsLocked, d.AutoRenew, d.WhoisGuard, d.IsPremium, d.IsOurDNS))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.domains.getList">
    <DomainGetListResult>
      %s
    </DomainGetListResult>
    <Paging>
      <TotalItems>%d</TotalItems>
      <CurrentPage>%d</CurrentPage>
      <PageSize>%d</PageSize>
    </Paging>
  </CommandResponse>
</ApiResponse>`, strings.Join(lines, "\n      "), total, page, pageSize)
}

func xmlDNSGetList(domain string, isUsingOurDNS bool, nameservers []string) string {
	var ns []string
	for _, n := range nameservers {
		ns = append(ns, fmt.Sprintf(`<Nameserver>%s</Nameserver>`, n))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.domains.dns.getList">
    <DomainDNSGetListResult Domain="%s" IsUsingOurDNS="%t" IsPremiumDNS="false" IsUsingFreeDNS="false">
      %s
    </DomainDNSGetListResult>
  </CommandResponse>
</ApiResponse>`, domain, isUsingOurDNS, strings.Join(ns, "\n      "))
}

type dsHost struct {
	Name, Type, Address string
	MXPref, TTL         int
}

func xmlDNSGetHosts(domain, emailType string, hosts []dsHost) string {
	var lines []string
	for i, h := range hosts {
		lines = append(lines, fmt.Sprintf(
			`<host HostId="%d" Name="%s" Type="%s" Address="%s" MXPref="%d" TTL="%d" AssociatedAppTitle="" FriendlyName="" IsActive="true" IsDDNSEnabled="false" />`,
			i+1, h.Name, h.Type, h.Address, h.MXPref, h.TTL))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.domains.dns.getHosts">
    <DomainDNSGetHostsResult Domain="%s" EmailType="%s" IsUsingOurDNS="true">
      %s
    </DomainDNSGetHostsResult>
  </CommandResponse>
</ApiResponse>`, domain, emailType, strings.Join(lines, "\n      "))
}

// --- namecheap_domain --------------------------------------------------------

func TestDataSourceDomainRead_Success(t *testing.T) {
	const domain = "example.com"
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		switch command {
		case "namecheap.domains.getInfo":
			return xmlGetInfo(domain, true, true, "CUSTOM", false, []string{"ns1.example.net", "ns2.example.net"})
		case "namecheap.domains.getList":
			return xmlGetListPage([]dsDomainRow{
				{ID: "42", Name: domain, User: "u", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", IsLocked: true, AutoRenew: true, IsOurDNS: true},
			}, 1, 1, domainsPageSize)
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomain().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	// getInfo-sourced fields.
	assert.Equal(t, "CUSTOM", d.Get("dns_provider_type"))
	assert.Equal(t, false, d.Get("is_our_dns"))
	assert.Equal(t, true, d.Get("is_premium"))
	assert.Equal(t, true, d.Get("is_premium_dns"))
	assert.Equal(t, []interface{}{"ns1.example.net", "ns2.example.net"}, d.Get("nameservers"))
	// getList-sourced lifecycle fields (the gap this change closes).
	assert.Equal(t, "2099-06-02T00:00:00Z", d.Get("expires"))
	assert.Equal(t, "2021-06-02T00:00:00Z", d.Get("created"))
	assert.Equal(t, true, d.Get("is_locked"))
	assert.Equal(t, true, d.Get("auto_renew"))
	assert.Equal(t, "ENABLED", d.Get("whois_guard"))
	assert.Equal(t, false, d.Get("is_expired"))
	assert.Greater(t, d.Get("expires_in_days").(int), 0)
	assert.Equal(t, domain, d.Id())
}

func TestDataSourceDomainRead_NotFound(t *testing.T) {
	const domain = "does-not-exist-abc123.com"
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		return apiErrorXML("2019166", fmt.Sprintf("Domain %q not found", domain))
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomain().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "expected an error for an unknown domain")
	assert.Contains(t, diags[0].Summary, domain, "diagnostic should name the domain")
}

func TestDataSourceDomainRead_LifecycleMissing(t *testing.T) {
	const domain = "example.com"
	// getInfo confirms the domain exists, but the portfolio listing returns a
	// different domain, so the exact-match finds nothing: lifecycle fields stay
	// at their zero values and the read still succeeds.
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		switch command {
		case "namecheap.domains.getInfo":
			return xmlGetInfo(domain, false, false, "NAMECHEAP", true, []string{"dns1.registrar-servers.com"})
		case "namecheap.domains.getList":
			return xmlGetListPage([]dsDomainRow{
				{ID: "7", Name: "other-domain.com", User: "u", Created: "01/01/2020", Expires: "01/01/2030"},
			}, 1, 1, domainsPageSize)
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomain().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)
	assert.Equal(t, "NAMECHEAP", d.Get("dns_provider_type"))
	assert.Equal(t, "", d.Get("expires"), "lifecycle field should be empty when the domain is absent from the listing")
	assert.Equal(t, "", d.Get("whois_guard"))
}

func TestDataSourceDomainRead_GetListError(t *testing.T) {
	const domain = "example.com"
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		switch command {
		case "namecheap.domains.getInfo":
			return xmlGetInfo(domain, false, false, "NAMECHEAP", true, nil)
		case "namecheap.domains.getList":
			return apiErrorXML("4022336", "portfolio listing failed")
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomain().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "a getList transport/API error should surface")
	assert.Contains(t, diags[0].Summary, domain)
}

// --- namecheap_domains -------------------------------------------------------

func TestDataSourceDomainsRead_MultiPage(t *testing.T) {
	rows := []dsDomainRow{
		{ID: "1", Name: "one-example.com", User: "u", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		{ID: "2", Name: "two-example.com", User: "u", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		{ID: "3", Name: "three-example.com", User: "u", Created: "06/02/2021", Expires: "01/01/2000", WhoisGuard: "DISABLED", IsExpired: true},
	}
	var mu sync.Mutex
	var calls int
	const pageSize = 1 // force one row per page -> three pages
	client := startDataSourceServer(t, func(command string, r *http.Request) string {
		if command != "namecheap.domains.getList" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		mu.Lock()
		calls++
		mu.Unlock()
		page, _ := strconv.Atoi(r.FormValue("Page"))
		if page < 1 {
			page = 1
		}
		start := (page - 1) * pageSize
		end := start + pageSize
		if start > len(rows) {
			start = len(rows)
		}
		if end > len(rows) {
			end = len(rows)
		}
		return xmlGetListPage(rows[start:end], len(rows), page, pageSize)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomains().Schema, map[string]interface{}{"list_type": "ALL"})
	diags := dataSourceNamecheapDomainsRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	domains := d.Get("domains").([]interface{})
	require.Len(t, domains, 3, "all three domains across all pages must be returned")
	assert.GreaterOrEqual(t, calls, 3, "expected at least one getList call per page")

	first := domains[0].(map[string]interface{})
	assert.Equal(t, "one-example.com", first["name"])
	assert.Equal(t, "2099-06-02T00:00:00Z", first["expires"])
	assert.Greater(t, first["expires_in_days"].(int), 0)

	third := domains[2].(map[string]interface{})
	assert.Equal(t, true, third["is_expired"])
	assert.Less(t, third["expires_in_days"].(int), 0, "an expired domain should have a negative expires_in_days")
}

func TestDataSourceDomainsRead_Empty(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command == "namecheap.domains.getList" {
			return xmlGetListPage(nil, 0, 1, domainsPageSize)
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomains().Schema, map[string]interface{}{"list_type": "ALL"})
	diags := dataSourceNamecheapDomainsRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "an empty portfolio must not be an error")
	assert.Empty(t, d.Get("domains").([]interface{}))
}

func TestDataSourceDomainsRead_Error(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		return apiErrorXML("4022336", "listing failed")
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomains().Schema, map[string]interface{}{"list_type": "ALL"})
	diags := dataSourceNamecheapDomainsRead(context.Background(), d, client)
	assert.True(t, diags.HasError(), "a getList API error should surface")
}

// --- namecheap_domain_records ------------------------------------------------

func TestDataSourceDomainRecordsRead_OurDNS(t *testing.T) {
	const domain = "records-example.com"
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		switch command {
		case "namecheap.domains.dns.getList":
			return xmlDNSGetList(domain, true, nil)
		case "namecheap.domains.dns.getHosts":
			return xmlDNSGetHosts(domain, "MX", []dsHost{
				{Name: "@", Type: "A", Address: "10.0.0.1", MXPref: 10, TTL: 1800},
				{Name: "www", Type: "CNAME", Address: "records-example.com.", MXPref: 10, TTL: 3600},
			})
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomainRecords().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRecordsRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	assert.Equal(t, "MX", d.Get("email_type"))
	assert.Empty(t, d.Get("nameservers").([]interface{}))
	records := d.Get("records").([]interface{})
	require.Len(t, records, 2)
	first := records[0].(map[string]interface{})
	assert.Equal(t, "@", first["hostname"])
	assert.Equal(t, "A", first["type"])
	assert.Equal(t, "10.0.0.1", first["address"])
	assert.Equal(t, 1800, first["ttl"])
	assert.Equal(t, domain, d.Id())
}

func TestDataSourceDomainRecordsRead_CustomNS(t *testing.T) {
	const domain = "custom-ns-example.com"
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		switch command {
		case "namecheap.domains.dns.getList":
			return xmlDNSGetList(domain, false, []string{"dns1.p01.nsone.net", "dns2.p01.nsone.net"})
		case "namecheap.domains.dns.getHosts":
			return xmlDNSGetHosts(domain, "NONE", nil)
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomainRecords().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRecordsRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	ns := d.Get("nameservers").([]interface{})
	require.Len(t, ns, 2)
	assert.Equal(t, "dns1.p01.nsone.net", ns[0])
	assert.Empty(t, d.Get("records").([]interface{}))
}

func TestDataSourceDomainRecordsRead_GetListError(t *testing.T) {
	const domain = "records-example.com"
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		return apiErrorXML("2019166", fmt.Sprintf("Domain %q not found", domain))
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomainRecords().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRecordsRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "a dns.getList error should surface")
	assert.Contains(t, diags[0].Summary, domain)
}

func TestDataSourceDomainRecordsRead_GetHostsError(t *testing.T) {
	const domain = "records-example.com"
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		switch command {
		case "namecheap.domains.dns.getList":
			return xmlDNSGetList(domain, true, nil)
		case "namecheap.domains.dns.getHosts":
			return apiErrorXML("4022336", "getHosts failed")
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapDomainRecords().Schema, map[string]interface{}{"domain": domain})
	diags := dataSourceNamecheapDomainRecordsRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "a dns.getHosts error should surface")
	assert.Contains(t, diags[0].Summary, domain)
}
