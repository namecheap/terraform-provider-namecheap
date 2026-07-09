//go:build testacc

package namecheap_provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// mockCheckNameserverIP asserts the mock persisted the given personal
// nameserver with the expected glue IP for the domain.
func mockCheckNameserverIP(m *namecheapMock, domain, nameserver, wantIP string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return fmt.Errorf("mock has no state for %q", domain)
		}
		got, ok := st.personalNS[nameserver]
		if !ok {
			return fmt.Errorf("mock state for %q missing personal nameserver %q (have %+v)", domain, nameserver, st.personalNS)
		}
		if got != wantIP {
			return fmt.Errorf("mock IP for %q/%q = %q, want %q", domain, nameserver, got, wantIP)
		}
		return nil
	}
}

// mockCheckNameserverAbsent is a CheckDestroy that asserts the personal
// nameserver was removed from the backend when the resource was destroyed.
func mockCheckNameserverAbsent(m *namecheapMock, domain, nameserver string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return nil
		}
		if _, ok := st.personalNS[nameserver]; ok {
			return fmt.Errorf("expected personal nameserver %q for %q to be deleted on destroy, mock still has it", nameserver, domain)
		}
		return nil
	}
}

// TestAccMockNameserverLifecycle drives a full create -> update (IP change) ->
// import -> destroy lifecycle for namecheap_nameserver against the stateful
// mock, asserting both Terraform state and the mock backend at each step.
func TestAccMockNameserverLifecycle(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"
	const nameserver = "ns1.mock-example.com"
	const resourceName = "namecheap_nameserver.test"

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckNameserverAbsent(m, domain, nameserver),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "namecheap_nameserver" "test" {
  domain     = "%s"
  nameserver = "%s"
  ip         = "1.2.3.4"
}
`, domain, nameserver),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "ip", "1.2.3.4"),
					resource.TestCheckResourceAttr(resourceName, "id", domain+"/"+nameserver),
					mockCheckNameserverIP(m, domain, nameserver, "1.2.3.4"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "namecheap_nameserver" "test" {
  domain     = "%s"
  nameserver = "%s"
  ip         = "5.6.7.8"
}
`, domain, nameserver),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "ip", "5.6.7.8"),
					mockCheckNameserverIP(m, domain, nameserver, "5.6.7.8"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateId:     domain + "/" + nameserver,
				ImportStateVerify: true,
			},
		},
	})
}
