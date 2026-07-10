//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// This file covers the OVERWRITE unmanaged-record-deletion safety net (#65,
// #250): warning diagnostics that surface what OVERWRITE mode is about to
// delete, at zero extra API calls during plan/refresh. Warning diagnostic
// content (summary/detail text, HCL rendering) is unit-tested in
// namecheap_domain_record_functions_test.go and
// namecheap_domain_record_crud_handler_test.go - terraform-plugin-testing
// v1.16.0 has no TestStep support for asserting on warning diagnostics, so
// these acceptance tests assert the behavioral effects instead: payloads,
// call counts, and plan emptiness.

// TestAccMockOverwriteSafety_CreateOverExisting covers the #65 repro: records
// already exist on a domain before Terraform ever reads it, so create gets no
// refresh to show the deletion in a plan. The apply-time pre-flight in
// createRecordsOverwrite must still let the create succeed and end with only
// the configured record on the zone.
func TestAccMockOverwriteSafety_CreateOverExisting(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"
	const resourceName = "namecheap_domain_records.test"

	// Out-of-band records exist before Terraform ever manages this domain.
	m.seed(domain, []hostEntry{
		{Name: "old1", Type: "A", Address: "10.9.9.1", MXPref: 10, TTL: 1800},
		{Name: "old2", Type: "A", Address: "10.9.9.2", MXPref: 10, TTL: 1800},
		{Name: "old3", Type: "A", Address: "10.9.9.3", MXPref: 10, TTL: 1800},
	}, "NONE", nil)

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
    ttl      = 1800
  }
}
`, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
					mockCheckHostCount(m, domain, 1),
					mockCheckHostContains(m, domain, "www", "A", "10.0.0.1"),
				),
			},
		},
	})
}

// TestAccMockOverwriteSafety_PlanAddsNoExtraAPICalls covers the "no extra API
// calls to plan" acceptance criterion: once an out-of-band record has
// appeared, detecting and warning about it during refresh/plan must not add
// any GetHosts calls beyond the ordinary single refresh read.
func TestAccMockOverwriteSafety_PlanAddsNoExtraAPICalls(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"
	const resourceName = "namecheap_domain_records.test"

	config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
    ttl      = 1800
  }
}
`, domain)

	var getHostsBeforeDrift int

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckHostsCleared(m, domain),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
					mockCheckHostCount(m, domain, 1),
				),
			},
			{
				// An out-of-band record appears on the zone. The unmanaged
				// set is derived entirely from the read this refresh already
				// performs - no additional request should result.
				PreConfig: func() {
					getHostsBeforeDrift = m.commandCount("namecheap.domains.dns.getHosts")
					m.seed(domain, []hostEntry{
						{Name: "www", Type: "A", Address: "10.0.0.1", MXPref: 10, TTL: 1800},
						{Name: "api", Type: "A", Address: "10.0.0.2", MXPref: 10, TTL: 1800},
					}, "NONE", nil)
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						got := m.commandCount("namecheap.domains.dns.getHosts") - getHostsBeforeDrift
						if got != 1 {
							return fmt.Errorf(
								"plan issued %d GetHosts call(s) after the drift, want exactly 1 (the ordinary refresh read) - "+
									"the unmanaged-record warning must not add API calls at plan time", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// Destroy's own unmanaged-deletion warning (deleteRecordsOverwrite's
// pre-flight against priorStateRecords) is unit-tested directly in
// namecheap_domain_record_crud_delete_records_test.go
// (TestDeleteRecordsOverwrite_WarnsAboutUnmanagedRecords), rather than at the
// acceptance layer: the CLI always refreshes before planning a destroy, and
// that refresh's OVERWRITE Read adopts any out-of-band record into state
// (readRecordsOverwrite returns every live non-parking record, managed or
// not), so a record injected between an acceptance test's steps is no longer
// "unmanaged" by the time destroy actually runs - there is no way to
// reproduce the race through TestStep without fighting the framework's
// refresh timing. Destroy's ordinary "wipe everything" success path remains
// covered by CheckDestroy in every test in this file.

// TestAccMockOverwriteSafety_GoldenAdoptionRoundTrip covers the drift-adoption
// escape hatch: pasting the HCL block the warning would have suggested for an
// out-of-band record into the config must produce a clean apply with no
// further pending changes. adoptedConfig's second record block is
// byte-for-byte what formatRecordHCL renders for the seeded "api" record
// below (always emits ttl; A records never emit mx_pref).
func TestAccMockOverwriteSafety_GoldenAdoptionRoundTrip(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"
	const resourceName = "namecheap_domain_records.test"

	singleRecordConfig := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
    ttl      = 1800
  }
}
`, domain)

	adoptedConfig := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
    ttl      = 1800
  }

  record {
    hostname = "api"
    type     = "A"
    address  = "10.0.0.2"
    ttl      = 1800
  }
}
`, domain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckHostsCleared(m, domain),
		Steps: []resource.TestStep{
			{
				Config: singleRecordConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
					mockCheckHostCount(m, domain, 1),
				),
			},
			{
				// The out-of-band "api" record appears; adoptedConfig pastes
				// in exactly the block the warning would have suggested.
				PreConfig: func() {
					m.seed(domain, []hostEntry{
						{Name: "www", Type: "A", Address: "10.0.0.1", MXPref: 10, TTL: 1800},
						{Name: "api", Type: "A", Address: "10.0.0.2", MXPref: 10, TTL: 1800},
					}, "NONE", nil)
				},
				Config: adoptedConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "record.#", "2"),
					mockCheckHostCount(m, domain, 2),
					mockCheckHostContains(m, domain, "www", "A", "10.0.0.1"),
					mockCheckHostContains(m, domain, "api", "A", "10.0.0.2"),
				),
				// ExpectNonEmptyPlan defaults to false, so the test framework
				// itself asserts that a follow-up plan against this same
				// config is empty - proving the pasted block is a true
				// golden adoption with nothing left pending.
			},
		},
	})
}

// TestAccMockOverwriteSafety_PreflightAPIErrorAbortsBeforeDestruction covers
// the negative path: when the apply-time pre-flight GetHosts call fails, the
// destructive SetHosts must never be reached and the pre-existing backend
// state must be left untouched.
func TestAccMockOverwriteSafety_PreflightAPIErrorAbortsBeforeDestruction(t *testing.T) {
	m := newNamecheapMock(t)
	const domain = "mock-example.com"

	// A record already exists on the zone out-of-band (Terraform has no
	// state for this domain yet), and every GetHosts call is made to fail -
	// this forces the create's apply-time pre-flight to error out.
	m.seed(domain, []hostEntry{
		{Name: "www", Type: "A", Address: "10.0.0.1", MXPref: 10, TTL: 1800},
	}, "NONE", nil)

	config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "OVERWRITE"

  record {
    hostname = "www"
    type     = "A"
    address  = "10.0.0.1"
    ttl      = 1800
  }

  record {
    hostname = "new"
    type     = "A"
    address  = "10.0.0.9"
    ttl      = 1800
  }
}
`, domain)

	m.failOn("namecheap.domains.dns.getHosts", "2019166", "Domain not found")

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`Domain not found`),
			},
		},
	})

	// The pre-flight failed before any SetHosts could run, so the backend
	// must still show exactly the pre-existing record.
	st := m.state(domain)
	if st == nil || len(st.hosts) != 1 || st.hosts[0].Name != "www" {
		t.Fatalf("expected backend state to be untouched after a failed pre-flight, got %+v", st)
	}
}
