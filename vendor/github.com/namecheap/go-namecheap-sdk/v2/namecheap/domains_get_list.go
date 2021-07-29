package namecheap

import (
	"encoding/xml"
	"fmt"
	"strconv"
)

var allowedListTypeValues = []string{"ALL", "EXPIRING", "EXPIRED"}
var allowedSortByValues = []string{"NAME", "NAME_DESC", "EXPIREDATE", "EXPIREDATE_DESC", "CREATEDATE", "CREATEDATE_DESC"}

type DomainsGetListResponse struct {
	XMLName *xml.Name `xml:"ApiResponse"`
	Errors  *[]struct {
		Message *string `xml:",chardata"`
		Number  *string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse *DomainsGetListCommandResponse `xml:"CommandResponse"`
}

type DomainsGetListCommandResponse struct {
	Domains *[]Domain             `xml:"DomainGetListResult>Domain"`
	Paging  *DomainsGetListPaging `xml:"Paging"`
}

type DomainsGetListPaging struct {
	TotalItems  *int `xml:"TotalItems"`
	CurrentPage *int `xml:"CurrentPage"`
	PageSize    *int `xml:"PageSize"`
}

type Domain struct {
	ID         *string   `xml:"ID,attr"`
	Name       *string   `xml:"Name,attr"`
	User       *string   `xml:"User,attr"`
	Created    *DateTime `xml:"Created,attr"`
	Expires    *DateTime `xml:"Expires,attr"`
	IsExpired  *bool     `xml:"IsExpired,attr"`
	IsLocked   *bool     `xml:"IsLocked,attr"`
	AutoRenew  *bool     `xml:"AutoRenew,attr"`
	WhoisGuard *string   `xml:"WhoisGuard,attr"`
	IsPremium  *bool     `xml:"IsPremium,attr"`
	IsOurDNS   *bool     `xml:"IsOurDNS,attr"`
}

func (d Domain) String() string {
	return fmt.Sprintf("{ID: %s, Name: %s, User: %s, Created: %s, Expires: %s, IsExpired: %t, IsLocked: %t, AutoRenew: %t, WhoisGuard: %s, IsPremium: %t, IsOurDNS: %t}",
		*d.ID, *d.Name, *d.User, *d.Created, d.Expires.Time, *d.IsExpired, *d.IsLocked, *d.AutoRenew, *d.WhoisGuard, *d.IsPremium, *d.IsOurDNS)
}

// DomainsGetListArgs struct is an input arguments for Client.DomainsGetList function
// Please consider Page and PageSize parameters to be set.
type DomainsGetListArgs struct {
	// Possible values are ALL, EXPIRING, or EXPIRED
	// Default Value: ALL
	ListType *string
	// Keyword to look for in the domain list
	SearchTerm *string
	// Page to return
	// Default value: 1
	Page *int
	// Number of domains to be listed on a page. Minimum value is 10, and maximum value is 100.
	// Default value: 20
	PageSize *int
	// Possible values are NAME, NAME_DESC, EXPIREDATE, EXPIREDATE_DESC, CREATEDATE, CREATEDATE_DESC
	SortBy *string
}

// GetList returns a list of domains for the particular user
// Returns DomainsGetListCommandResponse with list of user Domain and paging DomainsGetListPaging
// DomainsGetListArgs is the input arguments. When nil is passed, then nothing will be passed through.
// In this case revert to the official documentation to check defaults
//
// Namecheap doc: https://www.namecheap.com/support/api/methods/domains/get-list/
func (ds *DomainsService) GetList(args *DomainsGetListArgs) (*DomainsGetListCommandResponse, error) {
	var domainsResponse DomainsGetListResponse
	params := map[string]string{
		"Command": "namecheap.domains.getList",
	}

	// parse input arguments
	parsedArgsMap, err := parseDomainsGetListArgs(args)
	if err != nil {
		return nil, err
	}

	// merge parsed arguments with params
	for k, v := range *parsedArgsMap {
		params[k] = v
	}

	_, err = ds.client.DoXML(params, &domainsResponse)
	if err != nil {
		return nil, err
	}
	if domainsResponse.Errors != nil && len(*domainsResponse.Errors) > 0 {
		apiErr := (*domainsResponse.Errors)[0]
		return nil, fmt.Errorf("%s (%s)", *apiErr.Message, *apiErr.Number)
	}

	return domainsResponse.CommandResponse, nil
}

func parseDomainsGetListArgs(args *DomainsGetListArgs) (*map[string]string, error) {
	params := map[string]string{}

	if args == nil {
		return &params, nil
	}

	if args.ListType != nil {
		if isValidListType(*args.ListType) {
			params["ListType"] = *args.ListType
		} else {
			return nil, fmt.Errorf("invalid ListType value: %s", *args.ListType)
		}
	}

	if args.SortBy != nil {
		if isValidSortBy(*args.SortBy) {
			params["SortBy"] = *args.SortBy
		} else {
			return nil, fmt.Errorf("invalid SortBy value: %s", *args.SortBy)
		}
	}

	if args.Page != nil {
		if *args.Page > 0 {
			params["Page"] = strconv.Itoa(*args.Page)
		} else {
			return nil, fmt.Errorf("invalid Page value: %d, minimum value is 1", *args.Page)
		}
	}

	if args.PageSize != nil {
		if *args.PageSize >= 10 && *args.PageSize <= 100 {
			params["PageSize"] = strconv.Itoa(*args.PageSize)
		} else {
			return nil, fmt.Errorf("invalid PageSize value: %d, minimum value is 10, and maximum value is 100", *args.PageSize)
		}
	}

	if args.SearchTerm != nil {
		params["SearchTerm"] = *args.SearchTerm
	}

	return &params, nil
}

func isValidListType(listType string) bool {
	for _, value := range allowedListTypeValues {
		if listType == value {
			return true
		}
	}
	return false
}

func isValidSortBy(sortBy string) bool {
	for _, value := range allowedSortByValues {
		if sortBy == value {
			return true
		}
	}
	return false
}
