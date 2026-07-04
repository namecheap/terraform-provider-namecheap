package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/namecheap/terraform-provider-namecheap/namecheap/internal/mutexkv"
)

// Defaults for the client resilience options below. They intentionally match
// the go-namecheap-sdk v2.7.0 documented defaults (see RateLimitOptions and
// RetryOptions in that module) so that a provider configuration which does not
// set any of these four fields behaves exactly as it did before they existed.
const (
	defaultRequestsPerMinute = 20
	defaultMaxRetries        = 4
	defaultRetryMaxElapsed   = "2m"
	defaultRequestTimeout    = "30s"

	minRequestsPerMinute = 1
	maxRequestsPerMinute = 20
)

func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"user_name": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "A registered user name for namecheap",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_USER_NAME", nil),
			},

			"api_user": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "A registered api user for namecheap",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_API_USER", nil),
			},

			"api_key": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "The namecheap API key",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_API_KEY", nil),
			},

			"client_ip": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Client IP address",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_CLIENT_IP", nil),
				Default:     "0.0.0.0",
			},

			"use_sandbox": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "Use sandbox API endpoints",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_USE_SANDBOX", false),
			},

			"requests_per_minute": {
				Type:             schema.TypeInt,
				Optional:         true,
				Description:      "Client-side rate limit applied to the Namecheap API, in requests per minute. Must be between 1 and 20 (Namecheap's documented primary quota). Defaults to 20, matching the SDK's built-in limiter.",
				DefaultFunc:      schema.EnvDefaultFunc("NAMECHEAP_REQUESTS_PER_MINUTE", defaultRequestsPerMinute),
				ValidateDiagFunc: validateRequestsPerMinute,
			},

			"max_retries": {
				Type:             schema.TypeInt,
				Optional:         true,
				Description:      "Total number of attempts (including the first) for a single API call before giving up. Must be >= 0. Defaults to 4, matching the SDK's built-in retry policy. Note: because the underlying SDK treats a zero value as \"unset\", setting this to 0 falls back to the SDK default of 4 attempts rather than disabling retries.",
				DefaultFunc:      schema.EnvDefaultFunc("NAMECHEAP_MAX_RETRIES", defaultMaxRetries),
				ValidateDiagFunc: validateMaxRetries,
			},

			"retry_max_elapsed": {
				Type:             schema.TypeString,
				Optional:         true,
				Description:      "Maximum total wall-clock time to spend retrying a single API call, as a Go duration string (e.g. \"2m\", \"90s\"). Must parse and be greater than zero. Defaults to \"2m\", matching the SDK's built-in retry policy.",
				DefaultFunc:      schema.EnvDefaultFunc("NAMECHEAP_RETRY_MAX_ELAPSED", defaultRetryMaxElapsed),
				ValidateDiagFunc: validatePositiveDuration,
			},

			"request_timeout": {
				Type:             schema.TypeString,
				Optional:         true,
				Description:      "Timeout applied to the underlying HTTP client for a single request to the Namecheap API, as a Go duration string (e.g. \"30s\", \"1m\"). Must parse and be greater than zero. Defaults to \"30s\".",
				DefaultFunc:      schema.EnvDefaultFunc("NAMECHEAP_REQUEST_TIMEOUT", defaultRequestTimeout),
				ValidateDiagFunc: validatePositiveDuration,
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"namecheap_domain_records": resourceNamecheapDomainRecords(),
		},
		ConfigureContextFunc: configureContext,
	}
}

func configureContext(ctx context.Context, data *schema.ResourceData) (interface{}, diag.Diagnostics) {
	userName := strings.TrimSpace(data.Get("user_name").(string))
	apiUser := strings.TrimSpace(data.Get("api_user").(string))
	apiKey := strings.TrimSpace(data.Get("api_key").(string))
	clientIp := data.Get("client_ip").(string)
	useSandbox := data.Get("use_sandbox").(bool)

	var missing []string
	if userName == "" {
		missing = append(missing, "user_name (NAMECHEAP_USER_NAME)")
	}
	if apiUser == "" {
		missing = append(missing, "api_user (NAMECHEAP_API_USER)")
	}
	if apiKey == "" {
		missing = append(missing, "api_key (NAMECHEAP_API_KEY)")
	}
	if len(missing) > 0 {
		return nil, diag.Diagnostics{
			diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "Missing required provider configuration",
				Detail:   "The following provider attributes must be set either in the configuration or via environment variables: " + strings.Join(missing, ", "),
			},
		}
	}

	requestsPerMinute := data.Get("requests_per_minute").(int)
	maxRetries := data.Get("max_retries").(int)

	retryMaxElapsedRaw := data.Get("retry_max_elapsed").(string)
	retryMaxElapsed, err := time.ParseDuration(retryMaxElapsedRaw)
	if err != nil {
		return nil, diag.Diagnostics{
			{
				Severity:      diag.Error,
				Summary:       "Invalid retry_max_elapsed",
				Detail:        fmt.Sprintf("retry_max_elapsed %q is not a valid Go duration string: %s", retryMaxElapsedRaw, err),
				AttributePath: cty.Path{cty.GetAttrStep{Name: "retry_max_elapsed"}},
			},
		}
	}

	requestTimeoutRaw := data.Get("request_timeout").(string)
	requestTimeout, err := time.ParseDuration(requestTimeoutRaw)
	if err != nil {
		return nil, diag.Diagnostics{
			{
				Severity:      diag.Error,
				Summary:       "Invalid request_timeout",
				Detail:        fmt.Sprintf("request_timeout %q is not a valid Go duration string: %s", requestTimeoutRaw, err),
				AttributePath: cty.Path{cty.GetAttrStep{Name: "request_timeout"}},
			},
		}
	}

	client := namecheap.NewClient(&namecheap.ClientOptions{
		UserName:   userName,
		ApiUser:    apiUser,
		ApiKey:     apiKey,
		ClientIp:   clientIp,
		UseSandbox: useSandbox,
		HTTPClient: &http.Client{Timeout: requestTimeout},
		RateLimit: &namecheap.RateLimitOptions{
			PerMinute: requestsPerMinute,
		},
		Retry: &namecheap.RetryOptions{
			MaxAttempts: maxRetries,
			MaxElapsed:  retryMaxElapsed,
		},
	})

	return client, diag.Diagnostics{}
}

// validateRequestsPerMinute enforces that requests_per_minute stays within
// Namecheap's documented primary quota (1-20 requests/minute).
func validateRequestsPerMinute(v interface{}, _ cty.Path) diag.Diagnostics {
	value := v.(int)
	if value < minRequestsPerMinute || value > maxRequestsPerMinute {
		return diag.Diagnostics{
			{
				Severity: diag.Error,
				Summary:  "Invalid requests_per_minute",
				Detail:   fmt.Sprintf("requests_per_minute must be between %d and %d, got %d", minRequestsPerMinute, maxRequestsPerMinute, value),
			},
		}
	}
	return nil
}

// validateMaxRetries enforces that max_retries is not negative.
func validateMaxRetries(v interface{}, _ cty.Path) diag.Diagnostics {
	value := v.(int)
	if value < 0 {
		return diag.Diagnostics{
			{
				Severity: diag.Error,
				Summary:  "Invalid max_retries",
				Detail:   fmt.Sprintf("max_retries must be >= 0, got %d", value),
			},
		}
	}
	return nil
}

// validatePositiveDuration enforces that a duration-typed string field both
// parses as a Go duration and is strictly greater than zero. It backs both
// retry_max_elapsed and request_timeout.
func validatePositiveDuration(v interface{}, _ cty.Path) diag.Diagnostics {
	value := v.(string)
	d, err := time.ParseDuration(value)
	if err != nil {
		return diag.Diagnostics{
			{
				Severity: diag.Error,
				Summary:  "Invalid duration",
				Detail:   fmt.Sprintf("%q is not a valid Go duration string: %s", value, err),
			},
		}
	}
	if d <= 0 {
		return diag.Diagnostics{
			{
				Severity: diag.Error,
				Summary:  "Invalid duration",
				Detail:   fmt.Sprintf("duration must be greater than zero, got %q", value),
			},
		}
	}
	return nil
}

var ncMutexKV = mutexkv.NewMutexKV()
