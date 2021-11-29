package namecheap

import (
	"encoding/xml"
	"fmt"
)

type DomainsGetInfoResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *DomainsGetInfoCommandResponse `xml:"CommandResponse"`
}

type DomainsGetInfoCommandResponse struct {
	DomainDNSGetListResult *DomainsGetInfoResult `xml:"DomainGetInfoResult"`
}

type DomainsGetInfoResult struct {
	DomainName             *string                 `xml:"DomainName,attr"`
	IsPremium              *bool                   `xml:"IsPremium,attr"`
	PremiumDnsSubscription *PremiumDnsSubscription `xml:"PremiumDnsSubscription"`
	DnsDetails             *DnsDetails             `xml:"DnsDetails"`
}

type PremiumDnsSubscription struct {
	IsActive *bool `xml:"IsActive"`
}

type DnsDetails struct {
	ProviderType  *string   `xml:"ProviderType,attr"`
	IsUsingOurDNS *bool     `xml:"IsUsingOurDNS,attr"`
	Nameservers   *[]string `xml:"Nameserver"`
}

func (ds *DomainsService) GetInfo(domain string) (*DomainsGetInfoCommandResponse, error) {
	var response DomainsGetInfoResponse

	params := map[string]string{
		"Command":    "namecheap.domains.getInfo",
		"DomainName": domain,
		"HostName":   domain,
	}

	_, err := ds.client.DoXML(params, &response)
	if err != nil {
		return nil, err
	}
	if response.Errors != nil && len(*response.Errors) > 0 {
		apiErr := (*response.Errors)[0]

		return nil, fmt.Errorf("%s (%s)", *apiErr.Message, *apiErr.Number)
	}

	return response.CommandResponse, nil
}
