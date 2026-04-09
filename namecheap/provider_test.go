package namecheap_provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

var testAccNamecheapProvider *schema.Provider
var testAccProviderFactories map[string]func() (*schema.Provider, error)
var namecheapSDKClient *namecheap.Client
var testAccDomain *string

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
		ClientIp:   "0.0.0.0",
		UseSandbox: strings.EqualFold(os.Getenv("NAMECHEAP_USE_SANDBOX"), "true"),
	})

	testDomain := os.Getenv("NAMECHEAP_TEST_DOMAIN")
	testAccDomain = &testDomain
}

func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("NAMECHEAP_USER_NAME") == "" {
		t.Skip("NAMECHEAP_USER_NAME must be set for acceptance testing")
	}
	if os.Getenv("NAMECHEAP_API_USER") == "" {
		t.Skip("NAMECHEAP_API_USER must be set for acceptance testing")
	}
	if os.Getenv("NAMECHEAP_API_KEY") == "" {
		t.Skip("NAMECHEAP_API_KEY must be set for acceptance testing")
	}
	if os.Getenv("NAMECHEAP_TEST_DOMAIN") == "" {
		t.Skip("NAMECHEAP_TEST_DOMAIN must be set for acceptance testing")
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
		"client_ip":   "0.0.0.0",
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
		"client_ip":   "0.0.0.0",
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
		"client_ip":   "0.0.0.0",
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
		"client_ip":   "0.0.0.0",
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
		"client_ip":   "0.0.0.0",
		"use_sandbox": false,
	}
	diags := rawProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(raw))
	assert.True(t, diags.HasError(), "expected error when credentials are whitespace-only")
	assert.Contains(t, diags[0].Detail, "user_name")
	assert.Contains(t, diags[0].Detail, "api_user")
	assert.Contains(t, diags[0].Detail, "api_key")
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

	resp, err := namecheapSDKClient.Domains.GetList(&namecheap.DomainsGetListArgs{
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
