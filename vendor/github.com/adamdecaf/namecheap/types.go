package namecheap

import (
	"encoding/xml"
	"fmt"
)

type RecordsResponse struct {
	XMLName xml.Name `xml:"ApiResponse"`
	Errors  []struct {
		Message string `xml:",chardata"`
		Number  string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse struct {
		Records []Record `xml:"DomainDNSGetHostsResult>host"`
	} `xml:"CommandResponse"`
}

type RecordsCreateResult struct {
	XMLName xml.Name `xml:"ApiResponse"`
	Errors  []struct {
		Message string `xml:",chardata"`
		Number  string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse struct {
		DomainDNSSetHostsResult struct {
			Domain    string `xml:"Domain,attr"`
			IsSuccess bool   `xml:"IsSuccess,attr"`
		} `xml:"DomainDNSSetHostsResult"`
	} `xml:"CommandResponse"`
}

type NSListResponse struct {
	XMLName xml.Name `xml:"ApiResponse"`
	Errors  []struct {
		Message string `xml:",chardata"`
		Number  string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse struct {
		DomainDNSGetListResult []string `xml:"DomainDNSGetListResult>Nameserver"`
	} `xml:"CommandResponse"`
}

type NSSetCustomRepsonse struct {
	XMLName xml.Name `xml:"ApiResponse"`
	Errors  []struct {
		Message string `xml:",chardata"`
		Number  string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse struct {
		DomainDNSSetCustomResult struct {
			Domain  string `xml:"Domain,attr"`
			Updated bool   `xml:"Updated,attr"`
		} `xml:"DomainDNSSetCustomResult"`
	} `xml:"CommandResponse"`
}

type NSSetDefaultResponse struct {
	XMLName xml.Name `xml:"ApiResponse"`
	Errors  []struct {
		Message string `xml:",chardata"`
		Number  string `xml:"Number,attr"`
	} `xml:"Errors>Error"`
	CommandResponse struct {
		DomainDNSSetDefaultResult struct {
			Domain  string `xml:"Domain,attr"`
			Updated bool   `xml:"Updated,attr"`
		} `xml:"DomainDNSSetDefaultResult"`
	} `xml:"CommandResponse"`
}

// Record is used to represent a retrieved Record. All properties
// are set as strings.
type Record struct {
	Name               string `xml:"Name,attr"`
	FriendlyName       string `xml:"FriendlyName,attr"`
	Address            string `xml:"Address,attr"`
	MXPref             int    `xml:"MXPref,attr"`
	AssociatedAppTitle string `xml:"AssociatedAppTitle,attr"`
	Id                 int    `xml:"HostId,attr"`
	RecordType         string `xml:"Type,attr"`
	TTL                int    `xml:"TTL,attr"`
	IsActive           bool   `xml:"IsActive,attr"`
	IsDDNSEnabled      bool   `xml:"IsDDNSEnabled,attr"`
}

// return a map[string]string of differences between two Records
func (r *Record) diff(other *Record) map[string]string {
	out := make(map[string]string, 0)

	if r.Name != other.Name {
		out["Name"] = other.Name
	}
	if r.FriendlyName != other.FriendlyName {
		out["FriendlyName"] = other.FriendlyName
	}
	if r.Address != other.Address {
		out["Address"] = other.Address
	}
	if r.MXPref != other.MXPref {
		out["MXPref"] = string(other.MXPref)
	}
	if r.AssociatedAppTitle != other.AssociatedAppTitle {
		out["AssociatedAppTitle"] = other.AssociatedAppTitle
	}
	if r.Id != other.Id {
		out["Id"] = string(other.Id)
	}
	if r.RecordType != other.RecordType {
		out["RecordType"] = other.RecordType
	}
	if r.TTL != other.TTL {
		out["TTL"] = string(other.TTL)
	}
	if r.IsActive != other.IsActive {
		out["IsActive"] = fmt.Sprintf("%v", other.IsActive)
	}
	if r.IsDDNSEnabled != other.IsDDNSEnabled {
		out["IsDDNSEnabled"] = fmt.Sprintf("%v", other.IsDDNSEnabled)
	}

	return out
}

func (r *Record) Equal(other *Record) bool {
	return len(r.diff(other)) == 0
}
