//go:build !testacc

package namecheap_provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// In a normal (non-testacc) build the endpoint override must be a no-op so the
// released provider cannot be redirected to an arbitrary API host, even when
// NAMECHEAP_API_URL is set in the environment. This drives the real
// configureContext path (via Configure) and inspects the constructed client.
func TestProviderConfigureIgnoresEndpointOverrideInReleaseBuild(t *testing.T) {
	// A loopback URL is deliberately used: it is exactly what the testacc build
	// WOULD honor, so observing that it is ignored proves the override code is
	// absent from this build rather than merely being rejected by validation.
	t.Setenv("NAMECHEAP_API_URL", "http://127.0.0.1:65535")
	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "unexpected diags: %v", diags)

	client, ok := rawProvider.Meta().(*namecheap.Client)
	assert.True(t, ok, "expected provider meta to be *namecheap.Client")

	// Assert the exact default endpoint (production, since use_sandbox=false) so
	// this catches a redirect to ANY other host, not merely a loopback one.
	wantDefault := namecheap.NewClient(&namecheap.ClientOptions{UseSandbox: false}).BaseURL
	assert.Equal(t, wantDefault, client.BaseURL,
		"release build must leave BaseURL at the default endpoint, ignoring NAMECHEAP_API_URL")
}

// applyTestEndpointOverride must not mutate the client in a release build.
func TestApplyTestEndpointOverrideNoopInReleaseBuild(t *testing.T) {
	t.Setenv("NAMECHEAP_API_URL", "http://127.0.0.1:65535")
	client := namecheap.NewClient(&namecheap.ClientOptions{
		UserName: "u", ApiUser: "u", ApiKey: "k", ClientIp: "203.0.113.1",
	})
	want := client.BaseURL
	applyTestEndpointOverride(client)
	assert.Equal(t, want, client.BaseURL, "release build must leave BaseURL unchanged")
}
