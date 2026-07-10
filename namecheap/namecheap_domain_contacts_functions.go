package namecheap_provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// contactSchemaField pairs a Namecheap ContactInfo struct field name with the
// Terraform schema attribute it is exposed as. The list is the single source of
// truth used by the schema builder, the expand/flatten helpers and the
// completeness test (which reflects over ContactInfo and asserts every field
// appears here), so a future SDK addition surfaces loudly rather than silently
// dropping data.
type contactSchemaField struct {
	structField string // field name on namecheap.ContactInfo
	attr        string // Terraform schema attribute name
	required    bool
	description string
}

// contactSchemaFields enumerates every namecheap.ContactInfo field and its
// mapping. Order matches ContactInfo: nine required fields, then three optional.
var contactSchemaFields = []contactSchemaField{
	{"FirstName", "first_name", true, "The contact's first name."},
	{"LastName", "last_name", true, "The contact's last name."},
	{"Address1", "address1", true, "The primary street address."},
	{"City", "city", true, "The contact's city."},
	{"StateProvince", "state_province", true, "The state or province."},
	{"PostalCode", "postal_code", true, "The postal/ZIP code."},
	{"Country", "country", true, "The two-letter ISO 3166-1 alpha-2 country code (e.g. US, PT)."},
	{"Phone", "phone", true, "The phone number in +NNN.NNNNNNNNNN format (e.g. +1.6613102107)."},
	{"EmailAddress", "email_address", true, "The contact e-mail address."},
	{"OrganizationName", "organization", false, "The contact's organization."},
	{"JobTitle", "job_title", false, "The contact's job title."},
	{"Address2", "address2", false, "The secondary street address."},
}

var (
	// contactPhoneRegexp matches Namecheap's documented +NNN.NNNNNNN phone
	// format: a leading "+", a country-code digit group, a ".", then the number.
	contactPhoneRegexp = regexp.MustCompile(`^\+[0-9]+\.[0-9]+$`)
	// contactCountryRegexp matches a two-letter ISO 3166-1 alpha-2 country code.
	contactCountryRegexp = regexp.MustCompile(`^[A-Za-z]{2}$`)
	// contactEmailRegexp is a pragmatic e-mail syntax check applied at plan time
	// to catch obvious typos before the slow server-side rejection.
	contactEmailRegexp = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// contactBlockSchema builds the per-block nested schema shared by the
// registrant/tech/admin/aux_billing blocks from contactSchemaFields, attaching
// plan-time validators to the fields the API constrains.
func contactBlockSchema() map[string]*schema.Schema {
	m := make(map[string]*schema.Schema, len(contactSchemaFields))
	for _, f := range contactSchemaFields {
		s := &schema.Schema{
			Type:        schema.TypeString,
			Description: f.description,
		}
		if f.required {
			s.Required = true
		} else {
			s.Optional = true
		}
		switch f.attr {
		case "phone":
			s.ValidateFunc = validation.StringMatch(contactPhoneRegexp,
				"must be in international format +NNN.NNNNNNNNNN (e.g. +1.6613102107)")
		case "country":
			s.ValidateFunc = validation.StringMatch(contactCountryRegexp,
				"must be a two-letter ISO 3166-1 alpha-2 country code (e.g. US, PT)")
		case "email_address":
			s.ValidateFunc = validation.StringMatch(contactEmailRegexp,
				"must be a valid e-mail address")
		}
		m[f.attr] = s
	}
	return m
}

// expandContactBlock converts a schema block value (a one-element TypeList of a
// map) into a namecheap.ContactInfo. A nil/empty block yields the zero value.
func expandContactBlock(raw interface{}) namecheap.ContactInfo {
	list, ok := raw.([]interface{})
	if !ok || len(list) == 0 || list[0] == nil {
		return namecheap.ContactInfo{}
	}
	m := list[0].(map[string]interface{})
	getString := func(key string) string {
		if v, ok := m[key]; ok && v != nil {
			return v.(string)
		}
		return ""
	}
	return namecheap.ContactInfo{
		FirstName:        getString("first_name"),
		LastName:         getString("last_name"),
		Address1:         getString("address1"),
		City:             getString("city"),
		StateProvince:    getString("state_province"),
		PostalCode:       getString("postal_code"),
		Country:          getString("country"),
		Phone:            getString("phone"),
		EmailAddress:     getString("email_address"),
		OrganizationName: getString("organization"),
		JobTitle:         getString("job_title"),
		Address2:         getString("address2"),
	}
}

// contactIsEmpty reports whether a schema block value carries no contact (an
// omitted optional block).
func contactIsEmpty(raw interface{}) bool {
	list, ok := raw.([]interface{})
	return !ok || len(list) == 0 || list[0] == nil
}

// contactOrDefault returns the expanded block, or the registrant fallback when
// the block is omitted — the default-to-registrant rule.
func contactOrDefault(raw interface{}, fallback namecheap.ContactInfo) namecheap.ContactInfo {
	if contactIsEmpty(raw) {
		return fallback
	}
	return expandContactBlock(raw)
}

// flattenContactInfo converts a namecheap.ContactInfo from a getContacts
// response into the one-element list a TypeList block expects. A nil contact
// yields an empty list.
func flattenContactInfo(c *namecheap.ContactInfo) []interface{} {
	if c == nil {
		return []interface{}{}
	}
	return []interface{}{map[string]interface{}{
		"first_name":     c.FirstName,
		"last_name":      c.LastName,
		"address1":       c.Address1,
		"city":           c.City,
		"state_province": c.StateProvince,
		"postal_code":    c.PostalCode,
		"country":        c.Country,
		"phone":          c.Phone,
		"email_address":  c.EmailAddress,
		"organization":   c.OrganizationName,
		"job_title":      c.JobTitle,
		"address2":       c.Address2,
	}}
}

// customizeContactsDiff makes the default-to-registrant behavior explicit in the
// plan: for each optional block the user did not set, it plans the registrant's
// values rather than leaving the (Computed) block unknown. Reading intent from
// the raw config lets it distinguish "user omitted the block" from "user set it
// equal to the registrant".
func customizeContactsDiff(_ context.Context, diff *schema.ResourceDiff, _ interface{}) error {
	rawConfig := diff.GetRawConfig()
	if rawConfig.IsNull() {
		return nil
	}

	// The registrant (or one of its fields) may be interpolated from another
	// resource and not yet known at plan time. When it is unknown, the optional
	// blocks that default to it must stay computed rather than being planned
	// with the current (partial/empty) registrant values — otherwise apply-time
	// re-evaluation yields different values and Terraform fails with "Provider
	// produced inconsistent final plan".
	registrantKnown := diff.NewValueKnown("registrant")
	registrant := diff.Get("registrant")
	if registrantKnown && contactIsEmpty(registrant) {
		// Registrant is known and empty: nothing to default from at this stage.
		return nil
	}

	for _, block := range []string{"tech", "admin", "aux_billing"} {
		cfg := rawConfig.GetAttr(block)
		// A block whose config value is unknown (e.g. a dynamic block driven by
		// an unknown for_each) cannot be measured or defaulted yet. Guard
		// LengthInt, which panics on an unknown cty value, and leave it computed.
		if !cfg.IsKnown() {
			if err := diff.SetNewComputed(block); err != nil {
				return err
			}
			continue
		}
		if !cfg.IsNull() && cfg.LengthInt() > 0 {
			// User set the block explicitly; leave it untouched.
			continue
		}
		if !registrantKnown {
			// Defaults to the registrant, whose values are not known yet: keep
			// the block computed instead of baking in empty strings.
			if err := diff.SetNewComputed(block); err != nil {
				return err
			}
			continue
		}
		if err := diff.SetNew(block, registrant); err != nil {
			return err
		}
	}

	return nil
}
