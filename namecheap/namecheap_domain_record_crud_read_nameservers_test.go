package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
