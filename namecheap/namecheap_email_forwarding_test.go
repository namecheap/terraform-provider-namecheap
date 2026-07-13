package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

func emailForwardingTestData(t *testing.T, domain string, forwards map[string]interface{}) *schema.ResourceData {
	t.Helper()
	return schema.TestResourceDataRaw(t, resourceNamecheapEmailForwarding().Schema, map[string]interface{}{
		"domain":   domain,
		"forwards": forwards,
	})
}

func getEmailForwardingXML(domain string, forwards map[string]string) string {
	var lines string
	for mailbox, forwardTo := range forwards {
		lines += fmt.Sprintf(`<Forward mailbox="%s" ForwardTo="%s" />`, mailbox, forwardTo)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainEmailForwardingResult Domain="%s">
      %s
    </DomainEmailForwardingResult>
  </CommandResponse>
</ApiResponse>`, domain, lines)
}

func setEmailForwardingSuccessXML(domain string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainEmailForwardingResult Domain="%s" IsSuccess="true" />
  </CommandResponse>
</ApiResponse>`, domain)
}

// --- validateForwards ---

func TestValidateForwards_Valid(t *testing.T) {
	cases := []map[string]interface{}{
		{"info": "me@example.com"},
		{"info": "me@example.com", "sales": "sales@example.com"},
		{"*": "catchall@example.com"},
	}
	for i, forwards := range cases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			diags := validateForwards(forwards, cty.Path{})
			assert.Empty(t, diags)
		})
	}
}

func TestValidateForwards_EmptyMapRejected(t *testing.T) {
	diags := validateForwards(map[string]interface{}{}, cty.Path{})
	if assert.Len(t, diags, 1) {
		assert.Equal(t, diag.Error, diags[0].Severity)
		assert.Contains(t, diags[0].Summary, "must not be empty")
	}
}

func TestValidateForwards_InvalidMailboxKeys(t *testing.T) {
	cases := []string{"info@example.com", "Info", "in fo", "in\tfo"}
	for _, mailbox := range cases {
		t.Run(mailbox, func(t *testing.T) {
			diags := validateForwards(map[string]interface{}{mailbox: "me@example.com"}, cty.Path{})
			if assert.Len(t, diags, 1) {
				assert.Equal(t, diag.Error, diags[0].Severity)
				assert.Contains(t, diags[0].Summary, "mailbox alias")
				assert.Equal(t, cty.Path{cty.IndexStep{Key: cty.StringVal(mailbox)}}, diags[0].AttributePath)
			}
		})
	}
}

func TestValidateForwards_InvalidDestinations(t *testing.T) {
	cases := []string{"", "no-at-sign", "@example.com", "me@", "me@a@example.com"}
	for _, dest := range cases {
		t.Run(dest, func(t *testing.T) {
			diags := validateForwards(map[string]interface{}{"info": dest}, cty.Path{})
			if assert.Len(t, diags, 1) {
				assert.Equal(t, diag.Error, diags[0].Severity)
				assert.Contains(t, diags[0].Summary, "destination")
				assert.Equal(t, cty.Path{cty.IndexStep{Key: cty.StringVal("info")}}, diags[0].AttributePath)
			}
		})
	}
}

func TestValidateForwards_AttributePathScopedToOffendingKey(t *testing.T) {
	diags := validateForwards(map[string]interface{}{
		"info": "me@example.com",
		"bad@": "also-bad",
	}, cty.Path{cty.GetAttrStep{Name: "forwards"}})

	if assert.Len(t, diags, 2) {
		for _, d := range diags {
			assert.Equal(t, cty.Path{
				cty.GetAttrStep{Name: "forwards"},
				cty.IndexStep{Key: cty.StringVal("bad@")},
			}, d.AttributePath)
		}
	}
}

// --- forwardsMapToSlice / forwardsSliceToMap ---

func TestForwardsMapToSlice_DeterministicOrder(t *testing.T) {
	forwards := map[string]interface{}{
		"zeta":  "z@example.com",
		"alpha": "a@example.com",
		"mid":   "m@example.com",
	}

	result := forwardsMapToSlice(forwards)

	assert.Equal(t, []namecheap.EmailForward{
		{Mailbox: "alpha", ForwardTo: "a@example.com"},
		{Mailbox: "mid", ForwardTo: "m@example.com"},
		{Mailbox: "zeta", ForwardTo: "z@example.com"},
	}, result)
}

func TestForwardsSliceToMap(t *testing.T) {
	t.Run("nil_slice", func(t *testing.T) {
		assert.Equal(t, map[string]string{}, forwardsSliceToMap(nil))
	})

	t.Run("lowercases_keys", func(t *testing.T) {
		result := forwardsSliceToMap([]namecheap.EmailForward{
			{Mailbox: "Info", ForwardTo: "Me@Example.com"},
		})
		assert.Equal(t, map[string]string{"info": "Me@Example.com"}, result)
	})

	t.Run("duplicate_after_lowercasing_last_wins", func(t *testing.T) {
		result := forwardsSliceToMap([]namecheap.EmailForward{
			{Mailbox: "Info", ForwardTo: "first@example.com"},
			{Mailbox: "info", ForwardTo: "second@example.com"},
		})
		assert.Equal(t, map[string]string{"info": "second@example.com"}, result)
	})
}

// --- import ---

func TestResourceEmailForwardingImport(t *testing.T) {
	d := schema.TestResourceDataRaw(t, resourceNamecheapEmailForwarding().Schema, map[string]interface{}{})
	d.SetId("Example.com")

	res, err := resourceNamecheapEmailForwarding().Importer.StateContext(context.Background(), d, nil)
	assert.NoError(t, err)
	assert.Len(t, res, 1)
	assert.Equal(t, "example.com", d.Get("domain"))
	assert.Equal(t, "example.com", d.Id())
}

// --- CRUD ---

func TestResourceEmailForwardingCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.setEmailForwarding":
			assert.Equal(t, "example.com", r.FormValue("DomainName"))
			assert.Equal(t, "info", r.FormValue("mailbox1"))
			assert.Equal(t, "me@example.com", r.FormValue("ForwardTo1"))
			_, _ = fmt.Fprint(w, setEmailForwardingSuccessXML("example.com"))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("FWD", nil))
		default:
			t.Fatalf("unexpected command: %s", r.FormValue("Command"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"info": "me@example.com"})

	diags := resourceEmailForwardingCreate(context.Background(), d, client)
	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	assert.Empty(t, diags, "no warning expected when DNS mode and email_type are both correct")
	assert.Equal(t, "example.com", d.Id())
}

func TestResourceEmailForwardingCreate_SetAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("2019166", "Domain not found"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"info": "me@example.com"})

	diags := resourceEmailForwardingCreate(context.Background(), d, client)
	assert.True(t, diags.HasError())
	assert.Empty(t, d.Id())
}

func TestResourceEmailForwardingCreate_WarnsOnCustomDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.setEmailForwarding":
			_, _ = fmt.Fprint(w, setEmailForwardingSuccessXML("example.com"))
		case "namecheap.domains.dns.getHosts":
			// Custom-DNS domains fail GetHosts, mirroring resourceRecordRead's
			// existing handling of the same condition.
			_, _ = fmt.Fprint(w, apiErrorXML("2022222", "Domain uses custom DNS"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"info": "me@example.com"})

	diags := resourceEmailForwardingCreate(context.Background(), d, client)
	assert.False(t, diags.HasError())
	if assert.Len(t, diags, 1) {
		assert.Equal(t, diag.Warning, diags[0].Severity)
		assert.Contains(t, diags[0].Summary, "BasicDNS")
	}
	// Create must still succeed and set the ID despite the warning.
	assert.Equal(t, "example.com", d.Id())
}

func TestResourceEmailForwardingCreate_WarnsOnWrongEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.setEmailForwarding":
			_, _ = fmt.Fprint(w, setEmailForwardingSuccessXML("example.com"))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("MX", nil))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"info": "me@example.com"})

	diags := resourceEmailForwardingCreate(context.Background(), d, client)
	assert.False(t, diags.HasError())
	if assert.Len(t, diags, 1) {
		assert.Equal(t, diag.Warning, diags[0].Severity)
		assert.Contains(t, diags[0].Summary, "email_type")
		assert.Contains(t, diags[0].Detail, "FWD")
	}
}

func TestResourceEmailForwardingUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.setEmailForwarding":
			assert.Equal(t, "sales", r.FormValue("mailbox1"))
			_, _ = fmt.Fprint(w, setEmailForwardingSuccessXML("example.com"))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("FWD", nil))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"sales": "sales@example.com"})
	d.SetId("example.com")

	diags := resourceEmailForwardingUpdate(context.Background(), d, client)
	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
}

func TestResourceEmailForwardingRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getEmailForwardingXML("example.com", map[string]string{
			"info": "me@example.com",
		}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{})
	d.SetId("example.com")

	diags := resourceEmailForwardingRead(context.Background(), d, client)
	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	assert.Equal(t, map[string]interface{}{"info": "me@example.com"}, d.Get("forwards"))
}

// TestResourceEmailForwardingRead_EmptyResultKeepsID covers a Status=OK
// response carrying no DomainEmailForwardingResult element at all - the
// observed real-API shape for a domain with zero forwarding rules. This must
// NOT be treated as "the domain is gone" (only an actual API error does
// that): the ID must be kept and forwards set to an empty map.
func TestResourceEmailForwardingRead_EmptyResultKeepsID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse />
</ApiResponse>`)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"info": "me@example.com"})
	d.SetId("example.com")

	diags := resourceEmailForwardingRead(context.Background(), d, client)
	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
	assert.Equal(t, "example.com", d.Id(), "a domain with zero forwards must not be treated as gone")
	assert.Equal(t, map[string]interface{}{}, d.Get("forwards"))
}

func TestResourceEmailForwardingRead_DomainGone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("2019166", "Domain not found"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{})
	d.SetId("example.com")

	diags := resourceEmailForwardingRead(context.Background(), d, client)
	assert.False(t, diags.HasError(), "a gone domain must not error; got %v", diags)
	assert.Empty(t, d.Id(), "a gone domain should be removed from state")
}

func TestResourceEmailForwardingRead_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("99999", "Some other error"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{})
	d.SetId("example.com")

	diags := resourceEmailForwardingRead(context.Background(), d, client)
	assert.True(t, diags.HasError(), "a non-gone API error must surface")
	assert.Equal(t, "example.com", d.Id(), "state must be left intact on a hard error")
}

func TestResourceEmailForwardingDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setEmailForwarding", r.FormValue("Command"))
		assert.Empty(t, r.FormValue("mailbox1"), "destroy must clear the table")
		_, _ = fmt.Fprint(w, setEmailForwardingSuccessXML("example.com"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"info": "me@example.com"})
	d.SetId("example.com")

	diags := resourceEmailForwardingDelete(context.Background(), d, client)
	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)
}

func TestResourceEmailForwardingDelete_DomainGoneIsNoOp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("2019166", "Domain not found"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	d := emailForwardingTestData(t, "example.com", map[string]interface{}{"info": "me@example.com"})
	d.SetId("example.com")

	diags := resourceEmailForwardingDelete(context.Background(), d, client)
	assert.False(t, diags.HasError(), "destroying an already-gone domain must not error; got %v", diags)
}
