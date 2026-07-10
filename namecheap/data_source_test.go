package namecheap_provider

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// ncDate builds a *namecheap.DateTime from a Namecheap "MM/DD/YYYY" date string,
// exercising the same UnmarshalText path the SDK uses to parse API responses.
func ncDate(t *testing.T, s string) *namecheap.DateTime {
	t.Helper()
	dt := &namecheap.DateTime{}
	if err := dt.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("failed to parse date %q: %v", s, err)
	}
	return dt
}

// TestDataSourcesRegistered asserts the provider exposes the three data sources.
func TestDataSourcesRegistered(t *testing.T) {
	p := Provider()
	for _, name := range []string{"namecheap_domain", "namecheap_domains", "namecheap_domain_records"} {
		ds, ok := p.DataSourcesMap[name]
		assert.True(t, ok, "data source %s should be registered", name)
		assert.NotNil(t, ds, "data source %s should be non-nil", name)
	}
}

// TestDataSourcesInternalValidate makes sure the registered data-source schemas
// pass the SDK's internal validation (part of Provider().InternalValidate, but
// asserted explicitly here for a targeted failure signal).
func TestDataSourcesInternalValidate(t *testing.T) {
	assert.NoError(t, Provider().InternalValidate())
}

// TestDomainRecordFieldParity asserts that the record object shape exported by
// the namecheap_domain_records data source matches the record block of the
// namecheap_domain_records resource attribute-for-attribute, so a data-source
// record composes into a resource record block without field remapping.
func TestDomainRecordFieldParity(t *testing.T) {
	resourceRecord := resourceNamecheapDomainRecords().Schema["record"]
	resourceElem := resourceRecord.Elem.(*schema.Resource).Schema

	dataElem := domainRecordElemSchema()

	assert.Equal(t, len(resourceElem), len(dataElem),
		"data source record object must have the same number of attributes as the resource record block")

	for name, resAttr := range resourceElem {
		dsAttr, ok := dataElem[name]
		assert.True(t, ok, "data source record object is missing attribute %q present on the resource", name)
		if ok {
			assert.Equal(t, resAttr.Type, dsAttr.Type,
				"attribute %q should have the same type on the data source and the resource", name)
		}
	}
	for name := range dataElem {
		_, ok := resourceElem[name]
		assert.True(t, ok, "data source record object has extra attribute %q not present on the resource", name)
	}
}

func TestFormatDateTime(t *testing.T) {
	assert.Equal(t, "", formatDateTime(nil), "nil DateTime should render as empty string")

	dt := ncDate(t, "06/02/2027")
	assert.Equal(t, "2027-06-02T00:00:00Z", formatDateTime(dt),
		"a Namecheap date should render as an RFC3339 midnight-UTC timestamp")
}

func TestDaysUntil(t *testing.T) {
	// Pin "now" to an arbitrary time-of-day to prove the calculation ignores
	// the wall-clock time and is purely calendar-date based.
	now := time.Date(2026, 7, 9, 15, 30, 0, 0, time.UTC)

	cases := []struct {
		name   string
		target time.Time
		want   int
	}{
		{"same calendar day, earlier time", time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC), 0},
		{"same calendar day, later time", time.Date(2026, 7, 9, 23, 59, 59, 0, time.UTC), 0},
		{"tomorrow", time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), 1},
		{"yesterday", time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), -1},
		{"thirty days out", time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC), 30},
		{"one year in the past", time.Date(2025, 7, 9, 0, 0, 0, 0, time.UTC), -365},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, daysUntil(tc.target, now))
		})
	}
}

// TestDaysUntilTimezoneSafe proves that a target expressed in a non-UTC zone is
// reduced to its UTC calendar date before counting, so the result does not drift
// with the input timezone.
func TestDaysUntilTimezoneSafe(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	// 2026-07-10T01:00:00+05:00 is 2026-07-09T20:00:00Z -> same UTC day as now.
	loc := time.FixedZone("UTC+5", 5*60*60)
	target := time.Date(2026, 7, 10, 1, 0, 0, 0, loc)
	assert.Equal(t, 0, daysUntil(target, now))
}
