package namecheap

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

var testAccProviders map[string]*schema.Provider
var testAccProvider *schema.Provider

func init() {
	testAccProvider = Provider()
	testAccProviders = map[string]*schema.Provider{
		"namecheap": testAccProvider,
	}
}

func TestProvider(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestProvider_impl(t *testing.T) {
	var _ *schema.Provider = Provider()
}

func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("NAMECHEAP_USERNAME"); v == "" {
		t.Fatal("NAMECHEAP_USERNAME must be set for acceptance tests")
	}

	if v := os.Getenv("NAMECHEAP_API_USER"); v == "" {
		t.Fatal("NAMECHEAP_API_USER must be set for acceptance tests")
	}

	if v := os.Getenv("NAMECHEAP_IP"); v == "" {
		t.Fatal("NAMECHEAP_IP must be set for acceptance tests")
	}

	if v := os.Getenv("NAMECHEAP_TOKEN"); v == "" {
		t.Fatal("NAMECHEAP_TOKEN must be set for acceptance tests")
	}

	if v := os.Getenv("NAMECHEAP_USE_SANDBOX"); v == "" {
		t.Fatal("NAMECHEAP_USE_SANDBOX must be set for acceptance tests")
	}

	if v := os.Getenv("NAMECHEAP_DOMAIN"); v == "" {
		t.Fatal("NAMECHEAP_DOMAIN must be set for acceptance tests. The domain is used to ` and destroy record against.")
	}
}
