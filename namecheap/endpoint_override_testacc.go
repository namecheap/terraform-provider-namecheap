//go:build testacc

package namecheap_provider

import (
	"log"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// testEndpointOverrideEnv names the environment variable that, in `testacc`
// builds only, redirects the Namecheap SDK client at an alternate base URL.
// It exists solely so acceptance tests can point the provider at a local
// httptest mock server; it is never compiled into a released provider binary.
const testEndpointOverrideEnv = "NAMECHEAP_API_URL"

// applyTestEndpointOverride overrides the SDK client's BaseURL from
// NAMECHEAP_API_URL when that variable names a loopback http(s) URL.
//
// Only loopback hosts are honored: even in a test binary this prevents the
// override from being abused to divert real API traffic (and the credentials
// it carries) to an external host. A non-loopback or malformed value is
// ignored with a warning rather than silently trusted.
func applyTestEndpointOverride(client *namecheap.Client) {
	raw := strings.TrimSpace(os.Getenv(testEndpointOverrideEnv))
	if raw == "" {
		return
	}
	if !isLoopbackHTTPURL(raw) {
		log.Printf("[WARN] namecheap: ignoring %s=%q; only http(s) loopback URLs are honored", testEndpointOverrideEnv, raw)
		return
	}
	client.BaseURL = raw
}

// isLoopbackHTTPURL reports whether raw is an http or https URL whose host is a
// loopback address (127.0.0.0/8, ::1) or the literal "localhost".
//
// The "localhost" match is exact and case-sensitive with no trailing dot, so
// harness authors should use a lowercase "localhost" or a 127.x.x.x / [::1]
// literal; any other spelling falls through to the default endpoint.
func isLoopbackHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
