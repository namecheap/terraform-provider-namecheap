//go:build testacc

package namecheap_provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const mockContactsDomain = "mock-contacts-example.com"

// mockCheckContactField asserts the mock persisted the given value for a
// contact field (e.g. block "Registrant", field "EmailAddress") — proving the
// setContacts payload reached the backend end-to-end through the provider.
func mockCheckContactField(m *namecheapMock, domain, block, field, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return fmt.Errorf("mock has no state for %q", domain)
		}
		got := st.contacts[block][field]
		if got != want {
			return fmt.Errorf("mock contact %s.%s for %q = %q, want %q", block, field, domain, got, want)
		}
		return nil
	}
}

// registrantOnlyConfig is a config that sets only the registrant block, using
// the given email so update steps can vary it.
func registrantOnlyConfig(domain, email string) string {
	return fmt.Sprintf(`
resource "namecheap_domain_contacts" "test" {
  domain = "%s"

  registrant {
    first_name     = "Jane"
    last_name      = "Doe"
    address1       = "1 Main St"
    city           = "Lisbon"
    state_province = "Lisboa"
    postal_code    = "1000-001"
    country        = "PT"
    phone          = "+351.123456789"
    email_address  = "%s"
    organization   = "Example Corp"
  }
}
`, domain, email)
}

// TestAccMockDomainContactsLifecycle drives create -> update -> import -> destroy
// with an explicit tech block distinct from the registrant, asserting both
// Terraform state and the mock backend at each step.
func TestAccMockDomainContactsLifecycle(t *testing.T) {
	m := newNamecheapMock(t)
	const resourceName = "namecheap_domain_contacts.test"

	configWithTech := func(regEmail string) string {
		return fmt.Sprintf(`
resource "namecheap_domain_contacts" "test" {
  domain = "%s"

  registrant {
    first_name     = "Jane"
    last_name      = "Doe"
    address1       = "1 Main St"
    city           = "Lisbon"
    state_province = "Lisboa"
    postal_code    = "1000-001"
    country        = "PT"
    phone          = "+351.123456789"
    email_address  = "%s"
  }

  tech {
    first_name     = "Tim"
    last_name      = "Tech"
    address1       = "2 Second St"
    city           = "Porto"
    state_province = "Porto"
    postal_code    = "4000-002"
    country        = "PT"
    phone          = "+351.987654321"
    email_address  = "tech@example.com"
  }
}
`, mockContactsDomain, regEmail)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: configWithTech("jane@example.com"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "registrant.0.first_name", "Jane"),
					resource.TestCheckResourceAttr(resourceName, "registrant.0.email_address", "jane@example.com"),
					resource.TestCheckResourceAttr(resourceName, "tech.0.first_name", "Tim"),
					mockCheckContactField(m, mockContactsDomain, "Registrant", "EmailAddress", "jane@example.com"),
					mockCheckContactField(m, mockContactsDomain, "Tech", "FirstName", "Tim"),
					// Admin/AuxBilling were omitted, so they default to the registrant.
					mockCheckContactField(m, mockContactsDomain, "Admin", "FirstName", "Jane"),
					mockCheckContactField(m, mockContactsDomain, "AuxBilling", "EmailAddress", "jane@example.com"),
				),
			},
			{
				// Update the registrant email; the setContacts call must carry
				// the new value and drop no other field.
				Config: configWithTech("jane.doe@example.com"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "registrant.0.email_address", "jane.doe@example.com"),
					mockCheckContactField(m, mockContactsDomain, "Registrant", "EmailAddress", "jane.doe@example.com"),
					mockCheckContactField(m, mockContactsDomain, "Tech", "FirstName", "Tim"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateId:     mockContactsDomain,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccMockDomainContactsDefaultsToRegistrant proves the default-to-registrant
// behavior is visible in the plan (materialized state) and in the API payload:
// with only a registrant block configured, all three optional blocks are set to
// the registrant's values, and a re-plan is empty (the defaults are stable).
func TestAccMockDomainContactsDefaultsToRegistrant(t *testing.T) {
	m := newNamecheapMock(t)
	const resourceName = "namecheap_domain_contacts.test"

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: registrantOnlyConfig(mockContactsDomain, "jane@example.com"),
				Check: resource.ComposeTestCheckFunc(
					// Plan rendering: the omitted blocks carry the registrant values.
					resource.TestCheckResourceAttr(resourceName, "tech.0.first_name", "Jane"),
					resource.TestCheckResourceAttr(resourceName, "tech.0.email_address", "jane@example.com"),
					resource.TestCheckResourceAttr(resourceName, "admin.0.phone", "+351.123456789"),
					resource.TestCheckResourceAttr(resourceName, "aux_billing.0.country", "PT"),
					// API payload: all four blocks were sent with the registrant values.
					mockCheckContactField(m, mockContactsDomain, "Tech", "FirstName", "Jane"),
					mockCheckContactField(m, mockContactsDomain, "Admin", "FirstName", "Jane"),
					mockCheckContactField(m, mockContactsDomain, "AuxBilling", "FirstName", "Jane"),
				),
			},
			{
				// The materialized defaults must be stable: a re-plan of the same
				// config produces no diff.
				Config:   registrantOnlyConfig(mockContactsDomain, "jane@example.com"),
				PlanOnly: true,
			},
		},
	})
}

// TestAccMockDomainContactsDrift covers drift detection: a dashboard-side change
// to a contact field must surface as a non-empty plan on the next refresh.
func TestAccMockDomainContactsDrift(t *testing.T) {
	m := newNamecheapMock(t)
	config := registrantOnlyConfig(mockContactsDomain, "jane@example.com")

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				// Simulate an out-of-band change to the registrant email, then
				// plan the unchanged config: refresh must observe the drift.
				PreConfig: func() {
					st := m.state(mockContactsDomain)
					st.contacts["Registrant"]["EmailAddress"] = "hacked@evil.example"
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccMockDomainContactsImport seeds pre-existing contacts (a portfolio
// domain not yet Terraform-managed) and imports them by domain name.
func TestAccMockDomainContactsImport(t *testing.T) {
	m := newNamecheapMock(t)
	const resourceName = "namecheap_domain_contacts.test"

	m.seedContacts(mockContactsDomain, map[string]map[string]string{
		"Registrant": fullContactWire(),
		"Tech":       fullContactWire(),
		"Admin":      fullContactWire(),
		"AuxBilling": fullContactWire(),
	})

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:        registrantOnlyConfig(mockContactsDomain, "jane@example.com"),
				ResourceName:  resourceName,
				ImportState:   true,
				ImportStateId: mockContactsDomain,
				// Verify the imported Terraform state was seeded from getContacts:
				// the registrant and every optional block are populated.
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported state, got %d", len(states))
					}
					attrs := states[0].Attributes
					for key, want := range map[string]string{
						"registrant.0.first_name":    "Jane",
						"registrant.0.email_address": "jane@example.com",
						"registrant.0.organization":  "Example Corp",
						"tech.0.first_name":          "Jane",
						"admin.0.country":            "PT",
						"aux_billing.0.phone":        "+351.123456789",
					} {
						if attrs[key] != want {
							return fmt.Errorf("imported state %q = %q, want %q", key, attrs[key], want)
						}
					}
					return nil
				},
			},
		},
	})
}

// fullContactWire is the wire (Namecheap field name) form of a complete contact,
// used to seed the mock for import tests.
func fullContactWire() map[string]string {
	return map[string]string{
		"FirstName":        "Jane",
		"LastName":         "Doe",
		"Address1":         "1 Main St",
		"City":             "Lisbon",
		"StateProvince":    "Lisboa",
		"PostalCode":       "1000-001",
		"Country":          "PT",
		"Phone":            "+351.123456789",
		"EmailAddress":     "jane@example.com",
		"OrganizationName": "Example Corp",
	}
}
