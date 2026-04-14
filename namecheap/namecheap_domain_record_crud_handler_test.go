package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
)

func TestReadImportMode_NamecheapDNS_ConvertsModeToMerge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "@", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	resource := resourceNamecheapDomainRecords()
	data := resource.TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeImport)

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, ncModeMerge, data.Get("mode").(string))
}

func TestReadImportMode_CustomNameservers_ConvertsModeToMerge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.custom.com", "ns2.custom.com"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	resource := resourceNamecheapDomainRecords()
	data := resource.TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeImport)

	diags := resourceRecordRead(context.TODO(), data, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, ncModeMerge, data.Get("mode").(string))
}

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
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
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
		assert.Equal(t, "MX", r.FormValue("EmailType"))
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
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
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
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
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeMerge)
	_ = data.Set("email_type", "FWD")

	diags := resourceRecordUpdate(context.TODO(), data, client)
	assert.False(t, diags.HasError())
}

func TestResourceRecordUpdate_OverwriteNoEmailNoRecordsResetsToNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
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
