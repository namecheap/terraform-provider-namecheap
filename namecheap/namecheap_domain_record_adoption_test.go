package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
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

// setHostsRecorder serves a zone holding www and home and records the hostnames
// of every setHosts call, i.e. what the provider decided to keep at Namecheap.
type setHostsRecorder struct {
	mu    sync.Mutex
	calls [][]string
}

func (rec *setHostsRecorder) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "home", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			var hostnames []string
			for key, values := range r.Form {
				if strings.HasPrefix(key, "HostName") {
					hostnames = append(hostnames, values[0])
				}
			}
			rec.mu.Lock()
			rec.calls = append(rec.calls, hostnames)
			rec.mu.Unlock()
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		}
	}))
}

func (rec *setHostsRecorder) sent() [][]string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.calls
}

// planAndApply drives the real plan -> apply path (Diff runs CustomizeDiff, Apply
// runs Update) from a prior state, the way Terraform does, so the old/new values
// Update sees are the prior state and the planned configuration.
func planAndApply(t *testing.T, serverURL string, prior *terraform.InstanceState, records ...map[string]interface{}) (*terraform.InstanceDiff, *terraform.InstanceState, diag.Diagnostics) {
	t.Helper()

	var recordList []interface{}
	for _, record := range records {
		recordList = append(recordList, record)
	}
	config := terraform.NewResourceConfigRaw(map[string]interface{}{
		"domain": "test.com",
		"mode":   ncModeMerge,
		"record": recordList,
	})

	client := newTestClient(serverURL)
	resource := resourceNamecheapDomainRecords()
	diff, err := resource.Diff(context.TODO(), prior, config, client)
	require.NoError(t, err)
	require.NotNil(t, diff, "an apply must be planned")

	state, diags := resource.Apply(context.TODO(), prior, diff, client)
	return diff, state, diags
}

func priorState(t *testing.T, adopted bool, records ...map[string]interface{}) *terraform.InstanceState {
	t.Helper()
	return persistedRecordsResource(t, adopted, records...).State()
}

func TestUpdate_AdoptedMerge_KeepsImportedRecordsAtNamecheap(t *testing.T) {
	rec := &setHostsRecorder{}
	server := rec.server()
	defer server.Close()

	// State holds the whole imported zone; the configuration declares only www.
	prior := priorState(t, true,
		adoptionTestRecord("www", "A", "1.2.3.4"),
		adoptionTestRecord("home", "A", "5.6.7.8"))

	_, state, diags := planAndApply(t, server.URL, prior, adoptionTestRecord("www", "A", "1.2.3.4"))

	require.False(t, diags.HasError())
	require.Len(t, rec.sent(), 1)
	assert.ElementsMatch(t, []string{"www", "home"}, rec.sent()[0], "the imported home record must be sent back, not dropped")
	assert.Equal(t, "false", state.Attributes["adopted"], "the first apply settles ownership")
	assert.Equal(t, "1", state.Attributes["record.#"], "home leaves state")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.Warning, diags[0].Severity)
	assert.Contains(t, diags[0].Summary, "Released 1 imported item(s)")
	assert.Contains(t, diags[0].Detail, "hostname = home")
}

// The contrast that makes the test above meaningful: the same change on a
// resource that was not imported is a genuine removal and is still deleted.
func TestUpdate_NotAdoptedMerge_StillRemovesDroppedRecord(t *testing.T) {
	rec := &setHostsRecorder{}
	server := rec.server()
	defer server.Close()

	prior := priorState(t, false,
		adoptionTestRecord("www", "A", "1.2.3.4"),
		adoptionTestRecord("home", "A", "5.6.7.8"))

	diff, state, diags := planAndApply(t, server.URL, prior, adoptionTestRecord("www", "A", "1.2.3.4"))

	require.False(t, diags.HasError())
	require.Len(t, rec.sent(), 1)
	assert.Equal(t, []string{"www"}, rec.sent()[0])
	assert.Empty(t, diags, "no release warning for a resource that was not imported")
	assert.NotContains(t, diff.Attributes, "adopted", "an ordinary update plan must not mention the marker")
	assert.NotContains(t, state.Attributes, "adopted", "an ordinary update must not write the marker")
}

func TestUpdate_AdoptedMerge_MalformedRecordFailsBeforeAnyWrite(t *testing.T) {
	rec := &setHostsRecorder{}
	server := rec.server()
	defer server.Close()

	// A CAA address needs three parts; this one has two.
	prior := priorState(t, true, adoptionTestRecord("www", "A", "1.2.3.4"))

	_, _, diags := planAndApply(t, server.URL, prior, adoptionTestRecord("caa", "CAA", "0 issue"))

	assert.True(t, diags.HasError())
	assert.Empty(t, rec.sent(), "nothing may be written when the records cannot be compared")
}

// CustomizeDiff: an imported resource always plans the apply that settles it,
// even when the configuration matches the zone exactly; an ordinary resource
// with no changes plans nothing.
func TestCustomizeDiff_AdoptedAlwaysPlansSettlingApply(t *testing.T) {
	config := terraform.NewResourceConfigRaw(map[string]interface{}{
		"domain": "test.com",
		"mode":   ncModeMerge,
		"record": []interface{}{adoptionTestRecord("www", "A", "1.2.3.4")},
	})
	resource := resourceNamecheapDomainRecords()

	t.Run("adopted, configuration matches", func(t *testing.T) {
		prior := priorState(t, true, adoptionTestRecord("www", "A", "1.2.3.4"))
		diff, err := resource.Diff(context.TODO(), prior, config, nil)
		require.NoError(t, err)
		require.NotNil(t, diff)
		require.Contains(t, diff.Attributes, "adopted")
		assert.True(t, diff.Attributes["adopted"].NewComputed)
	})

	t.Run("not adopted, configuration matches", func(t *testing.T) {
		prior := priorState(t, false, adoptionTestRecord("www", "A", "1.2.3.4"))
		diff, err := resource.Diff(context.TODO(), prior, config, nil)
		require.NoError(t, err)
		assert.Nil(t, diff, "an unchanged, non-imported resource must plan nothing")
	})
}

func TestReleasedRecords_MalformedAddressIsAnError(t *testing.T) {
	valid := adoptionTestRecord("www", "A", "1.2.3.4")
	malformed := adoptionTestRecord("caa", "CAA", "0 issue")

	_, err := releasedRecords([]interface{}{valid}, []interface{}{malformed})
	assert.Error(t, err, "malformed declared record")

	_, err = releasedRecords([]interface{}{malformed}, []interface{}{valid})
	assert.Error(t, err, "malformed imported record")
}

// If the first apply after import fails part-way, whatever state Terraform is
// left with must never make the retry delete the undeclared imported records.
func TestUpdate_AdoptedMerge_FailedFirstApplyDoesNotArmDeletion(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "home", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		default:
			_, _ = fmt.Fprint(w, apiErrorXML("2019166", "temporary failure"))
		}
	}))
	defer failing.Close()

	prior := priorState(t, true,
		adoptionTestRecord("www", "A", "1.2.3.4"),
		adoptionTestRecord("home", "A", "5.6.7.8"))
	www := adoptionTestRecord("www", "A", "1.2.3.4")

	_, afterFailure, diags := planAndApply(t, failing.URL, prior, www)
	require.True(t, diags.HasError(), "the first apply is meant to fail")
	require.NotNil(t, afterFailure)

	// The invariant that keeps a retry safe: after the failure the imported home
	// record is either no longer in state (so it can never be deleted by this
	// resource) or the marker still protects it. Today the SDK stores the planned
	// state, i.e. the first case.
	homeInState := false
	for key, value := range afterFailure.Attributes {
		if strings.HasSuffix(key, ".hostname") && value == "home" {
			homeInState = true
		}
	}
	assert.True(t, !homeInState || afterFailure.Attributes["adopted"] == "true",
		"home is in state without the marker: the next apply would delete it")

	// Retry against a healthy API from exactly the state the failure left.
	rec := &setHostsRecorder{}
	healthy := rec.server()
	defer healthy.Close()

	config := terraform.NewResourceConfigRaw(map[string]interface{}{
		"domain": "test.com",
		"mode":   ncModeMerge,
		"record": []interface{}{www},
	})
	client := newTestClient(healthy.URL)
	resource := resourceNamecheapDomainRecords()
	diff, err := resource.Diff(context.TODO(), afterFailure, config, client)
	require.NoError(t, err)
	if diff == nil {
		return // nothing planned, so nothing can be deleted
	}
	_, retryDiags := resource.Apply(context.TODO(), afterFailure, diff, client)

	require.False(t, retryDiags.HasError())
	for _, sent := range rec.sent() {
		assert.Contains(t, sent, "home", "the retry must not delete the imported home record")
	}
}
