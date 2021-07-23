package namecheap_provider

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
	"os"
	"strings"
	"testing"
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

func TestAccProviderImpl(t *testing.T) {
	skipTestIfNoTFAccFlag(t)
	assert.NotNil(t, testAccNamecheapProvider)
}

func TestAccSDKImpl(t *testing.T) {
	skipTestIfNoTFAccFlag(t)
	assert.NotNil(t, namecheapSDKClient)
}

func TestAccDomainImpl(t *testing.T) {
	skipTestIfNoTFAccFlag(t)
	assert.NotNil(t, testAccDomain)
	assert.NotEmpty(t, *testAccDomain)
}

func TestAccDomainAvailability(t *testing.T) {
	skipTestIfNoTFAccFlag(t)
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
