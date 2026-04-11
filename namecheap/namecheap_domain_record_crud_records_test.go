package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
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

func TestReadRecordsMerge_FindsMatchingRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
			{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 600},
			{Name: "blog", Type: "CNAME", Address: "example.com.", MXPref: 10, TTL: 1800},
		}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	currentRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	foundRecords, emailType, diags := readRecordsMerge("test.com", currentRecords, client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, foundRecords)
	assert.Len(t, *foundRecords, 1)
	assert.Equal(t, "www", (*foundRecords)[0]["hostname"])
	assert.Equal(t, "A", (*foundRecords)[0]["type"])
	assert.Equal(t, "NONE", *emailType)
}

func TestReadRecordsMerge_NoMatchingRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
			{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 600},
		}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	currentRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	foundRecords, _, diags := readRecordsMerge("test.com", currentRecords, client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, foundRecords)
	assert.Len(t, *foundRecords, 0)
}

func TestReadRecordsMerge_CNAMEWithDotFix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
			{Name: "blog", Type: "CNAME", Address: "example.com.", MXPref: 10, TTL: 1800},
		}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// User specifies without dot, API returns with dot - should still match
	currentRecords := []interface{}{
		map[string]interface{}{
			"hostname": "blog",
			"type":     "CNAME",
			"address":  "example.com",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	foundRecords, _, diags := readRecordsMerge("test.com", currentRecords, client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, foundRecords)
	assert.Len(t, *foundRecords, 1)
	// Address in result should be the user's original (without dot)
	assert.Equal(t, "example.com", (*foundRecords)[0]["address"])
}

func TestReadRecordsMerge_EmptyRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	currentRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	foundRecords, _, diags := readRecordsMerge("test.com", currentRecords, client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, foundRecords)
	assert.Len(t, *foundRecords, 0)
}

func TestReadRecordsOverwrite_ReturnsAllRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
			{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 600},
		}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	currentRecords := []interface{}{
		map[string]interface{}{
			"hostname": "www",
			"type":     "A",
			"address":  "1.2.3.4",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	foundRecords, emailType, diags := readRecordsOverwrite("test.com", currentRecords, client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, foundRecords)
	assert.Len(t, *foundRecords, 2)
	assert.Equal(t, "NONE", *emailType)
}

func TestReadRecordsOverwrite_EmptyRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	foundRecords, _, diags := readRecordsOverwrite("test.com", []interface{}{}, client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, foundRecords)
	assert.Len(t, *foundRecords, 0)
}

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

// Complex error-path tests with multi-command routing

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

// CNAME dot-fix and email-type resolution tests

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

func TestReadRecordsOverwrite_CNAMEDotFix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
			{Name: "blog", Type: "CNAME", Address: "example.com.", MXPref: 10, TTL: 1800},
		}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// User specifies without dot - should match and preserve user's address
	currentRecords := []interface{}{
		map[string]interface{}{
			"hostname": "blog",
			"type":     "CNAME",
			"address":  "example.com",
			"mx_pref":  10,
			"ttl":      1800,
		},
	}

	foundRecords, _, diags := readRecordsOverwrite("test.com", currentRecords, client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, foundRecords)
	assert.Len(t, *foundRecords, 1)
	assert.Equal(t, "example.com", (*foundRecords)[0]["address"])
}

// Simple API error tests consolidated into table-driven subtests.
// These all follow the same pattern: create a server that returns an error,
// call the function, and assert that diagnostics contain an error.

func TestRecordsOverwrite_SimpleAPIErrors(t *testing.T) {
	tests := []struct {
		name      string
		serverFn  func(w http.ResponseWriter, r *http.Request)
		callFn    func(client *namecheap.Client) diag.Diagnostics
		assertNil bool // whether to also assert a nil result
	}{
		{
			name: "CreateRecordsOverwrite_SetHostsAPIError",
			serverFn: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
			},
			callFn: func(client *namecheap.Client) diag.Diagnostics {
				records := []interface{}{
					map[string]interface{}{
						"hostname": "www",
						"type":     "A",
						"address":  "1.2.3.4",
						"mx_pref":  10,
						"ttl":      1800,
					},
				}
				return createRecordsOverwrite("test.com", nil, records, client)
			},
		},
		{
			name: "DeleteRecordsOverwrite_SetHostsAPIError",
			serverFn: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
			},
			callFn: func(client *namecheap.Client) diag.Diagnostics {
				return deleteRecordsOverwrite("test.com", client)
			},
		},
		{
			name: "ReadRecordsOverwrite_GetHostsAPIError",
			serverFn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(w, "internal error")
			},
			callFn: func(client *namecheap.Client) diag.Diagnostics {
				result, _, diags := readRecordsOverwrite("test.com", []interface{}{}, client)
				assert.Nil(t, result)
				return diags
			},
			assertNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tc.serverFn))
			defer server.Close()

			client := newTestClient(server.URL)
			diags := tc.callFn(client)
			assert.True(t, diags.HasError())
		})
	}
}

func TestRecordsMerge_SimpleAPIErrors(t *testing.T) {
	tests := []struct {
		name   string
		callFn func(client *namecheap.Client) diag.Diagnostics
	}{
		{
			name: "ReadRecordsMerge_APIError",
			callFn: func(client *namecheap.Client) diag.Diagnostics {
				result, _, diags := readRecordsMerge("test.com", []interface{}{}, client)
				assert.Nil(t, result)
				return diags
			},
		},
		{
			name: "DeleteRecordsMerge_APIError",
			callFn: func(client *namecheap.Client) diag.Diagnostics {
				return deleteRecordsMerge("test.com", []interface{}{}, client)
			},
		},
		{
			name: "UpdateRecordsMerge_APIError",
			callFn: func(client *namecheap.Client) diag.Diagnostics {
				return updateRecordsMerge("test.com", nil, []interface{}{}, []interface{}{}, client)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(w, "internal error")
			}))
			defer server.Close()

			client := newTestClient(server.URL)
			diags := tc.callFn(client)
			assert.True(t, diags.HasError())
		})
	}
}
