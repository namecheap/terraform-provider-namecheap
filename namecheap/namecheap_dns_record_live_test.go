package namecheap_provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// Live-sandbox acceptance coverage for namecheap_dns_record, against the real
// Namecheap API. Like the other TestAcc* tests these run only under TF_ACC and
// skip (via testAccPreCheck) without NAMECHEAP_* credentials and a test domain.
//
// What these are for, and what they are not: the mock suite
// (mock_dns_record_test.go) is the exhaustive one — it is deterministic, free, and
// covers every branch including the ones that need a zone in a state the API will
// not let you construct on demand. These tests exist to prove the API contract
// those mock assertions are built on is real: that Namecheap does add a trailing
// dot to a CNAME target, does report a fixed MX preference outside MX, does accept
// a second MX at another preference, and does leave records this resource never
// mentioned alone.
//
// They are deliberately frugal. Namecheap has no per-record API, so one apply of
// one record costs five calls (identity lookup, then the SDK's
// read-modify-write-verify, then the read-back), and CI splits a 20/min account
// quota between this suite's two clients — 10/min each (see the Acceptance Test
// step in ci.yml). The four tests below cost roughly 80 provider calls, about
// nine minutes of wall clock, inside the suite's 30m budget. Adding a record type
// here costs ~11 calls; add it to the mock suite instead unless the API's own
// behaviour for it is the thing in question.
//
// Every record these tests create outside Terraform is removed again in a
// t.Cleanup, and they seed through the SDK's record helpers rather than SetHosts,
// so a failure cannot take the shared sandbox domain's other records with it.

// liveTestConfigured reports whether the live environment is complete enough to
// touch the API. Test bodies do their seeding before resource.Test runs, so they
// cannot rely on PreCheck's skip to keep them off the network.
func liveTestConfigured() bool {
	return os.Getenv("TF_ACC") == "1" && os.Getenv("NAMECHEAP_API_KEY") != "" && *testAccDomain != ""
}

// liveRecordWant is one assertion about the sandbox zone: that a record with this
// identity is present (optionally with this TTL and preference), or that it is
// absent. TTL and MXPref are only checked when non-zero.
type liveRecordWant struct {
	host       string
	recordType string
	address    string
	ttl        int
	mxPref     int
	absent     bool
}

// checkLiveZone reads the sandbox zone once and asserts every expectation against
// it. One call per step, rather than one per assertion, because each read spends
// the same rate-limited quota the provider is competing for.
func checkLiveZone(t *testing.T, wants ...liveRecordWant) resource.TestCheckFunc {
	t.Helper()
	return func(*terraform.State) error {
		zone, err := liveZoneRecords()
		if err != nil {
			return err
		}

		for _, want := range wants {
			matches := liveZoneMatches(zone, want)
			if want.absent {
				if len(matches) != 0 {
					return fmt.Errorf("%s %s -> %q should be gone from %s, found %d",
						want.host, want.recordType, want.address, *testAccDomain, len(matches))
				}
				continue
			}
			if len(matches) != 1 {
				return fmt.Errorf("%s %s -> %q in %s: want exactly 1 record, found %d (zone: %s)",
					want.host, want.recordType, want.address, *testAccDomain, len(matches), liveZoneSummary(zone))
			}
			got := matches[0]
			if want.ttl != 0 && derefInt(got.TTL) != want.ttl {
				return fmt.Errorf("%s %s -> %q: TTL = %d, want %d",
					want.host, want.recordType, want.address, derefInt(got.TTL), want.ttl)
			}
			if want.mxPref != 0 && derefInt(got.MXPref) != want.mxPref {
				return fmt.Errorf("%s %s -> %q: MX preference = %d, want %d",
					want.host, want.recordType, want.address, derefInt(got.MXPref), want.mxPref)
			}
		}
		return nil
	}
}

// liveZoneMatches finds the records sharing want's identity, comparing the way the
// provider does so the API's own spelling (a lower-cased host, a trailing dot on a
// CNAME target) still matches what the test asked for.
func liveZoneMatches(zone []namecheap.DomainsDNSHostRecordDetailed, want liveRecordWant) []namecheap.DomainsDNSHostRecordDetailed {
	target := namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(want.host),
		RecordType: namecheap.String(want.recordType),
		Address:    namecheap.String(want.address),
	}
	if want.mxPref != 0 {
		target.MXPref = namecheap.UInt8(uint8(want.mxPref))
	}
	return dnsRecordMatches(zone, target)
}

// liveZoneRecords reads the test domain's host records through the helper client.
func liveZoneRecords() ([]namecheap.DomainsDNSHostRecordDetailed, error) {
	resp, err := namecheapSDKClient.DomainsDNS.GetHostsWithContext(context.Background(), *testAccDomain)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", *testAccDomain, err)
	}
	if resp == nil || resp.DomainDNSGetHostsResult == nil || resp.DomainDNSGetHostsResult.Hosts == nil {
		return nil, nil
	}
	return *resp.DomainDNSGetHostsResult.Hosts, nil
}

// liveZoneSummary renders a zone for a failure message.
func liveZoneSummary(zone []namecheap.DomainsDNSHostRecordDetailed) string {
	entries := make([]string, 0, len(zone))
	for _, host := range zone {
		entries = append(entries, fmt.Sprintf("%s %s %s (ttl=%d mxpref=%d)",
			derefString(host.Name), derefString(host.Type), derefString(host.Address), derefInt(host.TTL), derefInt(host.MXPref)))
	}
	return strings.Join(entries, "; ")
}

// seedLiveRecords adds records to the sandbox zone outside Terraform — standing in
// for whatever else already lives in a real zone — and registers their removal. It
// preserves every other record, because AddRecords is a read-modify-write rather
// than the whole-zone replace setHosts would be.
//
// addOpts and removeOpts are separate because the mail cases need them to differ:
// adding an MX record requires the zone to move to EmailType=MX, and removing the
// last one requires it to move back, so passing the add's option to the delete
// would leave a set the API refuses (an MX zone must keep an MX record).
func seedLiveRecords(t *testing.T, records []namecheap.DomainsDNSHostRecord, addOpts, removeOpts []namecheap.RecordOption) {
	t.Helper()
	if !liveTestConfigured() {
		return
	}

	if _, err := namecheapSDKClient.DomainsDNS.AddRecordsWithContext(
		context.Background(), *testAccDomain, records, addOpts...); err != nil {
		t.Fatalf("failed to seed %d unmanaged record(s) on %s: %v", len(records), *testAccDomain, err)
	}

	t.Cleanup(func() {
		for _, record := range records {
			if _, err := namecheapSDKClient.DomainsDNS.DeleteRecordsWithContext(
				context.Background(), *testAccDomain, dnsRecordSelector(record), removeOpts...); err != nil {
				t.Errorf("failed to remove seeded record %s %s -> %q from %s: %v",
					derefString(record.HostName), derefString(record.RecordType), derefString(record.Address), *testAccDomain, err)
			}
		}
	})
}

// liveAlreadyExistsError is the refusal both create and update answer with when the
// record a configuration asks for is already in the zone.
var liveAlreadyExistsError = regexp.MustCompile(`(?s)DNS record already exists.*terraform import`)

// liveRecord builds a seed record.
func liveRecord(host, recordType, address string, ttl int) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(host),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
		MXPref:     namecheap.UInt8(dnsRecordFixedMXPref),
		TTL:        namecheap.Int(ttl),
	}
}

// TestAccNamecheapDNSRecordLifecycle is the core live test: create, in-place
// update, replacement, import and destroy of one record against the real API,
// asserting at every step that a record this resource does not manage is still
// there. That surviving record is the whole promise of this resource — the API
// replaces the entire zone on every write, so a bug in the read-modify-write
// deletes data the configuration never mentioned.
func TestAccNamecheapDNSRecordLifecycle(t *testing.T) {
	const (
		unmanagedHost = "tf-dnsrec-untouched"
		managedHost   = "tf-dnsrec-lifecycle"
		movedHost     = "tf-dnsrec-lifecycle-moved"
	)
	seedLiveRecords(t, []namecheap.DomainsDNSHostRecord{
		liveRecord(unmanagedHost, "A", "203.0.113.240", 1800),
	}, nil, nil)

	config := func(host, address string, ttl int) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "lifecycle" {
  domain   = %[1]q
  hostname = %[2]q
  type     = "A"
  address  = %[3]q
  ttl      = %[4]d
}
`, *testAccDomain, host, address, ttl)
	}

	unmanagedIntact := liveRecordWant{host: unmanagedHost, recordType: "A", address: "203.0.113.240"}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(managedHost, "203.0.113.10", 1800),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.lifecycle", "address", "203.0.113.10"),
					resource.TestCheckResourceAttr("namecheap_dns_record.lifecycle", "id",
						strings.ToLower(*testAccDomain)+"/A/"+managedHost+"/203.0.113.10"),
					checkLiveZone(t,
						liveRecordWant{host: managedHost, recordType: "A", address: "203.0.113.10", ttl: 1800},
						unmanagedIntact,
					),
				),
			},
			{
				// address and ttl are not ForceNew: one write, in place, and the
				// address it replaces must not be left behind as a second record.
				Config: config(managedHost, "203.0.113.11", 600),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.lifecycle", "ttl", "600"),
					checkLiveZone(t,
						liveRecordWant{host: managedHost, recordType: "A", address: "203.0.113.11", ttl: 600},
						liveRecordWant{host: managedHost, recordType: "A", address: "203.0.113.10", absent: true},
						unmanagedIntact,
					),
				),
			},
			{
				// hostname is part of the identity, so this is a replacement: the old
				// host has to be gone rather than edited into the new one.
				Config: config(movedHost, "203.0.113.11", 600),
				Check: resource.ComposeTestCheckFunc(
					checkLiveZone(t,
						liveRecordWant{host: movedHost, recordType: "A", address: "203.0.113.11", ttl: 600},
						liveRecordWant{host: managedHost, recordType: "A", address: "203.0.113.11", absent: true},
						unmanagedIntact,
					),
				),
			},
			{
				// Importing what Terraform already manages must reproduce the same
				// state, which is what makes the ID format trustworthy.
				ResourceName:      "namecheap_dns_record.lifecycle",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
		CheckDestroy: checkLiveZone(t,
			liveRecordWant{host: movedHost, recordType: "A", address: "203.0.113.11", absent: true},
			unmanagedIntact,
		),
	})
}

// TestAccNamecheapDNSRecordRoundTrips is the test the case-preservation and
// trailing-dot handling rest on. Both are claims about what the *real* API does
// with a value on its way in and out: it lower-cases a host, and it appends a dot
// to a hostname-valued target. The mock reproduces both, and this proves the mock
// is not making them up.
//
// A record whose stored spelling differs from the configured one is the dangerous
// case — read it back into state and every subsequent plan proposes a change,
// which for a ForceNew field means destroying and recreating a live record on
// every apply. resource.Test fails a step whose post-apply plan is non-empty, so
// an apply that survives here is the assertion.
func TestAccNamecheapDNSRecordRoundTrips(t *testing.T) {
	// Deliberately mixed case, and a CNAME target written the way a configuration
	// writes one — without the trailing dot Namecheap stores.
	config := fmt.Sprintf(`
resource "namecheap_dns_record" "cname" {
  domain   = %[1]q
  hostname = "TF-DNSREC-Alias"
  type     = "CNAME"
  address  = "hosting.example.com"
}

resource "namecheap_dns_record" "txt" {
  domain   = %[1]q
  hostname = "tf-dnsrec-txt"
  type     = "TXT"
  address  = "v=spf1 include:example.com -all"
}
`, *testAccDomain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					// State keeps what the configuration said, capitals and all.
					resource.TestCheckResourceAttr("namecheap_dns_record.cname", "hostname", "TF-DNSREC-Alias"),
					resource.TestCheckResourceAttr("namecheap_dns_record.cname", "address", "hosting.example.com"),
					// The ID stays canonical regardless.
					resource.TestCheckResourceAttr("namecheap_dns_record.cname", "id",
						strings.ToLower(*testAccDomain)+"/CNAME/tf-dnsrec-alias/hosting.example.com"),
					// A TXT value with spaces must survive the round trip verbatim.
					resource.TestCheckResourceAttr("namecheap_dns_record.txt", "address", "v=spf1 include:example.com -all"),
					// And the zone holds the API's own spelling of both.
					checkLiveZone(t,
						liveRecordWant{host: "tf-dnsrec-alias", recordType: "CNAME", address: "hosting.example.com"},
						liveRecordWant{host: "tf-dnsrec-txt", recordType: "TXT", address: "v=spf1 include:example.com -all"},
					),
					checkLiveTrailingDot(t, "tf-dnsrec-alias", "CNAME"),
				),
			},
			{
				// An explicit second look: no drift, nothing to do.
				Config:   config,
				PlanOnly: true,
			},
		},
		CheckDestroy: checkLiveZone(t,
			liveRecordWant{host: "tf-dnsrec-alias", recordType: "CNAME", address: "hosting.example.com", absent: true},
			liveRecordWant{host: "tf-dnsrec-txt", recordType: "TXT", address: "v=spf1 include:example.com -all", absent: true},
		),
	})
}

// checkLiveTrailingDot asserts the API really does store a hostname-valued target
// with a trailing dot. If Namecheap ever stops doing that the provider still works
// — normalization tolerates both — but the mock would be modelling a behaviour
// that no longer exists, so this logs rather than fails.
func checkLiveTrailingDot(t *testing.T, host, recordType string) resource.TestCheckFunc {
	t.Helper()
	return func(*terraform.State) error {
		zone, err := liveZoneRecords()
		if err != nil {
			return err
		}
		for _, record := range zone {
			if !strings.EqualFold(derefString(record.Name), host) || derefString(record.Type) != recordType {
				continue
			}
			if !strings.HasSuffix(derefString(record.Address), ".") {
				t.Logf("note: %s %s is stored as %q, without the trailing dot the mock models — "+
					"harmless for the provider, but mockNormalizeAddress may be describing old API behaviour",
					host, recordType, derefString(record.Address))
			}
			return nil
		}
		return fmt.Errorf("%s %s not found in %s", host, recordType, *testAccDomain)
	}
}

// TestAccNamecheapDNSRecordMXPreference covers the mail case against the real API,
// which is the one place the SDK's own validation is known to be uncertain (see
// go-namecheap-sdk#162): it ties MX records to the zone's EmailType, and whether
// those rules match the live API has not been settled.
//
// It also proves the property the resource's MX identity handling exists for: a
// primary and a backup mail server naming the same host, distinguished only by
// preference. Creating the second must not look like a duplicate, and destroying
// it must leave the first — on an EmailType=MX zone, deleting both would leave a
// record set the API refuses outright.
func TestAccNamecheapDNSRecordMXPreference(t *testing.T) {
	mailHost := "mail." + *testAccDomain

	// The primary MX, and the EmailType=MX the API requires before any MX record can
	// be written. This resource does not manage EmailType, so the zone has to arrive
	// in that state — which is exactly how a real user reaches this resource.
	//
	// The removal carries the EmailType the zone had before, so the shared sandbox
	// domain is left as it was found: taking the last MX record out of an MX zone is
	// rejected unless the same write moves the type back down.
	restoreEmailType := liveEmailTypeOrDefault(t)
	seedLiveRecords(t,
		[]namecheap.DomainsDNSHostRecord{liveRecord("@", "MX", mailHost, 1800)},
		[]namecheap.RecordOption{namecheap.WithEmailType(namecheap.EmailTypeMX)},
		[]namecheap.RecordOption{namecheap.WithEmailType(restoreEmailType)},
	)

	config := func(pref int) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "backup_mx" {
  domain   = %[1]q
  hostname = "@"
  type     = "MX"
  address  = %[2]q
  mx_pref  = %[3]d
}
`, *testAccDomain, mailHost, pref)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				// A second MX for the same host at a different preference: the
				// preference is what tells it from the seeded primary.
				Config: config(20),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("namecheap_dns_record.backup_mx", "mx_pref", "20"),
					checkLiveZone(t,
						liveRecordWant{host: "@", recordType: "MX", address: mailHost, mxPref: 20},
						liveRecordWant{host: "@", recordType: "MX", address: mailHost, mxPref: dnsRecordFixedMXPref},
					),
				),
			},
			{
				// The preference is part of an MX record's identity, so changing it
				// still has to be an in-place change of this record — not a second
				// one, and not a rewrite of the primary.
				Config: config(30),
				Check: resource.ComposeTestCheckFunc(
					checkLiveZone(t,
						liveRecordWant{host: "@", recordType: "MX", address: mailHost, mxPref: 30},
						liveRecordWant{host: "@", recordType: "MX", address: mailHost, mxPref: dnsRecordFixedMXPref},
					),
				),
			},
		},
		// Destroying the backup must take only the backup — leaving the zone with
		// the MX record its EmailType requires. Deleting both would leave a record
		// set the API refuses outright, which is how this bug announced itself.
		CheckDestroy: checkLiveZone(t,
			liveRecordWant{host: "@", recordType: "MX", address: mailHost, mxPref: dnsRecordFixedMXPref},
			liveRecordWant{host: "@", recordType: "MX", address: mailHost, mxPref: 30, absent: true},
		),
	})
}

// liveEmailTypeOrDefault reads the test domain's current EmailType, so a test that
// has to move it can put it back. An unreadable or absent value falls back to NONE,
// which is what the API reports for a domain with no mail routing — and what the
// rest of this suite leaves the sandbox domain on.
func liveEmailTypeOrDefault(t *testing.T) string {
	t.Helper()
	if !liveTestConfigured() {
		return namecheap.EmailTypeNone
	}

	resp, err := namecheapSDKClient.DomainsDNS.GetHostsWithContext(context.Background(), *testAccDomain)
	if err != nil {
		t.Fatalf("failed to read the current EmailType of %s: %v", *testAccDomain, err)
	}
	if resp == nil || resp.DomainDNSGetHostsResult == nil ||
		resp.DomainDNSGetHostsResult.EmailType == nil || *resp.DomainDNSGetHostsResult.EmailType == "" {
		return namecheap.EmailTypeNone
	}
	return *resp.DomainDNSGetHostsResult.EmailType
}

// TestAccNamecheapDNSRecordRefusals covers the three cases where the right answer
// against the real API is to refuse and explain, not to write. Each one, written
// anyway, produces two records in the zone that no selector can separate — the
// resource would be wedged, fixable only in the dashboard.
//
// These steps are cheap: they fail during the identity lookup, before any write.
func TestAccNamecheapDNSRecordRefusals(t *testing.T) {
	// Two records on ONE host: the update guard has to be reached by changing
	// `address`, which is updated in place. Changing `hostname` would be a
	// replacement and would exercise create's guard all over again.
	const host = "tf-dnsrec-refusals"
	seedLiveRecords(t, []namecheap.DomainsDNSHostRecord{
		liveRecord(host, "A", "203.0.113.60", 1800),
		liveRecord(host, "A", "203.0.113.61", 1800),
	}, nil, nil)

	adopted := func(address string) string {
		return fmt.Sprintf(`
resource "namecheap_dns_record" "adopted" {
  domain   = %[1]q
  hostname = %[2]q
  type     = "A"
  address  = %[3]q
}
`, *testAccDomain, host, address)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				// Creating a record that already exists must point at import rather
				// than adding a second, indistinguishable copy.
				Config:      adopted("203.0.113.60"),
				ExpectError: liveAlreadyExistsError,
			},
			{
				// So take ownership of it the way the error says to.
				Config:             adopted("203.0.113.60"),
				ResourceName:       "namecheap_dns_record.adopted",
				ImportState:        true,
				ImportStateId:      strings.ToLower(*testAccDomain) + "/A/" + host + "/203.0.113.60",
				ImportStatePersist: true,
			},
			{
				// Now move it onto the other record's address. The SDK's upsert is a
				// filter-then-append with no collision check, so writing this would
				// leave two records nothing can tell apart and wedge the resource.
				Config:      adopted("203.0.113.61"),
				ExpectError: liveAlreadyExistsError,
			},
			{
				// The refusal must have changed nothing: both records still there,
				// exactly once each, and state still on the one it adopted.
				Config:   adopted("203.0.113.60"),
				PlanOnly: true,
				Check: checkLiveZone(t,
					liveRecordWant{host: host, recordType: "A", address: "203.0.113.60"},
					liveRecordWant{host: host, recordType: "A", address: "203.0.113.61"},
				),
			},
		},
	})
}
