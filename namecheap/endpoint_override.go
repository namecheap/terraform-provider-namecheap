//go:build !testacc

package namecheap_provider

import "github.com/namecheap/go-namecheap-sdk/v2/namecheap"

// applyTestEndpointOverride is a no-op in normal (released) builds.
//
// The acceptance-test endpoint override — which reads NAMECHEAP_API_URL to
// point the SDK client at a local mock server — is compiled only under the
// `testacc` build tag (see endpoint_override_testacc.go). Keeping the real
// implementation behind that tag guarantees the released provider binary
// contains no code path that could redirect API traffic (and the credentials
// it carries) to an arbitrary host, even if NAMECHEAP_API_URL is set in the
// environment.
func applyTestEndpointOverride(_ *namecheap.Client) {}
