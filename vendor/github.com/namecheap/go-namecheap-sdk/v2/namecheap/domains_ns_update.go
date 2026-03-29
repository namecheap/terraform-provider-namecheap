package namecheap

import (
	"encoding/xml"
	"fmt"
)

type NameserversUpdateResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *NameserversCreateCommandResponse `xml:"CommandResponse"`
}

type NameserversUpdateCommandResponse struct {
	DomainNameserverUpdateResult *DomainsNSUpdateResult `xml:"DomainNSUpdateResult"`
}

type DomainsNSUpdateResult struct {
	Domain     *string `xml:"Domain,attr"`
	Nameserver *string `xml:"Nameserver,attr"`
	IsSuccess  *bool   `xml:"IsSuccess,attr"`
}

func (s *DomainsNSService) Update(sld, tld, nameserver, oldIP, ip string) (*NameserversCreateCommandResponse, error) {
	var response NameserversUpdateResponse

	params := map[string]string{
		"Command":    "namecheap.domains.ns.update",
		"SLD":        sld,
		"TLD":        tld,
		"Nameserver": nameserver,
		"OldIP":      oldIP,
		"IP":         ip,
	}

	_, err := s.client.DoXML(params, &response)
	if err != nil {
		return nil, err
	}

	if response.Errors != nil && len(*response.Errors) > 0 {
		apiErr := (*response.Errors)[0]
		return nil, fmt.Errorf("%s (%s)", *apiErr.Message, *apiErr.Number)
	}

	return response.CommandResponse, nil
}
