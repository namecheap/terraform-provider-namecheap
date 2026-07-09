package namecheap_provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are ordinary (non-testacc) unit tests that drive the domain_contacts
// resource's CRUD/Read functions against an in-process httptest server via the
// real SDK client. The testacc mock suite is gated behind a build tag and so is
// invisible to the coverage upload; these exercise — and cover — the
// setContacts/getContacts round-trip and the read/not-found/error paths.

// contactsTestServer routes on the request Command to a per-test responder.
func contactsTestServer(t *testing.T, respond func(command string) string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, respond(r.FormValue("Command")))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// xmlContactBlock renders one <Block><FirstName>...</Block> ContactInfo element.
func xmlContactBlock(block, first, country, phone, email, org string) string {
	return fmt.Sprintf(`<%[1]s>
      <FirstName>%[2]s</FirstName>
      <LastName>Doe</LastName>
      <Address1>1 Main St</Address1>
      <City>Lisbon</City>
      <StateProvince>Lisboa</StateProvince>
      <PostalCode>1000-001</PostalCode>
      <Country>%[3]s</Country>
      <Phone>%[4]s</Phone>
      <EmailAddress>%[5]s</EmailAddress>
      <OrganizationName>%[6]s</OrganizationName>
    </%[1]s>`, block, first, country, phone, email, org)
}

func xmlGetContacts(domain string) string {
	blocks := ""
	for _, b := range []string{"Registrant", "Tech", "Admin", "AuxBilling"} {
		blocks += xmlContactBlock(b, "Jane", "PT", "+351.123456789", "jane@example.com", "Example Corp")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainContactsResult Domain="%s" domainnameid="12345" Readonly="false">
      %s
    </DomainContactsResult>
  </CommandResponse>
</ApiResponse>`, domain, blocks)
}

func xmlGetContactsEmpty() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse />
</ApiResponse>`
}

func xmlSetContactsOK(domain string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainSetContactResult Domain="%s" IsSuccess="true" />
  </CommandResponse>
</ApiResponse>`, domain)
}

// registrantRaw is the nested-block raw value for building resource data with a
// complete registrant.
func registrantRaw() map[string]interface{} {
	return map[string]interface{}{
		"domain": "example.com",
		"registrant": []interface{}{map[string]interface{}{
			"first_name":     "Jane",
			"last_name":      "Doe",
			"address1":       "1 Main St",
			"city":           "Lisbon",
			"state_province": "Lisboa",
			"postal_code":    "1000-001",
			"country":        "PT",
			"phone":          "+351.123456789",
			"email_address":  "jane@example.com",
			"organization":   "Example Corp",
		}},
	}
}

func TestResourceContactsRead_Success(t *testing.T) {
	url := contactsTestServer(t, func(command string) string {
		if command == "namecheap.domains.getContacts" {
			return xmlGetContacts("example.com")
		}
		return apiErrorXML("1010101", "unexpected "+command)
	})
	client := newTestClient(url)

	d := schema.TestResourceDataRaw(t, resourceNamecheapDomainContacts().Schema, map[string]interface{}{"domain": "example.com"})
	d.SetId("example.com")
	diags := resourceContactsRead(context.Background(), d, client)

	require.False(t, diags.HasError(), "unexpected diags: %+v", diags)
	reg := d.Get("registrant").([]interface{})
	require.Len(t, reg, 1)
	assert.Equal(t, "Jane", reg[0].(map[string]interface{})["first_name"])
	assert.Equal(t, "PT", reg[0].(map[string]interface{})["country"])
	tech := d.Get("tech").([]interface{})
	require.Len(t, tech, 1)
	assert.Equal(t, "jane@example.com", tech[0].(map[string]interface{})["email_address"])
}

func TestResourceContactsRead_NotFound(t *testing.T) {
	url := contactsTestServer(t, func(string) string { return xmlGetContactsEmpty() })
	client := newTestClient(url)

	d := schema.TestResourceDataRaw(t, resourceNamecheapDomainContacts().Schema, map[string]interface{}{"domain": "example.com"})
	d.SetId("example.com")
	diags := resourceContactsRead(context.Background(), d, client)

	require.False(t, diags.HasError(), "a missing domain must not error; got %+v", diags)
	assert.Empty(t, d.Id(), "a domain absent from the account should be dropped from state")
}

func TestResourceContactsRead_Error(t *testing.T) {
	url := contactsTestServer(t, func(string) string { return apiErrorXML("2019166", "Domain not found") })
	client := newTestClient(url)

	d := schema.TestResourceDataRaw(t, resourceNamecheapDomainContacts().Schema, map[string]interface{}{"domain": "example.com"})
	d.SetId("example.com")
	diags := resourceContactsRead(context.Background(), d, client)

	assert.True(t, diags.HasError(), "a getContacts API error should surface")
}

func TestResourceContactsCreate_Success(t *testing.T) {
	url := contactsTestServer(t, func(command string) string {
		switch command {
		case "namecheap.domains.setContacts":
			return xmlSetContactsOK("example.com")
		case "namecheap.domains.getContacts":
			return xmlGetContacts("example.com")
		}
		return apiErrorXML("1010101", "unexpected "+command)
	})
	client := newTestClient(url)

	d := schema.TestResourceDataRaw(t, resourceNamecheapDomainContacts().Schema, registrantRaw())
	diags := resourceContactsCreate(context.Background(), d, client)

	require.False(t, diags.HasError(), "unexpected diags: %+v", diags)
	assert.Equal(t, "example.com", d.Id(), "create should set the ID to the domain")
	// Create ends by reading back, so the blocks are populated from getContacts.
	assert.Len(t, d.Get("admin").([]interface{}), 1)
}

func TestResourceContactsCreate_Error(t *testing.T) {
	url := contactsTestServer(t, func(command string) string {
		if command == "namecheap.domains.setContacts" {
			return apiErrorXML("2010324", "Contact details are invalid")
		}
		return apiErrorXML("1010101", "unexpected "+command)
	})
	client := newTestClient(url)

	d := schema.TestResourceDataRaw(t, resourceNamecheapDomainContacts().Schema, registrantRaw())
	diags := resourceContactsCreate(context.Background(), d, client)

	assert.True(t, diags.HasError(), "a setContacts API error should surface")
}

// TestResourceContactsDelete asserts the API-free destroy: it clears the ID and
// returns a warning explaining contacts cannot actually be deleted.
func TestResourceContactsDelete(t *testing.T) {
	d := schema.TestResourceDataRaw(t, resourceNamecheapDomainContacts().Schema, map[string]interface{}{"domain": "example.com"})
	d.SetId("example.com")

	diags := resourceContactsDelete(context.Background(), d, nil)

	assert.False(t, diags.HasError(), "delete must not error")
	assert.Empty(t, d.Id(), "delete should clear the ID")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.Warning, diags[0].Severity)
}
