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
	"github.com/stretchr/testify/require"
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

func TestResourceRecordCreate_MergeWithNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("nameservers", []interface{}{"ns1.example.com", "ns2.example.com"})

	diags := resourceRecordCreate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "test.com", data.Id())
}

func TestResourceRecordCreate_OverwriteWithNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, setCustomSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("nameservers", []interface{}{"ns1.example.com", "ns2.example.com"})

	diags := resourceRecordCreate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, "test.com", data.Id())
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

func TestResourceRecordDelete_MergeWithNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com", "ns3.example.com"}))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("nameservers", []interface{}{"ns1.example.com"})

	diags := resourceRecordDelete(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordDelete_OverwriteWithNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, setDefaultSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("nameservers", []interface{}{"ns1.example.com", "ns2.example.com"})

	diags := resourceRecordDelete(context.TODO(), data, client)
	assert.False(t, diags.HasError())
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

func TestResourceRecordRead_MergeWithCustomNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("nameservers", []interface{}{"ns1.custom.com", "ns2.custom.com"})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordRead_OverwriteWithCustomNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeOverwrite)
	_ = data.Set("nameservers", []interface{}{"ns1.custom.com", "ns2.custom.com"})

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
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

// updateOnDelegatedDomain runs an update against a domain delegated to custom
// nameservers that are not in state, and returns the commands it sent.
func updateOnDelegatedDomain(t *testing.T, data *schema.ResourceData) (diag.Diagnostics, []string) {
	t.Helper()
	var commands []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")
		commands = append(commands, strings.TrimPrefix(command, "namecheap.domains.dns."))
		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.dns-host.net", "ns2.dns-host.net"}))
		case "namecheap.domains.dns.setDefault":
			_, _ = fmt.Fprint(w, setDefaultSuccessXML())
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	t.Cleanup(server.Close)

	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	return resourceRecordUpdate(context.TODO(), data, newTestClient(server.URL)), commands
}

// recordsDataWithPlannedRecord returns resource data whose plan adds one record.
// Values given to Set do not show up in GetChange, which the update reads, so
// the record comes in as configuration.
func recordsDataWithPlannedRecord(t *testing.T, mode string) *schema.ResourceData {
	t.Helper()
	return schema.TestResourceDataRaw(t, resourceNamecheapDomainRecords().Schema, map[string]interface{}{
		"domain": "test.com",
		"mode":   mode,
		"record": []interface{}{
			map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
		},
	})
}

// MERGE manages only the nameservers in its state. An update used to move a
// domain that was delegated in the dashboard back to Namecheap DNS before it
// wrote records, with nothing in the plan about the delegation.
func TestResourceRecordUpdate_MergeRefusesRecordsOnDomainDelegatedElsewhere(t *testing.T) {
	diags, commands := updateOnDelegatedDomain(t, recordsDataWithPlannedRecord(t, ncModeMerge))
	require.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "test.com is delegated to custom nameservers this resource does not manage")
	assert.Contains(t, diags[0].Detail, "ns1.dns-host.net")
	assert.Contains(t, diags[0].Detail, "ns2.dns-host.net")
	assert.Contains(t, diags[0].Detail, `"test.com/zone"`)
	assert.Equal(t, []string{"getList"}, commands, "a refused update must not write")
}

func TestResourceRecordUpdate_MergeRefusesEmailTypeOnDomainDelegatedElsewhere(t *testing.T) {
	data := recordsDataWithConfig(map[string]cty.Value{"email_type": cty.StringVal("GMAIL")})
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("email_type", "GMAIL")

	diags, commands := updateOnDelegatedDomain(t, data)
	require.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "delegated to custom nameservers this resource does not manage")
	assert.Equal(t, []string{"getList"}, commands, "a refused update must not write")
}

// An update that writes neither records nor the mail mode has no reason to
// touch the delegation. It used to reset it.
func TestResourceRecordUpdate_MergeWithNothingToWriteLeavesDelegation(t *testing.T) {
	data := resourceNamecheapDomainRecords().TestResourceData()
	_ = data.Set("mode", ncModeMerge)

	diags, commands := updateOnDelegatedDomain(t, data)
	assert.False(t, diags.HasError(), "%v", diags)
	assert.Equal(t, []string{"getList"}, commands)
}

// OVERWRITE owns the delegation: records in the configuration mean Namecheap
// DNS, so the domain is moved there before they are written.
func TestResourceRecordUpdate_OverwriteMovesDelegatedDomainToNamecheapDNS(t *testing.T) {
	diags, commands := updateOnDelegatedDomain(t, recordsDataWithPlannedRecord(t, ncModeOverwrite))
	assert.False(t, diags.HasError(), "%v", diags)
	assert.Equal(t, []string{"getList", "setDefault", "getHosts", "setHosts"}, commands)
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
