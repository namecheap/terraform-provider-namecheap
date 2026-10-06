//go:build testacc

package namecheap_provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// This file covers #355: `terraform import` of a namecheap_domain_records
// resource puts every live record (and nameserver) into state, so the first
// MERGE apply - or a destroy - treated records the configuration never declared
// as Terraform-owned and deleted them at Namecheap. Import now marks the
// resource `adopted`; the first apply settles ownership without deleting
// anything Terraform never declared, and a destroy before that apply deletes
// nothing.

const (
	adoptionDomain   = "mock-example.com"
	adoptionResource = "namecheap_domain_records.test"
)

func adoptionRecordsConfig(mode string, records ...string) string {
	body := ""
	for _, r := range records {
		body += r
	}
	return fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain = "%s"
  mode   = "%s"
%s}
`, adoptionDomain, mode, body)
}

func adoptionRecord(host, address string) string {
	return fmt.Sprintf(`
  record {
    hostname = "%s"
    type     = "A"
    address  = "%s"
    ttl      = 1800
  }
`, host, address)
}

func adoptionImportStep(config string) resource.TestStep {
	return resource.TestStep{
		Config:             config,
		ResourceName:       adoptionResource,
		ImportState:        true,
		ImportStateId:      adoptionDomain,
		ImportStatePersist: true,
	}
}

// The #355 reproduction: www is declared, home (owned by something else, e.g. a
// DDNS updater) is not. The first apply after import must leave home alone.
func TestAccMockImportAdoption_MergeFirstApplyKeepsUndeclared(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(adoptionDomain, []hostEntry{
		{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
		{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
	}, "NONE", nil)

	config := adoptionRecordsConfig("MERGE", adoptionRecord("www", "203.0.113.10"))

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		// Destroy removes only what the configuration declared.
		CheckDestroy: resource.ComposeTestCheckFunc(
			mockCheckHostCount(m, adoptionDomain, 1),
			mockCheckHostContains(m, adoptionDomain, "home", "A", "203.0.113.20"),
		),
		Steps: []resource.TestStep{
			adoptionImportStep(config),
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					mockCheckHostCount(m, adoptionDomain, 2),
					mockCheckHostContains(m, adoptionDomain, "home", "A", "203.0.113.20"),
					mockCheckHostContains(m, adoptionDomain, "www", "A", "203.0.113.10"),
					resource.TestCheckResourceAttr(adoptionResource, "record.#", "1"),
					resource.TestCheckResourceAttr(adoptionResource, "adopted", "false"),
				),
			},
			// Settled: no perpetual diff afterwards.
			{Config: config, PlanOnly: true},
		},
	})
}

// A destroy right after import - before any apply - used to wipe the whole zone.
func TestAccMockImportAdoption_DestroyRightAfterImportDeletesNothing(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(adoptionDomain, []hostEntry{
		{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
		{Name: "home", Type: "A", Address: "203.0.113.20", MXPref: 10, TTL: 1800},
	}, "NONE", nil)

	config := adoptionRecordsConfig("MERGE", adoptionRecord("www", "203.0.113.10"))

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy: resource.ComposeTestCheckFunc(
			mockCheckHostCount(m, adoptionDomain, 2),
			mockCheckHostContains(m, adoptionDomain, "www", "A", "203.0.113.10"),
			mockCheckHostContains(m, adoptionDomain, "home", "A", "203.0.113.20"),
		),
		Steps: []resource.TestStep{
			adoptionImportStep(config),
		},
	})
}

// A clean import (configuration matches the zone) must still be settled by one
// apply, otherwise a record removed from the configuration much later would be
// mistaken for an imported-but-undeclared one and silently left at Namecheap.
func TestAccMockImportAdoption_CleanImportThenRemovalStillDeletes(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(adoptionDomain, []hostEntry{
		{Name: "www", Type: "A", Address: "203.0.113.10", MXPref: 10, TTL: 1800},
		{Name: "old", Type: "A", Address: "203.0.113.30", MXPref: 10, TTL: 1800},
	}, "NONE", nil)

	both := adoptionRecordsConfig("MERGE",
		adoptionRecord("www", "203.0.113.10"),
		adoptionRecord("old", "203.0.113.30"))
	withoutOld := adoptionRecordsConfig("MERGE", adoptionRecord("www", "203.0.113.10"))

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckHostCount(m, adoptionDomain, 0),
		Steps: []resource.TestStep{
			adoptionImportStep(both),
			{
				Config: both,
				Check: resource.ComposeTestCheckFunc(
					mockCheckHostCount(m, adoptionDomain, 2),
					resource.TestCheckResourceAttr(adoptionResource, "adopted", "false"),
				),
			},
			{
				// Declared, applied, then removed: this is a real removal.
				Config: withoutOld,
				Check: resource.ComposeTestCheckFunc(
					mockCheckHostCount(m, adoptionDomain, 1),
					mockCheckHostContains(m, adoptionDomain, "www", "A", "203.0.113.10"),
				),
			},
		},
	})
}

// Imported custom nameservers go through the same removal logic: declaring a
// subset must not change the domain's delegation on the first apply.
func TestAccMockImportAdoption_MergeNameserversKeepsUndeclared(t *testing.T) {
	m := newNamecheapMock(t)
	m.seed(adoptionDomain, nil, "", []string{"ns1.example.net", "ns2.example.net", "ns3.example.net", "ns4.example.net"})

	config := fmt.Sprintf(`
resource "namecheap_domain_records" "test" {
  domain      = "%s"
  mode        = "MERGE"
  nameservers = ["ns1.example.net", "ns2.example.net"]
}
`, adoptionDomain)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		// Destroy releases only what the configuration declared.
		CheckDestroy: mockCheckNameservers(m, adoptionDomain, "ns3.example.net", "ns4.example.net"),
		Steps: []resource.TestStep{
			adoptionImportStep(config),
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					mockCheckNameservers(m, adoptionDomain, "ns1.example.net", "ns2.example.net", "ns3.example.net", "ns4.example.net"),
					resource.TestCheckResourceAttr(adoptionResource, "nameservers.#", "2"),
					resource.TestCheckResourceAttr(adoptionResource, "adopted", "false"),
				),
			},
		},
	})
}

// Compatibility guard for everybody who never imports: the new attribute is
// never written to their state, it does not show up in the plan of an existing
// resource, and ordinary removals keep deleting. (A create plan necessarily
// lists any new computed attribute as known-after-apply; that is SDK behaviour.)
func TestAccMockImportAdoption_NonImportedResourcesUnchanged(t *testing.T) {
	m := newNamecheapMock(t)

	first := adoptionRecordsConfig("MERGE",
		adoptionRecord("www", "203.0.113.10"),
		adoptionRecord("api", "203.0.113.11"))
	second := adoptionRecordsConfig("MERGE", adoptionRecord("www", "203.0.113.10"))

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckHostCount(m, adoptionDomain, 0),
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeTestCheckFunc(
					mockCheckHostCount(m, adoptionDomain, 2),
					resource.TestCheckNoResourceAttr(adoptionResource, "adopted"),
				),
			},
			{
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{planHasNoUnknownAttribute{"adopted"}},
				},
				Check: resource.ComposeTestCheckFunc(
					mockCheckHostCount(m, adoptionDomain, 1),
					mockCheckHostContains(m, adoptionDomain, "www", "A", "203.0.113.10"),
					resource.TestCheckNoResourceAttr(adoptionResource, "adopted"),
				),
			},
		},
	})
}

// planHasNoUnknownAttribute fails when any planned change leaves the named
// attribute unknown ("known after apply").
type planHasNoUnknownAttribute struct{ attribute string }

func (p planHasNoUnknownAttribute) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	for _, rc := range req.Plan.ResourceChanges {
		if rc.Change == nil {
			continue
		}
		unknown, ok := rc.Change.AfterUnknown.(map[string]interface{})
		if !ok {
			continue
		}
		if v, present := unknown[p.attribute]; present && v == true {
			resp.Error = fmt.Errorf("%s: attribute %q is planned as known-after-apply", rc.Address, p.attribute)
			return
		}
	}
}
