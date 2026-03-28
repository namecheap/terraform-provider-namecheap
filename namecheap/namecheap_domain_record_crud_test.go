package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "www", r.FormValue("HostName1"))
			assert.Equal(t, "A", r.FormValue("RecordType1"))
			assert.Equal(t, "1.2.3.4", r.FormValue("Address1"))
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			// Should contain both existing and new records
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "MX", r.FormValue("EmailType"))
			fmt.Fprint(w, setHostsSuccessXML())
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
		fmt.Fprint(w, apiErrorXML("123456", "API error"))
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
		fmt.Fprint(w, setHostsSuccessXML())
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
		fmt.Fprint(w, setHostsSuccessXML())
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
		fmt.Fprint(w, setHostsSuccessXML())
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
		fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
		fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
		fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
		fmt.Fprint(w, getHostsXML("NONE", nil))
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
		fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
		fmt.Fprint(w, getHostsXML("NONE", nil))
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.old.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			assert.Equal(t, "MX", r.FormValue("EmailType"))
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			for i := 1; ; i++ {
				if r.FormValue(fmt.Sprintf("RecordType%d", i)) == "" {
					break
				}
				setHostsRecordCount++
			}
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.test.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			// After removing MX record, email type should be resolved to NONE
			assert.Equal(t, "NONE", r.FormValue("EmailType"))
			fmt.Fprint(w, setHostsSuccessXML())
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
		fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			assert.Contains(t, r.FormValue("Nameservers"), "ns1.example.com")
			assert.Contains(t, r.FormValue("Nameservers"), "ns2.example.com")
			fmt.Fprint(w, setCustomSuccessXML())
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
			fmt.Fprint(w, getListXML(false, []string{"ns1.existing.com", "ns2.existing.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns1.existing.com")
			assert.Contains(t, ns, "ns2.existing.com")
			assert.Contains(t, ns, "ns3.new.com")
			assert.Contains(t, ns, "ns4.new.com")
			fmt.Fprint(w, setCustomSuccessXML())
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
			fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com"}))
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
			fmt.Fprint(w, getListXML(false, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}))
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
		fmt.Fprint(w, setCustomSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversOverwrite("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
}

// ===== readNameserversMerge tests =====

func TestReadNameserversMerge_FindsMatching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com", "ns3.other.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversMerge_CaseInsensitiveMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, getListXML(false, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com"}, *result)
}

func TestReadNameserversMerge_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, getListXML(true, nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameserversMerge_NoMatchFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, getListXML(false, []string{"ns1.other.com", "ns2.other.com"}))
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
		fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversOverwrite_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, getListXML(true, nil))
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
			fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com", "ns3.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns3.manual.com")
			assert.Contains(t, ns, "ns1.new.com")
			assert.Contains(t, ns, "ns2.new.com")
			assert.NotContains(t, ns, "ns1.old.com")
			assert.NotContains(t, ns, "ns2.old.com")
			fmt.Fprint(w, setCustomSuccessXML())
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
			fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.manual.com"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// Removing ns1.old.com leaves only ns2.manual.com (1 NS), which is invalid
	prev := []string{"ns1.old.com"}
	current := []string{}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "one remained nameserver")
}

func TestUpdateNameserversMerge_ZeroRemains_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com"}))
		case "namecheap.domains.dns.setDefault":
			setDefaultCalled = true
			fmt.Fprint(w, setDefaultSuccessXML())
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
			fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			fmt.Fprint(w, setCustomSuccessXML())
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
			fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com", "ns3.manual.com", "ns4.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns3.manual.com")
			assert.Contains(t, ns, "ns4.manual.com")
			assert.NotContains(t, ns, "ns1.managed.com")
			assert.NotContains(t, ns, "ns2.managed.com")
			fmt.Fprint(w, setCustomSuccessXML())
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
			fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.manual.com"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com"}, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "one remained nameserver")
}

func TestDeleteNameserversMerge_AllRemoved_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com"}))
		case "namecheap.domains.dns.setDefault":
			setDefaultCalled = true
			fmt.Fprint(w, setDefaultSuccessXML())
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
			fmt.Fprint(w, getListXML(true, nil))
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
		fmt.Fprint(w, setDefaultSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", nil))
		case "namecheap.domains.dns.setHosts":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?>
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
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

func TestReadNameserversMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
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
		fmt.Fprint(w, "internal error")
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
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteRecordsMerge("test.com", []interface{}{}, client)
	assert.True(t, diags.HasError())
}

func TestUpdateRecordsMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "CNAME", Address: "old.example.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			fmt.Fprint(w, setHostsSuccessXML())
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
			fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
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
			fmt.Fprint(w, getHostsXML("MX", []hostEntry{
				{Name: "@", Type: "MX", Address: "mail.test.com.", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			// Email type should be preserved since MX records still exist
			assert.Equal(t, "MX", r.FormValue("EmailType"))
			fmt.Fprint(w, setHostsSuccessXML())
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
