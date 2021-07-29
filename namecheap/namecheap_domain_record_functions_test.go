package namecheap_provider

import (
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
)

func TestFixCAAIodefAddressValue(t *testing.T) {
	cases := []struct {
		Input  string
		Output string
	}{
		{"0 iodef domain.com", `0 iodef "domain.com"`},
		{"0 iodef http://domain.com", `0 iodef "http://domain.com"`},
		{"  0 iodef http://domain.com  ", `0 iodef "http://domain.com"`},
		{`0 iodef "http://domain.com"`, `0 iodef "http://domain.com"`},
	}

	for i, caseItem := range cases {
		t.Run("test_"+strconv.Itoa(i+1), func(t *testing.T) {
			fixedValue, _ := fixCAAIodefAddressValue(&caseItem.Input)
			assert.Equal(t, caseItem.Output, *fixedValue)
		})
	}

	errorCases := []string{"", "random", "random string", `0 iodef "http://domain.com`, `0 iodef "http://domain.com`}

	for i, errorCaseItem := range errorCases {
		t.Run("test_error_"+strconv.Itoa(i+1), func(t *testing.T) {
			_, err := fixCAAIodefAddressValue(&errorCaseItem)
			assert.NotNil(t, err)
			assert.Errorf(t, err, `Invalid value "`+errorCaseItem+`"`)
		})
	}
}

func TestFixAddressEndWithDot(t *testing.T) {
	cases := []struct {
		Input  string
		Output string
	}{
		{"domain.com", "domain.com."},
		{"domain.com.", "domain.com."},
	}

	for i, caseItem := range cases {
		t.Run("test_"+strconv.Itoa(i+1), func(t *testing.T) {
			fixedValue := fixAddressEndWithDot(&caseItem.Input)
			assert.Equal(t, caseItem.Output, *fixedValue)
		})
	}
}

func TestGetFixedAddressOfRecord(t *testing.T) {
	cases := []struct {
		Name   string
		Input  namecheap.DomainsDNSHostRecord
		Output string
	}{
		{
			Name:   "cname_domain_without_dot",
			Input:  createRecordByTypeAndAddress("CNAME", "domain.com"),
			Output: "domain.com.",
		},
		{
			Name:   "alias_domain_without_dot",
			Input:  createRecordByTypeAndAddress("ALIAS", "domain.com"),
			Output: "domain.com.",
		},
		{
			Name:   "ns_domain_without_dot",
			Input:  createRecordByTypeAndAddress("NS", "domain.com"),
			Output: "domain.com.",
		},
		{
			Name:   "mx_domain_without_dot",
			Input:  createRecordByTypeAndAddress("MX", "domain.com"),
			Output: "domain.com.",
		},
		{
			Name:   "caa_domain_without_quotes",
			Input:  createRecordByTypeAndAddress("CAA", "0 iodef domain.com"),
			Output: `0 iodef "domain.com"`,
		},
	}

	for _, caseItem := range cases {
		t.Run(caseItem.Name, func(t *testing.T) {
			fixedAddress, err := getFixedAddressOfRecord(&caseItem.Input)
			if err != nil {
				t.Errorf("unable to fix address %e", err)
			}
			assert.Equal(t, caseItem.Output, *fixedAddress)
		})

	}
}

func TestResolveEmailType(t *testing.T) {
	cases := []struct {
		Name              string
		Records           []namecheap.DomainsDNSHostRecord
		EmailType         string
		ExpectedEmailType string
	}{
		{
			Name:              "email_type_mx_with_0_records",
			Records:           []namecheap.DomainsDNSHostRecord{},
			EmailType:         namecheap.EmailTypeMX,
			ExpectedEmailType: namecheap.EmailTypeNone,
		},
		{
			Name:              "email_type_mxe_with_0_records",
			Records:           []namecheap.DomainsDNSHostRecord{},
			EmailType:         namecheap.EmailTypeMXE,
			ExpectedEmailType: namecheap.EmailTypeNone,
		},
		{
			Name: "email_type_mx_with_no_mx_records",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeA, "11.11.11.11"),
			},
			EmailType:         namecheap.EmailTypeMX,
			ExpectedEmailType: namecheap.EmailTypeNone,
		},
		{
			Name: "email_type_mx_with_no_mxe_records",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeA, "11.11.11.11"),
			},
			EmailType:         namecheap.EmailTypeMXE,
			ExpectedEmailType: namecheap.EmailTypeNone,
		},
		{
			Name: "email_type_mx_with_mx_record",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeMX, "mail.server.com"),
			},
			EmailType:         namecheap.EmailTypeMX,
			ExpectedEmailType: namecheap.EmailTypeMX,
		},
		{
			Name: "email_type_mx_with_mxe_record",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeMXE, "mail.server.com"),
			},
			EmailType:         namecheap.EmailTypeMXE,
			ExpectedEmailType: namecheap.EmailTypeMXE,
		},
		{
			Name: "email_type_none",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeA, "11.11.11.11"),
			},
			EmailType:         namecheap.EmailTypeNone,
			ExpectedEmailType: namecheap.EmailTypeNone,
		},
		{
			Name: "email_type_fwd",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeA, "11.11.11.11"),
			},
			EmailType:         namecheap.EmailTypeForward,
			ExpectedEmailType: namecheap.EmailTypeForward,
		},
		{
			Name: "email_type_private",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeA, "11.11.11.11"),
			},
			EmailType:         namecheap.EmailTypePrivate,
			ExpectedEmailType: namecheap.EmailTypePrivate,
		},
		{
			Name: "email_type_gmail",
			Records: []namecheap.DomainsDNSHostRecord{
				createRecordByTypeAndAddress(namecheap.RecordTypeA, "11.11.11.11"),
			},
			EmailType:         namecheap.EmailTypeGmail,
			ExpectedEmailType: namecheap.EmailTypeGmail,
		},
	}

	for _, caseItem := range cases {
		t.Run(caseItem.Name, func(t *testing.T) {
			assert.Equal(t, &caseItem.ExpectedEmailType, resolveEmailType(&caseItem.Records, &caseItem.EmailType))
		})
	}
}

func TestFilterDefaultParkingRecords(t *testing.T) {
	t.Run("should_filter", func(t *testing.T) {
		domain := "domain.com"

		records := []namecheap.DomainsDNSHostRecordDetailed{
			{
				Name:    namecheap.String("www"),
				Type:    namecheap.String(namecheap.RecordTypeCNAME),
				Address: namecheap.String("parkingpage.namecheap.com."),
			},
			{
				Name:    namecheap.String("@"),
				Type:    namecheap.String(namecheap.RecordTypeURL),
				Address: namecheap.String("http://www.domain.com/?from=@"),
			},
		}

		filteredRecords := filterDefaultParkingRecords(&records, &domain)

		assert.NotNil(t, filteredRecords)
		assert.Len(t, *filteredRecords, 0)
	})

	t.Run("should_not_filter", func(t *testing.T) {
		domain := "domain.com"

		records := []namecheap.DomainsDNSHostRecordDetailed{
			{
				Name:    namecheap.String("www"),
				Type:    namecheap.String(namecheap.RecordTypeCNAME),
				Address: namecheap.String("page.another-domain.com."),
			},
			{
				Name:    namecheap.String("@"),
				Type:    namecheap.String(namecheap.RecordTypeURL),
				Address: namecheap.String("http://page.another-domain.com/?from=@"),
			},
		}

		filteredRecords := filterDefaultParkingRecords(&records, &domain)

		assert.NotNil(t, records)
		assert.Len(t, *filteredRecords, 2)
		assert.Equal(t, records, *filteredRecords)
	})
}

func TestConvertRecordTypeSetToDomainRecords(t *testing.T) {
	var recordsRaw []interface{}

	recordsRaw = append(recordsRaw, map[string]interface{}{
		"hostname": "www",
		"type":     namecheap.RecordTypeA,
		"address":  "11.11.11.11",
		"mx_pref":  10,
		"ttl":      1800,
	})

	recordsRaw = append(recordsRaw, map[string]interface{}{
		"hostname": "blog",
		"type":     namecheap.RecordTypeA,
		"address":  "22.22.22.22",
		"mx_pref":  10,
		"ttl":      600,
	})

	expectedRecords := []namecheap.DomainsDNSHostRecord{
		{
			HostName:   namecheap.String("www"),
			RecordType: namecheap.String(namecheap.RecordTypeA),
			Address:    namecheap.String("11.11.11.11"),
			MXPref:     namecheap.UInt8(10),
			TTL:        namecheap.Int(1800),
		},
		{
			HostName:   namecheap.String("blog"),
			RecordType: namecheap.String(namecheap.RecordTypeA),
			Address:    namecheap.String("22.22.22.22"),
			MXPref:     namecheap.UInt8(10),
			TTL:        namecheap.Int(600),
		},
	}

	convertedRecords := convertRecordTypeSetToDomainRecords(&recordsRaw)

	assert.NotNil(t, convertedRecords)
	assert.Len(t, *convertedRecords, 2)
	assert.Equal(t, expectedRecords, *convertedRecords)
}

func createRecordByTypeAndAddress(recordType string, address string) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String("hostname"),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
		MXPref:     namecheap.UInt8(10),
		TTL:        namecheap.Int(1799),
	}
}
