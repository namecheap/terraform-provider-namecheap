package namecheap

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type DomainsDNSSetCustomResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *DomainsDNSSetCustomCommandResponse `xml:"CommandResponse"`
}

type DomainsDNSSetCustomCommandResponse struct {
	DomainDNSSetCustomResult *DomainsDNSSetCustomResult `xml:"DomainDNSSetCustomResult"`
}

type DomainsDNSSetCustomResult struct {
	Domain  *string `xml:"Domain,attr"`
	Updated *bool   `xml:"Updated,attr"`
}

func (d DomainsDNSSetCustomResult) String() string {
	return fmt.Sprintf("{Domain: %s, Updated: %t}", *d.Domain, *d.Updated)
}

// SetCustom sets domain to use custom DNS servers
// NOTE: Services like URL forwarding, Email forwarding, Dynamic DNS will not work for domains using custom nameservers
//
// Namecheap doc: https://www.namecheap.com/support/api/methods/domains-dns/set-custom/
func (dds *DomainsDNSService) SetCustom(domain string, nameservers []string) (*DomainsDNSSetCustomCommandResponse, error) {
	var response DomainsDNSSetCustomResponse

	params := map[string]string{
		"Command": "namecheap.domains.dns.setCustom",
	}

	parsedDomain, err := ParseDomain(domain)
	if err != nil {
		return nil, err
	}

	params["SLD"] = parsedDomain.SLD
	params["TLD"] = parsedDomain.TLD

	nameserversString, err := validateAndParseCustomNameservers(nameservers)
	if err != nil {
		return nil, err
	}

	params["Nameservers"] = *nameserversString

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

func validateAndParseCustomNameservers(nameservers []string) (*string, error) {
	if len(nameservers) < 2 {
		return nil, fmt.Errorf("invalid nameservers: must contain minimum two items")
	}

	nameserversJoin := strings.Join(nameservers, ",")
	return &nameserversJoin, nil
}
