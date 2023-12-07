package namecheap

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	MinTTL int = 60
	MaxTTL int = 60000

	EmailTypeNone    = "NONE"
	EmailTypeMXE     = "MXE"
	EmailTypeMX      = "MX"
	EmailTypeForward = "FWD"
	EmailTypePrivate = "OX"
	EmailTypeGmail   = "GMAIL"

	RecordTypeA      = "A"
	RecordTypeAAAA   = "AAAA"
	RecordTypeAlias  = "ALIAS"
	RecordTypeCAA    = "CAA"
	RecordTypeCNAME  = "CNAME"
	RecordTypeMX     = "MX"
	RecordTypeMXE    = "MXE"
	RecordTypeNS     = "NS"
	RecordTypeTXT    = "TXT"
	RecordTypeURL    = "URL"
	RecordTypeURL301 = "URL301"
	RecordTypeFrame  = "FRAME"
)

var AllowedRecordTypeValues = []string{RecordTypeA, RecordTypeAAAA, RecordTypeAlias, RecordTypeCAA, RecordTypeCNAME, RecordTypeMX, RecordTypeMXE, RecordTypeNS, RecordTypeTXT, RecordTypeURL, RecordTypeURL301, RecordTypeFrame}
var AllowedEmailTypeValues = []string{EmailTypeNone, EmailTypeMXE, EmailTypeMX, EmailTypeForward, EmailTypePrivate, EmailTypeGmail}

var allowedTagValues = []string{"issue", "issuewild", "iodef"}
var validURLProtocolPrefix = regexp.MustCompile("[a-z]+://")

type DomainsDNSHostRecord struct {
	// Sub-domain/hostname to create the record for
	HostName *string
	// Possible values: A, AAAA, ALIAS, CAA, CNAME, MX, MXE, NS, TXT, URL, URL301, FRAME
	RecordType *string
	// Possible values are URL or ClientIp address. The value for this parameter is based on RecordType.
	Address *string
	// MX preference for host. Applicable for MX records only.
	MXPref *uint8
	// Time to live for all record types.Possible values: any value between 60 to 60000
	// Default Value: 1800 (if 0 value has been provided)
	TTL *int
}

type DomainsDNSSetHostsArgs struct {
	// Domain to setHosts
	Domain *string
	// DomainsDNSHostRecord list
	Records *[]DomainsDNSHostRecord
	// Possible values are MXE, MX, FWD, OX, GMAIL or NONE
	// If empty, then this field won't be forwarded
	// Follow https://www.namecheap.com/support/knowledgebase/article.aspx/322/2237/how-can-i-set-up-mx-records-required-for-mail-service/ to read more about email types
	EmailType *string
	// Is an unsigned integer between 0 and 255.
	// The flag value is an 8-bit number, the most significant bit of which indicates the criticality of understanding of a record by a CA.
	// It's recommended to use '0'
	// If nil provided, then this field is ignored
	Flag *uint8
	// A non-zero sequence of US-ASCII letters and numbers in lower case. The tag value can be one of the following values:
	// "issue" — specifies the certification authority that is authorized to issue a certificate for the domain name or subdomain record used in the title.
	// "issuewild" — specifies the certification authority that is allowed to issue a wildcard certificate for the domain name or subdomain record used in the title. The certificate applies to the domain name or subdomain directly and to all its subdomains.
	// "iodef" — specifies the e-mail address or URL (compliant with RFC 5070) a CA should use to notify a client if any issuance policy violation spotted by this CA.
	Tag *string
}

type DomainsDNSSetHostsResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *DomainsDNSSetHostsCommandResponse `xml:"CommandResponse"`
}

type DomainsDNSSetHostsCommandResponse struct {
	DomainDNSSetHostsResult *DomainDNSSetHostsResult `xml:"DomainDNSSetHostsResult"`
}

type DomainDNSSetHostsResult struct {
	Domain    *string `xml:"Domain,attr"`
	IsSuccess *bool   `xml:"IsSuccess,attr"`
}

func (d DomainDNSSetHostsResult) String() string {
	return fmt.Sprintf("{Domain: %s, IsSuccess: %t}", *d.Domain, *d.IsSuccess)
}

// SetHosts sets DNS host records settings for the requested domain
//
// Namecheap doc: https://www.namecheap.com/support/api/methods/domains-dns/set-hosts/
func (dds DomainsDNSService) SetHosts(args *DomainsDNSSetHostsArgs) (*DomainsDNSSetHostsCommandResponse, error) {
	var response DomainsDNSSetHostsResponse

	params := map[string]string{
		"Command": "namecheap.domains.dns.setHosts",
	}

	// validate input arguments
	err := validateDomainsDNSSetHostsArgs(args)
	if err != nil {
		return nil, err
	}

	// parse input arguments
	parsedArgsMap, err := parseDomainsDNSSetHostsArgs(args)
	if err != nil {
		return nil, err
	}

	// merge parsed arguments with params
	for k, v := range *parsedArgsMap {
		params[k] = v
	}

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

func validateDomainsDNSSetHostsArgs(args *DomainsDNSSetHostsArgs) error {
	if args.EmailType != nil && !isValidEmailType(*args.EmailType) {
		return fmt.Errorf("invalid EmailType value: %s", *args.EmailType)
	}

	if args.Tag != nil && !isValidTagValue(*args.Tag) {
		return fmt.Errorf("invalid Tag value: %s", *args.Tag)
	}

	mxRecordsCount := 0
	mxeRecordsCount := 0

	if args.Records != nil {
		for i, record := range *args.Records {
			if record.RecordType == nil {
				return fmt.Errorf("Records[%d].RecordType is required", i)
			}
			if !isValidRecordType(*record.RecordType) {
				return fmt.Errorf("invalid Records[%d].RecordType value: %s", i, *record.RecordType)
			}

			if record.HostName == nil {
				return fmt.Errorf("Records[%d].HostName is required", i)
			}

			if record.Address == nil {
				return fmt.Errorf("Records[%d].Address is required", i)
			}

			if record.TTL != nil && (*record.TTL < MinTTL || *record.TTL > MaxTTL) {
				return fmt.Errorf("invalid Records[%d].TTL value: %d", i, *record.TTL)
			}

			if *record.RecordType == "MX" {
				if record.MXPref == nil {
					return fmt.Errorf("Records[%d].MXPref is nil but required for MX record type", i)
				}
				if args.EmailType == nil {
					return fmt.Errorf("Records[%d].RecordType MX is not allowed for EmailType=nil", i)
				} else if *args.EmailType != "MX" {
					return fmt.Errorf("Records[%d].RecordType MX is not allowed for EmailType=%s", i, *args.EmailType)
				}
				mxRecordsCount++
			} else if *record.RecordType == "MXE" {
				if args.EmailType == nil {
					return fmt.Errorf("Records[%d].RecordType MXE is not allowed for EmailType=nil", i)
				} else if *args.EmailType != "MXE" {
					return fmt.Errorf("Records[%d].RecordType MXE is not allowed for EmailType=%s", i, *args.EmailType)
				}
				mxeRecordsCount++
			} else if *record.RecordType == "URL" || *record.RecordType == "URL301" || *record.RecordType == "FRAME" {
				if !validURLProtocolPrefix.MatchString(*record.Address) {
					return fmt.Errorf(`Records[%d].Address "%s" must contain a protocol prefix for %s record`, i, *record.Address, *record.RecordType)
				}
			} else if *record.RecordType == "CAA" {
				if strings.Contains(*record.Address, "iodef") && !validURLProtocolPrefix.MatchString(*record.Address) {
					return fmt.Errorf(`Records[%d].Address "%s" must contain a protocol prefix for %s iodef record`, i, *record.Address, *record.RecordType)
				}
			}
		}
	}

	if args.EmailType != nil {
		if *args.EmailType == "MXE" && mxeRecordsCount != 1 {
			return fmt.Errorf("one MXE record required for MXE EmailType")
		}

		if *args.EmailType == "MX" && mxRecordsCount == 0 {
			return fmt.Errorf("minimum 1 MX record required for MX EmailType")
		}
	}

	return nil
}

func parseDomainsDNSSetHostsArgs(args *DomainsDNSSetHostsArgs) (*map[string]string, error) {
	params := map[string]string{}

	parsedDomain, err := ParseDomain(*args.Domain)
	if err != nil {
		return nil, err
	}

	params["SLD"] = parsedDomain.SLD
	params["TLD"] = parsedDomain.TLD

	if args.EmailType != nil {
		params["EmailType"] = *args.EmailType
	}

	if args.Flag != nil {
		params["Flag"] = strconv.Itoa(int(*args.Flag))
	}

	if args.Tag != nil {
		params["Tag"] = *args.Tag
	}

	if args.Records != nil {
		for i, record := range *args.Records {
			recordIndexString := strconv.Itoa(i + 1)

			params["RecordType"+recordIndexString] = *record.RecordType

			if record.HostName != nil {
				params["HostName"+recordIndexString] = *record.HostName
			}

			if record.TTL != nil {
				params["TTL"+recordIndexString] = strconv.Itoa(*record.TTL)
			}

			if record.Address != nil {
				params["Address"+recordIndexString] = *record.Address
			}

			if record.MXPref != nil {
				params["MXPref"+recordIndexString] = strconv.Itoa(int(*record.MXPref))
			}

		}
	}

	return &params, nil
}

func isValidEmailType(emailType string) bool {
	for _, value := range AllowedEmailTypeValues {
		if emailType == value {
			return true
		}
	}
	return false
}

func isValidTagValue(tag string) bool {
	for _, value := range allowedTagValues {
		if tag == value {
			return true
		}
	}
	return false
}

func isValidRecordType(recordType string) bool {
	for _, value := range AllowedRecordTypeValues {
		if recordType == value {
			return true
		}
	}
	return false
}
