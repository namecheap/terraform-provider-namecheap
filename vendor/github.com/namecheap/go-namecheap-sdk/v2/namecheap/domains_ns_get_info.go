package namecheap

import (
	"encoding/xml"
	"fmt"
)

type NameserversGetInfoResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *NameserversGetInfoCommandResponse `xml:"CommandResponse"`
}

type NameserversGetInfoCommandResponse struct {
	DomainNameserverInfoResult *DomainNSInfoResult `xml:"DomainNSInfoResult"`
}

type DomainNSInfoResult struct {
	Domain             *string `xml:"Domain,attr"`
	Nameserver         *string `xml:"Nameserver,attr"`
	IP                 *string `xml:"IP,attr"`
	NameserverStatuses struct {
		Nameservers *[]string `xml:"Status"`
	} `xml:"NameserverStatuses"`
}

func (s *DomainsNSService) GetInfo(SLD string, TLD string, Nameserver string) (*NameserversGetInfoCommandResponse, error) {
	var response NameserversGetInfoResponse

	params := map[string]string{
		"Command":    "namecheap.domains.ns.getInfo",
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
