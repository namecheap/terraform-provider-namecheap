package namecheap_provider

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// ipDetectionURL is the endpoint queried to auto-detect the caller's public IP
// when client_ip is left unset. It is a fixed, provider-controlled constant
// (never derived from user input) and is always fetched over HTTPS with the
// caller-supplied *http.Client, whose transport performs standard TLS
// certificate verification. It is a package-level var only so tests can point
// detectClientIP at an httptest server; it is not exposed as configuration.
var ipDetectionURL = "https://api.ipify.org"

// maxDetectionBodyBytes caps how many bytes are read from the IP-detection
// response. Any valid IPv4/IPv6 text form fits comfortably under this; the cap
// guards against a misbehaving endpoint streaming an unbounded body within the
// request timeout (which bounds elapsed time but not bytes).
const maxDetectionBodyBytes = 512

// detectClientIP fetches this machine's public IP address from ipDetectionURL
// using the provided httpClient and returns it as a string.
//
// The response body is trimmed and validated with net.ParseIP. A non-200
// status, an unreadable body, or a body that is not a valid IP address each
// produce an error. TLS verification is never disabled; the transport of the
// supplied httpClient is used as-is.
func detectClientIP(ctx context.Context, httpClient *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipDetectionURL, nil)
	if err != nil {
		return "", fmt.Errorf("building public IP detection request for %s: %w", ipDetectionURL, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting public IP from %s: %w", ipDetectionURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("public IP detection from %s returned HTTP status %d", ipDetectionURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDetectionBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading public IP detection response from %s: %w", ipDetectionURL, err)
	}

	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("public IP detection from %s returned %q, which is not a valid IP address", ipDetectionURL, ip)
	}

	return ip, nil
}
