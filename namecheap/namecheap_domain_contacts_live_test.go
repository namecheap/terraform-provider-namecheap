package namecheap_provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// TestAccNamecheapDomainContacts is a live-sandbox acceptance test for the
// namecheap_domain_contacts resource. Like the other TestAcc* tests it runs only
// when TF_ACC is set and skips (via testAccPreCheck) when the NAMECHEAP_*
// credentials / NAMECHEAP_TEST_DOMAIN are absent, so it never touches the real
// API in CI without explicit opt-in. It exercises the setContacts + getContacts
// round-trip on the configured test domain.
//
// Because destroy is state-only (the API cannot delete contacts) the test would
// otherwise permanently overwrite the shared sandbox domain's real WHOIS
// contacts. To avoid that it captures the domain's current contacts up front and
// restores them in a t.Cleanup that runs after the test's own destroy.
func TestAccNamecheapDomainContacts(t *testing.T) {
	captureAndRestoreContacts(t)

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

// captureAndRestoreContacts snapshots the test domain's current WHOIS contacts
// and registers a t.Cleanup that writes them back, so a live run leaves the
// shared sandbox domain's contact data as it found it. It is a no-op unless the
// acceptance environment is fully configured (the test itself skips in that
// case, via testAccPreCheck).
func captureAndRestoreContacts(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" || os.Getenv("NAMECHEAP_API_KEY") == "" || *testAccDomain == "" {
		return
	}

	original, err := namecheapSDKClient.Domains.GetContactsWithContext(context.Background(), *testAccDomain)
	if err != nil {
		t.Fatalf("failed to capture existing contacts for %q before the test: %v", *testAccDomain, err)
	}
	if original == nil || original.DomainContactsResult == nil || original.DomainContactsResult.Registrant == nil {
		t.Logf("no existing registrant captured for %q; skipping restore", *testAccDomain)
		return
	}

	orig := original.DomainContactsResult
	registrant := *orig.Registrant
	t.Cleanup(func() {
		_, err := namecheapSDKClient.Domains.SetContactsWithContext(context.Background(), &namecheap.DomainsSetContactsArgs{
			DomainName: *testAccDomain,
			Registrant: registrant,
			Tech:       contactOrRegistrant(orig.Tech, registrant),
			Admin:      contactOrRegistrant(orig.Admin, registrant),
			AuxBilling: contactOrRegistrant(orig.AuxBilling, registrant),
		})
		if err != nil {
			t.Errorf("failed to restore original contacts for %q: %v", *testAccDomain, err)
		}
	})
}

// contactOrRegistrant returns *c when set, otherwise the registrant fallback —
// mirroring the resource's default-to-registrant rule when rebuilding the
// restore payload (setContacts requires all four blocks).
func contactOrRegistrant(c *namecheap.ContactInfo, registrant namecheap.ContactInfo) namecheap.ContactInfo {
	if c != nil {
		return *c
	}
	return registrant
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
