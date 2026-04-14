package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

func TestCreateRecordsMerge_EmptyRemote(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")
		callCount++

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "www", r.FormValue("HostName1"))
			assert.Equal(t, "A", r.FormValue("RecordType1"))
			assert.Equal(t, "1.2.3.4", r.FormValue("Address1"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		default:
			t.Fatalf("unexpected command: %s", command)
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

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, 2, callCount) // getHosts + setHosts
}

func TestCreateRecordsMerge_WithExistingRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			// Should contain both existing and new records
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		default:
			t.Fatalf("unexpected command: %s", command)
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

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.False(t, diags.HasError())
}

func TestCreateRecordsMerge_DuplicateRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			// Remote already has this record
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		default:
			// Should not reach setHosts
			t.Fatalf("unexpected command: %s", command)
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

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "Duplicate record")
}

func TestCreateRecordsMerge_FiltersParkingRecords(t *testing.T) {
	var setHostsRecordCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			// Return parking records + a real record
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "CNAME", Address: "parkingpage.namecheap.com.", MXPref: 10, TTL: 1800},
				{Name: "@", Type: "URL", Address: "http://www.test.com/?from=@", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			// Count how many records are being set
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
			"hostname": "blog",
			"type":     "A",
			"address":  "9.10.11.12",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.False(t, diags.HasError())
	// Should have: api (existing, not parking) + blog (new) = 2 records
	assert.Equal(t, 2, setHostsRecordCount)
}

func TestCreateRecordsMerge_WithEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "MX", r.FormValue("EmailType"))
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	emailType := namecheap.EmailTypeMX
	records := []interface{}{
		map[string]interface{}{
			"hostname": "@",
			"type":     "MX",
			"address":  "mail.test.com.",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := createRecordsMerge("test.com", &emailType, records, client)
	assert.False(t, diags.HasError())
}

func TestCreateRecordsMerge_GetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("123456", "API error"))
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

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.True(t, diags.HasError())
}

func TestCreateRecordsOverwrite_Simple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setHosts", r.FormValue("Command"))
		assert.Equal(t, "NONE", r.FormValue("EmailType"))
		assert.Equal(t, "www", r.FormValue("HostName1"))
		assert.Equal(t, "A", r.FormValue("RecordType1"))
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
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

	diags := createRecordsOverwrite("test.com", nil, records, client)
	assert.False(t, diags.HasError())
}

func TestCreateRecordsOverwrite_WithEmailType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "MX", r.FormValue("EmailType"))
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	emailType := namecheap.EmailTypeMX
	records := []interface{}{
		map[string]interface{}{
			"hostname": "@",
			"type":     "MX",
			"address":  "mail.test.com.",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := createRecordsOverwrite("test.com", &emailType, records, client)
	assert.False(t, diags.HasError())
}

func TestCreateRecordsOverwrite_EmptyRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	records := []interface{}{}

	diags := createRecordsOverwrite("test.com", nil, records, client)
	assert.False(t, diags.HasError())
}

func TestCreateRecordsMerge_SetHostsAPIError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")
		callCount++

		switch command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors><Error Number="99999">Set hosts failed</Error></Errors>
</ApiResponse>`)
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

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.True(t, diags.HasError())
}

func TestCreateRecordsMerge_CNAMEDotFixNoDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			// Remote has record with dot suffix
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "CNAME", Address: "old.example.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// New CNAME points to different target - should not be duplicate
	records := []interface{}{
		map[string]interface{}{
			"hostname": "blog",
			"type":     "CNAME",
			"address":  "new.example.com",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.False(t, diags.HasError())
}

func TestCreateRecordsMerge_CNAMEDotFixDetectsDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			// Remote has record with dot suffix
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "CNAME", Address: "target.example.com.", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// Same CNAME without dot - should detect as duplicate after dot fix
	records := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "CNAME",
			"address":  "target.example.com",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	diags := createRecordsMerge("test.com", nil, records, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "Duplicate record")
}

func TestCreateRecordsMerge_ResolvesEmailTypeWhenNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getHosts":
			// Remote has MX email type
			_, _ = fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.test.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			// Email type should be preserved since MX records still exist
			assert.Equal(t, "MX", r.FormValue("EmailType"))
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

	// emailType is nil - should be resolved from remote
	diags := createRecordsMerge("test.com", nil, records, client)
	assert.False(t, diags.HasError())
}

func TestCreateRecordsOverwrite_SetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
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

	diags := createRecordsOverwrite("test.com", nil, records, client)
	assert.True(t, diags.HasError())
}
