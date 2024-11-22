package namecheap

import (
	"encoding/xml"
	"fmt"
)

type NameserversDeleteResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *NameserversCreateCommandResponse `xml:"CommandResponse"`
}

type NameserversDeleteCommandResponse struct {
	DomainNameserverDeleteResult *DomainsNSDeleteResult `xml:"DomainNSDeleteResult"`
}

type DomainsNSDeleteResult struct {
	Domain     *string `xml:"Domain,attr"`
	Nameserver *string `xml:"Nameserver,attr"`
	IsSuccess  *bool   `xml:"IsSuccess,attr"`
}

func (s *DomainsNSService) Delete(SLD string, TLD string, Nameserver string) (*NameserversCreateCommandResponse, error) {
	var response NameserversDeleteResponse

	params := map[string]string{
		"Command":    "namecheap.domains.ns.delete",
		"SLD":        SLD,
		"TLD":        TLD,
		"Nameserver": Nameserver,
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
