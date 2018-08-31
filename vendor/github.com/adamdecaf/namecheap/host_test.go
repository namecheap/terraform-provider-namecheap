package namecheap

import (
	// "github.com/pearkes/dnsimple/testutil"
	// "strings"
	"testing"
)

func TestHost__GetHosts(t *testing.T) {
	if !clientEnabled {
		t.Skip("namecheap credentials not configured")
	}

	recs, err := testClient.GetHosts(testDomain)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Fatal("expected records")
	}
	var found bool
	for i := range recs {
		if recs[i].Name == "www" {
			found = true
			diff := recs[i].diff(testRecord)
			_, exists := diff["Id"]
			if len(diff) > 0 && !exists { // expected 'Id' different, but it's something else
				for k, v := range diff {
					t.Errorf("%s = %q\n", k, v)
				}
			}
		}
	}
	if !found {
		t.Error("didn't find record")
	}
}

// func (s *S) Test_SetHosts(c *gocheck.C) {
// 	testServer.Response(200, nil, hostSetExample)
// 	var records []Record

// 	record := Record{
// 		HostName:   "foobar",
// 		RecordType: "CNAME",
// 		Address:    "test.domain.",
// 	}

// 	records = append(records, record)

// 	_, err := s.client.SetHosts("example.com", records)

// 	_ = testServer.WaitRequest()

// 	c.Assert(err, gocheck.IsNil)
// }

// func (s *S) Test_SetHosts_fail(c *gocheck.C) {
// 	testServer.Response(200, nil, hostExampleError)

// 	var records []Record

// 	record := Record{
// 		HostName:   "foobar",
// 		RecordType: "CNAME",
// 		Address:    "test.domain.",
// 	}

// 	records = append(records, record)

// 	_, err := s.client.SetHosts("example.com", records)

// 	_ = testServer.WaitRequest()

// 	c.Assert(strings.Contains(err.Error(), "2019166"), gocheck.Equals, true)
// }

var hostExampleError = `
<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
    <Errors>
        <Error Number="2019166">The domain (huxtest3.com) doesn't seem to be associated with your account.</Error>

	</Errors>
	<Warnings />
	<RequestedCommand>namecheap.domains.dns.setHosts</RequestedCommand>
	<CommandResponse Type="namecheap.domains.dns.setHosts">
		<DomainDNSSetHostsResult Domain="huxtest3.com" EmailType="" IsSuccess="false">
			<Warnings />

		</DomainDNSSetHostsResult>
	</CommandResponse>
	<Server>PHX01SBAPI01</Server>
	<GMTTimeDifference>--5:00</GMTTimeDifference>
	<ExecutionTime>0.025</ExecutionTime>

</ApiResponse>
`

var hostSetExample = `
<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
    <Errors />
    <Warnings />
    <RequestedCommand>namecheap.domains.dns.setHosts</RequestedCommand>
    <CommandResponse Type="namecheap.domains.dns.setHosts">
        <DomainDNSSetHostsResult Domain="example.com" IsSuccess="true">
            <Warnings />

        </DomainDNSSetHostsResult>

    </CommandResponse>
    <Server>PHX01SBAPI01</Server>
    <GMTTimeDifference>--5:00</GMTTimeDifference>
    <ExecutionTime>0.498</ExecutionTime>

</ApiResponse>`

var hostGetExample = `
<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
    <Errors />
    <Warnings />
    <RequestedCommand>namecheap.domains.dns.getHosts</RequestedCommand>
    <CommandResponse Type="namecheap.domains.dns.getHosts">
        <DomainDNSGetHostsResult Domain="huxtest2.com" EmailType="FWD" IsUsingOurDNS="true">
            <host HostId="216107" Name="foobar" Type="CNAME" Address="test.domain." MXPref="10" TTL="1800" AssociatedAppTitle="" FriendlyName="" IsActive="true" IsDDNSEnabled="false" />

        </DomainDNSGetHostsResult>

    </CommandResponse>
    <Server>PHX01SBAPI01</Server>
    <GMTTimeDifference>--5:00</GMTTimeDifference>
    <ExecutionTime>0.704</ExecutionTime>

</ApiResponse>`
