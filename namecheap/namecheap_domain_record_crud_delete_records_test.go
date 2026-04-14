package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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

	diags := deleteRecordsMerge("test.com", records, client)
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

	diags := deleteRecordsMerge("test.com", records, client)
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

	diags := deleteRecordsMerge("test.com", records, client)
	assert.False(t, diags.HasError())
}

func TestDeleteRecordsOverwrite_ClearsAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setHosts", r.FormValue("Command"))
		assert.Equal(t, "NONE", r.FormValue("EmailType"))
		// No records should be sent
		assert.Empty(t, r.FormValue("HostName1"))
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsOverwrite("test.com", client)
	assert.False(t, diags.HasError())
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

	diags := deleteRecordsMerge("test.com", records, client)
	assert.True(t, diags.HasError())
}

func TestDeleteRecordsMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsMerge("test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
}

func TestDeleteRecordsOverwrite_SetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsOverwrite("test.com", client)
	assert.True(t, diags.HasError())
}
