package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configuredRecord builds a record as convertRecordTypeSetToDomainRecords
// returns it from config or state: the input struct for setHosts.
func configuredRecord(name, recordType, address string, mxPref uint8, ttl int) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String(name),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
		MXPref:     namecheap.UInt8(mxPref),
		TTL:        namecheap.Int(ttl),
	}
}

// zoneRows renders a setHosts record list as one comparable line per row.
func zoneRows(records []namecheap.DomainsDNSHostRecord) []string {
	rows := make([]string, 0, len(records))
	for _, r := range records {
		rows = append(rows, fmt.Sprintf("%s %s %s mx=%d ttl=%d", *r.HostName, *r.RecordType, *r.Address, *r.MXPref, *r.TTL))
	}
	return rows
}

// setHostsRows reads the records of a setHosts request as one line per row:
// "<host> <type> <address> ttl=<ttl>", in the order they were sent.
func setHostsRows(r *http.Request) []string {
	var rows []string
	for i := 1; ; i++ {
		idx := strconv.Itoa(i)
		name := r.FormValue("HostName" + idx)
		if name == "" {
			return rows
		}
		rows = append(rows, fmt.Sprintf("%s %s %s ttl=%s", name, r.FormValue("RecordType"+idx), r.FormValue("Address"+idx), r.FormValue("TTL"+idx)))
	}
}

// A row added to config that already exists live, while the resource holds
// other rows in state: update adopts it. It used to keep the live copy and
// append the configured one, leaving the zone with two.
func TestUpdateRecordsMerge_AdoptsLiveRowNewToConfig(t *testing.T) {
	var written []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch command := r.FormValue("Command"); command {
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "@", Type: "A", Address: "1.1.1.1", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "3.3.3.3", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "4.4.4.4", MXPref: 10, TTL: 1800},
			}))
		case "namecheap.domains.dns.setHosts":
			written = setHostsRows(r)
			_, _ = fmt.Fprint(w, setHostsSuccessXML())
		default:
			t.Fatalf("unexpected command: %s", command)
		}
	}))
	defer server.Close()

	record := func(hostname, address string, ttl int) map[string]interface{} {
		return map[string]interface{}{"hostname": hostname, "type": "A", "address": address, "mx_pref": 10, "ttl": ttl}
	}
	previous := []interface{}{record("@", "1.1.1.1", 1800)}
	current := []interface{}{record("@", "1.1.1.1", 1800), record("api", "3.3.3.3", 300)}

	diags := updateRecordsMerge(context.Background(), "test.com", nil, previous, current, newTestClient(server.URL))
	assert.False(t, diags.HasError(), "%v", diags)
	assert.Equal(t, []string{
		"api A 4.4.4.4 ttl=1800",
		"@ A 1.1.1.1 ttl=1800",
		"api A 3.3.3.3 ttl=300",
	}, written)
}

func TestMergeRecords(t *testing.T) {
	cases := []struct {
		name       string
		live       []namecheap.DomainsDNSHostRecordDetailed
		previous   []namecheap.DomainsDNSHostRecord
		configured []namecheap.DomainsDNSHostRecord
		want       []string
	}{
		{
			// Create after `terraform state rm`: the declared rows are live but
			// not in state. They are the same rows, so they are adopted, not
			// refused or appended a second time.
			name: "adopts_live_rows_matching_config",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("@", "A", "1.1.1.1", 10, 1800),
				detailedRecord("api", "A", "3.3.3.3", 10, 1800),
			},
			configured: []namecheap.DomainsDNSHostRecord{
				configuredRecord("@", "A", "1.1.1.1", 10, 1800),
				configuredRecord("api", "A", "3.3.3.3", 10, 300),
			},
			want: []string{
				"@ A 1.1.1.1 mx=10 ttl=1800",
				"api A 3.3.3.3 mx=10 ttl=300",
			},
		},
		{
			// A row added to config that already exists live; rows this
			// resource never declared (another resource's MX, the other value
			// of a multi-value record) pass through untouched.
			name: "keeps_undeclared_rows_while_adopting",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("@", "A", "1.1.1.1", 10, 1800),
				detailedRecord("mail", "MX", "mx1.example.com.", 10, 1800),
				detailedRecord("api", "A", "3.3.3.3", 10, 1800),
				detailedRecord("api", "A", "4.4.4.4", 10, 1800),
			},
			previous: []namecheap.DomainsDNSHostRecord{
				configuredRecord("@", "A", "1.1.1.1", 10, 1800),
			},
			configured: []namecheap.DomainsDNSHostRecord{
				configuredRecord("@", "A", "1.1.1.1", 10, 1800),
				configuredRecord("api", "A", "3.3.3.3", 10, 300),
			},
			want: []string{
				"mail MX mx1.example.com. mx=10 ttl=1800",
				"api A 4.4.4.4 mx=10 ttl=1800",
				"@ A 1.1.1.1 mx=10 ttl=1800",
				"api A 3.3.3.3 mx=10 ttl=300",
			},
		},
		{
			name: "deletes_previous_row_removed_from_config",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("@", "A", "1.1.1.1", 10, 1800),
				detailedRecord("api", "A", "3.3.3.3", 10, 300),
			},
			previous: []namecheap.DomainsDNSHostRecord{
				configuredRecord("@", "A", "1.1.1.1", 10, 1800),
				configuredRecord("api", "A", "3.3.3.3", 10, 300),
			},
			configured: []namecheap.DomainsDNSHostRecord{
				configuredRecord("@", "A", "1.1.1.1", 10, 1800),
			},
			want: []string{"@ A 1.1.1.1 mx=10 ttl=1800"},
		},
		{
			name: "updates_ttl_in_place",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("api", "A", "3.3.3.3", 10, 300),
			},
			previous:   []namecheap.DomainsDNSHostRecord{configuredRecord("api", "A", "3.3.3.3", 10, 300)},
			configured: []namecheap.DomainsDNSHostRecord{configuredRecord("api", "A", "3.3.3.3", 10, 60)},
			want:       []string{"api A 3.3.3.3 mx=10 ttl=60"},
		},
		{
			// The new address is already live as a hand-made row: the old
			// address goes, the new one is adopted, one copy remains.
			name: "address_change_onto_existing_row",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("api", "A", "1.1.1.1", 10, 1800),
				detailedRecord("api", "A", "2.2.2.2", 10, 1800),
			},
			previous:   []namecheap.DomainsDNSHostRecord{configuredRecord("api", "A", "1.1.1.1", 10, 1800)},
			configured: []namecheap.DomainsDNSHostRecord{configuredRecord("api", "A", "2.2.2.2", 10, 1800)},
			want:       []string{"api A 2.2.2.2 mx=10 ttl=1800"},
		},
		{
			// A zone left with two copies of a declared row (the old append
			// bug) ends with one.
			name: "collapses_live_duplicates_of_a_declared_row",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("api", "A", "3.3.3.3", 10, 1800),
				detailedRecord("api", "A", "3.3.3.3", 10, 1800),
			},
			configured: []namecheap.DomainsDNSHostRecord{configuredRecord("api", "A", "3.3.3.3", 10, 300)},
			want:       []string{"api A 3.3.3.3 mx=10 ttl=300"},
		},
		{
			// Duplicates nobody declared are not this resource's to fix.
			name: "keeps_undeclared_duplicates",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("x", "A", "9.9.9.9", 10, 1800),
				detailedRecord("x", "A", "9.9.9.9", 10, 1800),
			},
			configured: []namecheap.DomainsDNSHostRecord{configuredRecord("api", "A", "3.3.3.3", 10, 1800)},
			want: []string{
				"x A 9.9.9.9 mx=10 ttl=1800",
				"x A 9.9.9.9 mx=10 ttl=1800",
				"api A 3.3.3.3 mx=10 ttl=1800",
			},
		},
		{
			// Namecheap returns hostname-valued addresses with a trailing dot;
			// a config without it still names the same row.
			name: "adopts_cname_configured_without_trailing_dot",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("www", "CNAME", "target.example.com.", 10, 1800),
			},
			configured: []namecheap.DomainsDNSHostRecord{configuredRecord("www", "CNAME", "target.example.com", 10, 1800)},
			want:       []string{"www CNAME target.example.com mx=10 ttl=1800"},
		},
		{
			// mx_pref is not part of a row's identity: the config value wins.
			name: "config_mx_pref_wins_on_adoption",
			live: []namecheap.DomainsDNSHostRecordDetailed{
				detailedRecord("@", "MX", "mx1.example.com.", 20, 1800),
			},
			configured: []namecheap.DomainsDNSHostRecord{configuredRecord("@", "MX", "mx1.example.com", 10, 1800)},
			want:       []string{"@ MX mx1.example.com mx=10 ttl=1800"},
		},
		{
			name:       "empty_live_zone",
			configured: []namecheap.DomainsDNSHostRecord{configuredRecord("api", "A", "3.3.3.3", 10, 1800)},
			want:       []string{"api A 3.3.3.3 mx=10 ttl=1800"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeRecords(tc.live, tc.previous, tc.configured)
			require.NoError(t, err)
			assert.Equal(t, tc.want, zoneRows(got))
		})
	}
}

// A configured CAA value that cannot be parsed fails before anything is
// written, whether or not the zone has live records to compare it with.
func TestMergeRecords_InvalidCAAReturnsError(t *testing.T) {
	configured := []namecheap.DomainsDNSHostRecord{configuredRecord("@", "CAA", "random", 10, 1800)}

	for name, live := range map[string][]namecheap.DomainsDNSHostRecordDetailed{
		"live_zone":  {detailedRecord("@", "A", "1.1.1.1", 10, 1800)},
		"empty_zone": nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := mergeRecords(live, nil, configured)
			assert.Error(t, err)
		})
	}
}
