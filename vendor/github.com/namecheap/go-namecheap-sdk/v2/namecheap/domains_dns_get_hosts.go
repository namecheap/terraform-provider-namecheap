package namecheap

import (
	"encoding/xml"
	"fmt"
)

type DomainsDNSGetHostsResponse struct {
	XMLName xml.Name `xml:"ApiResponse"`
	Errors  []struct {
		Message string `xml:",chardata"`
		Number  string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *DomainsDNSGetHostsCommandResponse `xml:"CommandResponse"`
}

type DomainsDNSGetHostsCommandResponse struct {
	DomainDNSGetHostsResult *DomainDNSGetHostsResult `xml:"DomainDNSGetHostsResult"`
}

type DomainDNSGetHostsResult struct {
	Domain        *string                         `xml:"Domain,attr"`
	EmailType     *string                         `xml:"EmailType,attr"`
	IsUsingOurDNS *bool                           `xml:"IsUsingOurDNS,attr"`
	Hosts         *[]DomainsDNSHostRecordDetailed `xml:"host"`
}

type DomainsDNSHostRecordDetailed struct {
	HostId             *int    `xml:"HostId,attr"`
	Name               *string `xml:"Name,attr"`
	Type               *string `xml:"Type,attr"`
	Address            *string `xml:"Address,attr"`
	MXPref             *int    `xml:"MXPref,attr"`
	TTL                *int    `xml:"TTL,attr"`
	AssociatedAppTitle *string `xml:"AssociatedAppTitle,attr"`
	FriendlyName       *string `xml:"FriendlyName,attr"`
	IsActive           *bool   `xml:"IsActive,attr"`
	IsDDNSEnabled      *bool   `xml:"IsDDNSEnabled,attr"`
}

func (d DomainsDNSHostRecordDetailed) String() string {
	return fmt.Sprintf("{HostId: %d, Name: %s, Type: %s, Address: %s, MXPref: %d, TTL: %d, AssociatedAppTitle: %s, FriendlyName: %s, IsActive: %t, IsDDNSEnabled: %t}",
		*d.HostId, *d.Name, *d.Type, *d.Address, *d.MXPref, *d.TTL, *d.AssociatedAppTitle, *d.FriendlyName, *d.IsActive, *d.IsDDNSEnabled)
}

// GetHosts retrieves DNS host record settings for the requested domain.
//
// Namecheap doc: https://www.namecheap.com/support/api/methods/domains-dns/get-hosts/
func (dds *DomainsDNSService) GetHosts(domain string) (*DomainsDNSGetHostsCommandResponse, error) {
	var response DomainsDNSGetHostsResponse

	params := map[string]string{
		"Command": "namecheap.domains.dns.getHosts",
	}

	parsedDomain, err := ParseDomain(domain)
	if err != nil {
		return nil, err
	}

	params["SLD"] = parsedDomain.SLD
	params["TLD"] = parsedDomain.TLD

	_, err = dds.client.DoXML(params, &response)
	if err != nil {
		return nil, err
	}
	if len(response.Errors) > 0 {
		apiErr := response.Errors[0]
		return nil, fmt.Errorf("%s (%s)", apiErr.Message, apiErr.Number)
	}

	return response.CommandResponse, nil
}
