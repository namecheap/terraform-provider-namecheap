package namecheap

import (
	"encoding/xml"
	"fmt"
)

type NameserversCreateResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *NameserversCreateCommandResponse `xml:"CommandResponse"`
}

type NameserversCreateCommandResponse struct {
	DomainNameserverInfoResult *DomainsNSCreateResult `xml:"DomainNSCreateResult"`
}

type DomainsNSCreateResult struct {
	Domain     *string `xml:"Domain,attr"`
	Nameserver *string `xml:"Nameserver,attr"`
	IP         *string `xml:"IP,attr"`
	IsSuccess  *bool   `xml:"IsSuccess,attr"`
}

func (s *DomainsNSService) Create(SLD string, TLD string, Nameserver string, IPAddress string) (*NameserversCreateCommandResponse, error) {
	var response NameserversCreateResponse

	params := map[string]string{
		"Command":    "namecheap.domains.ns.create",
		"SLD":        SLD,
		"TLD":        TLD,
		"Nameserver": Nameserver,
		"IP":         IPAddress,
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
