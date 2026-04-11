package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

func TestUpdateRecordsMerge_ReplacesOldWithNew(t *testing.T) {
	var setHostsRecords []string
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
				hn := r.FormValue(fmt.Sprintf("HostName%d", i))
				if hn == "" {
					break
				}
				setHostsRecords = append(setHostsRecords, hn)
			}
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)

	oldRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	newRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "9.10.11.12",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := updateRecordsMerge("test.com", nil, oldRecords, newRecords, client)
	assert.False(t, diags.HasError())
	// Should contain: api (kept) + www new (replaced)
	assert.Len(t, setHostsRecords, 2)
}

func TestUpdateRecordsMerge_WithEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.old.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "MX", r.FormValue("EmailType"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	emailType := namecheap.EmailTypeMX

	oldRecords := []interface{}{
		map[string]interface{}{
			"hostname": "@",
			"type":     "MX",
			"address":  "mail.old.com.",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}
	newRecords := []interface{}{
		map[string]interface{}{
			"hostname": "@",
			"type":     "MX",
			"address":  "mail.new.com.",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := updateRecordsMerge("test.com", &emailType, oldRecords, newRecords, client)
	assert.False(t, diags.HasError())
}

func TestUpdateRecordsMerge_EmptyRemoteRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)

	oldRecords := []interface{}{}
	newRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := updateRecordsMerge("test.com", nil, oldRecords, newRecords, client)
	assert.False(t, diags.HasError())
}

func TestUpdateRecordsMerge_SetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	oldRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}
	newRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "9.10.11.12",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := updateRecordsMerge("test.com", nil, oldRecords, newRecords, client)
	assert.True(t, diags.HasError())
}

func TestUpdateRecordsMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := updateRecordsMerge("test.com", nil, []interface{}{}, []interface{}{}, client)
	assert.True(t, diags.HasError())
}
