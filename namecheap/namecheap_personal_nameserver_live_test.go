package namecheap_provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccNamecheapPersonalNameserver is a live-sandbox acceptance test for the
// namecheap_personal_nameserver resource. Like the other TestAcc* tests it runs
// only under `make testacc` (TF_ACC=1) and skips (via testAccPreCheck) when the
// NAMECHEAP_* credentials / NAMECHEAP_TEST_DOMAIN are absent, so it never touches
// the real API without explicit opt-in.
//
// It delivers the headline use case end-to-end against the live API: registering
// a pair of personal (glue/vanity) nameservers — ns1/ns2 under the test domain —
// so the domain can run on its own nameservers, then changing a glue IP in
// place, importing, and tearing them down. Every step is asserted both in
// Terraform state and against the live API via domains.ns.getInfo. The
// nameserver hosts carry a random label so a crashed prior run can never collide
// with a fresh one, and the glue IPs are RFC 5737 documentation addresses.
func TestAccNamecheapPersonalNameserver(t *testing.T) {
	domain := *testAccDomain
	// A per-run suffix (base36 of the current time) keeps the nameserver hosts
	// unique so a crashed prior run can never collide with a fresh one.
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	ns1 := fmt.Sprintf("ns1-%s.%s", suffix, domain)
	ns2 := fmt.Sprintf("ns2-%s.%s", suffix, domain)

	// The Namecheap API rejects reserved IP ranges for glue records (RFC 1918
	// private, RFC 5737 documentation, loopback, ...) with a "IP address ... is
	// reserved" policy error (3024278), so the glue IPs must be real, routable
	// public addresses. These are stable public placeholders (the example.com
	// block); in the sandbox they are only recorded as glue and never used.
	const (
		ns1IP        = "93.184.216.34"
		ns1IPUpdated = "93.184.216.35"
		ns2IP        = "93.184.216.36"
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckPersonalNameserverAbsent(domain, ns1),
			testAccCheckPersonalNameserverAbsent(domain, ns2),
		),
		Steps: []resource.TestStep{
			{
				// Register a vanity nameserver pair for the domain.
				Config: testAccPersonalNameserverPairConfig(domain, ns1, ns1IP, ns2, ns2IP),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_personal_nameserver.ns1", "ip", ns1IP),
					resource.TestCheckResourceAttr("namecheap_personal_nameserver.ns1", "id", domain+"/"+ns1),
					resource.TestCheckResourceAttr("namecheap_personal_nameserver.ns2", "ip", ns2IP),
					testAccCheckPersonalNameserverIP(domain, ns1, ns1IP),
					testAccCheckPersonalNameserverIP(domain, ns2, ns2IP),
				),
			},
			{
				// Change ns1's glue IP in place (issues domains.ns.update).
				Config: testAccPersonalNameserverPairConfig(domain, ns1, ns1IPUpdated, ns2, ns2IP),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_personal_nameserver.ns1", "ip", ns1IPUpdated),
					testAccCheckPersonalNameserverIP(domain, ns1, ns1IPUpdated),
				),
			},
			{
				// Import ns1 by its "<domain>/<nameserver>" composite ID.
				ResourceName:      "namecheap_personal_nameserver.ns1",
				ImportState:       true,
				ImportStateId:     domain + "/" + ns1,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccPersonalNameserverPairConfig(domain, ns1, ns1IP, ns2, ns2IP string) string {
	return fmt.Sprintf(`
resource "namecheap_personal_nameserver" "ns1" {
  domain     = %[1]q
  nameserver = %[2]q
  ip         = %[3]q
}

resource "namecheap_personal_nameserver" "ns2" {
  domain     = %[1]q
  nameserver = %[4]q
  ip         = %[5]q
}
`, domain, ns1, ns1IP, ns2, ns2IP)
}

// testAccCheckPersonalNameserverIP asserts, through the live SDK client, that the
// personal nameserver is registered with the expected glue IP (domains.ns.getInfo).
func testAccCheckPersonalNameserverIP(domain, nameserver, wantIP string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		sld, tld, err := nameserverSplitDomain(domain)
		if err != nil {
			return err
		}
		resp, err := namecheapSDKClient.DomainsNS.GetInfoWithContext(context.Background(), sld, tld, nameserver)
		if err != nil {
			return fmt.Errorf("ns.getInfo for %q failed: %w", nameserver, err)
		}
		if resp == nil || resp.DomainNameserverInfoResult == nil || resp.DomainNameserverInfoResult.IP == nil {
			return fmt.Errorf("ns.getInfo returned no IP for %q", nameserver)
		}
		if got := *resp.DomainNameserverInfoResult.IP; got != wantIP {
			return fmt.Errorf("personal nameserver %q IP = %q, want %q", nameserver, got, wantIP)
		}
		return nil
	}
}

// testAccCheckPersonalNameserverAbsent is a CheckDestroy that asserts the personal
// nameserver is no longer registered after the resources are destroyed. The
// Namecheap API returns an error for an unknown nameserver, which is the expected
// post-destroy state.
func testAccCheckPersonalNameserverAbsent(domain, nameserver string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		sld, tld, err := nameserverSplitDomain(domain)
		if err != nil {
			return err
		}
		resp, err := namecheapSDKClient.DomainsNS.GetInfoWithContext(context.Background(), sld, tld, nameserver)
		if err != nil {
			return nil
		}
		if resp != nil && resp.DomainNameserverInfoResult != nil {
			return fmt.Errorf("personal nameserver %q still registered after destroy", nameserver)
		}
		return nil
	}
}
