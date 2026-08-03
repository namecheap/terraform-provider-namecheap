//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
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

// TestAccMockDNSRecordRefusesAmbiguousMatch covers a zone holding two records
// with the same host, type and address, differing only in TTL. Nothing in the
// resource's identity — or in the API's selector — can tell them apart, and the
// SDK's delete matches *every* record a selector hits, so proceeding would take
// both records out on a destroy. Both the create path and the import path must
// refuse, naming the candidates.
func TestAccMockDNSRecordRefusesAmbiguousMatch(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(dnsRecordTestDomain, []hostEntry{
		{Name: "twin", Type: "A", Address: "10.0.0.1", TTL: 300, MXPref: 10},
		{Name: "twin", Type: "A", Address: "10.0.0.1", TTL: 7200, MXPref: 10},
	}, "NONE", nil)

	config := fmt.Sprintf(`
resource "namecheap_dns_record" "twin" {
  domain   = "%s"
  hostname = "twin"
  type     = "A"
  address  = "10.0.0.1"
}
`, dnsRecordTestDomain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)Ambiguous DNS record.*TTL 300.*TTL 7200`),
			},
			{
				Config:        config,
				ResourceName:  "namecheap_dns_record.twin",
				ImportState:   true,
				ImportStateId: dnsRecordTestDomain + "/A/twin/10.0.0.1",
				ExpectError:   regexp.MustCompile(`(?s)Ambiguous DNS record.*TTL 300.*TTL 7200`),
			},
		},
	})
}

// TestAccMockDNSRecordMXPreferenceIsPartOfIdentity covers two MX records for the
// same mail host at different preferences — a normal way to configure a backup
// mail server. The preference is what tells them apart, so it has to be part of
// what this resource matches on: without it, creating the second one looks like a
// duplicate, and destroying either one takes both, which on an EmailType=MX zone
// leaves a set the API refuses to write at all.
func TestAccMockDNSRecordMXPreferenceIsPartOfIdentity(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(dnsRecordTestDomain, []hostEntry{
		// The backup MX, managed by nobody, sharing this test's mail host.
		{Name: "@", Type: "MX", Address: "mail.example.com.", TTL: 1800, MXPref: 20},
	}, "MX", nil)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "namecheap_dns_record" "primary_mx" {
  domain   = "%s"
  hostname = "@"
  type     = "MX"
  address  = "mail.example.com"
  mx_pref  = 10
}
`, dnsRecordTestDomain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.primary_mx", "mx_pref", "10"),
					assertZoneMXPrefs(m, "@", "mail.example.com.", 10, 20),
				),
			},
		},
		// Destroying the record this configuration owns must leave the backup MX
		// alone — and therefore leave the zone with the MX record its EmailType
		// requires.
		CheckDestroy: assertZoneMXPrefs(m, "@", "mail.example.com.", 20),
	})
}

// TestAccMockDNSRecordMXPreferenceChangesInPlace covers changing the preference of
// an MX record. The preference is part of an MX record's identity, so the selector
// has to describe the record as it currently stands — the pre-change preference —
// or the write appends a second record instead of replacing the first.
func TestAccMockDNSRecordMXPreferenceChangesInPlace(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(dnsRecordTestDomain, []hostEntry{
		// An unmanaged primary, so the zone keeps an MX record throughout.
		{Name: "@", Type: "MX", Address: "mail1.example.com.", TTL: 1800, MXPref: 10},
	}, "MX", nil)

	config := func(pref int) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "backup_mx" {
  domain   = "%s"
  hostname = "@"
  type     = "MX"
  address  = "mail2.example.com"
  mx_pref  = %d
}
`, dnsRecordTestDomain, pref)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config(20),
				Check: resource.ComposeTestCheckFunc(
					assertZoneMXPrefs(m, "@", "mail2.example.com.", 20),
					assertZoneMXPrefs(m, "@", "mail1.example.com.", 10),
				),
			},
			{
				// mx_pref is not ForceNew: one record, changed in place, and the
				// preference it had must not be left behind as a second record.
				Config: config(30),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.backup_mx", "mx_pref", "30"),
					assertZoneMXPrefs(m, "@", "mail2.example.com.", 30),
					assertZoneMXPrefs(m, "@", "mail1.example.com.", 10),
				),
			},
		},
		CheckDestroy: assertZoneMXPrefs(m, "@", "mail1.example.com.", 10),
	})
}

// TestAccMockDNSRecordFailedUpdateKeepsStateOnTheLiveRecord covers a write that
// fails half way through an update. SDKv2 persists the *planned* values when
// UpdateContext returns an error, so unless the resource puts them back, state
// ends up describing a record the zone does not hold — and the record it does
// hold, the one this resource is responsible for, is orphaned: no later plan will
// ever mention it again.
func TestAccMockDNSRecordFailedUpdateKeepsStateOnTheLiveRecord(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	config := func(address string) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "flaky" {
  domain   = "%s"
  hostname = "flaky"
  type     = "A"
  address  = "%s"
}
`, dnsRecordTestDomain, address)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("10.6.0.1"),
				Check:  assertZoneHas(m, "flaky", "A", "10.6.0.1"),
			},
			{
				// The update is planned, then rejected by the API.
				PreConfig: func() {
					m.failOn("namecheap.domains.dns.setHosts", "4022337", "mock: setHosts rejected")
				},
				Config:      config("10.6.0.2"),
				ExpectError: regexp.MustCompile(`mock: setHosts rejected`),
			},
			{
				// State must still describe the record the zone actually holds, so
				// re-planning the *original* configuration is a no-op. If the failed
				// update leaked its planned address into state, this plan is not
				// empty and 10.6.0.1 is orphaned.
				PreConfig: func() {
					m.failOn("", "", "")
				},
				Config:   config("10.6.0.1"),
				PlanOnly: true,
				Check:    assertZoneHas(m, "flaky", "A", "10.6.0.1"),
			},
		},
	})
}

// TestAccMockDNSRecordKeepsConfiguredCase covers a configuration that spells the
// domain or the host with capitals. The API lower-cases both, so reading the
// API's spelling back into state contradicts the configuration on every plan —
// and because these are ForceNew, that plan destroys and recreates a live record,
// every single apply, forever.
func TestAccMockDNSRecordKeepsConfiguredCase(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	// Deliberately not built from dnsRecordTestDomain: the point is the capitals.
	const config = `
resource "namecheap_dns_record" "cased" {
  domain   = "DNS-Record-Example.com"
  hostname = "WWW"
  type     = "A"
  address  = "10.5.0.1"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				// resource.Test re-plans after every apply and fails the step on a
				// non-empty plan, so this step alone proves the round-trip is stable.
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.cased", "domain", "DNS-Record-Example.com"),
					resource.TestCheckResourceAttr("namecheap_dns_record.cased", "hostname", "WWW"),
					// The ID stays canonical regardless of how the config is spelled.
					resource.TestCheckResourceAttr("namecheap_dns_record.cased", "id", dnsRecordTestDomain+"/A/www/10.5.0.1"),
					assertZoneHas(m, "www", "A", "10.5.0.1"),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// TestAccMockDNSRecordTrailingDotIsStable covers the trailing dot Namecheap adds
// to the address of a hostname-valued record. Storing the API's spelling makes
// every subsequent plan want to rewrite the record back to the configuration's
// spelling — a permanent diff, and a full-zone read-modify-write each time it is
// applied.
func TestAccMockDNSRecordTrailingDotIsStable(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	config := fmt.Sprintf(`
resource "namecheap_dns_record" "shop" {
  domain   = "%s"
  hostname = "shop"
  type     = "CNAME"
  address  = "hosting.example.com"
}
`, dnsRecordTestDomain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.shop", "address", "hosting.example.com"),
					resource.TestCheckResourceAttr("namecheap_dns_record.shop", "id", dnsRecordTestDomain+"/CNAME/shop/hosting.example.com"),
					// The zone holds the API's spelling; state holds the config's.
					assertZoneHas(m, "shop", "CNAME", "hosting.example.com."),
				),
			},
			{Config: config, PlanOnly: true},
			{
				// Importing by the undotted address the config uses must produce
				// exactly the state the apply did.
				ResourceName:      "namecheap_dns_record.shop",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccMockDNSRecordUpdateRefusesExistingIdentity covers an update that moves a
// record onto the identity of one that already exists. The SDK's upsert is a
// filter-then-append with no collision check, so the write would land a second
// record the selector can no longer tell apart — and the read straight after
// would refuse to touch either, wedging the resource with no way out but the
// dashboard. Create refuses this case; so must update.
func TestAccMockDNSRecordUpdateRefusesExistingIdentity(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(dnsRecordTestDomain, []hostEntry{
		// Managed by nobody, and the address step 2 tries to move onto.
		{Name: "www", Type: "A", Address: "10.7.0.2", TTL: 1800, MXPref: 10},
	}, "NONE", nil)

	config := func(address string) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "www" {
  domain   = "%s"
  hostname = "www"
  type     = "A"
  address  = "%s"
}
`, dnsRecordTestDomain, address)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("10.7.0.1"),
				Check:  assertZoneHas(m, "www", "A", "10.7.0.1"),
			},
			{
				Config:      config("10.7.0.2"),
				ExpectError: regexp.MustCompile(`(?s)DNS record already exists.*terraform import`),
			},
			{
				// The refusal must leave everything as it was: one record at each
				// address, and state still describing the one this resource owns.
				// SDKv2 persists planned values on any update error, so without the
				// restore this plan is not empty.
				Config:   config("10.7.0.1"),
				PlanOnly: true,
				Check: resource.ComposeTestCheckFunc(
					assertZoneHas(m, "www", "A", "10.7.0.1"),
					assertZoneCount(m, "www", "A", "10.7.0.2", 1),
				),
			},
		},
	})
}

// TestAccMockDNSRecordRejectsEmptyIdentityFields covers empty strings, which
// Terraform's Required does not reject. An empty hostname silently means the apex
// through SDK normalization, but renders an ID the importer itself refuses to
// parse — so the resource would create a record it could never import.
func TestAccMockDNSRecordRejectsEmptyIdentityFields(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "namecheap_dns_record" "empty_host" {
  domain   = "%s"
  hostname = ""
  type     = "A"
  address  = "10.8.0.1"
}
`, dnsRecordTestDomain),
				ExpectError: regexp.MustCompile(`(?s)hostname.*empty`),
			},
			{
				Config: fmt.Sprintf(`
resource "namecheap_dns_record" "empty_address" {
  domain   = "%s"
  hostname = "www"
  type     = "A"
  address  = ""
}
`, dnsRecordTestDomain),
				ExpectError: regexp.MustCompile(`(?s)address.*empty`),
			},
		},
	})
}

// TestAccMockDNSRecordMXPrefIgnoredOutsideMX covers an explicit mx_pref on a
// record type that has no preference. The API answers 10 for all of them, so
// reading that back over the configured value leaves a diff that can never be
// resolved — and applying it rewrites the whole zone to change nothing.
func TestAccMockDNSRecordMXPrefIgnoredOutsideMX(t *testing.T) {
	m := newNamecheapMock(t)
	seedUnmanagedZone(m)

	config := fmt.Sprintf(`
resource "namecheap_dns_record" "with_pref" {
  domain   = "%s"
  hostname = "pref"
  type     = "A"
  address  = "10.9.0.1"
  mx_pref  = 20
}
`, dnsRecordTestDomain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				// resource.Test fails the step on a non-empty post-apply plan, so
				// this alone proves the value no longer round-trips into a diff.
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.with_pref", "mx_pref", "20"),
					assertZoneHas(m, "pref", "A", "10.9.0.1"),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// assertZoneCount checks how many records in the mock's zone match a host, type
// and address — the assertion that catches a write which duplicated a record
// rather than replacing it.
func assertZoneCount(m *namecheapMock, host, recordType, address string, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		state := m.state(dnsRecordTestDomain)
		if state == nil {
			return fmt.Errorf("domain %s has no state in the mock", dnsRecordTestDomain)
		}
		var got int
		for _, h := range state.hosts {
			if h.Name == host && h.Type == recordType && h.Address == address {
				got++
			}
		}
		if got != want {
			return fmt.Errorf("%s %s -> %q: want %d record(s), got %d (zone now %+v)", host, recordType, address, want, got, state.hosts)
		}
		return nil
	}
}

// assertZoneMXPrefs checks the MX records for a host and address carry exactly
// the given preferences, in any order.
func assertZoneMXPrefs(m *namecheapMock, host, address string, want ...int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		state := m.state(dnsRecordTestDomain)
		if state == nil {
			return fmt.Errorf("domain %s has no state in the mock", dnsRecordTestDomain)
		}
		var got []int
		for _, h := range state.hosts {
			if h.Name == host && h.Type == "MX" && h.Address == address {
				got = append(got, h.MXPref)
			}
		}
		sort.Ints(got)
		sorted := append([]int(nil), want...)
		sort.Ints(sorted)
		if !slices.Equal(got, sorted) {
			return fmt.Errorf("MX %s -> %q: want preferences %v, got %v (zone now %+v)", host, address, sorted, got, state.hosts)
		}
		return nil
	}
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
