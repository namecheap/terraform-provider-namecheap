//go:build testacc

package namecheap_provider

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// mockDefaultNameservers are the registrar nameservers the Namecheap API reports
// (with IsUsingOurDNS="true") for a domain that has not been switched to custom
// nameservers. They mirror the real API's default response.
var mockDefaultNameservers = []string{"dns1.registrar-servers.com", "dns2.registrar-servers.com"}

// mockDomainState is the persisted per-domain state of the mock API: the DNS
// host records, the email type, and any custom nameservers. An empty
// nameservers slice means the domain is using Namecheap's DNS.
type mockDomainState struct {
	hosts       []hostEntry
	emailType   string
	nameservers []string
	// personalNS holds registered personal (glue/vanity) nameservers for the
	// domain, keyed by nameserver host with its glue IP as the value. It backs
	// the namecheap.domains.ns.* commands, which are independent of the custom
	// nameserver assignment tracked by `nameservers`.
	personalNS map[string]string
	// contacts holds the four WHOIS contact blocks keyed by role prefix
	// ("Registrant", "Tech", "Admin", "AuxBilling"); each inner map is
	// fieldName -> value (e.g. "FirstName" -> "Jane"). nil until setContacts
	// is called.
	contacts map[string]map[string]string
	// forwards holds the domain's email forwarding table (mailbox alias ->
	// destination address), keyed exactly as setEmailForwarding received it.
	// nil until setEmailForwarding is called.
	forwards map[string]string
}

// namecheapMock is a minimal STATEFUL mock of the Namecheap DNS API, sufficient
// to drive a resource.Test lifecycle (create -> refresh -> update -> destroy)
// for namecheap_domain_records without touching the real API.
//
// Unlike the stateless per-command fixtures the CRUD unit tests use, this mock
// persists per-domain state across requests so that GetHosts/GetList reflect
// prior SetHosts/SetCustom/SetDefault calls — which is what a full Terraform
// lifecycle requires. It is a complement to, not a replacement for, the live
// sandbox suite: the live suite remains the source of truth for API-contract
// behavior.
type namecheapMock struct {
	server  *httptest.Server
	mu      sync.Mutex
	domains map[string]*mockDomainState

	// Portfolio (namecheap.domains.getList) state. portfolio holds the domains
	// the account "owns"; pageSizeCap, when >0, caps the effective page size so
	// a test can force multi-page pagination with a small seed; getListCalls
	// counts getList requests so a test can prove full pagination.
	portfolio    []mockPortfolioDomain
	pageSizeCap  int
	getListCalls int

	// getInfo (namecheap.domains.getInfo) state, keyed by domain name. A getInfo
	// request for a domain absent from this map returns a "Domain not found"
	// API error, exercising the not-found diagnostic.
	infos map[string]mockDomainInfo

	// Account-scoped money state. balances backs namecheap.users.getBalances
	// (nil until seeded, so an unseeded read surfaces an API error rather than
	// silently reporting zero funds); pricing backs namecheap.users.getPricing,
	// keyed by the lower-cased "action/product" pair the request narrows on.
	balances *mockAccountBalance
	pricing  map[string][]mockPriceTier

	// Optional fault injection: when failCommand is set, any request whose
	// Command equals it returns an API error with failCode/failMessage instead
	// of the normal response. Used to exercise the provider's error-surfacing.
	failCommand string
	failCode    string
	failMessage string

	// commandCounts tracks how many times each Command has been received,
	// so tests can assert call-count invariants (e.g. "no extra API calls
	// during plan/refresh", "exactly one added GetHosts per OVERWRITE apply").
	commandCounts map[string]int
}

// mockPortfolioDomain is one entry of the account portfolio returned by the
// stateful mock's namecheap.domains.getList handler. Created/Expires use the
// Namecheap "MM/DD/YYYY" wire format.
type mockPortfolioDomain struct {
	ID         string
	Name       string
	User       string
	Created    string
	Expires    string
	WhoisGuard string
	IsExpired  bool
	IsLocked   bool
	AutoRenew  bool
	IsPremium  bool
	IsOurDNS   bool
}

// mockAccountBalance is the account funds returned by the mock's
// namecheap.users.getBalances handler. Amounts are the exact decimal strings the
// API sends, so a test can assert that the provider never reformats them.
type mockAccountBalance struct {
	Currency                  string
	AvailableBalance          string
	AccountBalance            string
	EarnedAmount              string
	WithdrawableAmount        string
	FundsRequiredForAutoRenew string
}

// mockPriceTier is one <Price> node of the mock's namecheap.users.getPricing
// response. Currency and Promotion are rendered only when non-empty, mirroring
// the live API's habit of omitting them entirely rather than sending "".
type mockPriceTier struct {
	Duration                                            int
	DurationType                                        string
	Price, RegularPrice, YourPrice, Currency, Promotion string
}

// mockDomainInfo is the per-domain response of the mock's
// namecheap.domains.getInfo handler.
type mockDomainInfo struct {
	IsPremium     bool
	IsPremiumDNS  bool
	ProviderType  string
	IsUsingOurDNS bool
	Nameservers   []string
	// Full getInfo response fields (SDK v2.10.1 maps the whole subtree).
	// Created/Expires are MM/DD/YYYY; empty strings omit the elements.
	Created string
	Expires string
	// WhoisGuard uses getInfo's vocabulary: "True", "False" or "NotAlloted".
	// Empty omits the Whoisguard element entirely.
	WhoisGuard string
}

// newNamecheapMock starts a stateful mock server and registers its shutdown
// with t.Cleanup. The returned mock's server URL is what NAMECHEAP_API_URL must
// point at (via the testacc-build endpoint override) for the provider to talk
// to it.
func newNamecheapMock(t *testing.T) *namecheapMock {
	t.Helper()
	m := &namecheapMock{domains: map[string]*mockDomainState{}}
	m.server = httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(m.server.Close)
	return m
}

// url returns the base URL of the running mock server.
func (m *namecheapMock) url() string { return m.server.URL }

// failOn makes the mock return an API error (code/message) for every request
// whose Command equals the given command, until cleared. Safe to call while the
// server is running.
func (m *namecheapMock) failOn(command, code, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCommand = command
	m.failCode = code
	m.failMessage = message
}

// state returns the current mock state for a domain, or nil if the domain has
// never been touched. Callers must not mutate the returned pointer.
func (m *namecheapMock) state(domain string) *mockDomainState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.domains[domain]
}

// commandCount returns how many requests the mock has received for the given
// Command since it was created.
func (m *namecheapMock) commandCount(command string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.commandCounts[command]
}

// stateFor returns the state for a domain, creating it on first use.
func (m *namecheapMock) stateFor(domain string) *mockDomainState {
	st := m.domains[domain]
	if st == nil {
		st = &mockDomainState{emailType: "NONE"}
		m.domains[domain] = st
	}
	return st
}

// seed sets the initial backend state for a domain, simulating records,
// nameservers, or an email type that already exist before Terraform manages the
// domain. Call it before the provider issues any request (e.g. in a PreCheck).
func (m *namecheapMock) seed(domain string, hosts []hostEntry, emailType string, nameservers []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateFor(domain)
	st.hosts = hosts
	st.nameservers = nameservers
	if emailType != "" {
		st.emailType = emailType
	}
}

// seedForwards sets the initial email forwarding table for a domain,
// simulating rules that already exist before Terraform manages them. Call it
// before the provider issues any request (e.g. in a PreConfig/PreCheck).
// forwards is copied, so later mutation of the caller's map (or of the
// mock's own state) cannot alias the other.
func (m *namecheapMock) seedForwards(domain string, forwards map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateFor(domain)
	copied := make(map[string]string, len(forwards))
	for mailbox, dest := range forwards {
		copied[mailbox] = dest
	}
	st.forwards = copied
}

// addForward adds or overwrites a single forwarding rule for a domain,
// simulating an out-of-band change (e.g. made through the dashboard) after
// Terraform has already taken ownership of the resource. Safe to call
// concurrently with the mock server handling requests, unlike mutating the
// pointer returned by state() directly.
func (m *namecheapMock) addForward(domain, mailbox, destination string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateFor(domain)
	if st.forwards == nil {
		st.forwards = map[string]string{}
	}
	st.forwards[mailbox] = destination
}

// removeHost deletes a record from a domain's zone, simulating an out-of-band
// deletion (someone using the dashboard) after Terraform has taken ownership of
// it. Safe to call while the server is handling requests, unlike mutating the
// pointer returned by state().
func (m *namecheapMock) removeHost(domain, host, recordType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateFor(domain)
	kept := st.hosts[:0]
	for _, h := range st.hosts {
		if h.Name == host && h.Type == recordType {
			continue
		}
		kept = append(kept, h)
	}
	st.hosts = kept
}

// seedPortfolio sets the account portfolio returned by getList. cap, when >0,
// caps the per-page size so a small seed still spans multiple pages (used to
// exercise pagination end-to-end).
func (m *namecheapMock) seedPortfolio(cap int, domains ...mockPortfolioDomain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.portfolio = domains
	m.pageSizeCap = cap
}

// seedInfo registers the getInfo response for a domain.
func (m *namecheapMock) seedInfo(domain string, info mockDomainInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.infos == nil {
		m.infos = map[string]mockDomainInfo{}
	}
	m.infos[domain] = info
}

// seedBalances registers the account funds returned by users.getBalances. Until
// it is called, a balance read returns an API error rather than zero funds, so a
// test cannot accidentally assert against an implicit empty account.
func (m *namecheapMock) seedBalances(b mockAccountBalance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balances = &b
}

// seedPricing registers the price tiers users.getPricing returns for one
// (action, product) pair, e.g. seedPricing("REGISTER", "com", tiers...). Both
// keys are matched case-insensitively, as the real API does.
func (m *namecheapMock) seedPricing(action, product string, tiers ...mockPriceTier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pricing == nil {
		m.pricing = map[string][]mockPriceTier{}
	}
	m.pricing[pricingKey(action, product)] = tiers
}

// pricingKey builds the lookup key for the seeded pricing map.
func pricingKey(action, product string) string {
	return strings.ToLower(action) + "/" + strings.ToLower(product)
}

// getListCallCount returns how many getList requests the mock has served.
func (m *namecheapMock) getListCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getListCalls
}

func (m *namecheapMock) handler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	command := r.FormValue("Command")
	domain := r.FormValue("SLD") + "." + r.FormValue("TLD")
	// The contacts commands identify the domain with a single DomainName
	// parameter rather than the split SLD/TLD the DNS commands use.
	if dn := r.FormValue("DomainName"); dn != "" {
		domain = dn
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.commandCounts == nil {
		m.commandCounts = map[string]int{}
	}
	m.commandCounts[command]++

	w.Header().Set("Content-Type", "text/xml")

	// Fault injection: return an API error for the configured command.
	if m.failCommand != "" && command == m.failCommand {
		_, _ = io.WriteString(w, apiErrorXML(m.failCode, m.failMessage))
		return
	}

	// Portfolio and getInfo commands are account-/domain-scoped rather than
	// keyed on the DNS SLD.TLD state, so handle them before touching st.
	switch command {
	case "namecheap.domains.getList":
		m.getListCalls++
		page, _ := strconv.Atoi(r.FormValue("Page"))
		if page < 1 {
			page = 1
		}
		pageSize, _ := strconv.Atoi(r.FormValue("PageSize"))
		if pageSize <= 0 {
			pageSize = 20
		}
		_, _ = io.WriteString(w, m.renderGetPortfolioXML(page, pageSize))
		return
	case "namecheap.domains.getInfo":
		_, _ = io.WriteString(w, m.renderGetInfoXML(r.FormValue("DomainName")))
		return
	case "namecheap.users.getBalances":
		_, _ = io.WriteString(w, m.renderGetBalancesXML())
		return
	case "namecheap.users.getPricing":
		_, _ = io.WriteString(w, m.renderGetPricingXML(r.FormValue("ActionName"), r.FormValue("ProductName")))
		return
	}

	st := m.stateFor(domain)
	var resp string
	switch command {
	case "namecheap.domains.dns.getHosts":
		resp = renderGetHostsXML(domain, st)
	case "namecheap.domains.dns.setHosts":
		st.hosts = parseSetHostsRequest(r)
		if et := r.FormValue("EmailType"); et != "" {
			st.emailType = et
		}
		resp = renderResultXML("DomainDNSSetHostsResult", domain, `IsSuccess="true"`)
	case "namecheap.domains.dns.getList":
		resp = renderGetListXML(domain, st)
	case "namecheap.domains.dns.setCustom":
		st.nameservers = splitNameservers(r.FormValue("Nameservers"))
		resp = renderResultXML("DomainDNSSetCustomResult", domain, `Updated="true"`)
	case "namecheap.domains.dns.setDefault":
		st.nameservers = nil
		resp = renderResultXML("DomainDNSSetDefaultResult", domain, `Updated="true"`)
	case "namecheap.domains.ns.create":
		ns := r.FormValue("Nameserver")
		ip := r.FormValue("IP")
		if st.personalNS == nil {
			st.personalNS = map[string]string{}
		}
		st.personalNS[ns] = ip
		resp = renderResultXML("DomainNSCreateResult", domain, fmt.Sprintf(`Nameserver="%s" IP="%s" IsSuccess="true"`, ns, ip))
	case "namecheap.domains.ns.getInfo":
		ns := r.FormValue("Nameserver")
		ip, ok := st.personalNS[ns]
		if !ok {
			resp = apiErrorXML("5013160", "Nameserver not found")
			break
		}
		resp = renderNSInfoXML(domain, ns, ip)
	case "namecheap.domains.ns.update":
		ns := r.FormValue("Nameserver")
		ip := r.FormValue("IP")
		if st.personalNS == nil {
			st.personalNS = map[string]string{}
		}
		st.personalNS[ns] = ip
		resp = renderResultXML("DomainNSUpdateResult", domain, fmt.Sprintf(`Nameserver="%s" IsSuccess="true"`, ns))
	case "namecheap.domains.ns.delete":
		ns := r.FormValue("Nameserver")
		delete(st.personalNS, ns)
		resp = renderResultXML("DomainNSDeleteResult", domain, fmt.Sprintf(`Nameserver="%s" IsSuccess="true"`, ns))
	case "namecheap.domains.getContacts":
		resp = renderGetContactsXML(domain, st)
	case "namecheap.domains.setContacts":
		st.contacts = parseSetContactsRequest(r)
		resp = renderResultXML("DomainSetContactResult", domain, `IsSuccess="true"`)
	case "namecheap.domains.dns.getEmailForwarding":
		resp = renderGetEmailForwardingXML(domain, st)
	case "namecheap.domains.dns.setEmailForwarding":
		st.forwards = parseSetEmailForwardingRequest(r)
		resp = renderResultXML("DomainEmailForwardingResult", domain, `IsSuccess="true"`)
	default:
		resp = apiErrorXML("1010101", "mock: unsupported command "+command)
	}
	// Write failures reach only the in-process test client and are not
	// actionable in a mock, so the error is intentionally discarded.
	_, _ = io.WriteString(w, resp)
}

// parseSetHostsRequest extracts the 1-indexed HostNameN/RecordTypeN/AddressN/
// MXPrefN/TTLN parameters the SDK sends for SetHosts into hostEntry values.
func parseSetHostsRequest(r *http.Request) []hostEntry {
	var hosts []hostEntry
	for i := 1; ; i++ {
		idx := strconv.Itoa(i)
		name := r.FormValue("HostName" + idx)
		recordType := r.FormValue("RecordType" + idx)
		if name == "" && recordType == "" {
			break
		}
		mxPref, _ := strconv.Atoi(r.FormValue("MXPref" + idx))
		ttl, _ := strconv.Atoi(r.FormValue("TTL" + idx))
		hosts = append(hosts, hostEntry{
			Name:    name,
			Type:    recordType,
			Address: mockNormalizeAddress(recordType, r.FormValue("Address"+idx)),
			MXPref:  mockNormalizeMXPref(recordType, mxPref),
			TTL:     ttl,
		})
	}
	return hosts
}

// mockNormalizeAddress mimics the Namecheap server, which stores a trailing dot
// on the address of hostname-valued records (CNAME/ALIAS/NS/MX). Replicating it
// here exercises the provider's read-time address reconciliation — the same path
// that must round-trip cleanly against the real API.
func mockNormalizeAddress(recordType, address string) string {
	switch recordType {
	case "CNAME", "ALIAS", "NS", "MX":
		if address != "" && !strings.HasSuffix(address, ".") {
			return address + "."
		}
	}
	return address
}

// mockNormalizeMXPref mimics the Namecheap server, which keeps a preference only
// for MX records and reports a fixed 10 for every other type, whatever was sent.
// Echoing the submitted value back instead would hide a real defect: a provider
// that reads the preference into state for a type that has none leaves a diff the
// user can never resolve.
func mockNormalizeMXPref(recordType string, mxPref int) int {
	if recordType == "MX" {
		return mxPref
	}
	return 10
}

// splitNameservers parses the comma-separated Nameservers parameter, trimming
// whitespace and dropping empty entries.
func splitNameservers(raw string) []string {
	var out []string
	for _, ns := range strings.Split(raw, ",") {
		if ns = strings.TrimSpace(ns); ns != "" {
			out = append(out, ns)
		}
	}
	return out
}

// mockXMLAttrEscaper escapes values placed in XML attributes. It is needed for
// record addresses that legitimately contain quotes (e.g. a CAA iodef value like
// `0 iodef "mailto:..."`); without it the rendered response would be malformed.
var mockXMLAttrEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
)

func renderGetHostsXML(domain string, st *mockDomainState) string {
	var lines []string
	for i, h := range st.hosts {
		lines = append(lines, fmt.Sprintf(
			`<host HostId="%d" Name="%s" Type="%s" Address="%s" MXPref="%d" TTL="%d" AssociatedAppTitle="" FriendlyName="" IsActive="true" IsDDNSEnabled="false" />`,
			i+1, mockXMLAttrEscaper.Replace(h.Name), h.Type, mockXMLAttrEscaper.Replace(h.Address), h.MXPref, h.TTL))
	}
	emailType := st.emailType
	if emailType == "" {
		emailType = "NONE"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSGetHostsResult Domain="%s" EmailType="%s" IsUsingOurDNS="true">
      %s
    </DomainDNSGetHostsResult>
  </CommandResponse>
</ApiResponse>`, domain, emailType, strings.Join(lines, "\n      "))
}

func renderGetListXML(domain string, st *mockDomainState) string {
	usingOurDNS := len(st.nameservers) == 0
	nameservers := st.nameservers
	if usingOurDNS {
		nameservers = mockDefaultNameservers
	}
	var lines []string
	for _, ns := range nameservers {
		lines = append(lines, fmt.Sprintf(`<Nameserver>%s</Nameserver>`, ns))
	}
	usingStr := "false"
	if usingOurDNS {
		usingStr = "true"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSGetListResult Domain="%s" IsUsingOurDNS="%s" IsPremiumDNS="false" IsUsingFreeDNS="false">
      %s
    </DomainDNSGetListResult>
  </CommandResponse>
</ApiResponse>`, domain, usingStr, strings.Join(lines, "\n      "))
}

// renderNSInfoXML renders a domains.ns.getInfo success response for a
// registered personal nameserver, including its glue IP and a couple of
// representative statuses (which the provider does not consume but the SDK
// decodes).
func renderNSInfoXML(domain, nameserver, ip string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainNSInfoResult Domain="%s" Nameserver="%s" IP="%s">
      <NameserverStatuses>
        <Status>OK</Status>
        <Status>Linked</Status>
      </NameserverStatuses>
    </DomainNSInfoResult>
  </CommandResponse>
</ApiResponse>`, domain, nameserver, ip)
}

// renderGetPortfolioXML renders a namecheap.domains.getList response for the
// requested page. It honors pageSizeCap (when set) to force multi-page results
// from a small seed, and reports TotalItems/CurrentPage/PageSize so the provider
// can paginate to completion.
func (m *namecheapMock) renderGetPortfolioXML(page, pageSize int) string {
	eff := pageSize
	if m.pageSizeCap > 0 && m.pageSizeCap < eff {
		eff = m.pageSizeCap
	}
	total := len(m.portfolio)
	start := (page - 1) * eff
	if start > total {
		start = total
	}
	end := start + eff
	if end > total {
		end = total
	}

	var lines []string
	for _, d := range m.portfolio[start:end] {
		lines = append(lines, fmt.Sprintf(
			`<Domain ID="%s" Name="%s" User="%s" Created="%s" Expires="%s" IsExpired="%t" IsLocked="%t" AutoRenew="%t" WhoisGuard="%s" IsPremium="%t" IsOurDNS="%t" />`,
			d.ID, mockXMLAttrEscaper.Replace(d.Name), d.User, d.Created, d.Expires,
			d.IsExpired, d.IsLocked, d.AutoRenew, d.WhoisGuard, d.IsPremium, d.IsOurDNS))
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
</ApiResponse>`, strings.Join(lines, "\n      "), total, page, eff)
}

// renderGetInfoXML renders a namecheap.domains.getInfo response for the given
// domain, or a "Domain not found" API error when the domain was not seeded.
func (m *namecheapMock) renderGetInfoXML(domain string) string {
	info, ok := m.infos[domain]
	if !ok {
		return apiErrorXML("2019166", fmt.Sprintf("Domain %q not found", domain))
	}

	var nsLines []string
	for _, ns := range info.Nameservers {
		nsLines = append(nsLines, fmt.Sprintf(`<Nameserver>%s</Nameserver>`, ns))
	}

	var details string
	if info.Created != "" {
		details += fmt.Sprintf(`<CreatedDate>%s</CreatedDate>`, info.Created)
	}
	if info.Expires != "" {
		details += fmt.Sprintf(`<ExpiredDate>%s</ExpiredDate>`, info.Expires)
	}

	whois := ""
	if info.WhoisGuard != "" {
		whois = fmt.Sprintf(`<Whoisguard Enabled="%s"><ID>1</ID></Whoisguard>`, info.WhoisGuard)
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.domains.getInfo">
    <DomainGetInfoResult DomainName="%s" IsPremium="%t">
      <DomainDetails>%s</DomainDetails>
      <LockDetails />
      %s
      <PremiumDnsSubscription>
        <IsActive>%t</IsActive>
      </PremiumDnsSubscription>
      <DnsDetails ProviderType="%s" IsUsingOurDNS="%t">
        %s
      </DnsDetails>
      <Modificationrights />
    </DomainGetInfoResult>
  </CommandResponse>
</ApiResponse>`, domain, info.IsPremium, details, whois, info.IsPremiumDNS, info.ProviderType, info.IsUsingOurDNS, strings.Join(nsLines, "\n        "))
}

// renderGetBalancesXML renders the account funds for namecheap.users.getBalances.
// An unseeded account returns the API's "not enough privileges" style error so a
// test that forgets to seed fails loudly instead of reading zeros.
func (m *namecheapMock) renderGetBalancesXML() string {
	if m.balances == nil {
		return apiErrorXML("2011150", "Account balances are not available for this user")
	}
	b := m.balances
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.users.getBalances">
    <UserGetBalancesResult Currency="%s" AvailableBalance="%s" AccountBalance="%s" EarnedAmount="%s" WithdrawableAmount="%s" FundsRequiredForAutoRenew="%s" />
  </CommandResponse>
</ApiResponse>`, b.Currency, b.AvailableBalance, b.AccountBalance, b.EarnedAmount, b.WithdrawableAmount, b.FundsRequiredForAutoRenew)
}

// renderGetPricingXML renders the price sheet for one (action, product) pair.
// The real API narrows server-side on ActionName/ProductName, so the mock keys
// on exactly those parameters; an unseeded pair returns an empty (but valid)
// sheet, which is how the API answers for a TLD it does not sell.
func (m *namecheapMock) renderGetPricingXML(action, product string) string {
	tiers := m.pricing[pricingKey(action, product)]

	var prices []string
	for _, t := range tiers {
		attrs := fmt.Sprintf(`Duration="%d" DurationType="%s" Price="%s" RegularPrice="%s" YourPrice="%s"`,
			t.Duration, t.DurationType, t.Price, t.RegularPrice, t.YourPrice)
		if t.Currency != "" {
			attrs += fmt.Sprintf(` Currency="%s"`, t.Currency)
		}
		if t.Promotion != "" {
			attrs += fmt.Sprintf(` PromotionPrice="%s"`, t.Promotion)
		}
		prices = append(prices, fmt.Sprintf(`<Price %s />`, attrs))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.users.getPricing">
    <UserGetPricingResult>
      <ProductType Name="domains">
        <ProductCategory Name="%s">
          <Product Name="%s">
            %s
          </Product>
        </ProductCategory>
      </ProductType>
    </UserGetPricingResult>
  </CommandResponse>
</ApiResponse>`, strings.ToLower(action), strings.ToLower(product), strings.Join(prices, "\n            "))
}

// renderResultXML renders a generic success CommandResponse for write commands
// (SetHosts/SetCustom/SetDefault), which return a single self-closing result
// element carrying the domain and a status attribute.
func renderResultXML(element, domain, statusAttr string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <%s Domain="%s" %s />
  </CommandResponse>
</ApiResponse>`, element, domain, statusAttr)
}

// mockContactBlocks are the four WHOIS contact roles, using the request/response
// element prefixes the Namecheap contacts API uses.
var mockContactBlocks = []string{"Registrant", "Tech", "Admin", "AuxBilling"}

// mockContactFieldSuffixes are the ContactInfo field names as they appear both
// as setContacts request suffixes (e.g. "RegistrantFirstName") and getContacts
// response child elements.
var mockContactFieldSuffixes = []string{
	"FirstName", "LastName", "Address1", "City", "StateProvince", "PostalCode",
	"Country", "Phone", "EmailAddress", "OrganizationName", "JobTitle", "Address2",
}

// parseSetContactsRequest extracts the flattened <Prefix><Field> parameters the
// SDK sends for setContacts into a role -> field -> value map, keeping only
// non-empty values (the SDK omits unset optional fields).
func parseSetContactsRequest(r *http.Request) map[string]map[string]string {
	contacts := map[string]map[string]string{}
	for _, prefix := range mockContactBlocks {
		block := map[string]string{}
		for _, suffix := range mockContactFieldSuffixes {
			if v := r.FormValue(prefix + suffix); v != "" {
				block[suffix] = v
			}
		}
		if len(block) > 0 {
			contacts[prefix] = block
		}
	}
	return contacts
}

// renderGetContactsXML renders a getContacts response from the persisted contact
// state, emitting one element per role with its non-empty fields.
func renderGetContactsXML(domain string, st *mockDomainState) string {
	var blocks []string
	for _, prefix := range mockContactBlocks {
		fields := st.contacts[prefix]
		var children []string
		for _, suffix := range mockContactFieldSuffixes {
			if v, ok := fields[suffix]; ok && v != "" {
				children = append(children, fmt.Sprintf("<%s>%s</%s>", suffix, mockXMLAttrEscaper.Replace(v), suffix))
			}
		}
		blocks = append(blocks, fmt.Sprintf("<%s>%s</%s>", prefix, strings.Join(children, ""), prefix))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainContactsResult Domain="%s" domainnameid="12345" Readonly="false">
      %s
    </DomainContactsResult>
  </CommandResponse>
</ApiResponse>`, domain, strings.Join(blocks, "\n      "))
}

// parseSetEmailForwardingRequest extracts the 1-indexed mailboxN/ForwardToN
// parameters the SDK's SetEmailForwardingWithContext sends.
func parseSetEmailForwardingRequest(r *http.Request) map[string]string {
	forwards := map[string]string{}
	for i := 1; ; i++ {
		idx := strconv.Itoa(i)
		mailbox := r.FormValue("mailbox" + idx)
		forwardTo := r.FormValue("ForwardTo" + idx)
		if mailbox == "" && forwardTo == "" {
			break
		}
		forwards[mailbox] = forwardTo
	}
	return forwards
}

// renderGetEmailForwardingXML renders a getEmailForwarding response from the
// persisted forwarding table. Attribute casing matches the SDK's EmailForward
// struct exactly (lowercase "mailbox", capitalized "ForwardTo") - a mismatch
// would make the SDK's XML unmarshalling silently return an empty result.
func renderGetEmailForwardingXML(domain string, st *mockDomainState) string {
	mailboxes := make([]string, 0, len(st.forwards))
	for mailbox := range st.forwards {
		mailboxes = append(mailboxes, mailbox)
	}
	sort.Strings(mailboxes)

	var lines []string
	for _, mailbox := range mailboxes {
		lines = append(lines, fmt.Sprintf(
			`<Forward mailbox="%s" ForwardTo="%s" />`,
			mockXMLAttrEscaper.Replace(mailbox), mockXMLAttrEscaper.Replace(st.forwards[mailbox]),
		))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainEmailForwardingResult Domain="%s">
      %s
    </DomainEmailForwardingResult>
  </CommandResponse>
</ApiResponse>`, domain, strings.Join(lines, "\n      "))
}
