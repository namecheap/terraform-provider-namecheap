//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Mock-backed acceptance coverage for namecheap_dns_record, driving real
// Terraform against the stateful mock.
//
// The property these exist to prove is surgical modification: Namecheap has no
// per-record API, so every change here rewrites the whole zone. A bug in the
// read-modify-write would silently delete records the configuration never
// mentioned — which is exactly the failure this resource exists to avoid — so
// most of these tests seed unrelated records and assert they survive.

const dnsRecordTestDomain = "dns-record-example.com"

// seedZone puts records on the mock domain that no test manages, standing in for
// whatever else already lives in a real zone.
func seedUnmanagedZone(m *namecheapMock) {
	m.seed(dnsRecordTestDomain, []hostEntry{
		{Name: "@", Type: "A", Address: "10.0.0.1", TTL: 1800, MXPref: 10},
		{Name: "blog", Type: "CNAME", Address: "hosting.example.com.", TTL: 1800, MXPref: 10},
		{Name: "@", Type: "TXT", Address: "v=spf1 -all", TTL: 1800, MXPref: 10},
	}, "NONE", nil)
}

// assertUnmanagedZoneIntact checks the three seeded records are still present
// and unchanged.
func assertUnmanagedZoneIntact(m *namecheapMock) resource.TestCheckFunc {
	return func(*terraform.State) error {
		state := m.state(dnsRecordTestDomain)
		if state == nil {
			return fmt.Errorf("domain %s has no state in the mock", dnsRecordTestDomain)
		}
		want := map[string]string{
			"@|A":        "10.0.0.1",
			"blog|CNAME": "hosting.example.com.",
			"@|TXT":      "v=spf1 -all",
		}
		for key, address := range want {
			var found bool
			for _, h := range state.hosts {
				if h.Name+"|"+h.Type == key && h.Address == address {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("unmanaged record %s -> %q was lost; zone now %+v", key, address, state.hosts)
			}
		}
		return nil
	}
}

// TestAccMockDNSRecordLifecycle walks create, in-place update and destroy,
// asserting at every step that the records this resource does not manage are
// still there.
func TestAccMockDNSRecordLifecycle(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	config := func(address string, ttl int) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "www" {
  domain   = "%s"
  hostname = "www"
  type     = "A"
  address  = "%s"
  ttl      = %d
}
`, dnsRecordTestDomain, address, ttl)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("10.0.0.9", 1800),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.www", "address", "10.0.0.9"),
					resource.TestCheckResourceAttr("namecheap_dns_record.www", "id", dnsRecordTestDomain+"/A/www/10.0.0.9"),
					assertUnmanagedZoneIntact(m),
				),
			},
			{
				// address and ttl are not ForceNew, so this is an in-place update.
				Config: config("10.0.0.10", 600),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.www", "address", "10.0.0.10"),
					resource.TestCheckResourceAttr("namecheap_dns_record.www", "ttl", "600"),
					resource.TestCheckResourceAttr("namecheap_dns_record.www", "id", dnsRecordTestDomain+"/A/www/10.0.0.10"),
					// The old address must be gone, not left behind as a duplicate.
					assertZoneLacks(m, "www", "A", "10.0.0.9"),
					assertUnmanagedZoneIntact(m),
				),
			},
			{
				// Importing the record Terraform already manages must produce an
				// identical state, which is what makes the ID format trustworthy.
				ResourceName:      "namecheap_dns_record.www",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
		CheckDestroy: resource.ComposeTestCheckFunc(
			assertZoneLacks(m, "www", "A", "10.0.0.10"),
			assertUnmanagedZoneIntact(m),
		),
	})
}

// TestAccMockDNSRecordSurgicalAcrossManyRecords is the case the issue is really
// about: several records managed independently on one domain, added and removed
// without trampling each other or the records nobody manages.
//
// It uses count rather than for_each only because the SDKv2 test harness shims
// state into a legacy flatmap that cannot represent string instance keys, and
// any test using a TestCheckFunc forces that shim. The property under test —
// independent instances of this resource sharing one zone — is the same.
func TestAccMockDNSRecordSurgicalAcrossManyRecords(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	config := func(addresses ...string) string {
		quoted := make([]string, 0, len(addresses))
		for _, a := range addresses {
			quoted = append(quoted, fmt.Sprintf("%q", a))
		}
		return fmt.Sprintf(`
locals {
  addresses = [%s]
}

resource "namecheap_dns_record" "fleet" {
  count = length(local.addresses)

  domain   = "%s"
  hostname = "node${count.index + 1}"
  type     = "A"
  address  = local.addresses[count.index]
}
`, strings.Join(quoted, ", "), dnsRecordTestDomain)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("10.1.0.1", "10.1.0.2", "10.1.0.3"),
				Check: resource.ComposeTestCheckFunc(
					assertZoneHas(m, "node1", "A", "10.1.0.1"),
					assertZoneHas(m, "node2", "A", "10.1.0.2"),
					assertZoneHas(m, "node3", "A", "10.1.0.3"),
					assertUnmanagedZoneIntact(m),
				),
			},
			{
				// Dropping the last instance must remove only that record.
				Config: config("10.1.0.1", "10.1.0.2"),
				Check: resource.ComposeTestCheckFunc(
					assertZoneHas(m, "node1", "A", "10.1.0.1"),
					assertZoneHas(m, "node2", "A", "10.1.0.2"),
					assertZoneLacks(m, "node3", "A", "10.1.0.3"),
					assertUnmanagedZoneIntact(m),
				),
			},
		},
		CheckDestroy: resource.ComposeTestCheckFunc(
			assertZoneLacks(m, "node1", "A", "10.1.0.1"),
			assertZoneLacks(m, "node2", "A", "10.1.0.2"),
			assertUnmanagedZoneIntact(m),
		),
	})
}

// TestAccMockDNSRecordForceNewOnIdentity proves that changing the hostname
// replaces the record rather than editing a different one: the old host must be
// gone and the new one present.
func TestAccMockDNSRecordForceNewOnIdentity(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	config := func(hostname string) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "moving" {
  domain   = "%s"
  hostname = "%s"
  type     = "A"
  address  = "10.2.0.1"
}
`, dnsRecordTestDomain, hostname)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config("before"), Check: assertZoneHas(m, "before", "A", "10.2.0.1")},
			{
				Config: config("after"),
				Check: resource.ComposeTestCheckFunc(
					assertZoneHas(m, "after", "A", "10.2.0.1"),
					assertZoneLacks(m, "before", "A", "10.2.0.1"),
					assertUnmanagedZoneIntact(m),
				),
			},
		},
	})
}

// TestAccMockDNSRecordRefusesDuplicate covers the case that would otherwise
// create two indistinguishable records: creating one that already exists. The
// error must point at import.
func TestAccMockDNSRecordRefusesDuplicate(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(dnsRecordTestDomain, []hostEntry{
		{Name: "dupe", Type: "A", Address: "10.3.0.1", TTL: 1800, MXPref: 10},
	}, "NONE", nil)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "namecheap_dns_record" "dupe" {
  domain   = "%s"
  hostname = "dupe"
  type     = "A"
  address  = "10.3.0.1"
}
`, dnsRecordTestDomain),
				ExpectError: regexp.MustCompile(`already exists`),
			},
		},
	})
}

// TestAccMockDNSRecordDriftRecreates covers a record deleted outside Terraform:
// the read must drop it from state so the next plan offers to recreate it,
// rather than failing or reporting no change.
func TestAccMockDNSRecordDriftRecreates(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	config := fmt.Sprintf(`
resource "namecheap_dns_record" "drifted" {
  domain   = "%s"
  hostname = "drifted"
  type     = "A"
  address  = "10.4.0.1"
}
`, dnsRecordTestDomain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config, Check: assertZoneHas(m, "drifted", "A", "10.4.0.1")},
			{
				PreConfig: func() {
					// Someone deletes it in the dashboard.
					m.removeHost(dnsRecordTestDomain, "drifted", "A")
				},
				Config:             config,
				ExpectNonEmptyPlan: false,
				Check:              assertZoneHas(m, "drifted", "A", "10.4.0.1"),
			},
		},
	})
}

// TestAccMockDNSRecordImportFormats covers the import ID parser through real
// Terraform, including the malformed cases that should fail with a usable
// message rather than a panic.
func TestAccMockDNSRecordImportFormats(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(dnsRecordTestDomain, []hostEntry{
		{Name: "go", Type: "URL", Address: "https://example.org/a/b", TTL: 1800, MXPref: 10},
	}, "NONE", nil)

	config := fmt.Sprintf(`
resource "namecheap_dns_record" "go" {
  domain   = "%s"
  hostname = "go"
  type     = "URL"
  address  = "https://example.org/a/b"
}
`, dnsRecordTestDomain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				// An address containing the separator must still import.
				Config:             config,
				ResourceName:       "namecheap_dns_record.go",
				ImportState:        true,
				ImportStateId:      dnsRecordTestDomain + "/URL/go/https://example.org/a/b",
				ImportStatePersist: true,
			},
			{
				Config:        config,
				ResourceName:  "namecheap_dns_record.go",
				ImportState:   true,
				ImportStateId: dnsRecordTestDomain + "/URL/go",
				ExpectError:   regexp.MustCompile(`invalid import ID`),
			},
			{
				Config:        config,
				ResourceName:  "namecheap_dns_record.go",
				ImportState:   true,
				ImportStateId: dnsRecordTestDomain + "/A/nope/10.9.9.9",
				ExpectError:   regexp.MustCompile(`no A record`),
			},
		},
	})
}

// assertZoneHas checks a record is present in the mock's zone.
func assertZoneHas(m *namecheapMock, host, recordType, address string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if !zoneContains(m, host, recordType, address) {
			return fmt.Errorf("expected %s %s -> %q in the zone, not found", host, recordType, address)
		}
		return nil
	}
}

// assertZoneLacks checks a record is absent from the mock's zone.
func assertZoneLacks(m *namecheapMock, host, recordType, address string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if zoneContains(m, host, recordType, address) {
			return fmt.Errorf("expected %s %s -> %q to be gone from the zone, still present", host, recordType, address)
		}
		return nil
	}
}

func zoneContains(m *namecheapMock, host, recordType, address string) bool {
	state := m.state(dnsRecordTestDomain)
	if state == nil {
		return false
	}
	for _, h := range state.hosts {
		if h.Name == host && h.Type == recordType && h.Address == address {
			return true
		}
	}
	return false
}
