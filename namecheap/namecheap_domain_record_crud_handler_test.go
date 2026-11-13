package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
)

func TestResourceRecordCreate_MergeWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordCreate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "test.com", data.Id())
}

func TestResourceRecordCreate_OverwriteWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		default:
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordCreate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "test.com", data.Id())
}

// The configured nameservers are the domain's whole delegation in both modes:
// a create sends that list and nothing else, whatever the domain uses now.
func TestResourceRecordCreate_WithNameservers(t *testing.T) {
	for _, mode := range []string{ncModeMerge, ncModeOverwrite} {
		t.Run(mode, func(t *testing.T) {
			var commands []string
			var sent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				commands = append(commands, strings.TrimPrefix(r.FormValue("Command"), "namecheap.domains.dns."))
				sent = r.FormValue("Nameservers")
				_, _ = fmt.Fprint(w, setCustomSuccessXML())
			}))
			defer server.Close()

			data := resourceNamecheapDomainRecords().TestResourceData()
			_ = data.Set("domain", "test.com")
			_ = data.Set("mode", mode)
			_ = data.Set("nameservers", []interface{}{"ns1.example.com", "ns2.example.com"})

			diags := resourceRecordCreate(context.TODO(), data, newTestClient(server.URL))
			assert.False(t, diags.HasError(), "%v", diags)
			assert.Equal(t, "test.com", data.Id())
			assert.Equal(t, []string{"setCustom"}, commands)
			assert.ElementsMatch(t, []string{"ns1.example.com", "ns2.example.com"}, strings.Split(sent, ","))
		})
	}
}

func TestResourceRecordCreate_MergeRecordsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "API failure"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordCreate(context.TODO(), data, client)
	assert.True(t, diags.HasError())
}

func TestResourceRecordCreate_OverwriteRecordsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "API failure"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordCreate(context.TODO(), data, client)
	assert.True(t, diags.HasError())
}

func TestResourceRecordCreate_WithEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "MX", r.FormValue("EmailType"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := recordsDataWithConfig(map[string]cty.Value{"email_type": cty.StringVal("MX")})
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("email_type", "MX")
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "@", "type": "MX", "address": "mail.test.com.", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordCreate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordDelete_MergeWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordDelete(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordDelete_OverwriteWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		default:
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordDelete(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

// Destroying a resource that holds nameservers returns the domain to Namecheap
// DNS, in both modes.
func TestResourceRecordDelete_WithNameservers(t *testing.T) {
	for _, mode := range []string{ncModeMerge, ncModeOverwrite} {
		t.Run(mode, func(t *testing.T) {
			var commands []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				commands = append(commands, strings.TrimPrefix(r.FormValue("Command"), "namecheap.domains.dns."))
				_, _ = fmt.Fprint(w, setDefaultSuccessXML())
			}))
			defer server.Close()

			data := resourceNamecheapDomainRecords().TestResourceData()
			data.SetId("test.com")
			_ = data.Set("domain", "test.com")
			_ = data.Set("mode", mode)
			_ = data.Set("nameservers", []interface{}{"ns1.example.com", "ns2.example.com"})

			diags := resourceRecordDelete(context.TODO(), data, newTestClient(server.URL))
			assert.False(t, diags.HasError(), "%v", diags)
			assert.Equal(t, []string{"setDefault"}, commands)
		})
	}
}

func TestResourceRecordDelete_NoRecordsNoNameservers(t *testing.T) {
	client := newTestClient("http://unused")
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)

	diags := resourceRecordDelete(context.TODO(), data, client)
	assert.Nil(t, diags)
}

func TestResourceRecordRead_MergeWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	records := data.Get("record").(*schema.Set).List()
	assert.Len(t, records, 1)
}

func TestResourceRecordRead_OverwriteWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	records := data.Get("record").(*schema.Set).List()
	assert.Len(t, records, 1)
}

func TestResourceRecordRead_OverwriteWarnsAboutUnmanagedRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	// Refresh (the terraform plan path) must surface the warning even though
	// there is no error - this is the plan-time visibility #250 asks for.
	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	if assert.Len(t, diags, 1) {
		assert.Equal(t, diag.Warning, diags[0].Severity)
		assert.Contains(t, diags[0].Summary, "will delete 1 record(s)")
		assert.Contains(t, diags[0].Detail, "api")
	}
}

// A read stores every nameserver of a delegated domain, in both modes, so the
// plan lists the ones an apply removes. MERGE used to keep only the ones
// already in state, and hid a nameserver added in the dashboard.
func TestResourceRecordRead_StoresEveryCustomNameserver(t *testing.T) {
	for _, mode := range []string{ncModeMerge, ncModeOverwrite} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com", "ns3.dashboard.com"}))
			}))
			defer server.Close()

			data := resourceNamecheapDomainRecords().TestResourceData()
			data.SetId("test.com")
			_ = data.Set("domain", "test.com")
			_ = data.Set("mode", mode)
			_ = data.Set("nameservers", []interface{}{"ns1.custom.com", "ns2.custom.com"})

			diags := resourceRecordRead(context.TODO(), data, newTestClient(server.URL))
			assert.False(t, diags.HasError(), "%v", diags)
			assert.ElementsMatch(t, []string{"ns1.custom.com", "ns2.custom.com", "ns3.dashboard.com"},
				convertInterfacesToString(data.Get("nameservers").(*schema.Set).List()))
		})
	}
}

// Records in the configuration mean Namecheap DNS, in both modes: an update
// returns a delegated domain to it before it writes them.
func TestResourceRecordUpdate_RecordsMoveDelegatedDomainToNamecheapDNS(t *testing.T) {
	for _, mode := range []string{ncModeMerge, ncModeOverwrite} {
		t.Run(mode, func(t *testing.T) {
			var commands []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				command := r.FormValue("Command")
				commands = append(commands, strings.TrimPrefix(command, "namecheap.domains.dns."))
				switch command {
				case "namecheap.domains.dns.getList":
					_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com"}))
				case "namecheap.domains.dns.setDefault":
					_, _ = fmt.Fprint(w, setDefaultSuccessXML())
				case "namecheap.domains.dns.getHosts":
					_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
				case "namecheap.domains.dns.setHosts":
					_, _ = fmt.Fprint(w, setHostsSuccessXML())
				}
			}))
			defer server.Close()

			// Values given to Set do not show up in GetChange, which the update
			// reads, so the record comes in as configuration.
			data := schema.TestResourceDataRaw(t, resourceNamecheapDomainRecords().Schema, map[string]interface{}{
				"domain": "test.com",
				"mode":   mode,
				"record": []interface{}{
					map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
				},
			})
			data.SetId("test.com")

			diags := resourceRecordUpdate(context.TODO(), data, newTestClient(server.URL))
			assert.False(t, diags.HasError(), "%v", diags)
			assert.Equal(t, []string{"getList", "setDefault", "getHosts", "setHosts"}, commands)
		})
	}
}

// A domain on custom nameservers has no Namecheap mail mode in effect, so a read
// clears the one stored while it was on Namecheap DNS, as it clears record.
func TestResourceRecordRead_CustomNameserversClearsEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("email_type", "GMAIL")
	_ = data.Set("nameservers", []interface{}{"ns1.custom.com", "ns2.custom.com"})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "", data.Get("email_type").(string))
}

func TestResourceRecordRead_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.True(t, diags.HasError())
}

func TestResourceRecordRead_UsingOurDNSClearsNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("nameservers", []interface{}{"ns1.old.com", "ns2.old.com"})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	ns := data.Get("nameservers").(*schema.Set)
	assert.Equal(t, 0, ns.Len())
}

func TestResourceRecordUpdate_OverwriteWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordUpdate_MergeWithRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "5.6.7.8", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordUpdate_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.True(t, diags.HasError())
}

func TestResourceRecordUpdate_GetListNilResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "domain not found"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.True(t, diags.HasError())
}

func TestResourceRecordUpdate_MergeEmailTypeOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "FWD", r.FormValue("EmailType"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := recordsDataWithConfig(map[string]cty.Value{"email_type": cty.StringVal("FWD")})
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("email_type", "FWD")

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

// email_type is Computed, so state holds the mode the last refresh read. A
// config that leaves email_type out must resend the mode the zone has when the
// write runs, not that stored one: another resource on the domain may have
// changed it since the refresh.
func TestResourceRecordUpdate_OverwriteWithoutConfiguredEmailTypeSendsLiveMode(t *testing.T) {
	var sent atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("OX", nil))
		case "namecheap.domains.dns.setHosts":
			sent.Store(r.FormValue("EmailType"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := recordsDataWithConfig(nil)
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("email_type", "GMAIL")

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "OX", sent.Load())
}

func TestResourceRecordUpdate_OverwriteNoEmailNoRecordsResetsToNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "NONE", r.FormValue("EmailType"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordUpdate_OverwriteResetNameserversBeforeRecords(t *testing.T) {
	callOrder := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cmd := r.FormValue("Command")
		callOrder = append(callOrder, cmd)
		switch cmd {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com"}))
		case "namecheap.domains.dns.setDefault":
			_, _ = fmt.Fprint(w, setDefaultSuccessXML())
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	// Verify setDefault is called before setHosts to reset nameservers
	defaultIdx := -1
	hostsIdx := -1
	for i, cmd := range callOrder {
		if cmd == "namecheap.domains.dns.setDefault" {
			defaultIdx = i
		}
		if cmd == "namecheap.domains.dns.setHosts" {
			hostsIdx = i
		}
	}
	assert.Greater(t, defaultIdx, -1, "setDefault should be called")
	assert.Greater(t, hostsIdx, -1, "setHosts should be called")
	assert.Less(t, defaultIdx, hostsIdx, "setDefault should be called before setHosts")
}

func TestResourceRecordUpdate_OverwriteRecordsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		default:
			_, _ = fmt.Fprint(w, apiErrorXML("500", "API failure"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.True(t, diags.HasError())
}

func TestResourceRecordRead_OverwriteWithEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.test.com", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	// Pre-seed a value that differs from the remote response ("MX") so the
	// assertion below only passes if the read actually writes the remote
	// value back into state.
	_ = data.Set("email_type", "FWD")
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "@", "type": "MX", "address": "mail.test.com", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "MX", data.Get("email_type").(string))
}

func TestResourceRecordRead_MergeWithEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.test.com", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	// Pre-seed a value that differs from the remote response ("MX") so the
	// assertion below only passes if the read actually writes the remote
	// value back into state.
	_ = data.Set("email_type", "FWD")
	_ = data.Set("record", []interface{}{
		map[string]interface{}{"hostname": "@", "type": "MX", "address": "mail.test.com", "mx_pref": 10, "ttl": 1800},
	})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "MX", data.Get("email_type").(string))
}

// Read of a domain on custom nameservers must reuse the getList response it
// already fetched to check IsUsingOurDNS instead of requesting it again: each
// extra call counts against the account's API rate limit on every refresh.
func TestResourceRecordRead_CustomNameservers_SingleGetList(t *testing.T) {
	for _, mode := range []string{ncModeOverwrite, ncModeMerge} {
		t.Run(mode, func(t *testing.T) {
			var getListCalls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				if r.FormValue("Command") == "namecheap.domains.dns.getList" {
					atomic.AddInt32(&getListCalls, 1)
					_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com"}))
				}
			}))
			defer server.Close()

			client := newTestClient(server.URL)
			data := resourceNamecheapDomainRecords().TestResourceData()
			data.SetId("test.com")
			_ = data.Set("domain", "test.com")
			_ = data.Set("mode", mode)
			_ = data.Set("nameservers", []interface{}{"ns1.custom.com", "ns2.custom.com"})

			diags := resourceRecordRead(context.TODO(), data, client)
			assert.False(t, diags.HasError())
			assert.ElementsMatch(t, []interface{}{"ns1.custom.com", "ns2.custom.com"}, data.Get("nameservers").(*schema.Set).List())
			assert.Equal(t, int32(1), atomic.LoadInt32(&getListCalls))
		})
	}
}
