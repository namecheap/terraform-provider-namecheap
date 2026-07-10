package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/stretchr/testify/assert"
)

func TestDeleteRecordsMerge_RemovesOnlySpecifiedRecords(t *testing.T) {
	var setHostsRecordCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 600},
			}))
		case "namecheap.domains.dns.setHosts":
			for i := 1; ; i++ {
				if r.FormValue(fmt.Sprintf("RecordType%d", i)) == "" {
					break
				}
				setHostsRecordCount++
			}
			assert.Equal(t, "api", r.FormValue("HostName1"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	records := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := deleteRecordsMerge(context.Background(), "test.com", records, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, 1, setHostsRecordCount)
}

func TestDeleteRecordsMerge_RemovesAllManagedRecords(t *testing.T) {
	var setHostsRecordCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			for i := 1; ; i++ {
				if r.FormValue(fmt.Sprintf("RecordType%d", i)) == "" {
					break
				}
				setHostsRecordCount++
			}
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	records := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := deleteRecordsMerge(context.Background(), "test.com", records, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, 0, setHostsRecordCount) // No records remain
}

func TestDeleteRecordsMerge_EmailTypeResolvedToNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			// Remote has MX record with MX email type
			_, _ = fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.test.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			// After removing MX record, email type should be resolved to NONE
			assert.Equal(t, "NONE", r.FormValue("EmailType"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	records := []interface{}{
		map[string]interface{}{
			"hostname": "@",
			"type":     "MX",
			"address":  "mail.test.com.",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := deleteRecordsMerge(context.Background(), "test.com", records, client)
	assert.False(t, diags.HasError())
}

func TestDeleteRecordsOverwrite_ClearsAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "NONE", r.FormValue("EmailType"))
			// No records should be sent
			assert.Empty(t, r.FormValue("HostName1"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		default:
			t.Fatalf("unexpected command: %s", r.FormValue("Command"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	records := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := deleteRecordsOverwrite(context.Background(), "test.com", records, client)
	assert.False(t, diags.HasError())
	assert.Empty(t, diags, "the only live record is in priorStateRecords, so nothing is unmanaged")
}

func TestDeleteRecordsOverwrite_WarnsAboutUnmanagedRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			// Remote has a record Terraform never tracked in state - destroy
			// will wipe it out as a surprise (#65, #250).
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		default:
			t.Fatalf("unexpected command: %s", r.FormValue("Command"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	priorStateRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := deleteRecordsOverwrite(context.Background(), "test.com", priorStateRecords, client)
	assert.False(t, diags.HasError())
	if assert.Len(t, diags, 1) {
		assert.Equal(t, diag.Warning, diags[0].Severity)
		assert.Contains(t, diags[0].Summary, "will delete 1 record(s)")
		assert.Contains(t, diags[0].Detail, "api")
	}
}

func TestDeleteRecordsOverwrite_PreflightGetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("123456", "GetHosts failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// The pre-flight read must fail closed: SetHosts is never reached, so
	// nothing is destroyed on a transient API error.
	diags := deleteRecordsOverwrite(context.Background(), "test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
}

func TestDeleteRecordsMerge_SetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 600},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	records := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := deleteRecordsMerge(context.Background(), "test.com", records, client)
	assert.True(t, diags.HasError())
}

func TestDeleteRecordsMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsMerge(context.Background(), "test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
}

func TestDeleteRecordsOverwrite_SetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		default:
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsOverwrite(context.Background(), "test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
}
