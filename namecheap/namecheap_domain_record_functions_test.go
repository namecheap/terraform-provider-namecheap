package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

func TestFixCAAAddressValue(t *testing.T) {
	cases := []struct {
		Input  string
		Output string
	}{
		{"0 iodef domain.com", `0 iodef "domain.com"`},
		{"0 iodef http://domain.com", `0 iodef "http://domain.com"`},
		{"  0 iodef http://domain.com  ", `0 iodef "http://domain.com"`},
		{`0 iodef "http://domain.com"`, `0 iodef "http://domain.com"`},
		{"0 iodef mailto:admin@domain.com", `0 iodef "mailto:admin@domain.com"`},
		{`0 iodef "mailto:admin@domain.com"`, `0 iodef "mailto:admin@domain.com"`},
		{"0 issue domain.com", `0 issue "domain.com"`},
		{`0 issue "domain.com"`, `0 issue "domain.com"`},
		{"0 issuewild domain.com", `0 issuewild "domain.com"`},
		{`0 issuewild "domain.com"`, `0 issuewild "domain.com"`},
	}

	for i, caseItem := range cases {
		t.Run("test_"+strconv.Itoa(i+1), func(t *testing.T) {
			fixedValue, _ := fixCAAAddressValue(&caseItem.Input)
			assert.Equal(t, caseItem.Output, *fixedValue)
		})
	}

	errorCases := []string{"", "random", "random string", `0 iodef "http://domain.com`, `0 iodef "http://domain.com`}

	for i, errorCaseItem := range errorCases {
		t.Run("test_error_"+strconv.Itoa(i+1), func(t *testing.T) {
			_, err := fixCAAAddressValue(&errorCaseItem)
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

func TestResolveEmailType_NilEmailType(t *testing.T) {
	records := []namecheap.DomainsDNSHostRecord{
		createRecordByTypeAndAddress(namecheap.RecordTypeA, "1.2.3.4"),
	}
	result := resolveEmailType(&records, nil)
	assert.Equal(t, namecheap.EmailTypeNone, *result)
}

func TestValidateGetListResponse(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		err := validateGetListResponse(nil)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "unexpected nil response")
	})

	t.Run("nil_result", func(t *testing.T) {
		err := validateGetListResponse(&namecheap.DomainsDNSGetListCommandResponse{
			DomainDNSGetListResult: nil,
		})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "unexpected nil response")
	})

	t.Run("nil_is_using_our_dns", func(t *testing.T) {
		err := validateGetListResponse(&namecheap.DomainsDNSGetListCommandResponse{
			DomainDNSGetListResult: &namecheap.DomainDNSGetListResult{
				IsUsingOurDNS: nil,
			},
		})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "nil IsUsingOurDNS")
	})

	t.Run("valid_response", func(t *testing.T) {
		err := validateGetListResponse(&namecheap.DomainsDNSGetListCommandResponse{
			DomainDNSGetListResult: &namecheap.DomainDNSGetListResult{
				IsUsingOurDNS: namecheap.Bool(true),
			},
		})
		assert.Nil(t, err)
	})
}

func TestValidateGetHostsResponse(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		err := validateGetHostsResponse(nil)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "unexpected nil response")
	})

	t.Run("nil_result", func(t *testing.T) {
		err := validateGetHostsResponse(&namecheap.DomainsDNSGetHostsCommandResponse{
			DomainDNSGetHostsResult: nil,
		})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "unexpected nil response")
	})

	t.Run("valid_response", func(t *testing.T) {
		err := validateGetHostsResponse(&namecheap.DomainsDNSGetHostsCommandResponse{
			DomainDNSGetHostsResult: &namecheap.DomainDNSGetHostsResult{},
		})
		assert.Nil(t, err)
	})
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

func TestDomainValidateFunc(t *testing.T) {
	resource := resourceNamecheapDomainRecords()
	validateFunc := resource.Schema["domain"].ValidateFunc

	t.Run("valid_root_domain", func(t *testing.T) {
		warns, errs := validateFunc("example.com", "domain")
		assert.Empty(t, warns)
		assert.Empty(t, errs)
	})

	t.Run("valid_cctld_domain", func(t *testing.T) {
		warns, errs := validateFunc("example.co.uk", "domain")
		assert.Empty(t, warns)
		assert.Empty(t, errs)
	})

	t.Run("reject_subdomain", func(t *testing.T) {
		warns, errs := validateFunc("sub.example.com", "domain")
		assert.Empty(t, warns)
		assert.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "contains a subdomain")
		assert.Contains(t, errs[0].Error(), "example.com")
	})

	t.Run("reject_deep_subdomain", func(t *testing.T) {
		warns, errs := validateFunc("deep.sub.example.com", "domain")
		assert.Empty(t, warns)
		assert.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "contains a subdomain")
	})

	t.Run("reject_empty", func(t *testing.T) {
		warns, errs := validateFunc("", "domain")
		assert.Empty(t, warns)
		assert.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "must not be empty")
	})

	t.Run("reject_invalid_format", func(t *testing.T) {
		warns, errs := validateFunc("not a domain", "domain")
		assert.Empty(t, warns)
		assert.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "not a valid domain")
	})
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

func detailedRecord(name string, recordType string, address string, mxPref int, ttl int) namecheap.DomainsDNSHostRecordDetailed {
	return namecheap.DomainsDNSHostRecordDetailed{
		Name:    namecheap.String(name),
		Type:    namecheap.String(recordType),
		Address: namecheap.String(address),
		MXPref:  namecheap.Int(mxPref),
		TTL:     namecheap.Int(ttl),
	}
}

func TestFormatRecordHCL(t *testing.T) {
	cases := []struct {
		Name   string
		Record namecheap.DomainsDNSHostRecordDetailed
		Output string
	}{
		{
			Name:   "a_record",
			Record: detailedRecord("www", namecheap.RecordTypeA, "1.2.3.4", 10, 1800),
			Output: "  record {\n    hostname = \"www\"\n    type = \"A\"\n    address = \"1.2.3.4\"\n    ttl = 1800\n  }",
		},
		{
			Name:   "cname_dotted_target",
			Record: detailedRecord("blog", namecheap.RecordTypeCNAME, "example.com.", 10, 1799),
			Output: "  record {\n    hostname = \"blog\"\n    type = \"CNAME\"\n    address = \"example.com.\"\n    ttl = 1799\n  }",
		},
		{
			Name:   "mx_with_non_default_pref",
			Record: detailedRecord("@", namecheap.RecordTypeMX, "mail.test.com.", 20, 1800),
			Output: "  record {\n    hostname = \"@\"\n    type = \"MX\"\n    address = \"mail.test.com.\"\n    mx_pref = 20\n    ttl = 1800\n  }",
		},
		{
			Name:   "mx_with_default_pref_omitted",
			Record: detailedRecord("@", namecheap.RecordTypeMX, "mail.test.com.", 10, 1800),
			Output: "  record {\n    hostname = \"@\"\n    type = \"MX\"\n    address = \"mail.test.com.\"\n    ttl = 1800\n  }",
		},
		{
			Name:   "txt_with_embedded_quotes",
			Record: detailedRecord("@", namecheap.RecordTypeTXT, `v=spf1 "included" ~all`, 10, 1800),
			Output: "  record {\n    hostname = \"@\"\n    type = \"TXT\"\n    address = \"v=spf1 \\\"included\\\" ~all\"\n    ttl = 1800\n  }",
		},
		{
			Name:   "caa_quoted_value",
			Record: detailedRecord("@", namecheap.RecordTypeCAA, `0 issue "letsencrypt.org"`, 10, 1800),
			Output: "  record {\n    hostname = \"@\"\n    type = \"CAA\"\n    address = \"0 issue \\\"letsencrypt.org\\\"\"\n    ttl = 1800\n  }",
		},
		{
			Name:   "url_type",
			Record: detailedRecord("@", namecheap.RecordTypeURL, "http://www.test.com", 10, 1800),
			Output: "  record {\n    hostname = \"@\"\n    type = \"URL\"\n    address = \"http://www.test.com\"\n    ttl = 1800\n  }",
		},
	}

	for _, caseItem := range cases {
		t.Run(caseItem.Name, func(t *testing.T) {
			assert.Equal(t, caseItem.Output, formatRecordHCL(&caseItem.Record))
		})
	}
}

func TestFormatRecordHCL_NilFieldsDoNotPanic(t *testing.T) {
	record := namecheap.DomainsDNSHostRecordDetailed{}
	assert.NotPanics(t, func() {
		result := formatRecordHCL(&record)
		assert.Contains(t, result, "record {")
	})
}

func TestBuildUnmanagedDeletionWarning(t *testing.T) {
	unmanaged := []namecheap.DomainsDNSHostRecordDetailed{
		detailedRecord("api", namecheap.RecordTypeA, "5.6.7.8", 10, 1800),
		detailedRecord("@", namecheap.RecordTypeMX, "mail.test.com.", 20, 1800),
	}

	warning := buildUnmanagedDeletionWarning("test.com", unmanaged, "will delete")

	assert.Equal(t, diag.Warning, warning.Severity)
	assert.Contains(t, warning.Summary, "OVERWRITE mode will delete 2 record(s) not present in the configuration for test.com")
	assert.Contains(t, warning.Detail, "A api → 5.6.7.8")
	assert.Contains(t, warning.Detail, "(ttl 1800)")
	assert.Contains(t, warning.Detail, "MX @ → mail.test.com.")
	assert.Contains(t, warning.Detail, "(mx_pref 20)")
	assert.Contains(t, warning.Detail, "To keep these records, add them to your configuration:")
	assert.Contains(t, warning.Detail, "record {")
}

func TestBuildUnmanagedDeletionWarning_PastTense(t *testing.T) {
	unmanaged := []namecheap.DomainsDNSHostRecordDetailed{
		detailedRecord("api", namecheap.RecordTypeA, "5.6.7.8", 10, 1800),
	}

	warning := buildUnmanagedDeletionWarning("test.com", unmanaged, "deleted")

	assert.Contains(t, warning.Summary, "OVERWRITE mode deleted 1 record(s) not present in the configuration for test.com")
}

func TestUnmanagedRecordsOverwrite(t *testing.T) {
	t.Run("empty_remote", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		}))
		defer server.Close()

		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", []interface{}{}, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		assert.Empty(t, unmanaged)
	})

	t.Run("all_managed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
			}))
		}))
		defer server.Close()

		reference := []interface{}{
			map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
		}
		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", reference, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		assert.Empty(t, unmanaged)
	})

	t.Run("mixed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "A", Address: "1.2.3.4", MXPref: 10, TTL: 1800},
				{Name: "api", Type: "A", Address: "5.6.7.8", MXPref: 10, TTL: 1800},
			}))
		}))
		defer server.Close()

		reference := []interface{}{
			map[string]interface{}{"hostname": "www", "type": "A", "address": "1.2.3.4", "mx_pref": 10, "ttl": 1800},
		}
		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", reference, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		if assert.Len(t, unmanaged, 1) {
			assert.Equal(t, "api", *unmanaged[0].Name)
		}
	})

	t.Run("parking_records_excluded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "CNAME", Address: "parkingpage.namecheap.com.", MXPref: 10, TTL: 1800},
				{Name: "@", Type: "URL", Address: "http://www.test.com", MXPref: 10, TTL: 1800},
			}))
		}))
		defer server.Close()

		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", []interface{}{}, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		assert.Empty(t, unmanaged)
	})

	t.Run("explicitly_managed_parking_record_not_reported", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "www", Type: "CNAME", Address: "parkingpage.namecheap.com.", MXPref: 10, TTL: 1800},
			}))
		}))
		defer server.Close()

		// The user explicitly manages the default parking record - it must
		// count as managed either way, so it is never reported as unmanaged.
		reference := []interface{}{
			map[string]interface{}{"hostname": "www", "type": "CNAME", "address": "parkingpage.namecheap.com.", "mx_pref": 10, "ttl": 1800},
		}
		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", reference, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		assert.Empty(t, unmanaged)
	})

	t.Run("cname_dot_normalization_no_false_positive", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "blog", Type: "CNAME", Address: "example.com.", MXPref: 10, TTL: 1800},
			}))
		}))
		defer server.Close()

		// Config has no trailing dot - getFixedAddressOfRecord normalization
		// must still hash-match, so this must not be reported as unmanaged.
		reference := []interface{}{
			map[string]interface{}{"hostname": "blog", "type": "CNAME", "address": "example.com", "mx_pref": 10, "ttl": 1800},
		}
		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", reference, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		assert.Empty(t, unmanaged)
	})

	t.Run("caa_quoting_normalization_no_false_positive", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// getHostsXML interpolates Address directly into an XML attribute
			// with no escaping, so a literal quote must be passed as the XML
			// entity or it would truncate the attribute.
			_, _ = fmt.Fprint(w, getHostsXML("NONE", []hostEntry{
				{Name: "@", Type: "CAA", Address: `0 issue &#34;letsencrypt.org&#34;`, MXPref: 10, TTL: 1800},
			}))
		}))
		defer server.Close()

		// Config has no quotes around the domain - fixCAAAddressValue
		// normalization must still hash-match.
		reference := []interface{}{
			map[string]interface{}{"hostname": "@", "type": "CAA", "address": "0 issue letsencrypt.org", "mx_pref": 10, "ttl": 1800},
		}
		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", reference, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		assert.Empty(t, unmanaged)
	})

	t.Run("nil_hosts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, getHostsXML("NONE", nil))
		}))
		defer server.Close()

		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", []interface{}{}, newTestClient(server.URL))
		assert.False(t, diags.HasError())
		assert.Nil(t, unmanaged)
	})

	t.Run("get_hosts_api_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, "internal error")
		}))
		defer server.Close()

		unmanaged, diags := unmanagedRecordsOverwrite(context.Background(), "test.com", []interface{}{}, newTestClient(server.URL))
		assert.Nil(t, unmanaged)
		assert.True(t, diags.HasError())
	})
}
