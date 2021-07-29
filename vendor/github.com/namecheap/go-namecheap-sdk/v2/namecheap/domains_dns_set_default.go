package namecheap

import (
	"encoding/xml"
	"fmt"
)

type DomainsDNSSetDefaultResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *DomainsDNSSetDefaultCommandResponse `xml:"CommandResponse"`
}

type DomainsDNSSetDefaultCommandResponse struct {
	DomainDNSSetDefaultResult *DomainDNSSetDefaultResult `xml:"DomainDNSSetDefaultResult"`
}

type DomainDNSSetDefaultResult struct {
	Domain  *string `xml:"Domain,attr"`
	Updated *bool   `xml:"Updated,attr"`
}

func (d DomainDNSSetDefaultResult) String() string {
	return fmt.Sprintf("{Domain: %s, Updated: %t}", *d.Domain, *d.Updated)
}

// SetDefault sets domain to use our default DNS servers.
// Required for free services like Host record management, URL forwarding, email forwarding, dynamic dns and other value added services.
//
// Namecheap doc: https://www.namecheap.com/support/api/methods/domains-dns/set-default/
func (dds *DomainsDNSService) SetDefault(domain string) (*DomainsDNSSetDefaultCommandResponse, error) {
	var response DomainsDNSSetDefaultResponse

	params := map[string]string{
		"Command": "namecheap.domains.dns.setDefault",
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
	if response.Errors != nil && len(*response.Errors) > 0 {
		apiErr := (*response.Errors)[0]
		return nil, fmt.Errorf("%s (%s)", *apiErr.Message, *apiErr.Number)
	}

	return response.CommandResponse, nil
}
