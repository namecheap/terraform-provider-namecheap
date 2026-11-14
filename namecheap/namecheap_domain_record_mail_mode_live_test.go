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

	// Import used to leave email_type out of state, so the first plan after
	// importing a Google Workspace domain proposed "+ email_type". Every read
	// now stores the zone's mode, and leaving email_type out keeps it.
	t.Run("import_stores_the_zone_mail_mode", func(t *testing.T) {
		var resp namecheap.DomainsDNSGetHostsCommandResponse

		config := func(emailType string) string {
			line := ""
			if emailType != "" {
				line = fmt.Sprintf("email_type = %q", emailType)
			}
			return fmt.Sprintf(`
				resource "namecheap_domain_records" "mail-mode" {
					domain = "%s"
					mode   = "MERGE"
					%s

					record {
						hostname = "keep-mode"
						type     = "A"
						address  = "203.0.113.10"
					}
				}
			`, *testAccDomain, line)
		}

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				setDomainRecords(t, namecheap.String(namecheap.EmailTypeGmail), &[]namecheap.DomainsDNSHostRecord{
					{
						HostName:   namecheap.String("keep-mode"),
						RecordType: namecheap.String(namecheap.RecordTypeA),
						Address:    namecheap.String("203.0.113.10"),
						TTL:        namecheap.Int(1800),
					},
				})
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&resp),
				testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeGmail),
				testAccDomainRecordsLength(&resp, 0),
			),
			Steps: []resource.TestStep{
				{
					// The config declares the zone's only record, so this is the
					// whole-zone import.
					Config:             config(namecheap.EmailTypeGmail),
					ResourceName:       "namecheap_domain_records.mail-mode",
					ImportState:        true,
					ImportStateId:      *testAccDomain + "/zone",
					ImportStatePersist: true,
					ImportStateCheck: func(states []*terraform.InstanceState) error {
						if got := states[0].Attributes["email_type"]; got != namecheap.EmailTypeGmail {
							return fmt.Errorf("imported email_type = %q, want %q", got, namecheap.EmailTypeGmail)
						}
						return nil
					},
				},
				{
					Config:   config(namecheap.EmailTypeGmail),
					PlanOnly: true,
				},
				{
					Config:   config(""),
					PlanOnly: true,
				},
			},
		})
	})

	// A record-only MERGE config on a Google Workspace domain: create and an
	// address change leave the zone on GMAIL, and the refresh reads it into state.
	t.Run("merge_without_email_type_keeps_a_gmail_zone", func(t *testing.T) {
		var resp namecheap.DomainsDNSGetHostsCommandResponse

		config := func(address string) string {
			return fmt.Sprintf(`
				resource "namecheap_domain_records" "mail-mode" {
					domain = "%s"
					mode   = "MERGE"

					record {
						hostname = "keep-mode"
						type     = "A"
						address  = "%s"
					}
				}
			`, *testAccDomain, address)
		}

		resource.Test(t, resource.TestCase{
			PreCheck: func() {
				resetDomainNameservers(t)
				setDomainRecords(t, namecheap.String(namecheap.EmailTypeGmail), nil)
			},
			ProviderFactories: testAccProviderFactories,
			CheckDestroy: resource.ComposeTestCheckFunc(
				testAccDomainRecordsAPIFetch(&resp),
				testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeGmail),
				testAccDomainRecordsLength(&resp, 0),
			),
			Steps: []resource.TestStep{
				{
					Config: config("203.0.113.10"),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&resp),
						testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeGmail),
						testAccDomainRecordsLength(&resp, 1),
					),
				},
				{
					Config: config("203.0.113.11"),
					Check: resource.ComposeTestCheckFunc(
						testAccDomainRecordsAPIFetch(&resp),
						testAccDomainRecordsEmailType(&resp, namecheap.EmailTypeGmail),
						testAccDomainRecordsLength(&resp, 1),
						resource.TestCheckResourceAttr("namecheap_domain_records.mail-mode", "email_type", namecheap.EmailTypeGmail),
					),
				},
			},
		})
	})
}
