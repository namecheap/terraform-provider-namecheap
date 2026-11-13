package namecheap_provider

import (
	"context"
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

	diags := createNameserversMerge(context.Background(), "test.com", nameservers, client)
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

	diags := createNameserversMerge(context.Background(), "test.com", nameservers, client)
	assert.False(t, diags.HasError())
}

// A declared nameserver that is already live is adopted: the list is sent with
// it once, next to the live nameservers this resource did not declare.
func TestCreateNameserversMerge_AdoptsLiveNameserver(t *testing.T) {
	cases := []struct {
		name string
		live []string
		want string
	}{
		{"same case", []string{"ns1.example.com", "ns2.example.com"}, "ns1.example.com,ns2.example.com,ns3.example.com"},
		{"another case", []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}, "NS1.EXAMPLE.COM,NS2.EXAMPLE.COM,ns3.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				switch r.FormValue("Command") {
				case "namecheap.domains.dns.getList":
					_, _ = fmt.Fprint(w, getListXML(false, tc.live))
				case "namecheap.domains.dns.setCustom":
					sent = r.FormValue("Nameservers")
					_, _ = fmt.Fprint(w, setCustomSuccessXML())
				}
			}))
			defer server.Close()

			diags := createNameserversMerge(context.Background(), "test.com", []string{"ns1.example.com", "ns3.example.com"}, newTestClient(server.URL))
			assert.False(t, diags.HasError(), "%v", diags)
			assert.Equal(t, tc.want, sent)
		})
	}
}

func TestCreateNameserversOverwrite_Simple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setCustom", r.FormValue("Command"))
		_, _ = fmt.Fprint(w, setCustomSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversOverwrite(context.Background(), "test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
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
	diags := createNameserversMerge(context.Background(), "test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
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
	diags := createNameserversMerge(context.Background(), "test.com", []string{"ns3.new.com", "ns4.new.com"}, client)
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
	diags := createNameserversMerge(context.Background(), "test.com", []string{"ns1.new.com", "ns2.new.com"}, client)
	assert.False(t, diags.HasError())
}

func TestCreateNameserversMerge_GetListAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge(context.Background(), "test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

func TestCreateNameserversOverwrite_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversOverwrite(context.Background(), "test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}
