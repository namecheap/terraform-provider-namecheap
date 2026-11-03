package namecheap_provider

import (
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

func TestConfigHasMXERecord(t *testing.T) {
	record := func(recordType cty.Value) cty.Value {
		return cty.ObjectVal(map[string]cty.Value{"type": recordType})
	}
	recordSetType := cty.Set(cty.Object(map[string]cty.Type{"type": cty.String}))

	cases := []struct {
		name       string
		records    cty.Value
		wantHasMXE bool
		wantKnown  bool
	}{
		{"no record blocks", cty.NullVal(recordSetType), false, true},
		{"record set unknown", cty.UnknownVal(recordSetType), false, false},
		{"no MXE record", cty.SetVal([]cty.Value{record(cty.StringVal("A"))}), false, true},
		{"MXE record", cty.SetVal([]cty.Value{record(cty.StringVal("A")), record(cty.StringVal("MXE"))}), true, true},
		{"type unknown", cty.SetVal([]cty.Value{record(cty.StringVal("A")), record(cty.UnknownVal(cty.String))}), false, false},
		{"MXE next to an unknown type", cty.SetVal([]cty.Value{record(cty.UnknownVal(cty.String)), record(cty.StringVal("MXE"))}), true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hasMXE, known := configHasMXERecord(tc.records)
			assert.Equal(t, tc.wantHasMXE, hasMXE, "hasMXE")
			assert.Equal(t, tc.wantKnown, known, "known")
		})
	}
}

func TestZoneKeepsMXERecord(t *testing.T) {
	host := func(name, recordType, address string) namecheap.DomainsDNSHostRecordDetailed {
		return namecheap.DomainsDNSHostRecordDetailed{
			Name: namecheap.String(name), Type: namecheap.String(recordType), Address: namecheap.String(address),
		}
	}
	prior := func(hostname, recordType, address string) map[string]interface{} {
		return map[string]interface{}{"hostname": hostname, "type": recordType, "address": address, "mx_pref": 10, "ttl": 1800}
	}

	cases := []struct {
		name  string
		hosts *[]namecheap.DomainsDNSHostRecordDetailed
		prior []interface{}
		want  bool
	}{
		{"zone not read", nil, nil, false},
		{"no MXE row on the zone", &[]namecheap.DomainsDNSHostRecordDetailed{host("www", "A", "10.0.0.1")}, nil, false},
		{"MXE row another resource manages", &[]namecheap.DomainsDNSHostRecordDetailed{host("@", "MXE", "10.0.0.26")}, nil, true},
		{
			"only MXE row is this resource's",
			&[]namecheap.DomainsDNSHostRecordDetailed{host("@", "MXE", "10.0.0.25")},
			[]interface{}{prior("@", "MXE", "10.0.0.25")},
			false,
		},
		{
			"another MXE row next to this resource's",
			&[]namecheap.DomainsDNSHostRecordDetailed{host("@", "MXE", "10.0.0.25"), host("@", "MXE", "10.0.0.26")},
			[]interface{}{prior("@", "MXE", "10.0.0.25")},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := zoneKeepsMXERecord(tc.hosts, tc.prior)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestIsPriorRecord: the MERGE writes and the MXE plan check pick the live rows a
// resource owns with one rule. A prior CNAME or MX address is normalised the way
// the write normalises it, so it matches the live row with or without its
// trailing dot.
func TestIsPriorRecord(t *testing.T) {
	live := func(name, recordType, address string) namecheap.DomainsDNSHostRecordDetailed {
		return namecheap.DomainsDNSHostRecordDetailed{
			Name: namecheap.String(name), Type: namecheap.String(recordType), Address: namecheap.String(address),
		}
	}
	prior := func(hostname, recordType, address string) namecheap.DomainsDNSHostRecord {
		return namecheap.DomainsDNSHostRecord{
			HostName: namecheap.String(hostname), RecordType: namecheap.String(recordType), Address: namecheap.String(address),
		}
	}

	cases := []struct {
		name  string
		live  namecheap.DomainsDNSHostRecordDetailed
		prior []namecheap.DomainsDNSHostRecord
		want  bool
	}{
		{"CNAME without its trailing dot", live("www", "CNAME", "example.net."), []namecheap.DomainsDNSHostRecord{prior("www", "CNAME", "example.net")}, true},
		{"CNAME with its trailing dot", live("www", "CNAME", "example.net."), []namecheap.DomainsDNSHostRecord{prior("www", "CNAME", "example.net.")}, true},
		{"MX without its trailing dot", live("@", "MX", "mail.example.net."), []namecheap.DomainsDNSHostRecord{prior("@", "MX", "mail.example.net")}, true},
		{"MX with its trailing dot", live("@", "MX", "mail.example.net."), []namecheap.DomainsDNSHostRecord{prior("@", "MX", "mail.example.net.")}, true},
		{"case-insensitive host", live("www", "A", "10.0.0.1"), []namecheap.DomainsDNSHostRecord{prior("WWW", "A", "10.0.0.1")}, true},
		{"different address", live("www", "A", "10.0.0.1"), []namecheap.DomainsDNSHostRecord{prior("www", "A", "10.0.0.2")}, false},
		{"no prior records", live("www", "A", "10.0.0.1"), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := isPriorRecord(tc.live, tc.prior)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	_, err := isPriorRecord(live("@", "CAA", `0 issue "ca.example"`), []namecheap.DomainsDNSHostRecord{prior("@", "CAA", "not a caa value")})
	assert.Error(t, err, "an unparsable prior CAA value is reported, as the MERGE write reports it")
}
