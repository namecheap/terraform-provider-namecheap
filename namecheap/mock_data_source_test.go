//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccMockDataSourceDomain reads a single domain via namecheap_domain against
// the stateful mock and asserts the exported attributes. It exercises the
// three-call design end-to-end: getInfo supplies almost everything (DNS fields,
// dates, whois_guard), namecheap.domains.getList supplies auto_renew plus the
// authoritative is_expired flag, and namecheap.domains.getRegistrarLock supplies
// is_locked.
func TestAccMockDataSourceDomain(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "info-example.com"
	m.seedInfo(domain, mockDomainInfo{
		IsPremium:     true,
		IsPremiumDNS:  true,
		ProviderType:  "CUSTOM",
		IsUsingOurDNS: false,
		Nameservers:   []string{"ns1.example.net", "ns2.example.net"},
		Created:       "06/02/2021",
		Expires:       "06/02/2099",
		WhoisGuard:    "True", // getInfo vocabulary; must surface as ENABLED
	})
	// Seed the portfolio listing for the getList-sourced fields. Its dates and
	// whois state differ from getInfo's on purpose: the checks below prove
	// getInfo is the source for those attributes now. IsExpired=true contradicts
	// getInfo's 2099 expiry so the check proves the listing's own flag wins over
	// the date-derived fallback. IsLocked=false with the registrar lock on is
	// what the live API returns for a locked domain (#377): is_locked must
	// follow getRegistrarLock, not the listing.
	m.seedPortfolio(0,
		mockPortfolioDomain{ID: "10", Name: domain, User: "mock-user", Created: "01/01/2000", Expires: "01/01/2001", WhoisGuard: "DISABLED", IsExpired: true, IsLocked: false, AutoRenew: true, IsPremium: true, IsOurDNS: false},
	)
	m.seedRegistrarLock(domain, true)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "namecheap_domain" "test" {
  domain = "%s"
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					// getInfo-sourced.
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "dns_provider_type", "CUSTOM"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "is_our_dns", "false"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "is_premium", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "is_premium_dns", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "nameservers.#", "2"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "nameservers.0", "ns1.example.net"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "nameservers.1", "ns2.example.net"),
					// getInfo-sourced lifecycle fields (the seeded portfolio row
					// carries different values, proving the source).
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "created", "2021-06-02T00:00:00Z"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "expires", "2099-06-02T00:00:00Z"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "whois_guard", "ENABLED"),
					// getList-sourced fields (the only listing-dependent ones).
					// is_expired is the listing's flag, not the derived fallback.
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "is_expired", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "auto_renew", "true"),
					// getRegistrarLock-sourced: true although the listing says false.
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "is_locked", "true"),
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainRegistrarLockFallback: when getRegistrarLock fails,
// the read still succeeds and is_locked keeps the portfolio listing's flag.
func TestAccMockDataSourceDomainRegistrarLockFallback(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "lock-fallback-example.com"
	m.seedInfo(domain, mockDomainInfo{
		ProviderType:  "FREE",
		IsUsingOurDNS: true,
		Nameservers:   []string{"dns1.registrar-servers.com", "dns2.registrar-servers.com"},
		Created:       "06/02/2021",
		Expires:       "06/02/2099",
	})
	m.seedPortfolio(0,
		mockPortfolioDomain{ID: "11", Name: domain, User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "DISABLED", IsLocked: true, AutoRenew: true, IsOurDNS: true},
	)
	m.seedRegistrarLock(domain, false)
	m.failOn("namecheap.domains.getRegistrarLock", "2016166", "Domain is not associated with your account")

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "namecheap_domain" "test" {
  domain = "%s"
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "is_locked", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "auto_renew", "true"),
					func(*terraform.State) error {
						if got := m.commandCount("namecheap.domains.getRegistrarLock"); got == 0 {
							return fmt.Errorf("expected the read to call getRegistrarLock before falling back")
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainNotFound asserts that looking up a domain the
// account does not own yields a diagnostic that names the domain.
func TestAccMockDataSourceDomainNotFound(t *testing.T) {
	m := newNamecheapMock(t)
	const missing = "does-not-exist-9f8e7d.com"

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "namecheap_domain" "missing" {
  domain = "%s"
}
`, missing),
				ExpectError: regexp.MustCompile(regexp.QuoteMeta(missing)),
			},
		},
	})
}

// TestAccMockDataSourceDomainsEmpty asserts an empty portfolio reads cleanly as
// a zero-length list.
func TestAccMockDataSourceDomainsEmpty(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedPortfolio(0) // no domains

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_domains" "all" {
  list_type = "ALL"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.#", "0"),
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainsSinglePage reads a small portfolio that fits in a
// single page and asserts the mapped fields (including the RFC3339 date and
// expires_in_days convenience).
func TestAccMockDataSourceDomainsSinglePage(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedPortfolio(0,
		mockPortfolioDomain{ID: "1", Name: "alpha-example.com", User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", IsExpired: false, IsLocked: true, AutoRenew: true, IsPremium: false, IsOurDNS: true},
		mockPortfolioDomain{ID: "2", Name: "beta-example.net", User: "mock-user", Created: "01/15/2020", Expires: "01/15/2021", WhoisGuard: "DISABLED", IsExpired: true, IsLocked: false, AutoRenew: false, IsPremium: true, IsOurDNS: false},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_domains" "all" {
  list_type = "ALL"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.#", "2"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.name", "alpha-example.com"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.expires", "2099-06-02T00:00:00Z"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.is_locked", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.is_expired", "false"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.is_our_dns", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.name", "beta-example.net"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.is_expired", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.whois_guard", "DISABLED"),
					// beta expired on 2021-01-15, well in the past -> negative.
					resource.TestCheckResourceAttrWith("data.namecheap_domains.all", "domains.1.expires_in_days", func(v string) error {
						if v == "" || v[0] != '-' {
							return fmt.Errorf("expected a negative expires_in_days for an expired domain, got %q", v)
						}
						return nil
					}),
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainsPagination proves the data source paginates the
// full portfolio: with a mock page-size cap of 1 and three seeded domains, all
// three must be returned and the mock must have served getList at least once per
// page (a positive multiple of three).
func TestAccMockDataSourceDomainsPagination(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedPortfolio(1, // cap page size to 1 -> forces 3 pages
		mockPortfolioDomain{ID: "1", Name: "one-example.com", User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "2", Name: "two-example.com", User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "3", Name: "three-example.com", User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_domains" "all" {
  list_type = "ALL"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.#", "3"),
					func(*terraform.State) error {
						got := m.getListCallCount()
						if got < 3 || got%3 != 0 {
							return fmt.Errorf("expected getList calls to be a positive multiple of 3 (one per page, proving full pagination), got %d", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainsRegistrarLock pins include_registrar_lock. Each
// listing row's IsLocked disagrees with its getRegistrarLock status. Left at
// the default, the data source makes no getRegistrarLock call and reports the
// listing's flags. With the argument set, is_locked follows getRegistrarLock
// for each domain, in both directions.
func TestAccMockDataSourceDomainsRegistrarLock(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedPortfolio(0,
		mockPortfolioDomain{ID: "1", Name: "locked-example.com", User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", IsLocked: false, AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "2", Name: "unlocked-example.com", User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", IsLocked: true, AutoRenew: true, IsOurDNS: true},
	)
	m.seedRegistrarLock("locked-example.com", true)
	m.seedRegistrarLock("unlocked-example.com", false)

	const lockCommand = "namecheap.domains.getRegistrarLock"
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `data "namecheap_domains" "all" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "include_registrar_lock", "false"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.is_locked", "false"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.is_locked", "true"),
					func(*terraform.State) error {
						if got := m.commandCount(lockCommand); got != 0 {
							return fmt.Errorf("expected no getRegistrarLock calls without include_registrar_lock, got %d", got)
						}
						return nil
					},
				),
			},
			{
				Config: `
data "namecheap_domains" "all" {
  include_registrar_lock = true
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.#", "2"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.name", "locked-example.com"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.is_locked", "true"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.name", "unlocked-example.com"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.is_locked", "false"),
					func(*terraform.State) error {
						if got := m.commandCount(lockCommand); got < 2 || got%2 != 0 {
							return fmt.Errorf("expected getRegistrarLock calls to be a positive multiple of 2 (one per domain per read), got %d", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainRecords reads a domain's live record set, email
// type and nameservers via namecheap_domain_records, covering both a
// Namecheap-DNS domain (records exposed) and a custom-nameserver domain
// (nameservers exposed).
func TestAccMockDataSourceDomainRecords(t *testing.T) {
	m := newNamecheapMock(t)

	const ourDNS = "records-example.com"
	m.seed(ourDNS, []hostEntry{
		{Name: "@", Type: "A", Address: "10.0.0.1", MXPref: 10, TTL: 1800},
		{Name: "www", Type: "CNAME", Address: "records-example.com.", MXPref: 10, TTL: 3600},
	}, "MX", nil)

	const customNS = "custom-ns-example.com"
	m.seed(customNS, nil, "NONE", []string{"dns1.p01.nsone.net", "dns2.p01.nsone.net"})

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "namecheap_domain_records" "ourdns" {
  domain = "%s"
}

data "namecheap_domain_records" "customns" {
  domain = "%s"
}
`, ourDNS, customNS),
				Check: resource.ComposeTestCheckFunc(
					// Namecheap-DNS domain: records exposed, no nameservers.
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "email_type", "MX"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "nameservers.#", "0"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "records.#", "2"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "records.0.hostname", "@"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "records.0.type", "A"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "records.0.address", "10.0.0.1"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "records.0.ttl", "1800"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "records.1.hostname", "www"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.ourdns", "records.1.type", "CNAME"),
					// Custom-nameserver domain: nameservers exposed, no records.
					resource.TestCheckResourceAttr("data.namecheap_domain_records.customns", "nameservers.#", "2"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.customns", "nameservers.0", "dns1.p01.nsone.net"),
					resource.TestCheckResourceAttr("data.namecheap_domain_records.customns", "records.#", "0"),
				),
			},
		},
	})
}

// TestAccMockDataSourcePortfolioComposition exercises the headline composition
// pattern end-to-end: a namecheap_domains data source drives the creation of a
// uniform SPF record on every domain in the portfolio (data source -> 3 mock
// domains -> 3 planned record resources), asserting all three are applied
// against the mock backend and cleared on destroy.
//
// The documented user-facing pattern uses for_each keyed by domain name (see
// docs/data-sources/domains.md). The acceptance test uses count instead because
// the SDKv2 acceptance-test harness (terraform-plugin-testing's legacy state
// shim in helper/resource) cannot represent string-keyed for_each instances -
// count yields integer-indexed instances that the shim supports while
// exercising the identical data-source-driven composition.
func TestAccMockDataSourcePortfolioComposition(t *testing.T) {
	m := newNamecheapMock(t)
	domains := []string{"alpha-example.com", "beta-example.net", "gamma-example.org"}
	m.seedPortfolio(0,
		mockPortfolioDomain{ID: "1", Name: domains[0], User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "2", Name: domains[1], User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "3", Name: domains[2], User: "mock-user", Created: "06/02/2021", Expires: "06/02/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy: resource.ComposeTestCheckFunc(
			mockCheckHostsCleared(m, domains[0]),
			mockCheckHostsCleared(m, domains[1]),
			mockCheckHostsCleared(m, domains[2]),
		),
		Steps: []resource.TestStep{
			{
				Config: `
data "namecheap_domains" "all" {
  list_type = "ALL"
}

resource "namecheap_domain_records" "spf" {
  count = length(data.namecheap_domains.all.domains)

  domain = data.namecheap_domains.all.domains[count.index].name
  mode   = "OVERWRITE"

  record {
    hostname = "@"
    type     = "TXT"
    address  = "v=spf1 include:example.com -all"
    ttl      = 1800
  }
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.#", "3"),
					resource.TestCheckResourceAttr("namecheap_domain_records.spf.0", "record.#", "1"),
					mockCheckHostCount(m, domains[0], 1),
					mockCheckHostCount(m, domains[1], 1),
					mockCheckHostCount(m, domains[2], 1),
					mockCheckHostContains(m, domains[0], "@", "TXT", "v=spf1 include:example.com -all"),
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainsDateFormats seeds one portfolio row per date
// format the live API has been observed to send (captured in October 2026),
// so the mock mirrors real responses rather than the docs. Each row proves one
// rendering rule of the data source: calendar dates render as midnight UTC,
// a date with a time keeps its time (whether the AM/PM separator is an ASCII
// space or U+202F), a two-digit year is accepted, an empty value renders as an
// empty string, and a value the SDK cannot parse also renders as an empty
// string, with expires_in_days left at 0, instead of failing the read or
// leaking raw API text into an attribute documented as RFC3339.
func TestAccMockDataSourceDomainsDateFormats(t *testing.T) {
	m := newNamecheapMock(t)
	m.seedPortfolio(0,
		mockPortfolioDomain{ID: "1", Name: "padded.example", User: "mock-user", Created: "11/19/2020", Expires: "11/19/2099", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "2", Name: "time-space.example", User: "mock-user", Created: "4/30/2021 11:31:13 AM", Expires: "4/30/2099 11:31:13 AM", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "3", Name: "time-nnbsp.example", User: "mock-user", Created: "12/14/2020 1:01:07 PM", Expires: "12/14/2099 1:01:07 PM", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "4", Name: "two-digit-year.example", User: "mock-user", Created: "4/30/21", Expires: "4/30/40", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "5", Name: "empty-expiry.example", User: "mock-user", Created: "11/19/2020", Expires: "", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
		mockPortfolioDomain{ID: "6", Name: "unparseable.example", User: "mock-user", Created: "11/19/2020", Expires: "someday", WhoisGuard: "ENABLED", AutoRenew: true, IsOurDNS: true},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `data "namecheap_domains" "all" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.#", "6"),
					// zero-padded calendar date -> midnight UTC
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.created", "2020-11-19T00:00:00Z"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.0.expires", "2099-11-19T00:00:00Z"),
					// time with an ASCII space keeps its time
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.created", "2021-04-30T11:31:13Z"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.1.expires", "2099-04-30T11:31:13Z"),
					// time with U+202F before PM (the form a real renew returned)
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.2.created", "2020-12-14T13:01:07Z"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.2.expires", "2099-12-14T13:01:07Z"),
					resource.TestCheckResourceAttrWith("data.namecheap_domains.all", "domains.2.expires_in_days", func(v string) error {
						if v == "0" || v[0] == '-' {
							return fmt.Errorf("expected a positive expires_in_days for a 2099 expiry, got %q", v)
						}
						return nil
					}),
					// two-digit year (Go's rule: 69-99 -> 19xx, 00-68 -> 20xx)
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.3.created", "2021-04-30T00:00:00Z"),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.3.expires", "2040-04-30T00:00:00Z"),
					// empty element -> empty string, 0 days
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.4.expires", ""),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.4.expires_in_days", "0"),
					// unparseable -> empty, 0 days (never raw text, never a count from the zero time)
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.5.expires", ""),
					resource.TestCheckResourceAttr("data.namecheap_domains.all", "domains.5.expires_in_days", "0"),
				),
			},
		},
	})
}

// TestAccMockDataSourceDomainTimeBearingDates reads a single domain whose
// getInfo dates carry a time component (the form other commands have been
// observed to return), proving namecheap_domain renders the instant rather
// than flattening it to midnight or rejecting the response.
func TestAccMockDataSourceDomainTimeBearingDates(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "timed-example.com"
	m.seedInfo(domain, mockDomainInfo{
		ProviderType:  "FREE",
		IsUsingOurDNS: true,
		Nameservers:   []string{"dns1.registrar-servers.com", "dns2.registrar-servers.com"},
		Created:       "12/14/2020 1:01:07 PM",
		Expires:       "12/14/2099 1:01:07 PM",
		WhoisGuard:    "False",
	})
	m.seedPortfolio(0,
		mockPortfolioDomain{ID: "20", Name: domain, User: "mock-user", Created: "12/14/2020", Expires: "12/14/2099", WhoisGuard: "DISABLED", AutoRenew: true, IsOurDNS: true},
	)
	m.seedRegistrarLock(domain, true)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "namecheap_domain" "test" {
  domain = "%s"
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "created", "2020-12-14T13:01:07Z"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "expires", "2099-12-14T13:01:07Z"),
					resource.TestCheckResourceAttr("data.namecheap_domain.test", "is_expired", "false"),
				),
			},
		},
	})
}
