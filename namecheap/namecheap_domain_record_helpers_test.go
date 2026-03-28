package namecheap_provider

import (
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestHashRecord(t *testing.T) {
	cases := []struct {
		Name       string
		Hostname   string
		RecordType string
		Address    string
		Expected   string
	}{
		{
			Name:       "simple_a_record",
			Hostname:   "www",
			RecordType: "A",
			Address:    "1.2.3.4",
			Expected:   "[www:A:1.2.3.4]",
		},
		{
			Name:       "cname_record",
			Hostname:   "blog",
			RecordType: "CNAME",
			Address:    "example.com.",
			Expected:   "[blog:CNAME:example.com.]",
		},
		{
			Name:       "at_hostname",
			Hostname:   "@",
			RecordType: "TXT",
			Address:    "v=spf1 include:_spf.google.com ~all",
			Expected:   "[@:TXT:v=spf1 include:_spf.google.com ~all]",
		},
		{
			Name:       "empty_values",
			Hostname:   "",
			RecordType: "",
			Address:    "",
			Expected:   "[::]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			result := hashRecord(tc.Hostname, tc.RecordType, tc.Address)
			assert.Equal(t, tc.Expected, result)
		})
	}

	t.Run("same_inputs_same_hash", func(t *testing.T) {
		h1 := hashRecord("www", "A", "1.2.3.4")
		h2 := hashRecord("www", "A", "1.2.3.4")
		assert.Equal(t, h1, h2)
	})

	t.Run("different_inputs_different_hash", func(t *testing.T) {
		h1 := hashRecord("www", "A", "1.2.3.4")
		h2 := hashRecord("www", "A", "5.6.7.8")
		assert.NotEqual(t, h1, h2)
	})

	t.Run("order_matters", func(t *testing.T) {
		h1 := hashRecord("www", "A", "1.2.3.4")
		h2 := hashRecord("A", "www", "1.2.3.4")
		assert.NotEqual(t, h1, h2)
	})
}

func TestConvertDomainRecordDetailedToTypeSetRecord(t *testing.T) {
	cases := []struct {
		Name     string
		Input    namecheap.DomainsDNSHostRecordDetailed
		Expected map[string]interface{}
	}{
		{
			Name: "a_record",
			Input: namecheap.DomainsDNSHostRecordDetailed{
				Name:    namecheap.String("www"),
				Type:    namecheap.String(namecheap.RecordTypeA),
				Address: namecheap.String("1.2.3.4"),
				MXPref:  namecheap.Int(10),
				TTL:     namecheap.Int(1800),
			},
			Expected: map[string]interface{}{
				"hostname": "www",
				"type":     "A",
				"address":  "1.2.3.4",
				"mx_pref":  10,
				"ttl":      1800,
			},
		},
		{
			Name: "mx_record",
			Input: namecheap.DomainsDNSHostRecordDetailed{
				Name:    namecheap.String("@"),
				Type:    namecheap.String(namecheap.RecordTypeMX),
				Address: namecheap.String("mail.example.com."),
				MXPref:  namecheap.Int(5),
				TTL:     namecheap.Int(600),
			},
			Expected: map[string]interface{}{
				"hostname": "@",
				"type":     "MX",
				"address":  "mail.example.com.",
				"mx_pref":  5,
				"ttl":      600,
			},
		},
		{
			Name: "txt_record",
			Input: namecheap.DomainsDNSHostRecordDetailed{
				Name:    namecheap.String("@"),
				Type:    namecheap.String(namecheap.RecordTypeTXT),
				Address: namecheap.String("v=spf1 include:_spf.google.com ~all"),
				MXPref:  namecheap.Int(10),
				TTL:     namecheap.Int(300),
			},
			Expected: map[string]interface{}{
				"hostname": "@",
				"type":     "TXT",
				"address":  "v=spf1 include:_spf.google.com ~all",
				"mx_pref":  10,
				"ttl":      300,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			result := convertDomainRecordDetailedToTypeSetRecord(&tc.Input)
			assert.NotNil(t, result)
			assert.Equal(t, tc.Expected, *result)
		})
	}
}

func TestConvertInterfacesToString(t *testing.T) {
	t.Run("multiple_strings", func(t *testing.T) {
		input := []interface{}{"ns1.example.com", "ns2.example.com", "ns3.example.com"}
		result := convertInterfacesToString(input)
		assert.Equal(t, []string{"ns1.example.com", "ns2.example.com", "ns3.example.com"}, result)
	})

	t.Run("single_string", func(t *testing.T) {
		input := []interface{}{"ns1.example.com"}
		result := convertInterfacesToString(input)
		assert.Equal(t, []string{"ns1.example.com"}, result)
	})

	t.Run("empty_slice", func(t *testing.T) {
		input := []interface{}{}
		result := convertInterfacesToString(input)
		assert.Nil(t, result)
	})

	t.Run("empty_string_values", func(t *testing.T) {
		input := []interface{}{"", "ns1.example.com", ""}
		result := convertInterfacesToString(input)
		assert.Equal(t, []string{"", "ns1.example.com", ""}, result)
	})
}

func TestStringifyNCRecord(t *testing.T) {
	cases := []struct {
		Name     string
		Input    namecheap.DomainsDNSHostRecord
		Expected string
	}{
		{
			Name: "a_record",
			Input: namecheap.DomainsDNSHostRecord{
				HostName:   namecheap.String("www"),
				RecordType: namecheap.String("A"),
				Address:    namecheap.String("1.2.3.4"),
				MXPref:     namecheap.UInt8(10),
				TTL:        namecheap.Int(1800),
			},
			Expected: "{hostname = www, type = A, address = 1.2.3.4}",
		},
		{
			Name: "cname_record",
			Input: namecheap.DomainsDNSHostRecord{
				HostName:   namecheap.String("blog"),
				RecordType: namecheap.String("CNAME"),
				Address:    namecheap.String("example.com."),
				MXPref:     namecheap.UInt8(10),
				TTL:        namecheap.Int(1800),
			},
			Expected: "{hostname = blog, type = CNAME, address = example.com.}",
		},
		{
			Name: "mx_record",
			Input: namecheap.DomainsDNSHostRecord{
				HostName:   namecheap.String("@"),
				RecordType: namecheap.String("MX"),
				Address:    namecheap.String("mail.example.com."),
				MXPref:     namecheap.UInt8(5),
				TTL:        namecheap.Int(600),
			},
			Expected: "{hostname = @, type = MX, address = mail.example.com.}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			result := stringifyNCRecord(&tc.Input)
			assert.Equal(t, tc.Expected, result)
		})
	}
}

func TestConvertRecordTypeSetToDomainRecords_EmptyInput(t *testing.T) {
	var recordsRaw []interface{}
	result := convertRecordTypeSetToDomainRecords(&recordsRaw)
	assert.NotNil(t, result)
	assert.Len(t, *result, 0)
}

func TestConvertRecordTypeSetToDomainRecords_SingleRecord(t *testing.T) {
	recordsRaw := []interface{}{
		map[string]interface{}{
			"hostname": "@",
			"type":     namecheap.RecordTypeTXT,
			"address":  "v=spf1 ~all",
			"mx_pref":  0,
			"ttl":      300,
		},
	}

	expected := []namecheap.DomainsDNSHostRecord{
		{
			HostName:   namecheap.String("@"),
			RecordType: namecheap.String(namecheap.RecordTypeTXT),
			Address:    namecheap.String("v=spf1 ~all"),
			MXPref:     namecheap.UInt8(0),
			TTL:        namecheap.Int(300),
		},
	}

	result := convertRecordTypeSetToDomainRecords(&recordsRaw)
	assert.NotNil(t, result)
	assert.Equal(t, expected, *result)
}

func TestGetFixedAddressOfRecord_PassthroughTypes(t *testing.T) {
	passthroughTypes := []string{
		namecheap.RecordTypeA,
		namecheap.RecordTypeAAAA,
		namecheap.RecordTypeTXT,
		namecheap.RecordTypeURL,
		namecheap.RecordTypeURL301,
		namecheap.RecordTypeFrame,
		namecheap.RecordTypeMXE,
	}

	for _, recordType := range passthroughTypes {
		t.Run(recordType, func(t *testing.T) {
			record := namecheap.DomainsDNSHostRecord{
				HostName:   namecheap.String("www"),
				RecordType: namecheap.String(recordType),
				Address:    namecheap.String("somevalue"),
				MXPref:     namecheap.UInt8(10),
				TTL:        namecheap.Int(1800),
			}

			result, err := getFixedAddressOfRecord(&record)
			assert.Nil(t, err)
			assert.Equal(t, "somevalue", *result)
		})
	}
}

func TestGetFixedAddressOfRecord_DotSuffixTypes_AlreadyHasDot(t *testing.T) {
	dotTypes := []string{
		namecheap.RecordTypeCNAME,
		namecheap.RecordTypeAlias,
		namecheap.RecordTypeNS,
		namecheap.RecordTypeMX,
	}

	for _, recordType := range dotTypes {
		t.Run(recordType+"_with_dot", func(t *testing.T) {
			record := namecheap.DomainsDNSHostRecord{
				HostName:   namecheap.String("www"),
				RecordType: namecheap.String(recordType),
				Address:    namecheap.String("example.com."),
				MXPref:     namecheap.UInt8(10),
				TTL:        namecheap.Int(1800),
			}

			result, err := getFixedAddressOfRecord(&record)
			assert.Nil(t, err)
			assert.Equal(t, "example.com.", *result)
		})
	}
}

func TestFilterDefaultParkingRecords_MixedRecords(t *testing.T) {
	domain := "example.com"

	records := []namecheap.DomainsDNSHostRecordDetailed{
		{
			Name:    namecheap.String("www"),
			Type:    namecheap.String(namecheap.RecordTypeCNAME),
			Address: namecheap.String("parkingpage.namecheap.com."),
		},
		{
			Name:    namecheap.String("@"),
			Type:    namecheap.String(namecheap.RecordTypeA),
			Address: namecheap.String("1.2.3.4"),
		},
		{
			Name:    namecheap.String("@"),
			Type:    namecheap.String(namecheap.RecordTypeURL),
			Address: namecheap.String("http://www.example.com/?from=@"),
		},
		{
			Name:    namecheap.String("blog"),
			Type:    namecheap.String(namecheap.RecordTypeCNAME),
			Address: namecheap.String("blog.example.com."),
		},
	}

	result := filterDefaultParkingRecords(&records, &domain)
	assert.NotNil(t, result)
	assert.Len(t, *result, 2)
	assert.Equal(t, "1.2.3.4", *(*result)[0].Address)
	assert.Equal(t, "blog.example.com.", *(*result)[1].Address)
}

func TestFilterDefaultParkingRecords_EmptyInput(t *testing.T) {
	domain := "example.com"
	records := []namecheap.DomainsDNSHostRecordDetailed{}

	result := filterDefaultParkingRecords(&records, &domain)
	assert.NotNil(t, result)
	assert.Len(t, *result, 0)
}

func TestFilterDefaultParkingRecords_NonParkingCnameWww(t *testing.T) {
	domain := "example.com"
	records := []namecheap.DomainsDNSHostRecordDetailed{
		{
			Name:    namecheap.String("www"),
			Type:    namecheap.String(namecheap.RecordTypeCNAME),
			Address: namecheap.String("cdn.example.com."),
		},
	}

	result := filterDefaultParkingRecords(&records, &domain)
	assert.Len(t, *result, 1)
}

func TestFilterDefaultParkingRecords_UrlRecordDifferentDomain(t *testing.T) {
	domain := "example.com"
	records := []namecheap.DomainsDNSHostRecordDetailed{
		{
			Name:    namecheap.String("@"),
			Type:    namecheap.String(namecheap.RecordTypeURL),
			Address: namecheap.String("http://www.other.com"),
		},
	}

	result := filterDefaultParkingRecords(&records, &domain)
	assert.Len(t, *result, 1)
}

func TestResolveEmailType_MXWithMixedRecords(t *testing.T) {
	records := []namecheap.DomainsDNSHostRecord{
		createRecordByTypeAndAddress(namecheap.RecordTypeA, "1.2.3.4"),
		createRecordByTypeAndAddress(namecheap.RecordTypeMX, "mail.example.com"),
		createRecordByTypeAndAddress(namecheap.RecordTypeTXT, "v=spf1 ~all"),
	}

	emailType := namecheap.EmailTypeMX
	result := resolveEmailType(&records, &emailType)
	assert.Equal(t, namecheap.EmailTypeMX, *result)
}

func TestResolveEmailType_MXEWithMixedRecords(t *testing.T) {
	records := []namecheap.DomainsDNSHostRecord{
		createRecordByTypeAndAddress(namecheap.RecordTypeA, "1.2.3.4"),
		createRecordByTypeAndAddress(namecheap.RecordTypeMXE, "mail.example.com"),
	}

	emailType := namecheap.EmailTypeMXE
	result := resolveEmailType(&records, &emailType)
	assert.Equal(t, namecheap.EmailTypeMXE, *result)
}

func TestResolveEmailType_MXTypeButOnlyMXERecords(t *testing.T) {
	records := []namecheap.DomainsDNSHostRecord{
		createRecordByTypeAndAddress(namecheap.RecordTypeMXE, "mail.example.com"),
	}

	emailType := namecheap.EmailTypeMX
	result := resolveEmailType(&records, &emailType)
	assert.Equal(t, namecheap.EmailTypeNone, *result)
}

func TestResolveEmailType_MXETypeButOnlyMXRecords(t *testing.T) {
	records := []namecheap.DomainsDNSHostRecord{
		createRecordByTypeAndAddress(namecheap.RecordTypeMX, "mail.example.com"),
	}

	emailType := namecheap.EmailTypeMXE
	result := resolveEmailType(&records, &emailType)
	assert.Equal(t, namecheap.EmailTypeNone, *result)
}
