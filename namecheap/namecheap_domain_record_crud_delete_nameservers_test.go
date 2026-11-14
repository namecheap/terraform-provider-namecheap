package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResetNameservers_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setDefault", r.FormValue("Command"))
		setDefaultCalled = true
		_, _ = fmt.Fprint(w, setDefaultSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := resetNameservers(context.Background(), "test.com", client)
	assert.False(t, diags.HasError())
	assert.True(t, setDefaultCalled)
}

func TestResetNameservers_SetDefaultAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := resetNameservers(context.Background(), "test.com", client)
	assert.True(t, diags.HasError())
}
