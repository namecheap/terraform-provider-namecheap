package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

func TestHostRecordMailModeWarning(t *testing.T) {
	assert.Empty(t, hostRecordMailModeWarning("test.com", nil))
	assert.Empty(t, hostRecordMailModeWarning("test.com",
		&namecheap.RecordChangeResult{EmailTypeBefore: namecheap.EmailTypeMX, EmailType: namecheap.EmailTypeMX}))

	diags := hostRecordMailModeWarning("test.com",
		&namecheap.RecordChangeResult{EmailTypeBefore: namecheap.EmailTypeMX, EmailType: namecheap.EmailTypeMXE})
	if assert.Len(t, diags, 1) {
		assert.Equal(t, diag.Warning, diags[0].Severity)
		assert.Equal(t, "This write moved test.com's mail mode (EmailType) from MX to MXE", diags[0].Summary)
		assert.Contains(t, diags[0].Detail, "email_type")
	}
}

// TestDomainHostRecordCreateWarnsWhenMXEMovesTheMailMode is the second half of
// a type change from MX to MXE, which is ForceNew: the delete of the MX record
// leaves the zone on MX with no MX record, and the create of the MXE record
// moves it to MXE, where Namecheap serves it. The create must say so.
func TestDomainHostRecordCreateWarnsWhenMXEMovesTheMailMode(t *testing.T) {
	zone := &fakeHostsZone{emailType: namecheap.EmailTypeMX}
	server := httptest.NewServer(zone)
	defer server.Close()

	resource := resourceNamecheapDomainHostRecord()
	data := schema.TestResourceDataRaw(t, resource.Schema, map[string]interface{}{
		"domain":   "test.com",
		"hostname": "@",
		"type":     "MXE",
		"address":  "10.0.0.5",
	})

	diags := resource.CreateContext(context.Background(), data, newTestClient(server.URL))
	assert.False(t, diags.HasError(), "%v", diags)
	assert.Equal(t, namecheap.EmailTypeMXE, zone.emailType)
	if assert.Len(t, diags, 1) {
		assert.Equal(t, diag.Warning, diags[0].Severity)
		assert.Equal(t, "This write moved test.com's mail mode (EmailType) from MX to MXE", diags[0].Summary)
	}
}

// fakeHostsZone is a stateful getHosts/setHosts fake: setHosts stores the rows
// it is sent and, when given, the EmailType, so the SDK's verifying read sees
// its own write.
type fakeHostsZone struct {
	mu        sync.Mutex
	emailType string
	hosts     []hostEntry
}

func (z *fakeHostsZone) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	z.mu.Lock()
	defer z.mu.Unlock()

	switch r.FormValue("Command") {
	case "namecheap.domains.dns.getHosts":
		_, _ = fmt.Fprint(w, getHostsXML(z.emailType, z.hosts))
	case "namecheap.domains.dns.setHosts":
		z.hosts = nil
		for i := 1; r.FormValue(fmt.Sprintf("RecordType%d", i)) != ""; i++ {
			mxPref, _ := strconv.Atoi(r.FormValue(fmt.Sprintf("MXPref%d", i)))
			ttl, _ := strconv.Atoi(r.FormValue(fmt.Sprintf("TTL%d", i)))
			z.hosts = append(z.hosts, hostEntry{
				Name:    r.FormValue(fmt.Sprintf("HostName%d", i)),
				Type:    r.FormValue(fmt.Sprintf("RecordType%d", i)),
				Address: r.FormValue(fmt.Sprintf("Address%d", i)),
				MXPref:  mxPref,
				TTL:     ttl,
			})
		}
		if emailType := r.FormValue("EmailType"); emailType != "" {
			z.emailType = emailType
		}
		_, _ = fmt.Fprint(w, setHostsSuccessXML())
	default:
		http.Error(w, "unexpected command "+r.FormValue("Command"), http.StatusBadRequest)
	}
}
