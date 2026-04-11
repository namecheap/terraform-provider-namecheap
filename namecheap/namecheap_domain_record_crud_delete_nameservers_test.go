package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestDeleteNameserversOverwrite_SetDefaultAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversOverwrite("test.com", client)
	assert.True(t, diags.HasError())
}
