//go:build testacc

package namecheap_provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// MERGE adoption: a declared record that already exists live is the same
// record. Create adopts it instead of refusing with "Duplicate record", update
// adopts it instead of appending a second copy, and applying the same config
// again changes nothing. Records the resource never declared stay on the zone
// and never enter state.

const mockAdoptionDomain = "mock-example.com"
const mockAdoptionResource = "namecheap_domain_records.web"

// mockAdoptionZone is a zone shared with other owners: blog belongs to another
// config, the two api rows were created by hand.
func mockAdoptionZone() []hostEntry {
	return []hostEntry{
		{Name: "@", Type: "A", Address: "1.1.1.1", MXPref: 10, TTL: 1800},
		{Name: "blog", Type: "CNAME", Address: "other.example.com.", MXPref: 10, TTL: 1800},
		{Name: "api", Type: "A", Address: "3.3.3.3", MXPref: 10, TTL: 1800},
		{Name: "api", Type: "A", Address: "4.4.4.4", MXPref: 10, TTL: 1800},
	}
}

// mockAdoptionConfig declares @ and, when apiTTL > 0, api 3.3.3.3 with that TTL.
func mockAdoptionConfig(apiTTL int) string {
	api := ""
	if apiTTL > 0 {
		api = fmt.Sprintf(`
  record {
    hostname = "api"
    type     = "A"
    address  = "3.3.3.3"
    ttl      = %d
  }
`, apiTTL)
	}
	return fmt.Sprintf(`
resource "namecheap_domain_records" "web" {
  domain = "%s"
  mode   = "MERGE"

  record {
    hostname = "@"
    type     = "A"
    address  = "1.1.1.1"
  }
%s}
`, mockAdoptionDomain, api)
}

// mockCheckSingleHost asserts the mock holds exactly one copy of the record,
// with the given TTL.
func mockCheckSingleHost(m *namecheapMock, domain, name, recordType, address string, ttl int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return fmt.Errorf("mock has no state for %q", domain)
		}
		var copies []hostEntry
		for _, h := range st.hosts {
			if h.Name == name && h.Type == recordType && h.Address == address {
				copies = append(copies, h)
			}
		}
		if len(copies) != 1 {
			return fmt.Errorf("mock holds %d copies of %s/%s/%s, want 1 (have %+v)", len(copies), name, recordType, address, st.hosts)
		}
		if copies[0].TTL != ttl {
			return fmt.Errorf("%s/%s/%s has ttl %d, want %d", name, recordType, address, copies[0].TTL, ttl)
		}
		return nil
	}
}

// mockCheckHostAbsent asserts the mock holds no copy of the record.
func mockCheckHostAbsent(m *namecheapMock, domain, name, recordType, address string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return nil
		}
		for _, h := range st.hosts {
			if h.Name == name && h.Type == recordType && h.Address == address {
				return fmt.Errorf("mock still holds %s/%s/%s (have %+v)", name, recordType, address, st.hosts)
			}
		}
		return nil
	}
}

// mockCheckUndeclaredKept asserts the rows web never declared are still live.
func mockCheckUndeclaredKept(m *namecheapMock) resource.TestCheckFunc {
	return resource.ComposeTestCheckFunc(
		mockCheckSingleHost(m, mockAdoptionDomain, "blog", "CNAME", "other.example.com.", 1800),
		mockCheckSingleHost(m, mockAdoptionDomain, "api", "A", "4.4.4.4", 1800),
	)
}

// Case 1: the resource is not in state (a fresh config, or after
// `terraform state rm`) and its records are already live. Create adopts them;
// the same config applied again writes nothing; destroy removes the adopted
// records, which the resource now owns, and keeps the rest.
func TestAccMockMergeAdoption_CreateAdoptsLiveRecords(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(mockAdoptionDomain, mockAdoptionZone(), "NONE", nil)

	var setHostsAfterCreate int

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy: resource.ComposeTestCheckFunc(
			mockCheckHostCount(m, mockAdoptionDomain, 2),
			mockCheckHostAbsent(m, mockAdoptionDomain, "@", "A", "1.1.1.1"),
			mockCheckHostAbsent(m, mockAdoptionDomain, "api", "A", "3.3.3.3"),
			mockCheckUndeclaredKept(m),
		),
		Steps: []resource.TestStep{
			{
				Config: mockAdoptionConfig(300),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mockAdoptionResource, "record.#", "2"),
					mockCheckHostCount(m, mockAdoptionDomain, 4),
					mockCheckSingleHost(m, mockAdoptionDomain, "@", "A", "1.1.1.1", 1800),
					mockCheckSingleHost(m, mockAdoptionDomain, "api", "A", "3.3.3.3", 300),
					mockCheckUndeclaredKept(m),
					func(*terraform.State) error {
						setHostsAfterCreate = m.commandCount("namecheap.domains.dns.setHosts")
						return nil
					},
				),
			},
			{
				// Applying the same config again: empty plan, no write.
				Config: mockAdoptionConfig(300),
				Check: func(*terraform.State) error {
					if got := m.commandCount("namecheap.domains.dns.setHosts"); got != setHostsAfterCreate {
						return fmt.Errorf("re-apply sent setHosts: %d calls, want %d", got, setHostsAfterCreate)
					}
					return nil
				},
			},
		},
	})
}

// Cases 2, 4 and 5: the resource is in state and a record new to its config is
// already live. Update adopts it (one copy, config TTL), a later TTL change is
// an in-place update, and removing it from config deletes it.
func TestAccMockMergeAdoption_UpdateAdoptsLiveRecord(t *testing.T) {
	m := newNamecheapMock(t)
	// Without @: the first step is a plain create, so the adoption below goes
	// through update.
	m.seed(mockAdoptionDomain, mockAdoptionZone()[1:], "NONE", nil)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: mockAdoptionConfig(0),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mockAdoptionResource, "record.#", "1"),
					mockCheckHostCount(m, mockAdoptionDomain, 4),
				),
			},
			{
				// The hand-made api 3.3.3.3 is added to config.
				Config: mockAdoptionConfig(300),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mockAdoptionResource, "record.#", "2"),
					mockCheckHostCount(m, mockAdoptionDomain, 4),
					mockCheckSingleHost(m, mockAdoptionDomain, "api", "A", "3.3.3.3", 300),
					mockCheckUndeclaredKept(m),
				),
			},
			{
				Config: mockAdoptionConfig(60),
				Check: resource.ComposeTestCheckFunc(
					mockCheckHostCount(m, mockAdoptionDomain, 4),
					mockCheckSingleHost(m, mockAdoptionDomain, "api", "A", "3.3.3.3", 60),
				),
			},
			{
				Config: mockAdoptionConfig(0),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mockAdoptionResource, "record.#", "1"),
					mockCheckHostCount(m, mockAdoptionDomain, 3),
					mockCheckHostAbsent(m, mockAdoptionDomain, "api", "A", "3.3.3.3"),
					mockCheckUndeclaredKept(m),
				),
			},
		},
	})
}

// A zone already holding two copies of a declared record, as the old update
// left it, ends with one.
func TestAccMockMergeAdoption_CollapsesLiveDuplicates(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(mockAdoptionDomain, []hostEntry{
		{Name: "@", Type: "A", Address: "1.1.1.1", MXPref: 10, TTL: 1800},
		{Name: "api", Type: "A", Address: "3.3.3.3", MXPref: 10, TTL: 1800},
		{Name: "api", Type: "A", Address: "3.3.3.3", MXPref: 10, TTL: 1800},
	}, "NONE", nil)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: mockAdoptionConfig(300),
				Check: resource.ComposeTestCheckFunc(
					mockCheckHostCount(m, mockAdoptionDomain, 2),
					mockCheckSingleHost(m, mockAdoptionDomain, "api", "A", "3.3.3.3", 300),
				),
			},
		},
	})
}
