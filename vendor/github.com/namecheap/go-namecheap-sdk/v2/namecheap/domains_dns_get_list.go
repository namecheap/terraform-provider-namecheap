package namecheap

import (
	"encoding/xml"
	"fmt"
)

type DomainsDNSGetListResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *DomainsDNSGetListCommandResponse `xml:"CommandResponse"`
}

type DomainsDNSGetListCommandResponse struct {
	DomainDNSGetListResult *DomainDNSGetListResult `xml:"DomainDNSGetListResult"`
}

type DomainDNSGetListResult struct {
	Domain         *string   `xml:"Domain,attr"`
	IsUsingOurDNS  *bool     `xml:"IsUsingOurDNS,attr"`
	IsPremiumDNS   *bool     `xml:"IsPremiumDNS,attr"`
	IsUsingFreeDNS *bool     `xml:"IsUsingFreeDNS,attr"`
	Nameservers    *[]string `xml:"Nameserver"`
}

func (d DomainDNSGetListResult) String() string {
	return fmt.Sprintf("{Domain: %s, IsUsingOurDNS: %t, IsPremiumDNS: %t, IsUsingFreeDNS: %t, Nameservers: %v}",
		*d.Domain, *d.IsUsingOurDNS, *d.IsPremiumDNS, *d.IsUsingFreeDNS, *d.Nameservers,
	)
}

// GetList gets a list of DNS servers associated with the requested domain
//
// Namecheap doc: https://www.namecheap.com/support/api/methods/domains-dns/get-list/
func (dds *DomainsDNSService) GetList(domain string) (*DomainsDNSGetListCommandResponse, error) {
	var response DomainsDNSGetListResponse

	params := map[string]string{
		"Command": "namecheap.domains.dns.getList",
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

		if *apiErr.Number != "2019166" {
			return nil, fmt.Errorf("%s (%s)", *apiErr.Message, *apiErr.Number)
		}

		var domainInfo *DomainsGetInfoCommandResponse
		domainInfo, err = dds.client.Domains.GetInfo(domain)
		if err != nil {
			return nil, err
		}

		IsUsingFreeDNS := *domainInfo.DomainDNSGetListResult.DnsDetails.ProviderType == "FreeDNS"

		return &DomainsDNSGetListCommandResponse{
			DomainDNSGetListResult: &DomainDNSGetListResult{
				Domain:         domainInfo.DomainDNSGetListResult.DomainName,
				IsUsingOurDNS:  domainInfo.DomainDNSGetListResult.DnsDetails.IsUsingOurDNS,
				IsPremiumDNS:   domainInfo.DomainDNSGetListResult.PremiumDnsSubscription.IsActive,
				IsUsingFreeDNS: &IsUsingFreeDNS,
				Nameservers:    domainInfo.DomainDNSGetListResult.DnsDetails.Nameservers,
			},
		}, nil
	}

	return response.CommandResponse, nil
}
