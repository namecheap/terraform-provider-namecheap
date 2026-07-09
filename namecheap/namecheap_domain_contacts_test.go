package namecheap_provider

import (
	"reflect"
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// fullContactMap is a complete set of block attributes used to build schema
// block values in the expand/flatten tests.
func fullContactMap() map[string]interface{} {
	return map[string]interface{}{
		"first_name":     "Jane",
		"last_name":      "Doe",
		"address1":       "1 Main St",
		"city":           "Lisbon",
		"state_province": "Lisboa",
		"postal_code":    "1000-001",
		"country":        "PT",
		"phone":          "+351.123456789",
		"email_address":  "jane@example.com",
		"organization":   "Example Corp",
		"job_title":      "CEO",
		"address2":       "Suite 5",
	}
}

func fullContactInfo() namecheap.ContactInfo {
	return namecheap.ContactInfo{
		FirstName:        "Jane",
		LastName:         "Doe",
		Address1:         "1 Main St",
		City:             "Lisbon",
		StateProvince:    "Lisboa",
		PostalCode:       "1000-001",
		Country:          "PT",
		Phone:            "+351.123456789",
		EmailAddress:     "jane@example.com",
		OrganizationName: "Example Corp",
		JobTitle:         "CEO",
		Address2:         "Suite 5",
	}
}

func TestExpandContactBlock(t *testing.T) {
	got := expandContactBlock([]interface{}{fullContactMap()})
	assert.Equal(t, fullContactInfo(), got)
}

func TestExpandContactBlockEmpty(t *testing.T) {
	assert.Equal(t, namecheap.ContactInfo{}, expandContactBlock([]interface{}{}))
	assert.Equal(t, namecheap.ContactInfo{}, expandContactBlock(nil))
	assert.True(t, contactIsEmpty([]interface{}{}))
	assert.True(t, contactIsEmpty(nil))
	assert.False(t, contactIsEmpty([]interface{}{fullContactMap()}))
}

func TestFlattenContactInfo(t *testing.T) {
	assert.Equal(t, []interface{}{}, flattenContactInfo(nil))

	c := fullContactInfo()
	got := flattenContactInfo(&c)
	assert.Equal(t, []interface{}{fullContactMap()}, got)
}

func TestExpandFlattenRoundTrip(t *testing.T) {
	c := fullContactInfo()
	flattened := flattenContactInfo(&c)
	assert.Equal(t, c, expandContactBlock(flattened))
}

func TestContactOrDefault(t *testing.T) {
	registrant := fullContactInfo()

	// Omitted block falls back to the registrant.
	assert.Equal(t, registrant, contactOrDefault([]interface{}{}, registrant))
	assert.Equal(t, registrant, contactOrDefault(nil, registrant))

	// Present block is used verbatim.
	tech := fullContactInfo()
	tech.FirstName = "Tech"
	techBlock := flattenContactInfo(&tech)
	assert.Equal(t, tech, contactOrDefault(techBlock, registrant))
}

// TestContactSchemaCompleteness enumerates every field on the SDK ContactInfo
// struct and asserts that the resource maps each one to a schema attribute (via
// contactSchemaFields), and that every mapped attribute exists in the block
// schema. If a future SDK release adds a ContactInfo field, this test fails
// loudly rather than the provider silently dropping the new data.
func TestContactSchemaCompleteness(t *testing.T) {
	mapped := map[string]string{} // struct field -> attr
	for _, f := range contactSchemaFields {
		mapped[f.structField] = f.attr
	}

	block := contactBlockSchema()

	structType := reflect.TypeOf(namecheap.ContactInfo{})
	assert.Equal(t, structType.NumField(), len(contactSchemaFields),
		"contactSchemaFields count must match namecheap.ContactInfo field count; an SDK field was added or removed")

	for i := 0; i < structType.NumField(); i++ {
		name := structType.Field(i).Name
		attr, ok := mapped[name]
		assert.Truef(t, ok, "namecheap.ContactInfo field %q is not mapped in contactSchemaFields", name)
		if ok {
			_, inSchema := block[attr]
			assert.Truef(t, inSchema, "mapped attribute %q (for ContactInfo.%s) is missing from the block schema", attr, name)
		}
	}

	// Every schema attribute must trace back to a struct field (no stray attrs).
	assert.Equal(t, len(contactSchemaFields), len(block), "block schema attribute count must match contactSchemaFields")
}

func TestContactBlockRequiredFields(t *testing.T) {
	block := contactBlockSchema()
	required := []string{"first_name", "last_name", "address1", "city", "state_province", "postal_code", "country", "phone", "email_address"}
	optional := []string{"organization", "job_title", "address2"}

	for _, attr := range required {
		s, ok := block[attr]
		assert.Truef(t, ok, "missing required attr %q", attr)
		assert.Truef(t, s.Required, "attr %q must be Required", attr)
	}
	for _, attr := range optional {
		s, ok := block[attr]
		assert.Truef(t, ok, "missing optional attr %q", attr)
		assert.Truef(t, s.Optional, "attr %q must be Optional", attr)
	}
}

func TestContactFieldValidators(t *testing.T) {
	block := contactBlockSchema()

	cases := []struct {
		attr    string
		value   string
		wantErr bool
	}{
		{"phone", "+1.6613102107", false},
		{"phone", "+351.123456789", false},
		{"phone", "16613102107", true},
		{"phone", "+1-6613102107", true},
		{"phone", "", true},
		{"country", "US", false},
		{"country", "pt", false},
		{"country", "USA", true},
		{"country", "1", true},
		{"email_address", "jane@example.com", false},
		{"email_address", "not-an-email", true},
		{"email_address", "jane@example", true},
	}

	for _, tc := range cases {
		s := block[tc.attr]
		if s.ValidateFunc == nil {
			t.Fatalf("attr %q has no ValidateFunc", tc.attr)
		}
		_, errs := s.ValidateFunc(tc.value, tc.attr)
		if tc.wantErr {
			assert.NotEmptyf(t, errs, "expected validation error for %s=%q", tc.attr, tc.value)
		} else {
			assert.Emptyf(t, errs, "unexpected validation error for %s=%q: %v", tc.attr, tc.value, errs)
		}
	}
}

func TestDomainContactsResourceSchemaValid(t *testing.T) {
	assert.NoError(t, resourceNamecheapDomainContacts().InternalValidate(nil, true))
}
