package namecheap_provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
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

// dnsRecordFixture builds an SDK record for the identity tests below.
func dnsRecordFixture(hostname, recordType, address string, mxPref int) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(hostname),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
		MXPref:     namecheap.UInt8(uint8(mxPref)),
		TTL:        namecheap.Int(1800),
	}
}

// TestDNSRecordSelectorIdentity pins which fields identify a record. Host, type
// and address are the identity; TTL is an attribute of it, so selecting must not
// depend on it — otherwise changing a TTL would fail to find the record it is
// meant to update.
func TestDNSRecordSelectorIdentity(t *testing.T) {
	selector := dnsRecordSelector(dnsRecordFixture("WWW", "a", "10.0.0.1", 10))

	assert.NotNil(t, selector.HostName)
	assert.Equal(t, "www", *selector.HostName, "hostname is matched lower-cased")
	assert.NotNil(t, selector.RecordType)
	assert.Equal(t, "A", *selector.RecordType, "type is matched upper-cased")
	assert.NotNil(t, selector.Address)
	assert.Equal(t, "10.0.0.1", *selector.Address)

	assert.Nil(t, selector.MXPref, "outside MX, the API reports a fixed preference that means nothing")
	assert.False(t, selector.MatchAll, "a per-record selector must never match everything")
}

// TestDNSRecordSelectorIncludesMXPrefForMX is the property that keeps a
// primary/backup mail pair separable: two MX records naming the same host differ
// only in preference, and the SDK applies a change to every record a selector
// matches — so leaving the preference out of an MX selector would delete both.
func TestDNSRecordSelectorIncludesMXPrefForMX(t *testing.T) {
	for _, recordType := range []string{"MX", "mx"} {
		selector := dnsRecordSelector(dnsRecordFixture("@", recordType, "mail.example.com", 20))

		assert.NotNil(t, selector.MXPref, "%q: the MX preference is part of an MX record's identity", recordType)
		assert.Equal(t, uint8(20), *selector.MXPref, "%q", recordType)
	}

	// MXE carries no preference of its own, and a zone may hold only one.
	assert.Nil(t, dnsRecordSelector(dnsRecordFixture("@", "MXE", "10.0.0.1", 10)).MXPref)
}

// TestDNSRecordSelectorNeverMatchesAll is the safety property behind the ones
// above: the SDK deletes every record a selector matches, so a selector that
// degenerated to MatchAll would wipe the zone.
func TestDNSRecordSelectorNeverMatchesAll(t *testing.T) {
	for _, tc := range []struct{ hostname, recordType, address string }{
		{"www", "A", "10.0.0.1"},
		{"@", "TXT", ""},
		{"", "", ""},
	} {
		selector := dnsRecordSelector(dnsRecordFixture(tc.hostname, tc.recordType, tc.address, 10))
		assert.False(t, selector.MatchAll,
			"selector for %q/%q/%q must not match all records", tc.hostname, tc.recordType, tc.address)
	}
}

// TestDNSRecordIdentityMatchesWildcardsUnknownMXPref pins the one asymmetry
// between the two arguments: the import ID carries no preference, so a want
// without one has to match whatever the live record has. Getting this backwards
// makes every MX record unimportable.
func TestDNSRecordIdentityMatchesWildcardsUnknownMXPref(t *testing.T) {
	live := dnsRecordFixture("@", "MX", "mail.example.com.", 20)

	unknownPref := namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String("@"),
		RecordType: namecheap.String("MX"),
		Address:    namecheap.String("mail.example.com"),
	}
	assert.True(t, dnsRecordIdentityMatches(live, unknownPref),
		"a want with no preference — an import ID — must match any preference")

	assert.True(t, dnsRecordIdentityMatches(live, dnsRecordFixture("@", "MX", "mail.example.com", 20)),
		"the same preference matches, trailing dot notwithstanding")
	assert.False(t, dnsRecordIdentityMatches(live, dnsRecordFixture("@", "MX", "mail.example.com", 10)),
		"a different preference is a different MX record")

	// Outside MX the preference is not compared at all, in either direction.
	assert.True(t, dnsRecordIdentityMatches(
		dnsRecordFixture("www", "A", "10.0.0.1", 10),
		dnsRecordFixture("www", "A", "10.0.0.1", 20)))

	// TTL is never identity: it is an attribute of the record, and matching on it
	// would make a TTL changed in the dashboard look like a deletion.
	live.TTL = namecheap.Int(60)
	assert.True(t, dnsRecordIdentityMatches(live, dnsRecordFixture("@", "MX", "mail.example.com", 20)))
}

// TestDNSRecordEffectiveMXPref covers the value actually sent to the API. Only MX
// carries a preference; sending a configured one for any other type makes the
// SDK's post-write verification compare it against the API's fixed 10 and fail the
// apply as a lost race.
func TestDNSRecordEffectiveMXPref(t *testing.T) {
	assert.Equal(t, uint8(20), dnsRecordEffectiveMXPref("MX", 20))
	assert.Equal(t, uint8(20), dnsRecordEffectiveMXPref("mx", 20), "type case must not change what is sent")
	assert.Equal(t, uint8(0), dnsRecordEffectiveMXPref("MX", 0), "zero is a valid, most-preferred MX preference")

	for _, recordType := range []string{"A", "AAAA", "CNAME", "TXT", "MXE", "URL", "NS"} {
		assert.Equal(t, uint8(dnsRecordFixedMXPref), dnsRecordEffectiveMXPref(recordType, 20),
			"%s has no preference, so the API's fixed value is what must be sent", recordType)
	}
}

// TestDNSRecordSchemaRejectsEmptyIdentityFields covers empty strings, which
// Required alone accepts. An empty hostname would mean the apex through SDK
// normalization while rendering an ID the importer refuses to parse.
func TestDNSRecordSchemaRejectsEmptyIdentityFields(t *testing.T) {
	s := resourceNamecheapDNSRecord().Schema
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

// TestDNSRecordAmbiguousErrorListsCandidates covers the message a user gets when
// the zone holds records this resource cannot tell apart. It has to say what
// distinguishes them, since the only way out is to go and look at the zone.
func TestDNSRecordAmbiguousErrorListsCandidates(t *testing.T) {
	want := dnsRecordFixture("twin", "A", "10.0.0.1", 10)
	diags := dnsRecordAmbiguousError("example.com", want, []namecheap.DomainsDNSHostRecordDetailed{
		{Name: namecheap.String("twin"), Type: namecheap.String("A"), Address: namecheap.String("10.0.0.1"), TTL: namecheap.Int(300)},
		{Name: namecheap.String("twin"), Type: namecheap.String("A"), Address: namecheap.String("10.0.0.1"), TTL: namecheap.Int(7200)},
	})

	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "Ambiguous DNS record on example.com")
	assert.Contains(t, diags[0].Detail, "TTL 300")
	assert.Contains(t, diags[0].Detail, "TTL 7200")
	assert.NotContains(t, diags[0].Detail, "MX preference", "an A record has no meaningful preference")

	mx := dnsRecordFixture("@", "MX", "mail.example.com", 10)
	mxDiags := dnsRecordAmbiguousError("example.com", mx, []namecheap.DomainsDNSHostRecordDetailed{
		{Name: namecheap.String("@"), Type: namecheap.String("MX"), Address: namecheap.String("mail.example.com."), TTL: namecheap.Int(1800), MXPref: namecheap.Int(10)},
		{Name: namecheap.String("@"), Type: namecheap.String("MX"), Address: namecheap.String("mail.example.com."), TTL: namecheap.Int(1800), MXPref: namecheap.Int(10)},
	})
	assert.Contains(t, mxDiags[0].Detail, "MX preference 10", "for MX the preference is what a user has to look at")
}

// TestDNSRecordImportErrorKeepsDetail covers the flattening of diagnostics into
// the single error the importer interface allows: dropping the detail would throw
// away the ambiguous-match candidate list, which is the whole message.
func TestDNSRecordImportErrorKeepsDetail(t *testing.T) {
	withDetail := dnsRecordImportError("example.com", diag.Diagnostics{{
		Severity: diag.Error,
		Summary:  "Ambiguous DNS record on example.com",
		Detail:   "  - TTL 300\n  - TTL 7200",
	}})
	assert.ErrorContains(t, withDetail, "Ambiguous DNS record on example.com")
	assert.ErrorContains(t, withDetail, "TTL 7200")

	bare := dnsRecordImportError("example.com", diag.Diagnostics{{
		Severity: diag.Error,
		Summary:  "Domain not found",
	}})
	assert.ErrorContains(t, bare, "reading example.com during import: Domain not found")
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

// TestDNSRecordSchemaRejectsUnrepresentableMXPref covers a preference the API
// cannot express. setHosts takes an unsigned byte, so a larger value would be
// truncated on the way out — 256 arriving as 0, the *most* preferred, which is the
// opposite of what was asked for. It has to be rejected at plan time instead.
func TestDNSRecordSchemaRejectsUnrepresentableMXPref(t *testing.T) {
	validate := resourceNamecheapDNSRecord().Schema["mx_pref"].ValidateFunc
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
