package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetNameservers_Simple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setCustom", r.FormValue("Command"))
		_, _ = fmt.Fprint(w, setCustomSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := setNameservers(context.Background(), "test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
}

func TestSetNameservers_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := setNameservers(context.Background(), "test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}
