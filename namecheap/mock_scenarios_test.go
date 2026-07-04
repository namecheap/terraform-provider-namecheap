//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// This file ports the representative MERGE/OVERWRITE record and nameserver
// scenarios from the live acceptance suite (TestAccNamecheapDomainRecords) onto
// the stateful mock, so they run fast and without credentials on every PR. Each
// subtest gets its own mock (isolated state). The live sandbox suite remains the
// source of truth for API-contract fidelity.

const mockScenarioDomain = "mock-example.com"

// TestAccMockRecords covers the DNS-record MERGE/OVERWRITE code paths.
func TestAccMockRecords(t *testing.T) {
	const resourceName = "namecheap_domain_records.test"

	// MERGE onto an empty zone, then a MERGE update that replaces the managed
	// set. Exercises createRecordsMerge / updateRecordsMerge and the domain mutex.
	t.Run("merge_on_empty", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckHostsCleared(m, mockScenarioDomain),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "sub1"
    type     = "A"
    address  = "11.11.11.11"
    ttl      = 300
  }

  record {
    hostname = "sub2"
    type     = "A"
    address  = "22.22.22.22"
  }
}
`, mockScenarioDomain),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "record.#", "2"),
						mockCheckHostCount(m, mockScenarioDomain, 2),
						mockCheckHostContains(m, mockScenarioDomain, "sub1", "A", "11.11.11.11"),
						mockCheckHostContains(m, mockScenarioDomain, "sub2", "A", "22.22.22.22"),
					),
				},
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "sub11"
    type     = "A"
    address  = "111.111.111.111"
    ttl      = 1800
  }
}
`, mockScenarioDomain),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						mockCheckHostCount(m, mockScenarioDomain, 1),
						mockCheckHostContains(m, mockScenarioDomain, "sub11", "A", "111.111.111.111"),
					),
				},
			},
		})
	})

	// MERGE must leave pre-existing, unmanaged records intact — on both apply
	// and destroy.
	t.Run("merge_preserves_existing", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(mockScenarioDomain, []hostEntry{
			{Name: "sub1", Type: "A", Address: "22.22.22.22", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy: resource.ComposeTestCheckFunc(
				mockCheckHostCount(m, mockScenarioDomain, 1),
				mockCheckHostContains(m, mockScenarioDomain, "sub1", "A", "22.22.22.22"),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "sub2"
    type     = "A"
    address  = "33.33.33.33"
  }
}
`, mockScenarioDomain),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						mockCheckHostCount(m, mockScenarioDomain, 2),
						mockCheckHostContains(m, mockScenarioDomain, "sub1", "A", "22.22.22.22"),
						mockCheckHostContains(m, mockScenarioDomain, "sub2", "A", "33.33.33.33"),
					),
				},
			},
		})
	})

	// OVERWRITE replaces the whole zone with the configured records and sets the
	// email type; pre-existing records are dropped.
	t.Run("overwrite_replaces_all", func(t *testing.T) {
		m := newNamecheapMock(t)
		m.seed(mockScenarioDomain, []hostEntry{
			{Name: "old1", Type: "A", Address: "1.1.1.1", MXPref: 10, TTL: 1800},
			{Name: "old2", Type: "A", Address: "2.2.2.2", MXPref: 10, TTL: 1800},
		}, "NONE", nil)

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckHostsCleared(m, mockScenarioDomain),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain     = "%s"
  mode       = "OVERWRITE"
  email_type = "FWD"

  record {
    hostname = "www"
    type     = "A"
    address  = "5.5.5.5"
    ttl      = 1800
  }
}
`, mockScenarioDomain),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						resource.TestCheckResourceAttr(resourceName, "email_type", "FWD"),
						mockCheckHostCount(m, mockScenarioDomain, 1),
						mockCheckHostContains(m, mockScenarioDomain, "www", "A", "5.5.5.5"),
						mockCheckEmailType(m, mockScenarioDomain, "FWD"),
					),
				},
			},
		})
	})

	// Two MERGE resources declaring the same record must be rejected by the
	// provider's duplicate detection.
	t.Run("duplicate_conflict", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%[1]s"
  mode   = "MERGE"

  record {
    hostname = "sub1"
    type     = "A"
    address  = "22.22.22.22"
  }
}

resource "namecheap_domain_records" "test_two" {
  domain = "%[1]s"
  mode   = "MERGE"

  record {
    hostname = "sub1"
    type     = "A"
    address  = "22.22.22.22"
  }
}
`, mockScenarioDomain),
					ExpectError: regexp.MustCompile("Error: Duplicate record"),
				},
			},
		})
	})

	// MX record with an email type: the provider appends a trailing dot to the
	// MX address on write, so the mock persists the dotted form (as the real API
	// does) while the round-trip stays diff-free.
	t.Run("mx_email_type", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckHostsCleared(m, mockScenarioDomain),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain     = "%s"
  mode       = "OVERWRITE"
  email_type = "MX"

  record {
    hostname = "@"
    type     = "MX"
    address  = "mail.example.com"
    mx_pref  = 10
  }
}
`, mockScenarioDomain),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "record.#", "1"),
						resource.TestCheckResourceAttr(resourceName, "email_type", "MX"),
						mockCheckEmailType(m, mockScenarioDomain, "MX"),
						mockCheckHostContains(m, mockScenarioDomain, "@", "MX", "mail.example.com."),
					),
				},
			},
		})
	})
}

// TestAccMockNameservers covers the custom-nameserver MERGE/OVERWRITE code paths.
func TestAccMockNameservers(t *testing.T) {
	const resourceName = "namecheap_domain_records.test"

	t.Run("merge", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameserversDefault(m, mockScenarioDomain),
			Steps: []resource.TestStep{
				{
					Config: mockNameserversConfig("MERGE",
						"dns1.namecheaphosting.com", "dns2.namecheaphosting.com"),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(resourceName, "nameservers.#", "2"),
						mockCheckNameservers(m, mockScenarioDomain,
							"dns1.namecheaphosting.com", "dns2.namecheaphosting.com"),
					),
				},
			},
		})
	})

	t.Run("overwrite_update", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      mockCheckNameserversDefault(m, mockScenarioDomain),
			Steps: []resource.TestStep{
				{
					Config: mockNameserversConfig("OVERWRITE",
						"dns1.namecheaphosting.com", "dns2.namecheaphosting.com"),
					Check: resource.ComposeTestCheckFunc(
						mockCheckNameservers(m, mockScenarioDomain,
							"dns1.namecheaphosting.com", "dns2.namecheaphosting.com"),
					),
				},
				{
					Config: mockNameserversConfig("OVERWRITE",
						"ns-1467.awsdns-55.org", "ns-1076.awsdns-06.org"),
					Check: resource.ComposeTestCheckFunc(
						mockCheckNameservers(m, mockScenarioDomain,
							"ns-1467.awsdns-55.org", "ns-1076.awsdns-06.org"),
					),
				},
			},
		})
	})

	// Two MERGE resources sharing a nameserver must be rejected. Both declare two
	// nameservers (satisfying the minimum-two rule) with one overlapping, so the
	// duplicate — not the minimum-count — check is what fires.
	t.Run("duplicate_conflict", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain      = "%[1]s"
  mode        = "MERGE"
  nameservers = ["ns-1467.awsdns-55.org", "ns-1076.awsdns-06.org"]
}

resource "namecheap_domain_records" "test_two" {
  domain      = "%[1]s"
  mode        = "MERGE"
  nameservers = ["ns-1467.awsdns-55.org", "ns-2000.awsdns-99.org"]
}
`, mockScenarioDomain),
					ExpectError: regexp.MustCompile("Error: Duplicate nameserver"),
				},
			},
		})
	})
}

// mockNameserversConfig builds a resource managing the given nameservers.
func mockNameserversConfig(mode string, nameservers ...string) string {
	quoted := make([]string, 0, len(nameservers))
	for _, ns := range nameservers {
		quoted = append(quoted, fmt.Sprintf("%q", ns))
	}
	list := ""
	for i, s := range quoted {
		if i > 0 {
			list += ", "
		}
		list += s
	}
	return fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain      = "%s"
  mode        = "%s"
  nameservers = [%s]
}
`, mockScenarioDomain, mode, list)
}
