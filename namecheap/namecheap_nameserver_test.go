package namecheap_provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
)

// nsMockServer is a command-routed mock of the Namecheap domains.ns.* API used
// by the nameserver resource unit tests. It records the most recent request
// form so tests can assert the exact command and parameters the provider sent,
// and delegates the response body to a per-test respond func.
type nsMockServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	lastForm url.Values
	respond  func(command string, form url.Values) string
}

func newNSMockServer(t *testing.T, respond func(command string, form url.Values) string) *nsMockServer {
	t.Helper()
	m := &nsMockServer{respond: respond}
	m.server = httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(m.server.Close)
	return m
}

func (m *nsMockServer) handler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	m.mu.Lock()
	m.lastForm = r.Form
	m.mu.Unlock()
	w.Header().Set("Content-Type", "text/xml")
	_, _ = io.WriteString(w, m.respond(r.FormValue("Command"), r.Form))
}

func (m *nsMockServer) last() url.Values {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastForm
}

func nsCreateSuccessXML(domain, nameserver, ip string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainNSCreateResult Domain="%s" Nameserver="%s" IP="%s" IsSuccess="true" />
  </CommandResponse>
</ApiResponse>`, domain, nameserver, ip)
}

func nsGetInfoXML(domain, nameserver, ip string) string {
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

func nsGetInfoEmptyXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse />
</ApiResponse>`
}

func nsUpdateSuccessXML(domain, nameserver string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainNSUpdateResult Domain="%s" Nameserver="%s" IsSuccess="true" />
  </CommandResponse>
</ApiResponse>`, domain, nameserver)
}

func nsDeleteSuccessXML(domain, nameserver string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainNSDeleteResult Domain="%s" Nameserver="%s" IsSuccess="true" />
  </CommandResponse>
</ApiResponse>`, domain, nameserver)
}

func nsTestData(t *testing.T, domain, nameserver, ip string) *schema.ResourceData {
	t.Helper()
	return schema.TestResourceDataRaw(t, resourceNamecheapNameserver().Schema, map[string]interface{}{
		"domain":     domain,
		"nameserver": nameserver,
		"ip":         ip,
	})
}

func TestNameserverID(t *testing.T) {
	assert.Equal(t, "example.com/ns1.example.com", nameserverID("example.com", "ns1.example.com"))
}

func TestNameserverSplitDomain(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		sld, tld, err := nameserverSplitDomain("example.com")
		assert.NoError(t, err)
		assert.Equal(t, "example", sld)
		assert.Equal(t, "com", tld)
	})

	t.Run("invalid", func(t *testing.T) {
		_, _, err := nameserverSplitDomain("not a domain")
		assert.Error(t, err)
	})
}

func TestResourceNameserverCreate(t *testing.T) {
	m := newNSMockServer(t, func(_ string, _ url.Values) string {
		return nsCreateSuccessXML("example.com", "ns1.example.com", "1.2.3.4")
	})
	client := newTestClient(m.server.URL)

	d := nsTestData(t, "example.com", "ns1.example.com", "1.2.3.4")
	diags := resourceNameserverCreate(context.Background(), d, client)

	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	form := m.last()
	assert.Equal(t, "namecheap.domains.ns.create", form.Get("Command"))
	assert.Equal(t, "example", form.Get("SLD"))
	assert.Equal(t, "com", form.Get("TLD"))
	assert.Equal(t, "ns1.example.com", form.Get("Nameserver"))
	assert.Equal(t, "1.2.3.4", form.Get("IP"))
	assert.Equal(t, "example.com/ns1.example.com", d.Id())
}

func TestResourceNameserverCreateAPIError(t *testing.T) {
	m := newNSMockServer(t, func(_ string, _ url.Values) string {
		return apiErrorXML("2019166", "Domain not found")
	})
	client := newTestClient(m.server.URL)

	d := nsTestData(t, "example.com", "ns1.example.com", "1.2.3.4")
	diags := resourceNameserverCreate(context.Background(), d, client)

	assert.True(t, diags.HasError(), "expected an error diagnostic for an API error")
}

func TestResourceNameserverRead(t *testing.T) {
	m := newNSMockServer(t, func(_ string, _ url.Values) string {
		// Report a different IP than configured to prove Read reconciles state.
		return nsGetInfoXML("example.com", "ns1.example.com", "9.9.9.9")
	})
	client := newTestClient(m.server.URL)

	d := nsTestData(t, "example.com", "ns1.example.com", "1.2.3.4")
	d.SetId("example.com/ns1.example.com")
	diags := resourceNameserverRead(context.Background(), d, client)

	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	form := m.last()
	assert.Equal(t, "namecheap.domains.ns.getInfo", form.Get("Command"))
	assert.Equal(t, "ns1.example.com", form.Get("Nameserver"))
	assert.Equal(t, "9.9.9.9", d.Get("ip"))
	assert.Equal(t, "example.com/ns1.example.com", d.Id())
}

func TestResourceNameserverReadNotFound(t *testing.T) {
	m := newNSMockServer(t, func(_ string, _ url.Values) string {
		return nsGetInfoEmptyXML()
	})
	client := newTestClient(m.server.URL)

	d := nsTestData(t, "example.com", "ns1.example.com", "1.2.3.4")
	d.SetId("example.com/ns1.example.com")
	diags := resourceNameserverRead(context.Background(), d, client)

	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	assert.Empty(t, d.Id(), "a missing nameserver should be removed from state")
}

func TestResourceNameserverUpdate(t *testing.T) {
	m := newNSMockServer(t, func(_ string, _ url.Values) string {
		return nsUpdateSuccessXML("example.com", "ns1.example.com")
	})
	client := newTestClient(m.server.URL)

	d := nsTestData(t, "example.com", "ns1.example.com", "5.6.7.8")
	d.SetId("example.com/ns1.example.com")
	diags := resourceNameserverUpdate(context.Background(), d, client)

	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	form := m.last()
	assert.Equal(t, "namecheap.domains.ns.update", form.Get("Command"))
	assert.Equal(t, "ns1.example.com", form.Get("Nameserver"))
	assert.Equal(t, "5.6.7.8", form.Get("IP"))
	// The update command always sends OldIP alongside the new IP.
	assert.Contains(t, form, "OldIP")
}

func TestResourceNameserverDelete(t *testing.T) {
	m := newNSMockServer(t, func(_ string, _ url.Values) string {
		return nsDeleteSuccessXML("example.com", "ns1.example.com")
	})
	client := newTestClient(m.server.URL)

	d := nsTestData(t, "example.com", "ns1.example.com", "1.2.3.4")
	d.SetId("example.com/ns1.example.com")
	diags := resourceNameserverDelete(context.Background(), d, client)

	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	form := m.last()
	assert.Equal(t, "namecheap.domains.ns.delete", form.Get("Command"))
	assert.Equal(t, "example", form.Get("SLD"))
	assert.Equal(t, "com", form.Get("TLD"))
	assert.Equal(t, "ns1.example.com", form.Get("Nameserver"))
}

func TestResourceNameserverImport(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		d := schema.TestResourceDataRaw(t, resourceNamecheapNameserver().Schema, map[string]interface{}{})
		d.SetId("Example.com/NS1.example.com")

		res, err := resourceNameserverImport(context.Background(), d, nil)
		assert.NoError(t, err)
		assert.Len(t, res, 1)
		assert.Equal(t, "example.com", d.Get("domain"))
		assert.Equal(t, "ns1.example.com", d.Get("nameserver"))
		assert.Equal(t, "example.com/ns1.example.com", d.Id())
	})

	invalidIDs := []string{"", "no-separator", "example.com/", "/ns1.example.com"}
	for _, id := range invalidIDs {
		t.Run(fmt.Sprintf("invalid_%q", id), func(t *testing.T) {
			d := schema.TestResourceDataRaw(t, resourceNamecheapNameserver().Schema, map[string]interface{}{})
			d.SetId(id)

			_, err := resourceNameserverImport(context.Background(), d, nil)
			assert.Error(t, err)
		})
	}
}
