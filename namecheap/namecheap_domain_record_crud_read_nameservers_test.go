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

func TestReadNameserversMerge_FindsMatching(t *testing.T) {
	result, diags := readNameserversMerge(getListResponse(false, []string{"ns1.example.com", "ns2.example.com", "ns3.other.com"}), []string{"ns1.example.com", "ns2.example.com"})
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversMerge_CaseInsensitiveMatch(t *testing.T) {
	result, diags := readNameserversMerge(getListResponse(false, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}), []string{"ns1.example.com"})
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com"}, *result)
}

func TestReadNameserversMerge_UsingOurDNS(t *testing.T) {
	result, diags := readNameserversMerge(getListResponse(true, nil), []string{"ns1.example.com"})
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameserversMerge_NoMatchFound(t *testing.T) {
	result, diags := readNameserversMerge(getListResponse(false, []string{"ns1.other.com", "ns2.other.com"}), []string{"ns1.example.com"})
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameserversOverwrite_ReturnsAll(t *testing.T) {
	result, diags := readNameserversOverwrite(getListResponse(false, []string{"ns1.example.com", "ns2.example.com"}))
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversOverwrite_UsingOurDNS(t *testing.T) {
	result, diags := readNameserversOverwrite(getListResponse(true, nil))
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameserversOverwrite_NilNameservers(t *testing.T) {
	// Custom DNS (IsUsingOurDNS=false) but with no nameserver elements
	result, diags := readNameserversOverwrite(getListResponse(false, nil))
	assert.False(t, diags.HasError())
	assert.NotNil(t, result)
	// With nil nameservers and IsUsingOurDNS=false, should return empty list
	assert.Empty(t, *result)
}

func TestReadNameserversMerge_NilResponse(t *testing.T) {
	result, diags := readNameserversMerge(nil, []string{"ns1.example.com"})
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}

func TestReadNameserversOverwrite_NilResponse(t *testing.T) {
	result, diags := readNameserversOverwrite(nil)
	assert.True(t, diags.HasError())
	assert.Nil(t, result)
}
