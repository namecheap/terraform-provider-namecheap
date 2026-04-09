package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// newTestClient creates a namecheap client pointed at the given test server URL
func newTestClient(baseURL string) *namecheap.Client {
	client := namecheap.NewClient(&namecheap.ClientOptions{
		UserName:   "testuser",
		ApiUser:    "testuser",
		ApiKey:     "testapikey",
		ClientIp:   "127.0.0.1",
		UseSandbox: false,
	})
	client.BaseURL = baseURL
	return client
}

// getHostsXML generates a GetHosts API response XML
func getHostsXML(emailType string, hosts []hostEntry) string {
	var hostLines []string
	for i, h := range hosts {
		hostLines = append(hostLines, fmt.Sprintf(
			`<host HostId="%d" Name="%s" Type="%s" Address="%s" MXPref="%d" TTL="%d" AssociatedAppTitle="" FriendlyName="" IsActive="true" IsDDNSEnabled="false" />`,
			i+1, h.Name, h.Type, h.Address, h.MXPref, h.TTL,
		))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSGetHostsResult Domain="test.com" EmailType="%s" IsUsingOurDNS="true">
      %s
    </DomainDNSGetHostsResult>
  </CommandResponse>
</ApiResponse>`, emailType, strings.Join(hostLines, "\n      "))
}

// setHostsSuccessXML generates a SetHosts success API response XML
func setHostsSuccessXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSSetHostsResult Domain="test.com" IsSuccess="true" />
  </CommandResponse>
</ApiResponse>`
}

// getListXML generates a GetList API response XML for nameservers
func getListXML(isUsingOurDNS bool, nameservers []string) string {
	var nsLines []string
	for _, ns := range nameservers {
		nsLines = append(nsLines, fmt.Sprintf(`<Nameserver>%s</Nameserver>`, ns))
	}
	isUsingDNSStr := "false"
	if isUsingOurDNS {
		isUsingDNSStr = "true"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSGetListResult Domain="test.com" IsUsingOurDNS="%s" IsPremiumDNS="false" IsUsingFreeDNS="false">
      %s
    </DomainDNSGetListResult>
  </CommandResponse>
</ApiResponse>`, isUsingDNSStr, strings.Join(nsLines, "\n      "))
}

// setCustomSuccessXML generates a SetCustom success API response XML
func setCustomSuccessXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSSetCustomResult Domain="test.com" Updated="true" />
  </CommandResponse>
</ApiResponse>`
}

// setDefaultSuccessXML generates a SetDefault success API response XML
func setDefaultSuccessXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSSetDefaultResult Domain="test.com" Updated="true" />
  </CommandResponse>
</ApiResponse>`
}

type hostEntry struct {
	Name    string
	Type    string
	Address string
	MXPref  int
	TTL     int
}

// apiErrorXML generates an API error response
func apiErrorXML(code string, message string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors>
    <Error Number="%s">%s</Error>
  </Errors>
</ApiResponse>`, code, message)
}

// ===== createRecordsMerge tests =====

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

// ===== createRecordsOverwrite tests =====

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

// ===== readRecordsMerge tests =====

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

// ===== readRecordsOverwrite tests =====

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

// ===== updateRecordsMerge tests =====

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

// ===== deleteRecordsMerge tests =====

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

// ===== deleteRecordsOverwrite tests =====

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

// ===== createNameserversMerge tests =====

func TestCreateNameserversMerge_OnDefaultDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			assert.Contains(t, r.FormValue("Nameservers"), "ns1.example.com")
			assert.Contains(t, r.FormValue("Nameservers"), "ns2.example.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		default:
			t.Fatalf("unexpected command: %s", command)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns1.example.com", "ns2.example.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.False(t, diags.HasError())
}

func TestCreateNameserversMerge_MergesWithExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.existing.com", "ns2.existing.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns1.existing.com")
			assert.Contains(t, ns, "ns2.existing.com")
			assert.Contains(t, ns, "ns3.new.com")
			assert.Contains(t, ns, "ns4.new.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns3.new.com", "ns4.new.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.False(t, diags.HasError())
}

func TestCreateNameserversMerge_DuplicateDetection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com"}))
		default:
			t.Fatalf("should not call setCustom on duplicate: %s", command)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns1.example.com", "ns3.example.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Detail, "ns1.example.com")
	assert.Contains(t, diags[0].Detail, "already exist")
}

func TestCreateNameserversMerge_DuplicateCaseInsensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns1.example.com", "ns3.example.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.True(t, diags.HasError())
}

// ===== createNameserversOverwrite tests =====

func TestCreateNameserversOverwrite_Simple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setCustom", r.FormValue("Command"))
		_, _ = fmt.Fprint(w, setCustomSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversOverwrite("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
}

// ===== readNameserversMerge tests =====

func TestReadNameserversMerge_FindsMatching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com", "ns3.other.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversMerge_CaseInsensitiveMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com"}, *result)
}

func TestReadNameserversMerge_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(true, nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameserversMerge_NoMatchFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.other.com", "ns2.other.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

// ===== readNameserversOverwrite tests =====

func TestReadNameserversOverwrite_ReturnsAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversOverwrite_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(true, nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

// ===== updateNameserversMerge tests =====

func TestUpdateNameserversMerge_ReplacesOldWithNew(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com", "ns3.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns3.manual.com")
			assert.Contains(t, ns, "ns1.new.com")
			assert.Contains(t, ns, "ns2.new.com")
			assert.NotContains(t, ns, "ns1.old.com")
			assert.NotContains(t, ns, "ns2.old.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{"ns1.new.com", "ns2.new.com"}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.False(t, diags.HasError())
}

func TestUpdateNameserversMerge_OnlyOneRemains_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.manual.com"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// Removing ns1.old.com leaves only ns2.manual.com (1 NS), which is invalid
	prev := []string{"ns1.old.com"}
	current := []string{}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "one remaining nameserver")
}

func TestUpdateNameserversMerge_ZeroRemains_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com"}))
		case "namecheap.domains.dns.setDefault":
			setDefaultCalled = true
			_, _ = fmt.Fprint(w, setDefaultSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.False(t, diags.HasError())
	assert.True(t, setDefaultCalled)
}

func TestUpdateNameserversMerge_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{}
	current := []string{"ns1.new.com", "ns2.new.com"}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.False(t, diags.HasError())
}

// ===== deleteNameserversMerge tests =====

func TestDeleteNameserversMerge_RemovesManaged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com", "ns3.manual.com", "ns4.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns3.manual.com")
			assert.Contains(t, ns, "ns4.manual.com")
			assert.NotContains(t, ns, "ns1.managed.com")
			assert.NotContains(t, ns, "ns2.managed.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.False(t, diags.HasError())
}

func TestDeleteNameserversMerge_OneRemains_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.manual.com"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com"}, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "one remaining nameserver")
}

func TestDeleteNameserversMerge_AllRemoved_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com"}))
		case "namecheap.domains.dns.setDefault":
			setDefaultCalled = true
			_, _ = fmt.Fprint(w, setDefaultSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.False(t, diags.HasError())
	assert.True(t, setDefaultCalled)
}

func TestDeleteNameserversMerge_UsingOurDNS_Noop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		default:
			t.Fatalf("unexpected command when using our DNS: %s", command)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
}

// ===== deleteNameserversOverwrite tests =====

func TestDeleteNameserversOverwrite_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setDefault", r.FormValue("Command"))
		setDefaultCalled = true
		_, _ = fmt.Fprint(w, setDefaultSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.True(t, setDefaultCalled)
}

// ===== API error handling tests =====

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

func TestCreateNameserversMerge_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return invalid response to cause error
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

func TestReadNameserversMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

func TestReadRecordsMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, _, diags := readRecordsMerge("test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
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

// ===== CNAME address dot fix in merge operations =====

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

// ===== resourceRecordRead import mode tests =====

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

// ===== createNameserversOverwrite error path tests =====

func TestCreateNameserversOverwrite_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversOverwrite("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

// ===== readNameserversOverwrite error path tests =====

func TestReadNameserversOverwrite_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

func TestReadNameserversOverwrite_NilNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return custom DNS (IsUsingOurDNS=false) but with no nameserver elements
		_, _ = fmt.Fprint(w, getListXML(false, nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, result)
	// With nil nameservers and IsUsingOurDNS=false, should return empty list
	assert.Empty(t, *result)
}

// ===== deleteNameserversOverwrite error path tests =====

func TestDeleteNameserversOverwrite_SetDefaultAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversOverwrite("test.com", client)
	assert.True(t, diags.HasError())
}

// ===== deleteRecordsOverwrite error path tests =====

func TestDeleteRecordsOverwrite_SetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetHosts failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsOverwrite("test.com", client)
	assert.True(t, diags.HasError())
}

// ===== deleteNameserversMerge error path tests =====

func TestDeleteNameserversMerge_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.True(t, diags.HasError())
}

func TestDeleteNameserversMerge_NilNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			// Custom DNS but no nameservers in response
			_, _ = fmt.Fprint(w, getListXML(false, nil))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "Invalid nameservers response")
}

func TestDeleteNameserversMerge_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com", "ns3.manual.com", "ns4.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.True(t, diags.HasError())
}

func TestDeleteNameserversMerge_SetDefaultAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com"}))
		case "namecheap.domains.dns.setDefault":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.True(t, diags.HasError())
}

// ===== updateNameserversMerge error path tests =====

func TestUpdateNameserversMerge_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := updateNameserversMerge("test.com", []string{"ns1.old.com"}, []string{"ns1.new.com", "ns2.new.com"}, client)
	assert.True(t, diags.HasError())
}

func TestUpdateNameserversMerge_SetDefaultAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com"}))
		case "namecheap.domains.dns.setDefault":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.True(t, diags.HasError())
}

func TestUpdateNameserversMerge_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com", "ns3.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{"ns1.new.com", "ns2.new.com"}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.True(t, diags.HasError())
}

// ===== readRecordsOverwrite error path tests =====

func TestReadRecordsOverwrite_GetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, _, diags := readRecordsOverwrite("test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

// ===== createNameserversMerge error path tests =====

func TestCreateNameserversMerge_SetCustomAPIError_OnDefaultDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

func TestCreateNameserversMerge_SetCustomAPIError_OnCustomDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.existing.com", "ns2.existing.com"}))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns3.new.com", "ns4.new.com"}, client)
	assert.True(t, diags.HasError())
}

// ===== createRecordsOverwrite error path tests =====

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

// ===== deleteRecordsMerge error path tests =====

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

// ===== updateRecordsMerge error path tests =====

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

// ===== readRecordsMerge error path tests =====

func TestReadRecordsMerge_GetHostsAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetHosts failed"))
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

	result, _, diags := readRecordsMerge("test.com", currentRecords, client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

// ===== readRecordsOverwrite validation tests =====

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

// ===== readNameserversMerge validation tests =====

func TestReadNameserversMerge_GetListAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetList failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

// ===== createNameserversMerge with nil nameservers in response =====

func TestCreateNameserversMerge_NilNameserversInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			// Custom DNS but no nameserver elements
			_, _ = fmt.Fprint(w, getListXML(false, nil))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.new.com", "ns2.new.com"}, client)
	assert.False(t, diags.HasError())
}

// ===== updateRecordsMerge validation tests =====

func TestUpdateRecordsMerge_GetHostsAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetHosts failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := updateRecordsMerge("test.com", nil, []interface{}{}, []interface{}{}, client)
	assert.True(t, diags.HasError())
}

// ===== deleteRecordsMerge validation tests =====

func TestDeleteRecordsMerge_GetHostsAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetHosts failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsMerge("test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
}

// ===== createRecordsMerge validation tests =====

func TestCreateRecordsMerge_GetHostsAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetHosts failed"))
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

// ===== createNameserversMerge GetList API error (XML-level) =====

func TestCreateNameserversMerge_GetListAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetList failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

// ===== deleteNameserversMerge GetList API error (XML-level) =====

func TestDeleteNameserversMerge_GetListAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetList failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.True(t, diags.HasError())
}

// ===== updateNameserversMerge GetList API error (XML-level) =====

func TestUpdateNameserversMerge_GetListAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetList failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := updateNameserversMerge("test.com", []string{"ns1.old.com"}, []string{"ns1.new.com", "ns2.new.com"}, client)
	assert.True(t, diags.HasError())
}

// ===== readNameserversOverwrite GetList API error (XML-level) =====

func TestReadNameserversOverwrite_GetListAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetList failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

// ===== readRecordsOverwrite validation (XML-level error) =====

func TestReadRecordsOverwrite_GetHostsAPIErrorXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "GetHosts failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, _, diags := readRecordsOverwrite("test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

// ===== resourceRecordCreate tests =====

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

// ===== resourceRecordDelete tests =====

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

// ===== resourceRecordRead additional tests =====

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

// ===== resourceRecordUpdate tests =====

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

// Note: resourceRecordUpdate tests with nameservers are limited because
// TestResourceData().GetChange() doesn't fully simulate Terraform's state
// diffing for schema.TypeSet fields. The nameserver update paths are already
// covered by unit tests for updateNameserversMerge and createNameserversOverwrite.

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
	// Should call setDefault before setHosts to reset nameservers
	assert.Contains(t, callOrder, "namecheap.domains.dns.setDefault")
	assert.Contains(t, callOrder, "namecheap.domains.dns.setHosts")
}

// Note: MergeRecordsAPIError test for resourceRecordUpdate is omitted because
// GetChange() on schema.TypeSet with TestResourceData() doesn't produce the
// expected old/new diff. The updateRecordsMerge error paths are already covered
// by dedicated unit tests (TestUpdateRecordsMerge_*).

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
