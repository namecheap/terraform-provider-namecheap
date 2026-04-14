package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestReadRecordsOverwrite_GetHostsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, _, diags := readRecordsOverwrite("test.com", []interface{}{}, client)
	assert.Nil(t, result)
	assert.True(t, diags.HasError())
}

func TestReadRecordsMerge_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, _, diags := readRecordsMerge("test.com", []interface{}{}, client)
	assert.Nil(t, result)
	assert.True(t, diags.HasError())
}
