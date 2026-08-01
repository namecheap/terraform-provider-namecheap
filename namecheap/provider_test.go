package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

var testAccNamecheapProvider *schema.Provider
var testAccProviderFactories map[string]func() (*schema.Provider, error)
var namecheapSDKClient *namecheap.Client
var testAccDomain *string

// testPlaceholderClientIP is intentionally a non-routable placeholder used in
// provider configuration tests that do not require a real whitelisted client IP.
const testPlaceholderClientIP = "0.0.0.0"

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// acceptanceRequestsPerMinute returns the per-client share of the account's
// request budget for the live suite, read from the same NAMECHEAP_REQUESTS_PER_MINUTE
// variable the provider itself honours. It falls back to half the documented
// 20/min quota, because two clients share that quota (see namecheapSDKClient).
// An unparsable or out-of-range value falls back too rather than failing the
// suite on a misconfigured environment.
func acceptanceRequestsPerMinute() int {
	const fallback = defaultRequestsPerMinute / 2
	raw := os.Getenv("NAMECHEAP_REQUESTS_PER_MINUTE")
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minRequestsPerMinute || n > maxRequestsPerMinute {
		return fallback
	}
	return n
}

// acceptanceMaxRetries returns the retry-attempt budget for the live suite,
// read from the same NAMECHEAP_MAX_RETRIES variable the provider honours.
// Anything unusable falls back to the provider's own default.
func acceptanceMaxRetries() int {
	raw := os.Getenv("NAMECHEAP_MAX_RETRIES")
	if raw == "" {
		return defaultMaxRetries
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultMaxRetries
	}
	return n
}

// acceptanceRetryMaxElapsed returns the wall-clock retry budget for the live
// suite, read from the same NAMECHEAP_RETRY_MAX_ELAPSED variable the provider
// honours. Anything unusable falls back to the provider's own default.
func acceptanceRetryMaxElapsed() time.Duration {
	return acceptanceDuration("NAMECHEAP_RETRY_MAX_ELAPSED", defaultRetryMaxElapsed)
}

// acceptanceDuration reads a duration from env, falling back to the provider's
// own default string when it is unset or unusable, so the helper client and the
// provider under test are always tuned alike.
func acceptanceDuration(envVar, fallbackRaw string) time.Duration {
	fallback, _ := time.ParseDuration(fallbackRaw)
	raw := os.Getenv(envVar)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func init() {
	testAccNamecheapProvider = Provider()
	testAccProviderFactories = map[string]func() (*schema.Provider, error){
		"namecheap": func() (*schema.Provider, error) {
			return testAccNamecheapProvider, nil
		},
	}
	namecheapSDKClient = namecheap.NewClient(&namecheap.ClientOptions{
		UserName:   os.Getenv("NAMECHEAP_USER_NAME"),
		ApiUser:    os.Getenv("NAMECHEAP_API_USER"),
		ApiKey:     os.Getenv("NAMECHEAP_API_KEY"),
		ClientIp:   getEnvOrDefault("NAMECHEAP_CLIENT_IP", testPlaceholderClientIP),
		UseSandbox: strings.EqualFold(os.Getenv("NAMECHEAP_USE_SANDBOX"), "true"),
		// Namecheap's rate limit is per account, but limiters are per client, and
		// the live suite runs two of them: the provider under test, and this one
		// behind the setup/teardown helpers. Left at the default they would allow
		// 20/min each against a 20/min quota, so the account exceeds it, the API
		// answers 405, and the SDK's retries turn a healthy test into "after 4
		// attempts: retryable HTTP status". This client takes the same budget the
		// provider is given (NAMECHEAP_REQUESTS_PER_MINUTE), so the two together
		// stay inside the quota.
		RateLimit: &namecheap.RateLimitOptions{PerMinute: acceptanceRequestsPerMinute()},
		// Sharing the quota makes each request wait longer for a token, and that
		// wait counts against the retry budget. The helper client therefore takes
		// the same enlarged budget the provider is given, so a throttled call is
		// retried until the window reopens instead of giving up mid-suite.
		Retry: &namecheap.RetryOptions{
			MaxAttempts: acceptanceMaxRetries(),
			MaxElapsed:  acceptanceRetryMaxElapsed(),
			BaseDelay:   acceptanceDuration("NAMECHEAP_RETRY_BASE_DELAY", defaultRetryBaseDelay),
			MaxDelay:    acceptanceDuration("NAMECHEAP_RETRY_MAX_DELAY", defaultRetryMaxDelay),
		},
	})

	testDomain := os.Getenv("NAMECHEAP_TEST_DOMAIN")
	testAccDomain = &testDomain
}

func testAccPreCheck(t *testing.T) {
	t.Helper()

	requiredEnvVars := []string{
		"NAMECHEAP_USER_NAME",
		"NAMECHEAP_API_USER",
		"NAMECHEAP_API_KEY",
		"NAMECHEAP_TEST_DOMAIN",
	}

	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			t.Skipf("%s must be set for acceptance testing", envVar)
		}
	}
}

// Unit tests

func TestProviderSchemaValid(t *testing.T) {
	assert.NoError(t, Provider().InternalValidate())
}

func TestProviderCredentialFieldsAreOptional(t *testing.T) {
	p := Provider()
	for _, field := range []string{"user_name", "api_user", "api_key"} {
		s, ok := p.Schema[field]
		assert.True(t, ok, "field %s should exist", field)
		assert.True(t, s.Optional, "field %s should be Optional", field)
		assert.False(t, s.Required, "field %s should not be Required", field)
	}
}

func TestProviderCredentialFieldsAreSensitive(t *testing.T) {
	p := Provider()
	for _, field := range []string{"user_name", "api_user", "api_key"} {
		s, ok := p.Schema[field]
		assert.True(t, ok, "field %s should exist", field)
		assert.True(t, s.Sensitive, "field %s should be Sensitive", field)
	}
	for _, field := range []string{"client_ip", "use_sandbox"} {
		s, ok := p.Schema[field]
		assert.True(t, ok, "field %s should exist", field)
		assert.False(t, s.Sensitive, "field %s should not be Sensitive", field)
	}
}

func TestProviderConfigureFromEnvVars(t *testing.T) {
	envVars := map[string]string{
		"NAMECHEAP_USER_NAME": "test-user",
		"NAMECHEAP_API_USER":  "test-api-user",
		"NAMECHEAP_API_KEY":   "test-api-key",
	}
	for k, v := range envVars {
		t.Setenv(k, v)
	}

	rawProvider := Provider()
	raw := map[string]interface{}{
		"client_ip":   testPlaceholderClientIP,
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors when env vars are set, got: %v", diags)
}

func TestProviderConfigureMissingCredentials(t *testing.T) {
	for _, k := range []string{"NAMECHEAP_USER_NAME", "NAMECHEAP_API_USER", "NAMECHEAP_API_KEY"} {
		t.Setenv(k, "")
	}

	rawProvider := Provider()
	raw := map[string]interface{}{
		"client_ip":   testPlaceholderClientIP,
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.True(t, diags.HasError(), "expected error when all credentials are missing")
	assert.Contains(t, diags[0].Detail, "user_name")
	assert.Contains(t, diags[0].Detail, "api_user")
	assert.Contains(t, diags[0].Detail, "api_key")
}

func TestProviderConfigureFromInlineConfig(t *testing.T) {
	for _, k := range []string{"NAMECHEAP_USER_NAME", "NAMECHEAP_API_USER", "NAMECHEAP_API_KEY"} {
		t.Setenv(k, "")
	}

	rawProvider := Provider()
	raw := map[string]interface{}{
		"user_name":   "inline-user",
		"api_user":    "inline-api-user",
		"api_key":     "inline-api-key",
		"client_ip":   testPlaceholderClientIP,
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors when credentials are set inline, got: %v", diags)
}

func TestProviderConfigurePartialCredentials(t *testing.T) {
	t.Setenv("NAMECHEAP_USER_NAME", "test-user")
	t.Setenv("NAMECHEAP_API_USER", "")
	t.Setenv("NAMECHEAP_API_KEY", "")

	rawProvider := Provider()
	raw := map[string]interface{}{
		"client_ip":   testPlaceholderClientIP,
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.True(t, diags.HasError(), "expected error when some credentials are missing")
	assert.NotContains(t, diags[0].Detail, "user_name", "user_name should not be listed as missing")
	assert.Contains(t, diags[0].Detail, "api_user")
	assert.Contains(t, diags[0].Detail, "api_key")
}

func TestProviderConfigureWhitespaceOnlyCredentials(t *testing.T) {
	for _, k := range []string{"NAMECHEAP_USER_NAME", "NAMECHEAP_API_USER", "NAMECHEAP_API_KEY"} {
		t.Setenv(k, "")
	}

	rawProvider := Provider()
	raw := map[string]interface{}{
		"user_name":   "  ",
		"api_user":    "\t",
		"api_key":     " \n ",
		"client_ip":   testPlaceholderClientIP,
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.True(t, diags.HasError(), "expected error when credentials are whitespace-only")
	assert.Contains(t, diags[0].Detail, "user_name")
	assert.Contains(t, diags[0].Detail, "api_user")
	assert.Contains(t, diags[0].Detail, "api_key")
}

// client_ip auto-detection: the configureContext `if clientIp == ""` branch.

// setRequiredCredentialsWithoutClientIP sets the three required credential env
// vars and clears NAMECHEAP_CLIENT_IP so that an ambient value cannot leak in
// and suppress the auto-detect branch under test.
func setRequiredCredentialsWithoutClientIP(t *testing.T) {
	t.Helper()
	t.Setenv("NAMECHEAP_USER_NAME", "test-user")
	t.Setenv("NAMECHEAP_API_USER", "test-api-user")
	t.Setenv("NAMECHEAP_API_KEY", "test-api-key")
	t.Setenv("NAMECHEAP_CLIENT_IP", "")
}

func TestProviderConfigureAutoDetectsClientIPWhenUnset(t *testing.T) {
	setRequiredCredentialsWithoutClientIP(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7"))
	}))
	defer server.Close()
	withDetectionURL(t, server.URL)

	rawProvider := Provider()
	raw := map[string]interface{}{
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors on successful auto-detect, got: %v", diags)

	client := rawProvider.Meta().(*namecheap.Client)
	assert.Equal(t, "203.0.113.7", client.ClientOptions.ClientIp)
}

func TestProviderConfigureAutoDetectClientIPFailure(t *testing.T) {
	setRequiredCredentialsWithoutClientIP(t)

	// A server that has been shut down makes detection fail at the transport
	// layer (connection refused), exercising the diag.Error path.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	url := server.URL
	server.Close()
	withDetectionURL(t, url)

	rawProvider := Provider()
	raw := map[string]interface{}{
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.True(t, diags.HasError(), "expected error when auto-detect fails")
	assert.Equal(t, "Unable to auto-detect client_ip", diags[0].Summary)
	assert.Equal(t, cty.Path{cty.GetAttrStep{Name: "client_ip"}}, diags[0].AttributePath)
}

func TestProviderConfigureExplicitClientIPHonored(t *testing.T) {
	setRequiredCredentialsWithoutClientIP(t)

	// Point detection at a server that would return a different IP, to prove the
	// resolver is never consulted when client_ip is explicitly set.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7"))
	}))
	defer server.Close()
	withDetectionURL(t, server.URL)

	rawProvider := Provider()
	raw := map[string]interface{}{
		"client_ip":   "198.51.100.42",
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors when client_ip is set inline, got: %v", diags)

	client := rawProvider.Meta().(*namecheap.Client)
	assert.Equal(t, "198.51.100.42", client.ClientOptions.ClientIp)
}

func TestProviderConfigureWhitespaceClientIPTriggersDetection(t *testing.T) {
	setRequiredCredentialsWithoutClientIP(t)

	// A whitespace-only client_ip is trimmed to "" and must trigger auto-detect
	// rather than being passed through to the SDK verbatim.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7"))
	}))
	defer server.Close()
	withDetectionURL(t, server.URL)

	rawProvider := Provider()
	raw := map[string]interface{}{
		"client_ip":   "   ",
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors, got: %v", diags)

	client := rawProvider.Meta().(*namecheap.Client)
	assert.Equal(t, "203.0.113.7", client.ClientOptions.ClientIp)
}

// Resilience config options: requests_per_minute, max_retries,
// retry_max_elapsed, retry_base_delay, retry_max_delay, request_timeout.

const (
	testEnvRequestsPerMinute = "NAMECHEAP_REQUESTS_PER_MINUTE"
	testEnvMaxRetries        = "NAMECHEAP_MAX_RETRIES"
	testEnvRetryMaxElapsed   = "NAMECHEAP_RETRY_MAX_ELAPSED"
	testEnvRetryBaseDelay    = "NAMECHEAP_RETRY_BASE_DELAY"
	testEnvRetryMaxDelay     = "NAMECHEAP_RETRY_MAX_DELAY"
	testEnvRequestTimeout    = "NAMECHEAP_REQUEST_TIMEOUT"
)

// clearResilienceEnvVars ensures the four new env vars are unset for the
// duration of a test, so an ambient developer/CI environment cannot leak in.
func clearResilienceEnvVars(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		testEnvRequestsPerMinute, testEnvMaxRetries, testEnvRetryMaxElapsed,
		testEnvRetryBaseDelay, testEnvRetryMaxDelay, testEnvRequestTimeout,
	} {
		t.Setenv(k, "")
	}
}

// baseProviderConfig sets the three required credential env vars to test
// values and returns the raw config map other resilience-option tests build on.
func baseProviderConfig(t *testing.T) map[string]interface{} {
	t.Helper()
	t.Setenv("NAMECHEAP_USER_NAME", "test-user")
	t.Setenv("NAMECHEAP_API_USER", "test-api-user")
	t.Setenv("NAMECHEAP_API_KEY", "test-api-key")
	return map[string]interface{}{
		"client_ip":   testPlaceholderClientIP,
		"use_sandbox": false,
	}
}

func TestProviderResilienceFieldsAreOptional(t *testing.T) {
	p := Provider()
	for _, field := range []string{
		"requests_per_minute", "max_retries", "retry_max_elapsed",
		"retry_base_delay", "retry_max_delay", "request_timeout",
	} {
		s, ok := p.Schema[field]
		assert.True(t, ok, "field %s should exist", field)
		assert.True(t, s.Optional, "field %s should be Optional", field)
		assert.False(t, s.Required, "field %s should not be Required", field)
		// Without these, deleting a validator or an env default would leave the
		// suite green — the invalid-input tests reach the validators by other
		// routes, and nothing else checks the env plumbing exists.
		assert.NotNil(t, s.ValidateDiagFunc, "field %s should validate its input", field)
		assert.NotNil(t, s.DefaultFunc, "field %s should have an env-backed default", field)
	}
}

func TestProviderResilienceDefaultsPreserveCurrentBehavior(t *testing.T) {
	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors with only required fields set, got: %v", diags)

	client, ok := rawProvider.Meta().(*namecheap.Client)
	assert.True(t, ok, "expected provider meta to be *namecheap.Client")
	assert.Equal(t, defaultRequestsPerMinute, client.ClientOptions.RateLimit.PerMinute)
	assert.Equal(t, defaultMaxRetries, client.ClientOptions.Retry.MaxAttempts)
	assert.Equal(t, 2*time.Minute, client.ClientOptions.Retry.MaxElapsed)
	// The backoff defaults must equal the SDK's own, so a configuration that does
	// not mention them retries exactly as it did before they existed.
	assert.Equal(t, 500*time.Millisecond, client.ClientOptions.Retry.BaseDelay)
	assert.Equal(t, 30*time.Second, client.ClientOptions.Retry.MaxDelay)
	assert.Equal(t, 30*time.Second, client.ClientOptions.HTTPClient.Timeout)
}

func TestProviderResilienceFieldsFromInlineConfig(t *testing.T) {
	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)
	raw["requests_per_minute"] = 5
	raw["max_retries"] = 10
	raw["retry_max_elapsed"] = "90s"
	raw["retry_base_delay"] = "2s"
	raw["retry_max_delay"] = "45s"
	raw["request_timeout"] = "45s"

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors, got: %v", diags)

	client := rawProvider.Meta().(*namecheap.Client)
	assert.Equal(t, 5, client.ClientOptions.RateLimit.PerMinute)
	assert.Equal(t, 10, client.ClientOptions.Retry.MaxAttempts)
	assert.Equal(t, 90*time.Second, client.ClientOptions.Retry.MaxElapsed)
	assert.Equal(t, 2*time.Second, client.ClientOptions.Retry.BaseDelay)
	assert.Equal(t, 45*time.Second, client.ClientOptions.Retry.MaxDelay)
	assert.Equal(t, 45*time.Second, client.ClientOptions.HTTPClient.Timeout)
}

func TestProviderResilienceFieldsFromEnvVars(t *testing.T) {
	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)
	t.Setenv(testEnvRequestsPerMinute, "7")
	t.Setenv(testEnvMaxRetries, "6")
	t.Setenv(testEnvRetryMaxElapsed, "3m")
	t.Setenv(testEnvRequestTimeout, "20s")

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors, got: %v", diags)

	client := rawProvider.Meta().(*namecheap.Client)
	assert.Equal(t, 7, client.ClientOptions.RateLimit.PerMinute)
	assert.Equal(t, 6, client.ClientOptions.Retry.MaxAttempts)
	assert.Equal(t, 3*time.Minute, client.ClientOptions.Retry.MaxElapsed)
	assert.Equal(t, 20*time.Second, client.ClientOptions.HTTPClient.Timeout)
}

func TestProviderConfigureInvalidRetryMaxElapsedDuration(t *testing.T) {
	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)
	raw["retry_max_elapsed"] = "not-a-duration"

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.True(t, diags.HasError(), "expected error for unparseable retry_max_elapsed")
	assert.Contains(t, diags[0].Summary, "retry_max_elapsed")
}

func TestProviderConfigureInvalidRequestTimeoutDuration(t *testing.T) {
	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)
	raw["request_timeout"] = "not-a-duration"

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.True(t, diags.HasError(), "expected error for unparseable request_timeout")
	assert.Contains(t, diags[0].Summary, "request_timeout")
}

// Plan-time validation (ValidateDiagFunc), exercised via Provider().Validate,
// mirroring how Terraform core invokes it during `terraform validate`/plan.

func TestProviderValidateRequestsPerMinuteBounds(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"below minimum", 0, true},
		{"negative", -1, true},
		{"above maximum", 21, true},
		{"minimum boundary", 1, false},
		{"maximum boundary", 20, false},
		{"typical value", 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Provider()
			raw := map[string]interface{}{"requests_per_minute": tt.value}
			diags := p.Validate(terraform.NewResourceConfigRaw(raw))
			assert.Equal(t, tt.wantErr, diags.HasError(), "requests_per_minute=%d diags=%v", tt.value, diags)
		})
	}
}

func TestProviderValidateMaxRetriesBounds(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"negative", -1, true},
		{"zero is allowed by validation", 0, false},
		{"typical value", 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Provider()
			raw := map[string]interface{}{"max_retries": tt.value}
			diags := p.Validate(terraform.NewResourceConfigRaw(raw))
			assert.Equal(t, tt.wantErr, diags.HasError(), "max_retries=%d diags=%v", tt.value, diags)
		})
	}
}

func TestProviderValidateRetryMaxElapsedBounds(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"unparseable", "not-a-duration", true},
		{"zero", "0s", true},
		{"negative", "-5s", true},
		{"valid", "2m", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Provider()
			raw := map[string]interface{}{"retry_max_elapsed": tt.value}
			diags := p.Validate(terraform.NewResourceConfigRaw(raw))
			assert.Equal(t, tt.wantErr, diags.HasError(), "retry_max_elapsed=%q diags=%v", tt.value, diags)
		})
	}
}

func TestProviderValidateRequestTimeoutBounds(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"unparseable", "not-a-duration", true},
		{"zero", "0s", true},
		{"negative", "-5s", true},
		{"valid", "30s", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Provider()
			raw := map[string]interface{}{"request_timeout": tt.value}
			diags := p.Validate(terraform.NewResourceConfigRaw(raw))
			assert.Equal(t, tt.wantErr, diags.HasError(), "request_timeout=%q diags=%v", tt.value, diags)
		})
	}
}

// Direct unit tests for the new validation functions, exercised outside the
// schema/provider machinery for full branch coverage.

func TestValidateRequestsPerMinuteFunc(t *testing.T) {
	assert.Empty(t, validateRequestsPerMinute(1, cty.Path{}))
	assert.Empty(t, validateRequestsPerMinute(20, cty.Path{}))
	assert.NotEmpty(t, validateRequestsPerMinute(0, cty.Path{}))
	assert.NotEmpty(t, validateRequestsPerMinute(21, cty.Path{}))
	assert.NotEmpty(t, validateRequestsPerMinute(-1, cty.Path{}))
}

func TestValidateMaxRetriesFunc(t *testing.T) {
	assert.Empty(t, validateMaxRetries(0, cty.Path{}))
	assert.Empty(t, validateMaxRetries(4, cty.Path{}))
	assert.NotEmpty(t, validateMaxRetries(-1, cty.Path{}))
}

func TestValidatePositiveDurationFunc(t *testing.T) {
	assert.Empty(t, validatePositiveDuration("2m", cty.Path{}))
	assert.Empty(t, validatePositiveDuration("30s", cty.Path{}))
	assert.NotEmpty(t, validatePositiveDuration("not-a-duration", cty.Path{}))
	assert.NotEmpty(t, validatePositiveDuration("0s", cty.Path{}))
	assert.NotEmpty(t, validatePositiveDuration("-5s", cty.Path{}))
}

// Acceptance tests

func TestAccProviderImpl(t *testing.T) {
	skipTestIfNoTFAccFlag(t)
	assert.NotNil(t, testAccNamecheapProvider)
}

func TestAccProviderConfigureFromEnvVars(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "namecheap_domain_records" "test" {
						domain = "%s"
						mode   = "MERGE"
					}
				`, *testAccDomain),
			},
		},
	})
}

func TestAccProviderMissingCredentials(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"namecheap": func() (*schema.Provider, error) {
				return Provider(), nil
			},
		},
		Steps: []resource.TestStep{
			{
				Config: `
					provider "namecheap" {
						user_name = ""
						api_user  = ""
						api_key   = ""
					}

					resource "namecheap_domain_records" "test" {
						domain = "example.com"
						mode   = "MERGE"
					}
				`,
				ExpectError: regexp.MustCompile(`Missing required provider configuration`),
			},
		},
	})
}

func TestAccDomainAvailability(t *testing.T) {
	skipTestIfNoTFAccFlag(t)
	testAccPreCheck(t)

	resp, err := namecheapSDKClient.Domains.GetListWithContext(context.Background(), &namecheap.DomainsGetListArgs{
		SearchTerm: namecheap.String(*testAccDomain),
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Domains == nil {
		t.Fatal("Empty response")
	}

	found := false

	for _, domain := range *resp.Domains {
		if *domain.Name == *testAccDomain {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf(`Domain "%s" is unavailable`, *testAccDomain)
	}
}

func skipTestIfNoTFAccFlag(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Skipped unless env 'TF_ACC' set")
	}
}

// TestAcceptanceRequestsPerMinute pins the live suite's share of the account
// request budget: a valid override is honoured, and anything unusable falls back
// to half the quota rather than letting a misconfigured environment either fail
// the suite or silently restore the double-budget this split exists to prevent.
func TestAcceptanceRequestsPerMinute(t *testing.T) {
	const fallback = defaultRequestsPerMinute / 2

	tests := []struct {
		name string
		env  string
		want int
	}{
		{"unset falls back to half the quota", "", fallback},
		{"valid override honoured", "10", 10},
		{"lower bound honoured", "1", minRequestsPerMinute},
		{"upper bound honoured", "20", maxRequestsPerMinute},
		{"zero falls back", "0", fallback},
		{"negative falls back", "-5", fallback},
		{"above quota falls back", "50", fallback},
		{"non-numeric falls back", "many", fallback},
		{"empty-ish falls back", "  ", fallback},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NAMECHEAP_REQUESTS_PER_MINUTE", tc.env)
			assert.Equal(t, tc.want, acceptanceRequestsPerMinute())
		})
	}
}

// TestAcceptanceClientBudgetFitsQuota is the invariant the split protects: the
// helper client and the provider each take a share, and the two together must
// not exceed Namecheap's documented per-account quota. Exceeding it is what
// makes the API answer 405 and the SDK report "after 4 attempts".
func TestAcceptanceClientBudgetFitsQuota(t *testing.T) {
	t.Setenv("NAMECHEAP_REQUESTS_PER_MINUTE", "10")
	perClient := acceptanceRequestsPerMinute()
	assert.LessOrEqual(t, perClient*2, maxRequestsPerMinute,
		"helper client + provider must stay within the %d/min account quota", maxRequestsPerMinute)
}

// TestAcceptanceRetryBudget pins the retry budget the live suite runs with.
// Sharing the account quota lengthens the wait for a rate-limit token, and that
// wait is charged against the retry budget, so the budget has to be able to
// outlast the rolling window rather than expiring inside it.
func TestAcceptanceRetryBudget(t *testing.T) {
	defaultElapsed, err := time.ParseDuration(defaultRetryMaxElapsed)
	assert.NoError(t, err)

	t.Run("attempts", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			env  string
			want int
		}{
			{"unset uses the provider default", "", defaultMaxRetries},
			{"override honoured", "8", 8},
			{"zero falls back", "0", defaultMaxRetries},
			{"negative falls back", "-1", defaultMaxRetries},
			{"non-numeric falls back", "lots", defaultMaxRetries},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv("NAMECHEAP_MAX_RETRIES", tc.env)
				assert.Equal(t, tc.want, acceptanceMaxRetries())
			})
		}
	})

	t.Run("elapsed", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			env  string
			want time.Duration
		}{
			{"unset uses the provider default", "", defaultElapsed},
			{"override honoured", "5m", 5 * time.Minute},
			{"zero falls back", "0s", defaultElapsed},
			{"negative falls back", "-1m", defaultElapsed},
			{"unparsable falls back", "five minutes", defaultElapsed},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv("NAMECHEAP_RETRY_MAX_ELAPSED", tc.env)
				assert.Equal(t, tc.want, acceptanceRetryMaxElapsed())
			})
		}
	})
}

// TestProviderRetryBackoffFromEnvVars asserts the two backoff options are
// readable from the environment, which is how CI tunes them for the live suite.
func TestProviderRetryBackoffFromEnvVars(t *testing.T) {
	clearResilienceEnvVars(t)
	t.Setenv(testEnvRetryBaseDelay, "5s")
	t.Setenv(testEnvRetryMaxDelay, "1m")
	raw := baseProviderConfig(t)

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors, got: %v", diags)

	client := rawProvider.Meta().(*namecheap.Client)
	assert.Equal(t, 5*time.Second, client.ClientOptions.Retry.BaseDelay)
	assert.Equal(t, time.Minute, client.ClientOptions.Retry.MaxDelay)
}

// TestProviderRetryBackoffInvalid covers the rejections, driving each case
// through Configure so the schema validators and the configure-time ordering
// rule are both exercised on the path a real provider takes. An earlier version
// of this test called validatePositiveDuration directly and concatenated its
// diagnostics with Configure's, which hid the fact that Configure itself
// accepted a zero or negative duration.
func TestProviderRetryBackoffInvalid(t *testing.T) {
	tests := []struct {
		name      string
		base      string
		max       string
		wantInSum string
	}{
		{"unparsable base", "soon", "30s", "retry_base_delay"},
		{"unparsable max", "500ms", "eventually", "retry_max_delay"},
		{"zero base", "0s", "30s", "retry_base_delay"},
		{"negative base", "-5s", "30s", "retry_base_delay"},
		{"zero max", "500ms", "0s", "retry_max_delay"},
		// The cap is the one at fault here, so the diagnostic must name it rather
		// than blaming the base delay for being "too large".
		{"negative max", "500ms", "-1s", "retry_max_delay"},
		{"base above cap", "45s", "30s", "exceeds retry_max_delay"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearResilienceEnvVars(t)
			raw := baseProviderConfig(t)
			raw["retry_base_delay"] = tc.base
			raw["retry_max_delay"] = tc.max

			rawProvider := Provider()
			diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
			assert.True(t, diags.HasError(), "expected Configure to reject base=%q max=%q", tc.base, tc.max)

			var text string
			for _, d := range diags {
				text += d.Summary + " " + d.Detail + " "
			}
			assert.Contains(t, text, tc.wantInSum)
		})
	}
}

// TestProviderRetryBackoffValidatorsRejectDurations covers the same shapes at the
// schema-validation layer, which is where a plan surfaces them before Configure
// ever runs.
func TestProviderRetryBackoffValidatorsRejectDurations(t *testing.T) {
	for _, field := range []string{"retry_base_delay", "retry_max_delay"} {
		validate := Provider().Schema[field].ValidateDiagFunc
		assert.NotNil(t, validate, "%s must have a validator", field)
		for _, value := range []string{"", "soon", "0s", "-1s"} {
			diags := validate(value, cty.Path{cty.GetAttrStep{Name: field}})
			assert.True(t, diags.HasError(), "%s = %q should be rejected at validate time", field, value)
		}
		for _, value := range []string{"500ms", "5s", "1m"} {
			diags := validate(value, cty.Path{cty.GetAttrStep{Name: field}})
			assert.False(t, diags.HasError(), "%s = %q should be accepted, got %v", field, value, diags)
		}
	}
}

// ciRetry mirrors the retry policy the live acceptance job configures in
// .github/workflows/ci.yml. The tests below assert the property those values
// were chosen for, so changing one without the other fails here rather than
// silently degrading the sandbox suite.
var ciRetry = struct {
	attempts   int
	baseDelay  time.Duration
	maxDelay   time.Duration
	maxElapsed time.Duration
}{attempts: 5, baseDelay: 10 * time.Second, maxDelay: 60 * time.Second, maxElapsed: 8 * time.Minute}

// backoffBounds returns the minimum and maximum total time a policy can spend
// sleeping between attempts, mirroring the SDK's schedule: each delay doubles
// from base, is clamped to maxDelay, and is then jittered to between 50% and
// 100% of that value ("equal jitter"). The minimum is the figure that matters
// for "can this outlast a rate-limit window" — the worst case for the property.
func backoffBounds(base, maxDelay time.Duration, attempts int) (minTotal, maxTotal time.Duration) {
	delay := base
	for i := 1; i < attempts; i++ {
		if delay > maxDelay {
			delay = maxDelay
		}
		minTotal += delay / 2
		maxTotal += delay
		delay *= 2
	}
	return minTotal, maxTotal
}

// TestProviderRetryBackoffOutlastsRateWindow is the property the whole change
// exists for: under a per-minute rate limit, a retry policy is only useful if
// its backoff can outlast the window — and it must do so on FEW attempts,
// because every attempt is itself a request counted against the same quota.
//
// It asserts the jittered MINIMUM, not the nominal schedule: the SDK jitters
// every delay down to as little as half, so a policy whose nominal total clears
// a minute can still fail most of the time. The shipped 5 x 10s/60s policy has a
// minimum of 65s, so it clears a one-minute window on every run.
func TestProviderRetryBackoffOutlastsRateWindow(t *testing.T) {
	const rateWindow = time.Minute

	minTotal, maxTotal := backoffBounds(ciRetry.baseDelay, ciRetry.maxDelay, ciRetry.attempts)
	assert.Greater(t, minTotal, rateWindow,
		"even the most heavily jittered run must outlast a %s rate window (min %s, max %s)", rateWindow, minTotal, maxTotal)
	assert.LessOrEqual(t, maxTotal, ciRetry.maxElapsed,
		"the whole schedule must fit inside retry_max_elapsed or the last attempts never happen")
	assert.LessOrEqual(t, ciRetry.attempts, defaultMaxRetries+2,
		"attempts are requests: prefer waiting longer over retrying more")

	// The pre-change policy is the counter-example: eight attempts spend eight
	// requests and can still finish inside the window.
	stockMin, _ := backoffBounds(500*time.Millisecond, 30*time.Second, 8)
	assert.Less(t, stockMin, rateWindow,
		"the stock backoff can finish inside the window, which is why the knobs are needed")
}

// TestProviderRetryBackoffConfigMatchesCI pins that the values asserted above are
// the values a provider actually resolves, so the test cannot drift from the
// configuration it claims to describe.
func TestProviderRetryBackoffConfigMatchesCI(t *testing.T) {
	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)
	raw["max_retries"] = ciRetry.attempts
	raw["retry_base_delay"] = ciRetry.baseDelay.String()
	raw["retry_max_delay"] = ciRetry.maxDelay.String()
	raw["retry_max_elapsed"] = ciRetry.maxElapsed.String()

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors, got: %v", diags)

	retry := rawProvider.Meta().(*namecheap.Client).ClientOptions.Retry
	assert.Equal(t, ciRetry.attempts, retry.MaxAttempts)
	assert.Equal(t, ciRetry.baseDelay, retry.BaseDelay)
	assert.Equal(t, ciRetry.maxDelay, retry.MaxDelay)
	assert.Equal(t, ciRetry.maxElapsed, retry.MaxElapsed)
}

// TestProviderRetryCostsOneRequestPerAttempt measures the claim rather than
// modelling it: it drives a real SDK client, configured through the provider,
// against a server that always answers 405 (the status Namecheap returns when
// rate-limiting), and counts the requests that actually leave.
//
// The delays are scaled down so the test is fast; the property under test is the
// request count, which is scale-independent. This is what makes "every retry is
// itself a request" a tested fact rather than a claim in a comment.
func TestProviderRetryCostsOneRequestPerAttempt(t *testing.T) {
	const attempts = 5

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	clearResilienceEnvVars(t)
	raw := baseProviderConfig(t)
	raw["max_retries"] = attempts
	raw["retry_base_delay"] = "20ms"
	raw["retry_max_delay"] = "100ms"
	raw["retry_max_elapsed"] = "30s"

	rawProvider := Provider()
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.False(t, diags.HasError(), "expected no errors, got: %v", diags)

	client := rawProvider.Meta().(*namecheap.Client)
	client.BaseURL = server.URL

	start := time.Now()
	_, err := client.DomainsDNS.GetHostsWithContext(context.Background(), "example.com")
	elapsed := time.Since(start)

	assert.Error(t, err, "a persistently throttled call must surface an error, not succeed silently")
	assert.Equal(t, int32(attempts), atomic.LoadInt32(&requests),
		"a throttled call costs one request per attempt — this is why fewer attempts spaced further apart is the right response to rate limiting")

	minTotal, maxTotal := backoffBounds(20*time.Millisecond, 100*time.Millisecond, attempts)
	assert.GreaterOrEqual(t, elapsed, minTotal, "elapsed should be at least the jittered minimum backoff")
	// Generous upper bound: the assertion of interest is that the SDK sleeps at
	// all between attempts, not the precise scheduling under a loaded CI box.
	assert.Less(t, elapsed, maxTotal+10*time.Second, "elapsed should be near the modelled schedule")
}
