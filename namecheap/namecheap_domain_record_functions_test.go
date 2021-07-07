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
		{"0 iodef http://google.com", `0 iodef "http://google.com"`},
	}

	for i, caseItem := range cases {
		t.Run("test_"+strconv.Itoa(i+1), func(t *testing.T) {
			fixedValue, _ := fixCAAIodefAddressValue(&caseItem.Input)
			assert.Equal(t, caseItem.Output, *fixedValue)
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

func createRecordByTypeAndAddress(recordType string, address string) namecheap.DomainsDNSHostRecord {
	return namecheap.DomainsDNSHostRecord{
		HostName:   namecheap.String("hostname"),
		RecordType: namecheap.String(recordType),
		Address:    namecheap.String(address),
		MXPref:     namecheap.UInt8(10),
		TTL:        namecheap.Int(1799),
	}
}
