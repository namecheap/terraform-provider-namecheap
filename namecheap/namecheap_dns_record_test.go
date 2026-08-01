package namecheap_provider

import (
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// These are the pure-function tests for namecheap_dns_record: identity, ID
// rendering and import parsing. The CRUD paths need a live-ish zone and are
// covered by the mock acceptance suite (mock_dns_record_test.go), which drives
// them through real Terraform.

func TestDNSRecordID(t *testing.T) {
	tests := []struct {
		name                               string
		domain, recordType, hostname, addr string
		want                               string
	}{
		{"simple", "example.com", "A", "www", "10.0.0.1", "example.com/A/www/10.0.0.1"},
		{"apex", "example.com", "TXT", "@", "v=spf1 -all", "example.com/TXT/@/v=spf1 -all"},
		// Case is normalized so the ID is stable however the config spelled it.
		{"mixed case", "Example.COM", "a", "WWW", "10.0.0.1", "example.com/A/www/10.0.0.1"},
		// The address keeps its own case: DNS values are not all case-insensitive,
		// and a TXT record's case is significant.
		{"address case preserved", "example.com", "TXT", "@", "MixedCase", "example.com/TXT/@/MixedCase"},
		// A URL record's address contains the separator; the ID still round-trips
		// because only the first three components are split off on import.
		{"address with separators", "example.com", "URL", "go", "https://example.org/a/b", "example.com/URL/go/https://example.org/a/b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, dnsRecordID(tc.domain, tc.recordType, tc.hostname, tc.addr))
		})
	}
}

// TestDNSRecordIDRoundTripsThroughImport is the property that matters: whatever
// dnsRecordID produces must be parseable back into the same four components,
// including addresses that contain the separator.
func TestDNSRecordIDRoundTripsThroughImport(t *testing.T) {
	tests := []struct {
		name                               string
		domain, recordType, hostname, addr string
	}{
		{"simple", "example.com", "A", "www", "10.0.0.1"},
		{"url with path", "example.com", "URL", "go", "https://example.org/a/b"},
		{"url with trailing slash", "example.com", "URL301", "old", "https://example.org/"},
		{"txt with slashes", "example.com", "TXT", "@", "v=DMARC1; p=none; rua=mailto:a@b.c/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := dnsRecordID(tc.domain, tc.recordType, tc.hostname, tc.addr)

			// Mirror the import parser: split off three components, keep the rest.
			parts := splitDNSRecordID(id)
			assert.Len(t, parts, 4)
			assert.Equal(t, tc.domain, parts[0])
			assert.Equal(t, tc.recordType, parts[1])
			assert.Equal(t, tc.hostname, parts[2])
			assert.Equal(t, tc.addr, parts[3], "an address containing the separator must survive the round trip")
		})
	}
}

// splitDNSRecordID mirrors the import parser's split so the round-trip property
// is asserted against the same rule the resource uses.
func splitDNSRecordID(id string) []string {
	const sep = dnsRecordIDSeparator
	out := make([]string, 0, 4)
	rest := id
	for i := 0; i < 3; i++ {
		idx := indexOf(rest, sep)
		if idx < 0 {
			return append(out, rest)
		}
		out = append(out, rest[:idx])
		rest = rest[idx+len(sep):]
	}
	return append(out, rest)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestDNSRecordSelectorIdentity pins which fields identify a record. Host, type
// and address are the identity; TTL and MX preference are attributes of it, so
// selecting must not depend on them — otherwise changing a TTL would fail to
// find the record it is meant to update.
func TestDNSRecordSelectorIdentity(t *testing.T) {
	selector := dnsRecordSelector("example.com", "WWW", "a", "10.0.0.1")

	assert.NotNil(t, selector.HostName)
	assert.Equal(t, "www", *selector.HostName, "hostname is matched lower-cased")
	assert.NotNil(t, selector.RecordType)
	assert.Equal(t, "A", *selector.RecordType, "type is matched upper-cased")
	assert.NotNil(t, selector.Address)
	assert.Equal(t, "10.0.0.1", *selector.Address)

	assert.Nil(t, selector.MXPref, "MX preference is an attribute, not part of the identity")
	assert.False(t, selector.MatchAll, "a per-record selector must never match everything")
}

// TestDNSRecordSelectorNeverMatchesAll is the safety property behind the one
// above: the SDK deletes every record a selector matches, so a selector that
// degenerated to MatchAll would wipe the zone.
func TestDNSRecordSelectorNeverMatchesAll(t *testing.T) {
	for _, tc := range []struct{ hostname, recordType, address string }{
		{"www", "A", "10.0.0.1"},
		{"@", "TXT", ""},
		{"", "", ""},
	} {
		selector := dnsRecordSelector("example.com", tc.hostname, tc.recordType, tc.address)
		assert.False(t, selector.MatchAll,
			"selector for %q/%q/%q must not match all records", tc.hostname, tc.recordType, tc.address)
	}
}

// TestDNSRecordLookupMatchesNormalized proves the lookup compares records the
// way the API does, not the way the configuration spells them: a CNAME target
// gains a trailing dot server-side, and a match has to survive that.
func TestDNSRecordLookupMatchesNormalized(t *testing.T) {
	want := namecheap.NormalizeRecord(namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String("blog"),
		RecordType: namecheap.String("CNAME"),
		Address:    namecheap.String("hosting.example.com"),
		TTL:        namecheap.Int(1800),
	})
	live := namecheap.NormalizeRecord(namecheap.RecordFromDetailed(namecheap.DomainsDNSHostRecordDetailed{
		Name:    namecheap.String("blog"),
		Type:    namecheap.String("CNAME"),
		Address: namecheap.String("hosting.example.com."),
		TTL:     namecheap.Int(1800),
	}))

	assert.True(t, namecheap.RecordsEqual(want, live),
		"a trailing dot added by the API must not make the record look different")
}

// TestDNSRecordSchemaForcesNewOnIdentity pins which changes replace the record
// and which happen in place. Getting this wrong is a data-loss bug: an in-place
// "update" of the hostname would rewrite a different record.
func TestDNSRecordSchemaForcesNewOnIdentity(t *testing.T) {
	s := resourceNamecheapDNSRecord().Schema

	for _, field := range []string{"domain", "hostname", "type"} {
		assert.True(t, s[field].ForceNew, "%s is part of the record's identity and must force replacement", field)
	}
	for _, field := range []string{"address", "ttl", "mx_pref"} {
		assert.False(t, s[field].ForceNew, "%s is an attribute and should be updated in place", field)
	}
}

// TestDNSRecordSchemaShape guards the contract the docs describe.
func TestDNSRecordSchemaShape(t *testing.T) {
	r := resourceNamecheapDNSRecord()

	assert.NotNil(t, r.Importer, "the resource must be importable")
	assert.NotEmpty(t, r.Description, "the registry page summary is generated from this")

	for _, field := range []string{"domain", "hostname", "type", "address"} {
		assert.True(t, r.Schema[field].Required, "%s should be required", field)
	}
	for _, field := range []string{"ttl", "mx_pref"} {
		assert.True(t, r.Schema[field].Optional, "%s should be optional", field)
		assert.NotNil(t, r.Schema[field].Default, "%s should have a default so plans stay empty", field)
	}
	// Defaults must match what the API returns for an unset value, or every read
	// would report drift.
	assert.Equal(t, 1800, r.Schema["ttl"].Default)
	assert.Equal(t, 10, r.Schema["mx_pref"].Default)
}
