package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func adoptionTestRecord(hostname, recordType, address string) map[string]interface{} {
	return map[string]interface{}{"hostname": hostname, "type": recordType, "address": address, "mx_pref": 10, "ttl": 1800}
}

// persistedRecordsResource returns a resource as Terraform would hand it to
// Delete: built from a prior state that holds the given records and, when
// adopted is true, the import marker.
func persistedRecordsResource(t *testing.T, adopted bool, records ...map[string]interface{}) *schema.ResourceData {
	t.Helper()

	var recordList []interface{}
	for _, record := range records {
		recordList = append(recordList, record)
	}

	seed := resourceNamecheapDomainRecords().TestResourceData()
	seed.SetId("test.com")
	require.NoError(t, seed.Set("domain", "test.com"))
	require.NoError(t, seed.Set("mode", ncModeMerge))
	require.NoError(t, seed.Set("record", recordList))
	if adopted {
		require.NoError(t, seed.Set("adopted", true))
	}

	return resourceNamecheapDomainRecords().Data(seed.State())
}

func TestReadImportMode_MarksResourceAdopted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "home", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		}
	}))
	defer server.Close()

	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId("test.com")
	_ = data.Set("domain", "test.com")
	_ = data.Set("mode", ncModeImport)

	diags := resourceRecordRead(context.TODO(), data, newTestClient(server.URL))

	assert.False(t, diags.HasError())
	assert.Equal(t, true, data.Get("adopted"))
	assert.Equal(t, 2, data.Get("record").(*schema.Set).Len())
}

// Compatibility: only an import ever writes the marker. A refresh of any other
// resource must leave it out of state entirely.
func TestRead_NotImported_NeverWritesAdopted(t *testing.T) {
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

	for _, mode := range []string{ncModeMerge, ncModeOverwrite} {
		t.Run(mode, func(t *testing.T) {
			data := persistedRecordsResource(t, false, adoptionTestRecord("www", "A", "1.2.3.4"))
			require.NoError(t, data.Set("mode", mode))

			diags := resourceRecordRead(context.TODO(), data, newTestClient(server.URL))

			assert.False(t, diags.HasError())
			_, written := data.State().Attributes["adopted"]
			assert.False(t, written, "adopted must not appear in state of a resource that was not imported")
		})
	}
}

func countingServer(calls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		calls.Add(1)
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "home", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		default:
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
}

func TestDelete_Adopted_MakesNoAPICalls(t *testing.T) {
	var calls atomic.Int32
	server := countingServer(&calls)
	defer server.Close()

	data := persistedRecordsResource(t, true,
		adoptionTestRecord("www", "A", "1.2.3.4"),
		adoptionTestRecord("home", "A", "5.6.7.8"))

	diags := resourceRecordDelete(context.TODO(), data, newTestClient(server.URL))

	assert.False(t, diags.HasError())
	assert.EqualValues(t, 0, calls.Load(), "destroying an imported, never-applied resource must not touch Namecheap")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.Warning, diags[0].Severity)
	assert.Contains(t, diags[0].Summary, "Nothing was deleted")
}

// The same state without the marker is an ordinary managed resource: its
// records are still deleted.
func TestDelete_NotAdopted_StillDeletes(t *testing.T) {
	var calls atomic.Int32
	server := countingServer(&calls)
	defer server.Close()

	data := persistedRecordsResource(t, false, adoptionTestRecord("www", "A", "1.2.3.4"))

	diags := resourceRecordDelete(context.TODO(), data, newTestClient(server.URL))

	assert.False(t, diags.HasError())
	assert.Positive(t, calls.Load())
}

func TestReleasedRecords(t *testing.T) {
	www := adoptionTestRecord("www", "A", "1.2.3.4")
	home := adoptionTestRecord("home", "A", "5.6.7.8")

	t.Run("undeclared imported record is released", func(t *testing.T) {
		released, err := releasedRecords([]interface{}{www, home}, []interface{}{www})
		require.NoError(t, err)
		require.Len(t, released, 1)
		assert.Equal(t, "home", *released[0].HostName)
	})

	t.Run("everything declared releases nothing", func(t *testing.T) {
		released, err := releasedRecords([]interface{}{www, home}, []interface{}{home, www})
		require.NoError(t, err)
		assert.Empty(t, released)
	})

	t.Run("nothing declared releases everything", func(t *testing.T) {
		released, err := releasedRecords([]interface{}{www, home}, nil)
		require.NoError(t, err)
		assert.Len(t, released, 2)
	})

	t.Run("a changed ttl is the same record", func(t *testing.T) {
		retimed := adoptionTestRecord("www", "A", "1.2.3.4")
		retimed["ttl"] = 300
		released, err := releasedRecords([]interface{}{www}, []interface{}{retimed})
		require.NoError(t, err)
		assert.Empty(t, released)
	})

	t.Run("matching ignores case and the CNAME trailing dot", func(t *testing.T) {
		live := adoptionTestRecord("Blog", "CNAME", "target.example.com")
		declared := adoptionTestRecord("blog", "CNAME", "target.example.com.")
		released, err := releasedRecords([]interface{}{live}, []interface{}{declared})
		require.NoError(t, err)
		assert.Empty(t, released)
	})
}

func TestReleasedNameservers(t *testing.T) {
	assert.Equal(t,
		[]string{"ns3.example.net"},
		releasedNameservers(
			[]string{"ns1.example.net", "NS2.example.net", "ns3.example.net"},
			[]string{"NS1.example.net", "ns2.example.net"}))
	assert.Empty(t, releasedNameservers([]string{"ns1.example.net"}, []string{"ns1.example.net"}))
	assert.Empty(t, releasedNameservers(nil, []string{"ns1.example.net"}))
}

func TestBuildReleasedWarning(t *testing.T) {
	released, err := releasedRecords(
		[]interface{}{adoptionTestRecord("home", "A", "5.6.7.8")}, nil)
	require.NoError(t, err)

	warning := buildReleasedWarning("test.com", released, []string{"ns3.example.net"})

	assert.Equal(t, diag.Warning, warning.Severity)
	assert.Contains(t, warning.Summary, "Released 2 imported item(s) on test.com")
	assert.Contains(t, warning.Detail, "hostname = home, type = A, address = 5.6.7.8")
	assert.Contains(t, warning.Detail, "nameserver ns3.example.net")
	assert.Contains(t, warning.Detail, "NOT changed at Namecheap")
}
