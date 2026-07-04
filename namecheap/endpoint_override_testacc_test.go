//go:build testacc

package namecheap_provider

import (
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// newClientForOverrideTest returns a client whose BaseURL is the SDK default so
// tests can assert whether applyTestEndpointOverride changed it.
func newClientForOverrideTest() *namecheap.Client {
	return namecheap.NewClient(&namecheap.ClientOptions{
		UserName: "u", ApiUser: "u", ApiKey: "k", ClientIp: "203.0.113.1",
	})
}

// With NAMECHEAP_API_URL unset — or whitespace-only, which TrimSpace reduces to
// empty — the override must leave BaseURL at the endpoint selected by
// use_sandbox.
func TestApplyTestEndpointOverrideUnset(t *testing.T) {
	for _, v := range []string{"", "   ", "\t\n"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(testEndpointOverrideEnv, v)
			client := newClientForOverrideTest()
			want := client.BaseURL
			applyTestEndpointOverride(client)
			assert.Equal(t, want, client.BaseURL, "empty/whitespace override must not change BaseURL")
		})
	}
}

// A loopback http(s) URL must be honored so the acceptance harness can point
// the provider at a local mock server.
func TestApplyTestEndpointOverrideLoopbackHonored(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1:8080",
		"http://127.0.0.1:8080/xml.response",
		"http://localhost:9000",
		"https://127.0.0.1:7000",
		"https://[::1]:7000",
	} {
		t.Run(u, func(t *testing.T) {
			t.Setenv(testEndpointOverrideEnv, u)
			client := newClientForOverrideTest()
			applyTestEndpointOverride(client)
			assert.Equal(t, u, client.BaseURL, "loopback override must be applied")
		})
	}
}

// Whitespace around a loopback URL must be trimmed and the URL still honored.
func TestApplyTestEndpointOverrideTrimsWhitespace(t *testing.T) {
	t.Setenv(testEndpointOverrideEnv, "  http://127.0.0.1:8080  ")
	client := newClientForOverrideTest()
	applyTestEndpointOverride(client)
	assert.Equal(t, "http://127.0.0.1:8080", client.BaseURL)
}

// A non-loopback host, wrong scheme, or malformed value must be ignored so the
// override cannot divert real API traffic to an external host.
func TestApplyTestEndpointOverrideNonLoopbackIgnored(t *testing.T) {
	for _, u := range []string{
		"http://example.com",
		"https://api.namecheap.com/xml.response",
		"http://8.8.8.8",
		"ftp://127.0.0.1",
		"127.0.0.1:8080", // no scheme
		"not-a-url",
		"://bad",
	} {
		t.Run(u, func(t *testing.T) {
			t.Setenv(testEndpointOverrideEnv, u)
			client := newClientForOverrideTest()
			want := client.BaseURL
			applyTestEndpointOverride(client)
			assert.Equal(t, want, client.BaseURL, "non-loopback override must be ignored")
		})
	}
}

// isLoopbackHTTPURL classification, exercised directly for full branch coverage.
func TestIsLoopbackHTTPURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://127.0.0.1", true},
		{"http://127.0.0.1:8080/path", true},
		{"https://127.0.0.53", true},
		{"http://localhost:9000", true},
		{"https://[::1]:7000", true},
		{"http://example.com", false},
		{"https://8.8.8.8", false},
		{"ftp://127.0.0.1", false},
		{"127.0.0.1:8080", false},
		{"not-a-url", false},
		{"://bad", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.want, isLoopbackHTTPURL(c.in))
		})
	}
}
