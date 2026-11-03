package namecheap_provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// Live-sandbox coverage for the zone's mail mode (EmailType) when
// namecheap_domain_records has no email_type in its config. Without email_type
// the resource owns records, not the mail mode, so a write must leave the mode
// the zone already has: a record edit must never switch a Private Email or
// Google Workspace domain to "No email service".

func testAccDomainRecordsTypeCount(response *namecheap.DomainsDNSGetHostsCommandResponse, recordType string, want int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		got := 0
		if response.DomainDNSGetHostsResult != nil && response.DomainDNSGetHostsResult.Hosts != nil {
			for _, record := range *response.DomainDNSGetHostsResult.Hosts {
				if record.Type != nil && *record.Type == recordType {
					got++
				}
			}
		}
		if got != want {
			return fmt.Errorf("zone holds %d %s records, want %d", got, recordType, want)
		}
		return nil
	}
}

func TestAccNamecheapDomainRecordsMailMode(t *testing.T) {
	// MERGE sends the zone's MX rows back with every write, and the SDK refuses
	// MX rows sent without EmailType, so a MERGE write must resend the current
	// mode rather than omit it.
	t.Run("merge_without_email_type_resends_mx_on_a_zone_with_mx_rows", func(t *testing.T) {
		var resp namecheap.DomainsDNSGetHostsCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				setDomainRecords(t, namecheap.String(namecheap.EmailTypeMX), &[]namecheap.DomainsDNSHostRecord{
					{
						HostName:   namecheap.String("@"),
						RecordType: namecheap.String(namecheap.RecordTypeMX),
						Address:    namecheap.String("mx-unmanaged.example.net"),
						MXPref:     namecheap.UInt8(10),
						TTL:        namecheap.Int(1800),
					},
				})
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&resp),
				testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeMX),
				testAccDomainRecordsTypeCount(&resp, namecheap.RecordTypeMX, 1),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "mail-mode" {
							domain = "%s"
							mode   = "MERGE"

							record {
								hostname = "keep-mode"
								type     = "A"
								address  = "203.0.113.10"
							}
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&resp),
						testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeMX),
						testAccDomainRecordsTypeCount(&resp, namecheap.RecordTypeMX, 1),
						testAccDomainRecordsTypeCount(&resp, namecheap.RecordTypeA, 1),
					),
				},
			},
		})
	})

	// OVERWRITE without email_type used to send NONE on every write and on
	// destroy, so the first record edit on a Private Email domain switched its
	// mail off.
	t.Run("overwrite_without_email_type_keeps_a_mail_preset", func(t *testing.T) {
		var resp namecheap.DomainsDNSGetHostsCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				setDomainRecords(t, namecheap.String(namecheap.EmailTypePrivate), nil)
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&resp),
				testAccDomainRecordsEmailType(&resp, namecheap.EmailTypePrivate),
				testAccDomainRecordsLength(&resp, 0),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "mail-mode" {
							domain = "%s"
							mode   = "OVERWRITE"

							record {
								hostname = "keep-mode"
								type     = "A"
								address  = "203.0.113.10"
							}
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&resp),
						testAccDomainRecordsEmailType(&resp, namecheap.EmailTypePrivate),
						testAccDomainRecordsLength(&resp, 1),
					),
				},
			},
		})
	})

	// Removing the last MX record together with email_type hands the mail mode
	// back to the zone: it stays MX, now with no MX rows, instead of being
	// switched to NONE.
	t.Run("merge_without_email_type_keeps_mx_when_the_last_mx_row_goes", func(t *testing.T) {
		var resp namecheap.DomainsDNSGetHostsCommandResponse

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				resetDomainRecords(t)
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&resp),
				testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeMX),
			),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "mail-mode" {
							domain     = "%s"
							mode       = "MERGE"
							email_type = "MX"

							record {
								hostname = "@"
								type     = "MX"
								address  = "mx1.example.net"
								mx_pref  = 10
							}

							record {
								hostname = "keep-mode"
								type     = "A"
								address  = "203.0.113.10"
							}
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&resp),
						testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeMX),
						testAccDomainRecordsTypeCount(&resp, namecheap.RecordTypeMX, 1),
					),
				},
				{
					Config: fmt.Sprintf(`
						resource "namecheap_domain_records" "mail-mode" {
							domain = "%s"
							mode   = "MERGE"

							record {
								hostname = "keep-mode"
								type     = "A"
								address  = "203.0.113.10"
							}
						}
					`, *testAccDomain),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&resp),
						testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeMX),
						testAccDomainRecordsTypeCount(&resp, namecheap.RecordTypeMX, 0),
					),
				},
			},
		})
	})
}
