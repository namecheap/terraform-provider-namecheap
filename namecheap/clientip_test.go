package namecheap_provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withDetectionURL points ipDetectionURL at url for the duration of the test
// and restores the original value afterwards.
func withDetectionURL(t *testing.T, url string) {
	t.Helper()
	original := ipDetectionURL
	ipDetectionURL = url
	t.Cleanup(func() { ipDetectionURL = original })
}

func TestDetectClientIPSuccess(t *testing.T) {
	// A valid IP, padded with surrounding whitespace, must be trimmed and
	// returned verbatim.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("  203.0.113.7\n"))
	}))
	defer server.Close()
	withDetectionURL(t, server.URL)

	ip, err := detectClientIP(context.Background(), server.Client())
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.7", ip)
}

func TestDetectClientIPInvalidBody(t *testing.T) {
	// A 200 response whose body is not an IP address must be rejected.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-an-ip-address"))
	}))
	defer server.Close()
	withDetectionURL(t, server.URL)

	ip, err := detectClientIP(context.Background(), server.Client())
	require.Error(t, err)
	assert.Empty(t, ip)
	assert.Contains(t, err.Error(), "not a valid IP address")
}

func TestDetectClientIPEmptyBody(t *testing.T) {
	// An empty body is likewise not a valid IP address.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	withDetectionURL(t, server.URL)

	ip, err := detectClientIP(context.Background(), server.Client())
	require.Error(t, err)
	assert.Empty(t, ip)
}

func TestDetectClientIPNon200(t *testing.T) {
	// A non-200 status must produce an error even if the body looks like an IP.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("203.0.113.7"))
	}))
	defer server.Close()
	withDetectionURL(t, server.URL)

	ip, err := detectClientIP(context.Background(), server.Client())
	require.Error(t, err)
	assert.Empty(t, ip)
	assert.Contains(t, err.Error(), "HTTP status 500")
}

func TestDetectClientIPTransportFailure(t *testing.T) {
	// A server that has been shut down makes the request fail at the transport
	// layer (connection refused), exercising the httpClient.Do error path.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7"))
	}))
	client := server.Client()
	url := server.URL
	server.Close()
	withDetectionURL(t, url)

	ip, err := detectClientIP(context.Background(), client)
	require.Error(t, err)
	assert.Empty(t, ip)
}
