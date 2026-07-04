//go:build testacc

package namecheap_provider

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// state returns the current mock state for a domain, or nil if the domain has
// never been touched. Callers must not mutate the returned pointer.
func (m *namecheapMock) state(domain string) *mockDomainState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.domains[domain]
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

func (m *namecheapMock) handler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	command := r.FormValue("Command")
	domain := r.FormValue("SLD") + "." + r.FormValue("TLD")

	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateFor(domain)

	w.Header().Set("Content-Type", "text/xml")
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
			MXPref:  mxPref,
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
