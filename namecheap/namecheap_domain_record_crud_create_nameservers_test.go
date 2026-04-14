package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestCreateNameserversMerge_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

func TestCreateNameserversOverwrite_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversOverwrite("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}
