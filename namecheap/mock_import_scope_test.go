//go:build testacc

package namecheap_provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// TestAccMockDomainRecordsImportScope covers the two import IDs of
// namecheap_domain_records.
//
// The zone is the one from GitHub #355: a Google Workspace domain whose "www"
// row is managed with Terraform and whose "home" row belongs to a dynamic DNS
// updater. An import used to store both rows, and the first apply deleted
// "home" because the configuration did not declare it.
func TestAccMockDomainRecordsImportScope(t *testing.T) {
	const domain = hostRecordTestDomain
	const recordsResource = "namecheap_domain_records.mail"
	const hostResource = "namecheap_domain_host_record.www"
	const zoneID = domain + "/zone"

	seed := func(m *namecheapMock) {
		// getHosts reports MXPref="10" on every row, MX or not.
		m.seed(domain, []hostEntry{
			{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
			{Name: "home", Type: "A", Address: "198.51.100.7", MXPref: 10, TTL: 60},
		}, "GMAIL", nil)
	}

	const wwwBlock = `
  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
`
	const homeBlock = `
  record {
    hostname = "home"
    type     = "A"
    address  = "198.51.100.7"
    ttl      = 60
  }
`
	// records is a MERGE namecheap_domain_records with the given record blocks.
	records := func(blocks string) string {
		return fmt.Sprintf(`
resource "namecheap_domain_records" "mail" {
  domain     = %q
  email_type = "GMAIL"
%s}
`, domain, blocks)
	}
	hostRecord := func(address string) string {
		return fmt.Sprintf(`
resource "namecheap_domain_host_record" "www" {
  domain   = %q
  hostname = "www"
  type     = "A"
  address  = %q
}
`, domain, address)
	}

	// importStores checks how many records the import put in state. The plan
	// after the import cannot tell the two IDs apart on its own: a declared
	// record that is missing from state makes it non-empty too.
	importStores := func(want int) resource.ImportStateCheckFunc {
		return func(states []*terraform.InstanceState) error {
			if len(states) != 1 {
				return fmt.Errorf("import returned %d resources, want 1", len(states))
			}
			got := states[0].Attributes["record.#"]
			if got == "" {
				got = "0"
			}
			if got != fmt.Sprint(want) {
				return fmt.Errorf("imported record.# = %s, want %d", got, want)
			}
			return nil
		}
	}

	// The walkthrough of the #355 reporter: a mail-only namecheap_domain_records
	// next to one namecheap_domain_host_record per owned row. "home" never enters
	// state, so nothing Terraform does can delete it.
	t.Run("plain ID with a mail-only config next to host_record", func(t *testing.T) {
		m := newNamecheapMock(t)
		config := func(address string) string { return records("") + hostRecord(address) }

		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m) },
			ProviderFactories: mockProviderFactories(),
			// Destroy removes the row host_record owns and nothing else.
			CheckDestroy: resource.ComposeTestCheckFunc(
				assertZoneLacks(m, "www", "A", "203.0.113.20"),
				assertZoneHas(m, "home", "A", "198.51.100.7"),
				mockCheckHostCount(m, domain, 1),
				mockCheckEmailType(m, domain, "GMAIL"),
			),
			Steps: []resource.TestStep{
				{
					Config:             config("203.0.113.10"),
					ResourceName:       recordsResource,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
				},
				{
					Config:             config("203.0.113.10"),
					ResourceName:       hostResource,
					ImportState:        true,
					ImportStateId:      domain + "/A/www/203.0.113.10",
					ImportStatePersist: true,
				},
				{
					// "No changes" right after the two imports.
					Config:   config("203.0.113.10"),
					PlanOnly: true,
				},
				{
					Config: config("203.0.113.10"),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "record.#", "0"),
						resource.TestCheckResourceAttr(recordsResource, "email_type", "GMAIL"),
						resource.TestCheckResourceAttr(recordsResource, "mode", "MERGE"),
						mockCheckHostCount(m, domain, 2),
					),
				},
				{
					// A change to the owned row writes "home" and the mail mode
					// back unchanged, and the mail-only resource plans nothing.
					Config: config("203.0.113.20"),
					Check: resource.ComposeTestCheckFunc(
						assertZoneHas(m, "www", "A", "203.0.113.20"),
						assertZoneHas(m, "home", "A", "198.51.100.7"),
						mockCheckHostCount(m, domain, 2),
						mockCheckEmailType(m, domain, "GMAIL"),
						resource.TestCheckResourceAttr(recordsResource, "record.#", "0"),
					),
				},
			},
		})
	})

	// MERGE does not need an import: the same mail-only configuration applied to
	// a domain that was never imported sets the mail mode in one apply and leaves
	// every record alone. The create used to write nothing, so state said GMAIL
	// while the zone stayed on NONE until a second apply.
	t.Run("a mail-only config applied without an import sets the mail mode", func(t *testing.T) {
		m := newNamecheapMock(t)
		untouched := resource.ComposeTestCheckFunc(
			assertZoneHas(m, "www", "A", "203.0.113.10"),
			assertZoneHas(m, "home", "A", "198.51.100.7"),
			mockCheckHostCount(m, domain, 2),
			mockCheckEmailType(m, domain, "GMAIL"),
		)
		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				mockPreCheck(t, m)
				m.seed(domain, []hostEntry{
					{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
					{Name: "home", Type: "A", Address: "198.51.100.7", MXPref: 10, TTL: 60},
				}, "NONE", nil)
			},
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      untouched,
			Steps: []resource.TestStep{
				{
					Config: records(""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "email_type", "GMAIL"),
						resource.TestCheckResourceAttr(recordsResource, "record.#", "0"),
						untouched,
					),
				},
				{Config: records(""), PlanOnly: true},
			},
		})
	})

	// A record the configuration declares is adopted by the first apply; the
	// plan shows it being added to state. The undeclared row is never touched,
	// destroy included.
	t.Run("plain ID with a config that declares one live record", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m) },
			ProviderFactories: mockProviderFactories(),
			CheckDestroy: resource.ComposeTestCheckFunc(
				assertZoneLacks(m, "www", "A", "203.0.113.10"),
				assertZoneHas(m, "home", "A", "198.51.100.7"),
				mockCheckHostCount(m, domain, 1),
			),
			Steps: []resource.TestStep{
				{
					Config:             records(wwwBlock),
					ResourceName:       recordsResource,
					ImportState:        true,
					ImportStateId:      domain,
					ImportStatePersist: true,
					ImportStateCheck:   importStores(0),
				},
				{
					Config: records(wwwBlock),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "record.#", "1"),
						assertZoneCount(m, "www", "A", "203.0.113.10", 1),
						assertZoneHas(m, "home", "A", "198.51.100.7"),
						mockCheckHostCount(m, domain, 2),
						mockCheckEmailType(m, domain, "GMAIL"),
					),
				},
			},
		})
	})

	t.Run("/zone ID with a config that declares every record", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config:             records(wwwBlock + homeBlock),
					ResourceName:       recordsResource,
					ImportState:        true,
					ImportStateId:      zoneID,
					ImportStatePersist: true,
					ImportStateCheck:   importStores(2),
				},
				{
					Config:   records(wwwBlock + homeBlock),
					PlanOnly: true,
				},
				{
					Config: records(wwwBlock + homeBlock),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "id", domain),
						resource.TestCheckResourceAttr(recordsResource, "record.#", "2"),
						mockCheckHostCount(m, domain, 2),
					),
				},
				{
					// The resource owns both rows now, so removing every record
					// block deletes both and keeps the mail mode.
					Config: records(""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "record.#", "0"),
						mockCheckHostCount(m, domain, 0),
						mockCheckEmailType(m, domain, "GMAIL"),
					),
				},
			},
		})
	})

	// After a /zone import the resource owns every row, and a row the
	// configuration leaves out is removed like any record block taken out of a
	// MERGE configuration: the plan lists it. The provider cannot tell a row the
	// import brought in from one the author removed on purpose.
	for name, blocks := range map[string]string{"no record": "", "some records": wwwBlock} {
		t.Run("/zone ID with a config that declares "+name+" plans the removal of the rest", func(t *testing.T) {
			m := newNamecheapMock(t)
			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m); seed(m) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:             records(blocks),
						ResourceName:       recordsResource,
						ImportState:        true,
						ImportStateId:      zoneID,
						ImportStatePersist: true,
						ImportStateCheck:   importStores(2),
					},
					{
						Config:             records(blocks),
						PlanOnly:           true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
		})
	}

	// Removing every record block from a resource that keeps the mail mode
	// deletes the records it manages and nothing else. "home" was never
	// declared, so it survives that apply and the destroy after it.
	t.Run("removing every record block deletes the managed records only", func(t *testing.T) {
		m := newNamecheapMock(t)
		const apiBlock = `
  record {
    hostname = "api"
    type     = "A"
    address  = "203.0.113.11"
  }
`
		homeOnly := resource.ComposeTestCheckFunc(
			assertZoneHas(m, "home", "A", "198.51.100.7"),
			mockCheckHostCount(m, domain, 1),
			mockCheckEmailType(m, domain, "GMAIL"),
		)
		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				mockPreCheck(t, m)
				m.seed(domain, []hostEntry{{Name: "home", Type: "A", Address: "198.51.100.7", MXPref: 10, TTL: 60}}, "GMAIL", nil)
			},
			ProviderFactories: mockProviderFactories(),
			CheckDestroy:      homeOnly,
			Steps: []resource.TestStep{
				{
					Config: records(wwwBlock + apiBlock),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "record.#", "2"),
						mockCheckHostCount(m, domain, 3),
					),
				},
				{
					Config: records(""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(recordsResource, "record.#", "0"),
						resource.TestCheckResourceAttr(recordsResource, "email_type", "GMAIL"),
						homeOnly,
					),
				},
			},
		})
	})

	// The case of a domain name carries no meaning. A difference in case between
	// the import ID and the configuration must not plan a replacement, which
	// would delete every record the resource manages and create them again.
	for name, spelling := range map[string]struct{ id, configDomain string }{
		"an upper-case import ID with a lower-case config": {id: strings.ToUpper(domain) + "/zone", configDomain: domain},
		"a lower-case import ID with an upper-case config": {id: zoneID, configDomain: strings.ToUpper(domain)},
	} {
		t.Run(name+" plans no change", func(t *testing.T) {
			m := newNamecheapMock(t)
			config := fmt.Sprintf(`
resource "namecheap_domain_records" "mail" {
  domain     = %q
  email_type = "GMAIL"
%s}
`, spelling.configDomain, wwwBlock+homeBlock)

			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m); seed(m) },
				ProviderFactories: mockProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:             config,
						ResourceName:       recordsResource,
						ImportState:        true,
						ImportStateId:      spelling.id,
						ImportStatePersist: true,
					},
					{
						Config:   config,
						PlanOnly: true,
					},
					{
						Config: config,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(recordsResource, "id", domain),
							resource.TestCheckResourceAttr(recordsResource, "domain", domain),
							mockCheckHostCount(m, domain, 2),
						),
					},
				},
			})
		})
	}

	t.Run("an unknown scope is refused", func(t *testing.T) {
		m := newNamecheapMock(t)
		resource.Test(t, resource.TestCase{
			PreCheck:          func() { mockPreCheck(t, m); seed(m) },
			ProviderFactories: mockProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config:        records(""),
					ResourceName:  recordsResource,
					ImportState:   true,
					ImportStateId: domain + "/records",
					ExpectError:   regexp.MustCompile(`unexpected import ID`),
				},
			},
		})
	})
}

// TestAccMockDomainRecordsMoveToHostRecord is the recipe of the importing guide
// for moving a record out of a record block into namecheap_domain_host_record.
// Removing the block would delete the record, so the old resource is forgotten
// with a removed block and the domain is imported again, without records, under
// a new name. Nothing in the zone changes, the row nobody declared included.
func TestAccMockDomainRecordsMoveToHostRecord(t *testing.T) {
	const domain = hostRecordTestDomain

	const apiBlock = `
  record {
    hostname = "api"
    type     = "A"
    address  = "203.0.113.11"
  }
`
	before := fmt.Sprintf(`
resource "namecheap_domain_records" "all" {
  domain     = %q
  email_type = "GMAIL"

  record {
    hostname = "www"
    type     = "A"
    address  = "203.0.113.10"
  }
%s}
`, domain, apiBlock)

	// after is the configuration once "www" has moved; "api" stays in a record
	// block. move adds the blocks that are only needed for the apply that moves it.
	after := func(wwwAddress string) string {
		return fmt.Sprintf(`
resource "namecheap_domain_records" "settings" {
  domain     = %[1]q
  email_type = "GMAIL"
%[2]s}

resource "namecheap_domain_host_record" "www" {
  domain   = %[1]q
  hostname = "www"
  type     = "A"
  address  = %[3]q
}
`, domain, apiBlock, wwwAddress)
	}
	move := fmt.Sprintf(`
removed {
  from = namecheap_domain_records.all

  lifecycle {
    destroy = false
  }
}

import {
  to = namecheap_domain_records.settings
  id = %[1]q
}

import {
  to = namecheap_domain_host_record.www
  id = "%[1]s/A/www/203.0.113.10"
}
`, domain)

	m := newNamecheapMock(t)
	zoneIntact := func(wwwAddress string) resource.TestCheckFunc {
		return resource.ComposeTestCheckFunc(
			assertZoneCount(m, "www", "A", wwwAddress, 1),
			assertZoneCount(m, "api", "A", "203.0.113.11", 1),
			assertZoneCount(m, "home", "A", "198.51.100.7", 1),
			mockCheckHostCount(m, domain, 3),
			mockCheckEmailType(m, domain, "GMAIL"),
		)
	}

	resource.Test(t, resource.TestCase{
		// The removed block is Terraform 1.7.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_7_0),
		},
		PreCheck: func() {
			mockPreCheck(t, m)
			m.seed(domain, []hostEntry{{Name: "home", Type: "A", Address: "198.51.100.7", MXPref: 10, TTL: 60}}, "GMAIL", nil)
		},
		ProviderFactories: mockProviderFactories(),
		// Destroy removes the two rows the resources own and keeps "home".
		CheckDestroy: resource.ComposeTestCheckFunc(
			assertZoneHas(m, "home", "A", "198.51.100.7"),
			mockCheckHostCount(m, domain, 1),
		),
		Steps: []resource.TestStep{
			{
				Config: before,
				Check:  zoneIntact("203.0.113.10"),
			},
			{
				Config: after("203.0.113.10") + move,
				Check: resource.ComposeTestCheckFunc(
					zoneIntact("203.0.113.10"),
					resource.TestCheckResourceAttr("namecheap_domain_records.settings", "record.#", "1"),
					resource.TestCheckResourceAttr("namecheap_domain_host_record.www", "address", "203.0.113.10"),
				),
			},
			{
				// The removed and import blocks have done their job.
				Config:   after("203.0.113.10"),
				PlanOnly: true,
			},
			{
				Config: after("203.0.113.20"),
				Check:  zoneIntact("203.0.113.20"),
			},
		},
	})
}
