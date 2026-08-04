package namecheap_provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// These are the pure-function tests for namecheap_domain_host_record: identity, ID
// rendering and import parsing. The CRUD paths need a live-ish zone and are
// covered by the mock acceptance suite (mock_domain_host_record_test.go), which drives
// them through real Terraform.

func TestDomainHostRecordID(t *testing.T) {
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
			assert.Equal(t, tc.want, hostRecordID(tc.domain, tc.recordType, tc.hostname, tc.addr))
		})
	}
}

// TestDomainHostRecordIDRoundTripsThroughImport is the property that matters: whatever
// hostRecordID produces must be parseable back into the same four components,
// including addresses that contain the separator.
func TestDomainHostRecordIDRoundTripsThroughImport(t *testing.T) {
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
			id := hostRecordID(tc.domain, tc.recordType, tc.hostname, tc.addr)

			// Mirror the import parser: split off three components, keep the rest.
			parts := splitDomainHostRecordID(id)
			assert.Len(t, parts, 4)
			assert.Equal(t, tc.domain, parts[0])
			assert.Equal(t, tc.recordType, parts[1])
			assert.Equal(t, tc.hostname, parts[2])
			assert.Equal(t, tc.addr, parts[3], "an address containing the separator must survive the round trip")
		})
	}
}

// splitDomainHostRecordID mirrors the import parser's split so the round-trip property
// is asserted against the same rule the resource uses.
func splitDomainHostRecordID(id string) []string {
	const sep = hostRecordIDSeparator
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

// hostRecordFixture builds an SDK record for the identity tests below.
func hostRecordFixture(hostname, recordType, address string, mxPref int) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(hostname),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
		MXPref:     namecheap.UInt8(uint8(mxPref)),
		TTL:        namecheap.Int(1800),
	}
}

// TestDomainHostRecordSelectorIdentity pins which fields identify a record. Host, type
// and address are the identity; TTL is an attribute of it, so selecting must not
// depend on it — otherwise changing a TTL would fail to find the record it is
// meant to update.
func TestDomainHostRecordSelectorIdentity(t *testing.T) {
	selector := hostRecordSelector(hostRecordFixture("WWW", "a", "10.0.0.1", 10))

	assert.NotNil(t, selector.HostName)
	assert.Equal(t, "www", *selector.HostName, "hostname is matched lower-cased")
	assert.NotNil(t, selector.RecordType)
	assert.Equal(t, "A", *selector.RecordType, "type is matched upper-cased")
	assert.NotNil(t, selector.Address)
	assert.Equal(t, "10.0.0.1", *selector.Address)

	assert.Nil(t, selector.MXPref, "outside MX, the API reports a fixed preference that means nothing")
	assert.False(t, selector.MatchAll, "a per-record selector must never match everything")
}

// TestDomainHostRecordSelectorIncludesMXPrefForMX is the property that keeps a
// primary/backup mail pair separable: two MX records naming the same host differ
// only in preference, and the SDK applies a change to every record a selector
// matches — so leaving the preference out of an MX selector would delete both.
func TestDomainHostRecordSelectorIncludesMXPrefForMX(t *testing.T) {
	for _, recordType := range []string{"MX", "mx"} {
		selector := hostRecordSelector(hostRecordFixture("@", recordType, "mail.example.com", 20))

		assert.NotNil(t, selector.MXPref, "%q: the MX preference is part of an MX record's identity", recordType)
		assert.Equal(t, uint8(20), *selector.MXPref, "%q", recordType)
	}

	// MXE carries no preference of its own, and a zone may hold only one.
	assert.Nil(t, hostRecordSelector(hostRecordFixture("@", "MXE", "10.0.0.1", 10)).MXPref)
}

// TestDomainHostRecordSelectorNeverMatchesAll is the safety property behind the ones
// above: the SDK deletes every record a selector matches, so a selector that
// degenerated to MatchAll would wipe the zone.
func TestDomainHostRecordSelectorNeverMatchesAll(t *testing.T) {
	for _, tc := range []struct{ hostname, recordType, address string }{
		{"www", "A", "10.0.0.1"},
		{"@", "TXT", ""},
		{"", "", ""},
	} {
		selector := hostRecordSelector(hostRecordFixture(tc.hostname, tc.recordType, tc.address, 10))
		assert.False(t, selector.MatchAll,
			"selector for %q/%q/%q must not match all records", tc.hostname, tc.recordType, tc.address)
	}
}

// TestDomainHostRecordIdentityMatchesWildcardsUnknownMXPref pins the one asymmetry
// between the two arguments: the import ID carries no preference, so a want
// without one has to match whatever the live record has. Getting this backwards
// makes every MX record unimportable.
func TestDomainHostRecordIdentityMatchesWildcardsUnknownMXPref(t *testing.T) {
	live := hostRecordFixture("@", "MX", "mail.example.com.", 20)

	unknownPref := namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String("@"),
		RecordType: namecheap.String("MX"),
		Address:    namecheap.String("mail.example.com"),
	}
	assert.True(t, hostRecordIdentityMatches(live, unknownPref),
		"a want with no preference — an import ID — must match any preference")

	assert.True(t, hostRecordIdentityMatches(live, hostRecordFixture("@", "MX", "mail.example.com", 20)),
		"the same preference matches, trailing dot notwithstanding")
	assert.False(t, hostRecordIdentityMatches(live, hostRecordFixture("@", "MX", "mail.example.com", 10)),
		"a different preference is a different MX record")

	// Outside MX the preference is not compared at all, in either direction.
	assert.True(t, hostRecordIdentityMatches(
		hostRecordFixture("www", "A", "10.0.0.1", 10),
		hostRecordFixture("www", "A", "10.0.0.1", 20)))

	// TTL is never identity: it is an attribute of the record, and matching on it
	// would make a TTL changed in the dashboard look like a deletion.
	live.TTL = namecheap.Int(60)
	assert.True(t, hostRecordIdentityMatches(live, hostRecordFixture("@", "MX", "mail.example.com", 20)))
}

// TestDomainHostRecordEffectiveMXPref covers the value actually sent to the API. Only MX
// carries a preference; sending a configured one for any other type makes the
// SDK's post-write verification compare it against the API's fixed 10 and fail the
// apply as a lost race.
func TestDomainHostRecordEffectiveMXPref(t *testing.T) {
	assert.Equal(t, uint8(20), hostRecordEffectiveMXPref("MX", 20))
	assert.Equal(t, uint8(20), hostRecordEffectiveMXPref("mx", 20), "type case must not change what is sent")
	assert.Equal(t, uint8(0), hostRecordEffectiveMXPref("MX", 0), "zero is a valid, most-preferred MX preference")

	for _, recordType := range []string{"A", "AAAA", "CNAME", "TXT", "MXE", "URL", "NS"} {
		assert.Equal(t, uint8(hostRecordFixedMXPref), hostRecordEffectiveMXPref(recordType, 20),
			"%s has no preference, so the API's fixed value is what must be sent", recordType)
	}
}

// TestDomainHostRecordSchemaRejectsEmptyIdentityFields covers empty strings, which
// Required alone accepts. An empty hostname would mean the apex through SDK
// normalization while rendering an ID the importer refuses to parse.
func TestDomainHostRecordSchemaRejectsEmptyIdentityFields(t *testing.T) {
	s := resourceNamecheapDomainHostRecord().Schema
	for _, field := range []string{"hostname", "address"} {
		validate := s[field].ValidateFunc
		if !assert.NotNil(t, validate, "%s must reject an empty value", field) {
			continue
		}
		_, errs := validate("", field)
		assert.NotEmpty(t, errs, "%s = \"\" should be rejected", field)
		_, errs = validate("www", field)
		assert.Empty(t, errs, "%s = %q should be accepted", field, "www")
	}
}

// TestDomainHostRecordAmbiguousErrorListsCandidates covers the message a user gets when
// the zone holds records this resource cannot tell apart. It has to say what
// distinguishes them, since the only way out is to go and look at the zone.
func TestDomainHostRecordAmbiguousErrorListsCandidates(t *testing.T) {
	want := hostRecordFixture("twin", "A", "10.0.0.1", 10)
	diags := hostRecordAmbiguousError("example.com", want, []namecheap.DomainsDNSHostRecordDetailed{
		{Name: namecheap.String("twin"), Type: namecheap.String("A"), Address: namecheap.String("10.0.0.1"), TTL: namecheap.Int(300)},
		{Name: namecheap.String("twin"), Type: namecheap.String("A"), Address: namecheap.String("10.0.0.1"), TTL: namecheap.Int(7200)},
	})

	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "Ambiguous DNS record on example.com")
	assert.Contains(t, diags[0].Detail, "TTL 300")
	assert.Contains(t, diags[0].Detail, "TTL 7200")
	assert.NotContains(t, diags[0].Detail, "MX preference", "an A record has no meaningful preference")

	mx := hostRecordFixture("@", "MX", "mail.example.com", 10)
	mxDiags := hostRecordAmbiguousError("example.com", mx, []namecheap.DomainsDNSHostRecordDetailed{
		{Name: namecheap.String("@"), Type: namecheap.String("MX"), Address: namecheap.String("mail.example.com."), TTL: namecheap.Int(1800), MXPref: namecheap.Int(10)},
		{Name: namecheap.String("@"), Type: namecheap.String("MX"), Address: namecheap.String("mail.example.com."), TTL: namecheap.Int(1800), MXPref: namecheap.Int(10)},
	})
	assert.Contains(t, mxDiags[0].Detail, "MX preference 10", "for MX the preference is what a user has to look at")
}

// TestDomainHostRecordImportErrorKeepsDetail covers the flattening of diagnostics into
// the single error the importer interface allows: dropping the detail would throw
// away the ambiguous-match candidate list, which is the whole message.
func TestDomainHostRecordImportErrorKeepsDetail(t *testing.T) {
	withDetail := hostRecordImportError("example.com", diag.Diagnostics{{
		Severity: diag.Error,
		Summary:  "Ambiguous DNS record on example.com",
		Detail:   "  - TTL 300\n  - TTL 7200",
	}})
	assert.ErrorContains(t, withDetail, "Ambiguous DNS record on example.com")
	assert.ErrorContains(t, withDetail, "TTL 7200")

	bare := hostRecordImportError("example.com", diag.Diagnostics{{
		Severity: diag.Error,
		Summary:  "Domain not found",
	}})
	assert.ErrorContains(t, bare, "reading example.com during import: Domain not found")
}

// TestDomainHostRecordLookupMatchesNormalized proves the lookup compares records the
// way the API does, not the way the configuration spells them: a CNAME target
// gains a trailing dot server-side, and a match has to survive that.
func TestDomainHostRecordLookupMatchesNormalized(t *testing.T) {
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

// TestDomainHostRecordSchemaForcesNewOnIdentity pins which changes replace the record
// and which happen in place. Getting this wrong is a data-loss bug: an in-place
// "update" of the hostname would rewrite a different record.
func TestDomainHostRecordSchemaForcesNewOnIdentity(t *testing.T) {
	s := resourceNamecheapDomainHostRecord().Schema

	for _, field := range []string{"domain", "hostname", "type"} {
		assert.True(t, s[field].ForceNew, "%s is part of the record's identity and must force replacement", field)
	}
	for _, field := range []string{"address", "ttl", "mx_pref"} {
		assert.False(t, s[field].ForceNew, "%s is an attribute and should be updated in place", field)
	}
}

// TestDomainHostRecordSchemaRejectsUnrepresentableMXPref covers a preference the API
// cannot express. setHosts takes an unsigned byte, so a larger value would be
// truncated on the way out — 256 arriving as 0, the *most* preferred, which is the
// opposite of what was asked for. It has to be rejected at plan time instead.
func TestDomainHostRecordSchemaRejectsUnrepresentableMXPref(t *testing.T) {
	validate := resourceNamecheapDomainHostRecord().Schema["mx_pref"].ValidateFunc
	if !assert.NotNil(t, validate, "mx_pref must be validated, not silently truncated") {
		return
	}

	for _, valid := range []int{0, 10, 255} {
		_, errs := validate(valid, "mx_pref")
		assert.Empty(t, errs, "preference %d is representable", valid)
	}
	for _, invalid := range []int{-1, 256, 65535} {
		_, errs := validate(invalid, "mx_pref")
		assert.NotEmpty(t, errs, "preference %d cannot be sent to the API", invalid)
	}
}

// TestDomainHostRecordSchemaShape guards the contract the docs describe.
func TestDomainHostRecordSchemaShape(t *testing.T) {
	r := resourceNamecheapDomainHostRecord()

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
