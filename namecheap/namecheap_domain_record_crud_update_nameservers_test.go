package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
