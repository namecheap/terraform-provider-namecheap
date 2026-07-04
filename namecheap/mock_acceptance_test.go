//go:build testacc

package namecheap_provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// mockProviderFactories returns ProviderFactories that build the real
// Provider(). Combined with mockPreCheck (which points NAMECHEAP_API_URL at the
// mock server), resource.Test exercises the real configureContext path but with
// the SDK client redirected to the in-process mock rather than the live API.
func mockProviderFactories() map[string]func() (*schema.Provider, error) {
	return map[string]func() (*schema.Provider, error){
		"namecheap": func() (*schema.Provider, error) { return Provider(), nil },
	}
}

// mockPreCheck wires the provider to a mock server: it points the testacc-build
// endpoint override at the mock URL and supplies dummy credentials plus an
// explicit client_ip so configureContext neither fails its required-field check
// nor attempts to auto-detect a public IP over the network.
func mockPreCheck(t *testing.T, m *namecheapMock) {
	t.Helper()
	t.Setenv("NAMECHEAP_API_URL", m.url())
	t.Setenv("NAMECHEAP_USER_NAME", "mock-user")
	t.Setenv("NAMECHEAP_API_USER", "mock-user")
	t.Setenv("NAMECHEAP_API_KEY", "mock-key")
	t.Setenv("NAMECHEAP_CLIENT_IP", "127.0.0.1")
}

// mockCheckHostCount asserts the mock persisted exactly n host records for the
// domain — proving reads reflect prior writes end-to-end through the provider.
func mockCheckHostCount(m *namecheapMock, domain string, n int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return fmt.Errorf("mock has no state for %q", domain)
		}
		if len(st.hosts) != n {
			return fmt.Errorf("mock host count for %q = %d, want %d", domain, len(st.hosts), n)
		}
		return nil
	}
}

// mockCheckHostsCleared is a CheckDestroy that asserts the domain's records were
// removed from the backend when the resource was destroyed.
func mockCheckHostsCleared(m *namecheapMock, domain string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st != nil && len(st.hosts) != 0 {
			return fmt.Errorf("expected records for %q to be cleared on destroy, mock still has %d", domain, len(st.hosts))
		}
		return nil
	}
}

// TestAccMockDomainRecordsLifecycle drives a full create -> update -> destroy
// lifecycle for namecheap_domain_records against the stateful mock, asserting
// both Terraform state and the mock backend at each step. It proves the mock
// harness end-to-end; the broader scenario matrix is added in a follow-up.
func TestAccMockDomainRecordsLifecycle(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"
	const resourceName = "namecheap_domain_records.test"

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckHostsCleared(m, domain),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
    ttl      = 3600
  }
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
					mockCheckHostCount(m, domain, 1),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
    ttl      = 3600
  }

  record {
    hostname = "mail"
    type     = "A"
    address  = "10.0.0.2"
    ttl      = 3600
  }
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "record.#", "2"),
					mockCheckHostCount(m, domain, 2),
				),
			},
		},
	})
}
