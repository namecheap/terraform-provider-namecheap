package namecheap_provider

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"regexp"
	"testing"
)

func resetDomainNameservers(t *testing.T) {
	_, err := namecheapSDKClient.DomainsDNS.SetDefault(*testAccDomain)
	if err != nil {
		t.Fatal(err)
	}
}

func resetDomainRecords(t *testing.T) {
	_, err := namecheapSDKClient.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    namecheap.String(*testAccDomain),
		EmailType: namecheap.String("NONE"),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func setDomainRecords(t *testing.T, emailType *string, records *[]namecheap.DomainsDNSHostRecord) {
	_, err := namecheapSDKClient.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    namecheap.String(*testAccDomain),
		Records:   records,
		EmailType: emailType,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func setDomainNameservers(t *testing.T, nameservers *[]string) {
	_, err := namecheapSDKClient.DomainsDNS.SetCustom(*testAccDomain, *nameservers)
	if err != nil {
		t.Fatal(err)
	}
}

func testAccDomainRecordsAPIFetch(response *namecheap.DomainsDNSGetHostsCommandResponse) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		resp, err := namecheapSDKClient.DomainsDNS.GetHosts(*testAccDomain)
		if err != nil {
			return err
		}

		*response = *resp

		return nil
	}
}

func testAccDomainNameserversAPIFetch(response *namecheap.DomainsDNSGetListCommandResponse) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		resp, err := namecheapSDKClient.DomainsDNS.GetList(*testAccDomain)
		if err != nil {
			return err
		}

		*response = *resp

		return nil
	}
}

func testAccDomainRecordsLength(response *namecheap.DomainsDNSGetHostsCommandResponse, expectedLength int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if response == nil || response.DomainDNSGetHostsResult == nil {
			return fmt.Errorf("Empty response")
		}

		if expectedLength == 0 {
			if response.DomainDNSGetHostsResult.Hosts == nil {
				return nil
			}

			if len(*response.DomainDNSGetHostsResult.Domain) != 0 {
				return fmt.Errorf("Expected %d records", expectedLength)
			}
		} else {
			if response.DomainDNSGetHostsResult.Hosts == nil || len(*response.DomainDNSGetHostsResult.Hosts) != expectedLength {
				return fmt.Errorf("Expected %d records", expectedLength)
			}
		}

		return nil
	}
}

func testAccDomainRecordsContain(response *namecheap.DomainsDNSGetHostsCommandResponse, record *namecheap.DomainsDNSHostRecordDetailed) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if response.DomainDNSGetHostsResult.Hosts == nil {
			return fmt.Errorf("Doesn't contain expected record")
		}

		for _, currentRecord := range *response.DomainDNSGetHostsResult.Hosts {
			if equalDomainRecord(&currentRecord, record) {
				return nil
			}
		}

		return fmt.Errorf("Doesn't contain expected record")

	}
}

func testAccDomainNameserversLength(response *namecheap.DomainsDNSGetListCommandResponse, expectedLength int) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		if response == nil || response.DomainDNSGetListResult == nil {
			return fmt.Errorf("Empty response")
		}

		if expectedLength == 0 {
			if response.DomainDNSGetListResult.Nameservers == nil {
				return nil
			}

			if len(*response.DomainDNSGetListResult.Nameservers) != 0 {
				return fmt.Errorf("Expected %d nameservers", expectedLength)
			}
		} else {
			if response.DomainDNSGetListResult.Nameservers == nil || len(*response.DomainDNSGetListResult.Nameservers) != expectedLength {
				return fmt.Errorf("Expected %d nameservers", expectedLength)
			}
		}

		return nil
	}
}

func testAccDomainNameserversContain(response *namecheap.DomainsDNSGetListCommandResponse, nameserver string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		if *response.DomainDNSGetListResult.IsUsingOurDNS {
			return fmt.Errorf("Expected custom nameservers, but found default")
		}

		for _, currentNameserver := range *response.DomainDNSGetListResult.Nameservers {
			if currentNameserver == nameserver {
				return nil
			}
		}

		return fmt.Errorf("Doesn't contain expected nameserver")
	}
}

func testAccDomainNameserversDefault(response *namecheap.DomainsDNSGetListCommandResponse) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		if response == nil || response.DomainDNSGetListResult == nil {
			return fmt.Errorf("Empty response")
		}

		if !*response.DomainDNSGetListResult.IsUsingOurDNS {
			return fmt.Errorf("Expected default nameservers, but found custom")
		}

		return nil
	}
}

// equalDomainRecord compares only Name, Type, Address, TTL, MXPref fields only
func equalDomainRecord(sRec *namecheap.DomainsDNSHostRecordDetailed, dRec *namecheap.DomainsDNSHostRecordDetailed) bool {
	return *sRec.Name == *dRec.Name &&
		*sRec.Type == *dRec.Type &&
		*sRec.Address == *dRec.Address &&
		*sRec.TTL == *dRec.TTL &&
		*sRec.MXPref == *dRec.MXPref
}

func TestAccNamecheapDomainRecords_CreateMerge(t *testing.T) {
	t.Run("create_records_on_empty", func(t *testing.T) {
		var domainRecordsResp namecheap.DomainsDNSGetHostsCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				resetDomainRecords(t)
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&domainRecordsResp),
				testAccDomainRecordsLength(&domainRecordsResp, 0),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "my-domain" {
							domain = "%s"
							mode = "MERGE"

							record {
								hostname = "sub1"
								type = "A"
								address = "11.11.11.11"
							}
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&domainRecordsResp),
						testAccDomainRecordsLength(&domainRecordsResp, 1),
						testAccDomainRecordsContain(&domainRecordsResp, &namecheap.DomainsDNSHostRecordDetailed{
							Name:    namecheap.String("sub1"),
							Type:    namecheap.String("A"),
							Address: namecheap.String("11.11.11.11"),
							MXPref:  namecheap.Int(10),
							TTL:     namecheap.Int(1799),
						}),
					),
				},
			},
		})
	})

	t.Run("create_records_if_exists", func(t *testing.T) {
		var domainRecordsResp namecheap.DomainsDNSGetHostsCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				setDomainRecords(t, namecheap.String(namecheap.EmailTypeNone), &[]namecheap.DomainsDNSHostRecord{
					{
						HostName:   namecheap.String("sub1"),
						RecordType: namecheap.String(namecheap.RecordTypeA),
						Address:    namecheap.String("22.22.22.22"),
						TTL:        namecheap.Int(1799),
					},
				})
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&domainRecordsResp),
				testAccDomainRecordsLength(&domainRecordsResp, 1),
				testAccDomainRecordsContain(&domainRecordsResp, &namecheap.DomainsDNSHostRecordDetailed{
					Name:    namecheap.String("sub1"),
					Type:    namecheap.String(namecheap.RecordTypeA),
					Address: namecheap.String("22.22.22.22"),
					MXPref:  namecheap.Int(10),
					TTL:     namecheap.Int(1799),
				}),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "my-domain" {
							domain = "%s"
							mode = "MERGE"

							record {
								hostname = "sub2"
								type = "A"
								address = "33.33.33.33"
							}
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&domainRecordsResp),
						testAccDomainRecordsLength(&domainRecordsResp, 2),
						testAccDomainRecordsContain(&domainRecordsResp, &namecheap.DomainsDNSHostRecordDetailed{
							Name:    namecheap.String("sub1"),
							Type:    namecheap.String(namecheap.RecordTypeA),
							Address: namecheap.String("22.22.22.22"),
							MXPref:  namecheap.Int(10),
							TTL:     namecheap.Int(1799),
						}),
						testAccDomainRecordsContain(&domainRecordsResp, &namecheap.DomainsDNSHostRecordDetailed{
							Name:    namecheap.String("sub2"),
							Type:    namecheap.String(namecheap.RecordTypeA),
							Address: namecheap.String("33.33.33.33"),
							MXPref:  namecheap.Int(10),
							TTL:     namecheap.Int(1799),
						}),
					),
				},
			},
		})
	})

	t.Run("create_records_on_conflict", func(t *testing.T) {
		var domainRecordsResp namecheap.DomainsDNSGetHostsCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				setDomainRecords(t, namecheap.String(namecheap.EmailTypeNone), &[]namecheap.DomainsDNSHostRecord{
					{
						HostName:   namecheap.String("sub1"),
						RecordType: namecheap.String(namecheap.RecordTypeA),
						Address:    namecheap.String("22.22.22.22"),
						TTL:        namecheap.Int(1799),
					},
				})
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&domainRecordsResp),
				testAccDomainRecordsLength(&domainRecordsResp, 1),
				testAccDomainRecordsContain(&domainRecordsResp, &namecheap.DomainsDNSHostRecordDetailed{
					Name:    namecheap.String("sub1"),
					Type:    namecheap.String(namecheap.RecordTypeA),
					Address: namecheap.String("22.22.22.22"),
					MXPref:  namecheap.Int(10),
					TTL:     namecheap.Int(1799),
				}),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "my-domain" {
							domain = "%s"
							mode = "MERGE"

							record {
								hostname = "sub1"
								type = "A"
								address = "22.22.22.22"
							}
						}
					`, *testAccDomain),
					ExpectError: regexp.MustCompile("Error: Duplicate record"),
				},
			},
		})
	})

	t.Run("create_ns_on_empty", func(t *testing.T) {
		var domainRecordsResp namecheap.DomainsDNSGetListCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainNameserversAPIFetch(&domainRecordsResp),
				testAccDomainNameserversDefault(&domainRecordsResp),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "my-domain" {
							domain = "%s"
							mode = "MERGE"

							nameservers = [
								"dns1.namecheaphosting.com",
								"dns2.namecheaphosting.com",
							]
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainNameserversAPIFetch(&domainRecordsResp),
						testAccDomainNameserversLength(&domainRecordsResp, 2),
						testAccDomainNameserversContain(&domainRecordsResp, "dns1.namecheaphosting.com"),
						testAccDomainNameserversContain(&domainRecordsResp, "dns2.namecheaphosting.com"),
					),
				},
			},
		})
	})

	t.Run("create_ns_if_exists", func(t *testing.T) {
		var domainNameserversResponse namecheap.DomainsDNSGetListCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				setDomainNameservers(t, &[]string{"ns-380.awsdns-47.com", "ns-1076.awsdns-06.org"})
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainNameserversAPIFetch(&domainNameserversResponse),
				testAccDomainNameserversLength(&domainNameserversResponse, 2),
				testAccDomainNameserversContain(&domainNameserversResponse, "ns-380.awsdns-47.com"),
				testAccDomainNameserversContain(&domainNameserversResponse, "ns-1076.awsdns-06.org"),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "my-domain" {
							domain = "%s"
							mode = "MERGE"

							nameservers = [
								"dns1.namecheaphosting.com",
								"dns2.namecheaphosting.com",
							]
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainNameserversAPIFetch(&domainNameserversResponse),
						testAccDomainNameserversLength(&domainNameserversResponse, 4),
						testAccDomainNameserversContain(&domainNameserversResponse, "ns-380.awsdns-47.com"),
						testAccDomainNameserversContain(&domainNameserversResponse, "ns-1076.awsdns-06.org"),
						testAccDomainNameserversContain(&domainNameserversResponse, "dns1.namecheaphosting.com"),
						testAccDomainNameserversContain(&domainNameserversResponse, "dns2.namecheaphosting.com"),
					),
				},
			},
		})
	})

	t.Run("create_ns_on_conflict", func(t *testing.T) {
		var domainNameserversResponse namecheap.DomainsDNSGetListCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				setDomainNameservers(t, &[]string{"ns-380.awsdns-47.com", "ns-1076.awsdns-06.org", "dns1.namecheaphosting.com"})
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainNameserversAPIFetch(&domainNameserversResponse),
				testAccDomainNameserversLength(&domainNameserversResponse, 2),
				testAccDomainNameserversContain(&domainNameserversResponse, "ns-380.awsdns-47.com"),
				testAccDomainNameserversContain(&domainNameserversResponse, "ns-1076.awsdns-06.org"),
				testAccDomainNameserversContain(&domainNameserversResponse, "dns1.namecheaphosting.com"),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "my-domain" {
							domain = "%s"
							mode = "MERGE"

							nameservers = [
								"dns1.namecheaphosting.com",
							]
						}
					`, *testAccDomain),
					ExpectError: regexp.MustCompile("Error: Duplicate nameserver"),
				},
			},
		})
	})
}
