package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRecordsImportID(t *testing.T) {
	cases := []struct {
		id            string
		wantDomain    string
		wantWholeZone bool
		wantErr       string
	}{
		{id: "example.com", wantDomain: "example.com"},
		{id: "example.com/zone", wantDomain: "example.com", wantWholeZone: true},
		{id: "Example.COM", wantDomain: "example.com"},
		{id: "Example.COM/zone", wantDomain: "example.com", wantWholeZone: true},
		{id: "example.com/", wantErr: "unexpected import ID"},
		{id: "example.com/ZONE", wantErr: "unexpected import ID"},
		{id: "example.com/records", wantErr: "unexpected import ID"},
		{id: "example.com/zone/extra", wantErr: "unexpected import ID"},
		{id: "www.example.com", wantErr: "contains a subdomain"},
		{id: "/zone", wantErr: "must not be empty"},
		{id: "", wantErr: "must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			domain, wholeZone, err := parseRecordsImportID(tc.id)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantDomain, domain)
			assert.Equal(t, tc.wantWholeZone, wholeZone)
		})
	}
}

// importRecords runs the importer for id and then the read Terraform runs
// after every import, against a zone on Namecheap DNS that holds hosts, or on
// custom nameservers when nameservers is set.
func importRecords(t *testing.T, id, emailType string, hosts []hostEntry, nameservers []string) *schema.ResourceData {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.FormValue("Command") {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(len(nameservers) == 0, nameservers))
		case "namecheap.domains.dns.getHosts":
			_, _ = fmt.Fprint(w, getHostsXML(emailType, hosts))
		}
	}))
	t.Cleanup(server.Close)

	client := newTestClient(server.URL)
	data := resourceNamecheapDomainRecords().TestResourceData()
	data.SetId(id)

	imported, err := importRecordsState(context.TODO(), data, client)
	require.NoError(t, err)
	require.Len(t, imported, 1)

	diags := resourceRecordRead(context.TODO(), imported[0], client)
	require.False(t, diags.HasError(), "%v", diags)
	assert.Empty(t, diags, "an import reads; it has nothing to warn about")
	return imported[0]
}

func TestSuppressDomainCaseDiff(t *testing.T) {
	assert.True(t, suppressDomainCaseDiff("domain", "example.com", "Example.COM", nil))
	assert.True(t, suppressDomainCaseDiff("domain", "Example.com", "example.com", nil))
	assert.False(t, suppressDomainCaseDiff("domain", "example.com", "example.org", nil))
	assert.False(t, suppressDomainCaseDiff("domain", "", "example.com", nil), "a create is never suppressed")
}

func TestImportRecordsState(t *testing.T) {
	zone := []hostEntry{
		{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
		{Name: "home", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 60},
		{Name: "@", Type: "MX", Address: "mail.test.com.", MXPref: 10, TTL: 1800},
	}
	customNameservers := []string{"ns1.custom.com", "ns2.custom.com"}

	// The plain ID is the fix for records an import stored and the next apply
	// deleted: a record nobody declared never enters state.
	t.Run("the plain ID stores the settings and no records", func(t *testing.T) {
		data := importRecords(t, "test.com", "MX", zone, nil)
		assert.Equal(t, "test.com", data.Id())
		assert.Equal(t, "test.com", data.Get("domain"))
		assert.Equal(t, ncModeMerge, data.Get("mode"))
		assert.Equal(t, "MX", data.Get("email_type"))
		assert.Equal(t, 0, data.Get("record").(*schema.Set).Len())
		assert.Equal(t, 0, data.Get("nameservers").(*schema.Set).Len())
	})

	t.Run("the /zone ID stores every record", func(t *testing.T) {
		data := importRecords(t, "test.com/zone", "MX", zone, nil)
		assert.Equal(t, "test.com", data.Id(), "the scope is an import option, not part of the resource ID")
		assert.Equal(t, "test.com", data.Get("domain"))
		assert.Equal(t, ncModeMerge, data.Get("mode"))
		assert.Equal(t, "MX", data.Get("email_type"))
		assert.Equal(t, len(zone), data.Get("record").(*schema.Set).Len())
	})

	t.Run("the /zone ID leaves the default parking records out", func(t *testing.T) {
		data := importRecords(t, "test.com/zone", "NONE", []hostEntry{
			{Name: "www", Type: "CNAME", Address: "parkingpage.namecheap.com.", MXPref: 10, TTL: 1800},
			{Name: "@", Type: "URL", Address: "http://www.test.com/?from=@", MXPref: 10, TTL: 1800},
			{Name: "api", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
		}, nil)
		assert.Equal(t, 1, data.Get("record").(*schema.Set).Len())
	})

	// Nameservers follow the rule for records: in MERGE the ones in state are
	// the ones the resource owns, so the plain ID stores none.
	t.Run("the plain ID stores no custom nameservers", func(t *testing.T) {
		data := importRecords(t, "test.com", "", nil, customNameservers)
		assert.Equal(t, "test.com", data.Id())
		assert.Equal(t, ncModeMerge, data.Get("mode"))
		assert.Equal(t, 0, data.Get("nameservers").(*schema.Set).Len())
		assert.Equal(t, 0, data.Get("record").(*schema.Set).Len())
		assert.Equal(t, "", data.Get("email_type"))
	})

	t.Run("the /zone ID stores every custom nameserver", func(t *testing.T) {
		data := importRecords(t, "test.com/zone", "", nil, customNameservers)
		assert.Equal(t, "test.com", data.Id())
		assert.Equal(t, ncModeMerge, data.Get("mode"))
		assert.ElementsMatch(t, customNameservers, convertInterfacesToString(data.Get("nameservers").(*schema.Set).List()))
		assert.Equal(t, 0, data.Get("record").(*schema.Set).Len())
		assert.Equal(t, "", data.Get("email_type"))
	})

	// Everything the plain ID stores comes from the ID, and the read Terraform
	// runs after the import reports a domain it cannot reach.
	t.Run("the plain ID makes no API call", func(t *testing.T) {
		data := resourceNamecheapDomainRecords().TestResourceData()
		data.SetId("test.com")
		imported, err := importRecordsState(context.TODO(), data, newTestClient("http://127.0.0.1:0"))
		require.NoError(t, err)
		require.Len(t, imported, 1)
	})

	for _, id := range []string{"Test.com", "Test.com/zone"} {
		t.Run("an upper-case domain is stored in lower case by "+id, func(t *testing.T) {
			data := importRecords(t, id, "NONE", zone, nil)
			assert.Equal(t, "test.com", data.Id())
			assert.Equal(t, "test.com", data.Get("domain"))
		})
	}

	// An import is often the first call a new setup makes, so it is where a
	// wrong key or a missing IP whitelist entry shows up. The /zone ID reads in
	// the importer; after the plain ID the read that follows reports the same.
	apiErrors := []struct {
		code, message, want string
	}{
		{"1011102", "API Key is invalid or API access has not been enabled", "IP not whitelisted"},
		{"2019166", "Domain not found", "Verify the domain name is spelled correctly"},
		{"2016166", "Domain is not associated with your account", "Domain not associated with this account"},
	}
	for _, tc := range apiErrors {
		t.Run("API error "+tc.code+" keeps its remediation text", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, apiErrorXML(tc.code, tc.message))
			}))
			t.Cleanup(server.Close)

			data := resourceNamecheapDomainRecords().TestResourceData()
			data.SetId("test.com/zone")
			_, err := importRecordsState(context.TODO(), data, newTestClient(server.URL))
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
			assert.ErrorContains(t, err, tc.code)
		})
	}

	t.Run("an unknown scope fails before any API call", func(t *testing.T) {
		data := resourceNamecheapDomainRecords().TestResourceData()
		data.SetId("test.com/records")
		_, err := importRecordsState(context.TODO(), data, newTestClient("http://127.0.0.1:0"))
		assert.ErrorContains(t, err, `unexpected import ID "test.com/records"`)
	})
}
