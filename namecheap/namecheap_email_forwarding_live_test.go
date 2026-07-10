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

// TestAccNamecheapEmailForwarding is a live-sandbox acceptance test for the
// namecheap_email_forwarding resource. Like the other TestAcc* tests it runs
// only when TF_ACC is set and skips (via testAccPreCheck) when the
// NAMECHEAP_* credentials / NAMECHEAP_TEST_DOMAIN are absent, so it never
// touches the real API without explicit opt-in.
//
// setEmailForwarding is a full-table replace against the shared sandbox test
// domain, and destroy clears the table entirely (unlike namecheap_domain_contacts,
// whose destroy is state-only). To leave the domain as this test found it, it
// captures the existing forwarding table up front and restores it in a
// t.Cleanup that runs after the test's own destroy.
func TestAccNamecheapEmailForwarding(t *testing.T) {
	captureAndRestoreEmailForwarding(t)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "namecheap_email_forwarding" "test" {
  domain = %[1]q

  forwards = {
    info = "info-dest@example.com"
  }
}
`, *testAccDomain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_email_forwarding.test", "forwards.%", "1"),
					resource.TestCheckResourceAttr("namecheap_email_forwarding.test", "forwards.info", "info-dest@example.com"),
					testAccCheckEmailForwardingAPI(map[string]string{"info": "info-dest@example.com"}),
				),
			},
			{
				// Add a second alias and change the first's destination -
				// proves the full-table replace round-trips more than one
				// entry and that mailboxN/ForwardToN ordering is stable.
				Config: fmt.Sprintf(`
resource "namecheap_email_forwarding" "test" {
  domain = %[1]q

  forwards = {
    info  = "changed-dest@example.com"
    sales = "sales-dest@example.com"
  }
}
`, *testAccDomain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_email_forwarding.test", "forwards.%", "2"),
					testAccCheckEmailForwardingAPI(map[string]string{
						"info":  "changed-dest@example.com",
						"sales": "sales-dest@example.com",
					}),
				),
			},
			{
				ResourceName:      "namecheap_email_forwarding.test",
				ImportState:       true,
				ImportStateId:     *testAccDomain,
				ImportStateVerify: true,
			},
		},
	})
}

// captureAndRestoreEmailForwarding snapshots the test domain's current email
// forwarding table and registers a t.Cleanup that writes it back, so a live
// run leaves the shared sandbox domain's forwarding data as it found it. It
// is a no-op unless the acceptance environment is fully configured (the test
// itself skips in that case, via testAccPreCheck).
func captureAndRestoreEmailForwarding(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" || os.Getenv("NAMECHEAP_API_KEY") == "" || *testAccDomain == "" {
		return
	}

	original, err := namecheapSDKClient.DomainsDNS.GetEmailForwardingWithContext(context.Background(), *testAccDomain)
	if err != nil {
		t.Fatalf("failed to capture existing email forwarding for %q before the test: %v", *testAccDomain, err)
	}

	var originalForwards []namecheap.EmailForward
	if original != nil && original.DomainDNSGetEmailForwardingResult != nil && original.DomainDNSGetEmailForwardingResult.Forwards != nil {
		originalForwards = *original.DomainDNSGetEmailForwardingResult.Forwards
	}

	t.Cleanup(func() {
		_, err := namecheapSDKClient.DomainsDNS.SetEmailForwardingWithContext(context.Background(), *testAccDomain, originalForwards)
		if err != nil {
			t.Errorf("failed to restore original email forwarding for %q: %v", *testAccDomain, err)
		}
	})
}

// testAccCheckEmailForwardingAPI fetches the domain's email forwarding table
// through the live SDK client and asserts it exactly matches want.
func testAccCheckEmailForwardingAPI(want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		resp, err := namecheapSDKClient.DomainsDNS.GetEmailForwardingWithContext(context.Background(), *testAccDomain)
		if err != nil {
			return fmt.Errorf("getEmailForwarding failed: %w", err)
		}
		if resp == nil || resp.DomainDNSGetEmailForwardingResult == nil {
			return fmt.Errorf("getEmailForwarding returned no result for %q", *testAccDomain)
		}

		var forwards []namecheap.EmailForward
		if resp.DomainDNSGetEmailForwardingResult.Forwards != nil {
			forwards = *resp.DomainDNSGetEmailForwardingResult.Forwards
		}
		got := forwardsSliceToMap(forwards)

		if len(got) != len(want) {
			return fmt.Errorf("email forwarding table for %q = %+v, want %+v", *testAccDomain, got, want)
		}
		for mailbox, wantDest := range want {
			if got[mailbox] != wantDest {
				return fmt.Errorf("email forwarding %q for %q = %q, want %q", mailbox, *testAccDomain, got[mailbox], wantDest)
			}
		}
		return nil
	}
}
