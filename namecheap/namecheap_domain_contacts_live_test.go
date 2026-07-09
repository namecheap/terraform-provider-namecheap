package namecheap_provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccNamecheapDomainContacts is a live-sandbox acceptance test for the
// namecheap_domain_contacts resource. Like the other TestAcc* tests it runs only
// when TF_ACC is set and skips (via testAccPreCheck) when the NAMECHEAP_*
// credentials / NAMECHEAP_TEST_DOMAIN are absent, so it never touches the real
// API in CI without explicit opt-in. It exercises the setContacts + getContacts
// round-trip on the configured test domain.
func TestAccNamecheapDomainContacts(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "namecheap_domain_contacts" "test" {
						domain = "%s"

						registrant {
							first_name     = "Jane"
							last_name      = "Doe"
							address1       = "1 Main St"
							city           = "Lisbon"
							state_province = "Lisboa"
							postal_code    = "1000-001"
							country        = "PT"
							phone          = "+351.123456789"
							email_address  = "jane@example.com"
							organization   = "Example Corp"
						}
					}
				`, *testAccDomain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_domain_contacts.test", "registrant.0.first_name", "Jane"),
					// Optional blocks default to the registrant.
					resource.TestCheckResourceAttr("namecheap_domain_contacts.test", "tech.0.first_name", "Jane"),
					testAccDomainContactsAPIRegistrant("Jane"),
				),
			},
			{
				ResourceName:      "namecheap_domain_contacts.test",
				ImportState:       true,
				ImportStateId:     *testAccDomain,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccDomainContactsAPIRegistrant fetches the domain's contacts through the
// live SDK client and asserts the registrant first name round-tripped.
func testAccDomainContactsAPIRegistrant(wantFirstName string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		resp, err := namecheapSDKClient.Domains.GetContactsWithContext(context.Background(), *testAccDomain)
		if err != nil {
			return fmt.Errorf("getContacts failed: %w", err)
		}
		if resp == nil || resp.DomainContactsResult == nil || resp.DomainContactsResult.Registrant == nil {
			return fmt.Errorf("getContacts returned no registrant for %q", *testAccDomain)
		}
		if got := resp.DomainContactsResult.Registrant.FirstName; got != wantFirstName {
			return fmt.Errorf("registrant first name = %q, want %q", got, wantFirstName)
		}
		return nil
	}
}
