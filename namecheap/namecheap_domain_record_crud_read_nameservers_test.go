package namecheap_provider

import (
	"testing"

	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/stretchr/testify/assert"
)

// getListResponse builds the parsed getList response the nameserver readers
// receive from resourceRecordRead.
func getListResponse(isUsingOurDNS bool, nameservers []string) *namecheap.DomainsDNSGetListCommandResponse {
	result := &namecheap.DomainDNSGetListResult{IsUsingOurDNS: namecheap.Bool(isUsingOurDNS)}
	if nameservers != nil {
		result.Nameservers = &nameservers
	}
	return &namecheap.DomainsDNSGetListCommandResponse{DomainDNSGetListResult: result}
}

func TestReadNameservers_ReturnsAll(t *testing.T) {
	result, diags := readNameservers(getListResponse(false, []string{"ns1.example.com", "ns2.example.com"}))
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameservers_UsingOurDNS(t *testing.T) {
	result, diags := readNameservers(getListResponse(true, nil))
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameservers_NilNameservers(t *testing.T) {
	// Custom DNS (IsUsingOurDNS=false) but with no nameserver elements
	result, diags := readNameservers(getListResponse(false, nil))
	assert.False(t, diags.HasError())
	assert.NotNil(t, result)
	// With nil nameservers and IsUsingOurDNS=false, should return empty list
	assert.Empty(t, *result)
}

func TestReadNameservers_NilResponse(t *testing.T) {
	result, diags := readNameservers(nil)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}
