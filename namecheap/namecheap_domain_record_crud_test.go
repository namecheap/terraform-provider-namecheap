package namecheap_provider

import (
	"fmt"
	"strings"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// newTestClient creates a namecheap client pointed at the given test server URL
func newTestClient(baseURL string) *namecheap.Client {
	client := namecheap.NewClient(&namecheap.ClientOptions{
		UserName:   "testuser",
		ApiUser:    "testuser",
		ApiKey:     "testapikey",
		ClientIp:   "127.0.0.1",
		UseSandbox: false,
	})
	client.BaseURL = baseURL
	return client
}

// getHostsXML generates a GetHosts API response XML
func getHostsXML(emailType string, hosts []hostEntry) string {
	var hostLines []string
	for i, h := range hosts {
		hostLines = append(hostLines, fmt.Sprintf(
			`<host HostId="%d" Name="%s" Type="%s" Address="%s" MXPref="%d" TTL="%d" AssociatedAppTitle="" FriendlyName="" IsActive="true" IsDDNSEnabled="false" />`,
			i+1, h.Name, h.Type, h.Address, h.MXPref, h.TTL,
		))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSGetHostsResult Domain="test.com" EmailType="%s" IsUsingOurDNS="true">
      %s
    </DomainDNSGetHostsResult>
  </CommandResponse>
</ApiResponse>`, emailType, strings.Join(hostLines, "\n      "))
}

// setHostsSuccessXML generates a SetHosts success API response XML
func setHostsSuccessXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSSetHostsResult Domain="test.com" IsSuccess="true" />
  </CommandResponse>
</ApiResponse>`
}

// getListXML generates a GetList API response XML for nameservers
func getListXML(isUsingOurDNS bool, nameservers []string) string {
	var nsLines []string
	for _, ns := range nameservers {
		nsLines = append(nsLines, fmt.Sprintf(`<Nameserver>%s</Nameserver>`, ns))
	}
	isUsingDNSStr := "false"
	if isUsingOurDNS {
		isUsingDNSStr = "true"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSGetListResult Domain="test.com" IsUsingOurDNS="%s" IsPremiumDNS="false" IsUsingFreeDNS="false">
      %s
    </DomainDNSGetListResult>
  </CommandResponse>
</ApiResponse>`, isUsingDNSStr, strings.Join(nsLines, "\n      "))
}

// setCustomSuccessXML generates a SetCustom success API response XML
func setCustomSuccessXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSSetCustomResult Domain="test.com" Updated="true" />
  </CommandResponse>
</ApiResponse>`
}

// setDefaultSuccessXML generates a SetDefault success API response XML
func setDefaultSuccessXML() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse>
    <DomainDNSSetDefaultResult Domain="test.com" Updated="true" />
  </CommandResponse>
</ApiResponse>`
}

type hostEntry struct {
	Name    string
	Type    string
	Address string
	MXPref  int
	TTL     int
}

// apiErrorXML generates an API error response
func apiErrorXML(code string, message string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors>
    <Error Number="%s">%s</Error>
  </Errors>
</ApiResponse>`, code, message)
}
