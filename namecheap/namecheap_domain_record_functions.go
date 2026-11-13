package namecheap_provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"slices"
	"strings"
)

func validateGetListResponse(response *namecheap.DomainsDNSGetListCommandResponse) error {
	if response == nil || response.DomainDNSGetListResult == nil {
		return fmt.Errorf("unexpected nil response from Namecheap API (domain may not exist or may have been removed from the account)")
	}
	if response.DomainDNSGetListResult.IsUsingOurDNS == nil {
		return fmt.Errorf("unexpected nil IsUsingOurDNS in API response (domain may be in an unsupported state)")
	}
	return nil
}

func validateGetHostsResponse(response *namecheap.DomainsDNSGetHostsCommandResponse) error {
	if response == nil || response.DomainDNSGetHostsResult == nil {
		return fmt.Errorf("unexpected nil response from Namecheap API (domain may not exist or may have been removed from the account)")
	}
	return nil
}

// createNameserversMerge adds nameservers to the ones the domain already uses,
// so the ones set in the Namecheap dashboard or by another resource stay. A
// nameserver that is already live is adopted, not refused and not sent twice,
// as a record is: see mergeRecords. A domain on Namecheap DNS is delegated to
// the given nameservers.
func createNameserversMerge(ctx context.Context, domain string, nameservers []string, client *namecheap.Client) diag.Diagnostics {
	nsResponse, err := client.DomainsDNS.GetListWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if err := validateGetListResponse(nsResponse); err != nil {
		return diagFromClientError(err)
	}

	newNameservers := nameservers
	if live := nsResponse.DomainDNSGetListResult.Nameservers; !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS && live != nil {
		newNameservers = append(append([]string{}, *live...), nameserversNotIn(nameservers, *live)...)
	}

	_, err = client.DomainsDNS.SetCustomWithContext(ctx, domain, newNameservers)
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// delegatedDomainDiagnostic says why records and email_type are not written to
// a domain delegated to nameservers the resource does not manage, and what to
// do about it. The plan check and the apply report it in the same words.
func delegatedDomainDiagnostic(domain string, unmanaged []string) diag.Diagnostic {
	zoneID := domain + "/" + recordsImportScopeZone
	return diag.Diagnostic{
		Severity: diag.Error,
		Summary:  fmt.Sprintf("%s is delegated to custom nameservers this resource does not manage", domain),
		Detail: fmt.Sprintf("The domain uses nameservers that are not in the state of this resource:\n\n  %s\n\n"+
			"Records and email_type take effect only while the domain uses Namecheap DNS, and in MERGE mode the "+
			"resource does not remove nameservers it does not manage. Move the domain to Namecheap DNS in the "+
			"Namecheap dashboard, or import the resource again with the ID %q so that it manages these nameservers "+
			"and the plan lists their removal:\n\n"+
			"  terraform state rm <address>\n"+
			"  terraform import <address> %s",
			strings.Join(unmanaged, "\n  "), zoneID, zoneID),
	}
}

// createNameserversOverwrite force overwrites the nameservers
func createNameserversOverwrite(ctx context.Context, domain string, nameservers []string, client *namecheap.Client) diag.Diagnostics {
	_, err := client.DomainsDNS.SetCustomWithContext(ctx, domain, nameservers)
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// readNameserversMerge read real nameservers from an already fetched getList response, check whether there's
// available the current ones, return only the records from currentNameservers argument that are really exist
func readNameserversMerge(nsResponse *namecheap.DomainsDNSGetListCommandResponse, currentNameservers []string) (*[]string, diag.Diagnostics) {
	if err := validateGetListResponse(nsResponse); err != nil {
		return nil, diagFromClientError(err)
	}

	var foundNameservers []string

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS && nsResponse.DomainDNSGetListResult.Nameservers != nil {
		for _, currentNs := range currentNameservers {
			for _, remoteNs := range *nsResponse.DomainDNSGetListResult.Nameservers {
				if strings.EqualFold(currentNs, remoteNs) {
					foundNameservers = append(foundNameservers, currentNs)
					break
				}
			}
		}
	}

	return &foundNameservers, nil
}

// readNameserversOverwrite returns remote real nameservers from an already fetched getList response
func readNameserversOverwrite(nsResponse *namecheap.DomainsDNSGetListCommandResponse) (*[]string, diag.Diagnostics) {
	if err := validateGetListResponse(nsResponse); err != nil {
		return nil, diagFromClientError(err)
	}

	if *nsResponse.DomainDNSGetListResult.IsUsingOurDNS || nsResponse.DomainDNSGetListResult.Nameservers == nil {
		return &[]string{}, nil
	} else {
		return nsResponse.DomainDNSGetListResult.Nameservers, nil
	}
}

// nameserversNotIn returns the nameservers that are not in listed. The case of
// a nameserver carries no meaning.
func nameserversNotIn(nameservers []string, listed []string) []string {
	var missing []string
	for _, nameserver := range nameservers {
		if !slices.ContainsFunc(listed, func(other string) bool { return strings.EqualFold(nameserver, other) }) {
			missing = append(missing, nameserver)
		}
	}
	return missing
}

// updateNameserversMerge replaces previousNameservers, the nameservers this
// resource set last, with currentNameservers and keeps every other live
// nameserver, so the ones set in the Namecheap dashboard stay. A current
// nameserver that is already live is adopted, not sent twice: that is how the
// first apply after an import by the plain domain name takes over the declared
// nameservers of a delegated domain.
func updateNameserversMerge(ctx context.Context, domain string, previousNameservers []string, currentNameservers []string, client *namecheap.Client) diag.Diagnostics {
	nsResponse, err := client.DomainsDNS.GetListWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if err := validateGetListResponse(nsResponse); err != nil {
		return diagFromClientError(err)
	}

	var newNameservers []string

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS && nsResponse.DomainDNSGetListResult.Nameservers != nil {
		for _, remoteNs := range *nsResponse.DomainDNSGetListResult.Nameservers {
			found := false

			for _, prevNs := range previousNameservers {
				if strings.EqualFold(prevNs, remoteNs) {
					found = true
				}
			}

			if !found {
				newNameservers = append(newNameservers, remoteNs)
			}
		}
	}

	newNameservers = append(newNameservers, nameserversNotIn(currentNameservers, newNameservers)...)

	if len(newNameservers) == 1 {
		return diag.Errorf("Unable to proceed with one remaining nameserver, you must have at least 2 nameservers")
	}

	if len(newNameservers) == 0 {
		_, err := client.DomainsDNS.SetDefaultWithContext(ctx, domain)
		if err != nil {
			return diagFromClientError(err)
		}
		return nil
	}

	_, err = client.DomainsDNS.SetCustomWithContext(ctx, domain, newNameservers)
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// deleteNameserversMerge deletes the only nameservers that have been set in terraform file
// NOTE: be sure that after executing this method at least 2 nameservers should remain otherwise you will have a error
// NOTE: if there's remained 0 nameservers, the default ones will be set
func deleteNameserversMerge(ctx context.Context, domain string, previousNameservers []string, client *namecheap.Client) diag.Diagnostics {
	nsResponse, err := client.DomainsDNS.GetListWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if err := validateGetListResponse(nsResponse); err != nil {
		return diagFromClientError(err)
	}

	if *nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		// if upon removing you don't have custom nameservers, then nothing to remove
		return nil
	}

	if nsResponse.DomainDNSGetListResult.Nameservers == nil {
		return diag.Errorf("Invalid nameservers response (this is internal error, please report us about it)")
	}

	var remainNameservers []string

	for _, remoteNs := range *nsResponse.DomainDNSGetListResult.Nameservers {
		found := false

		for _, currentNs := range previousNameservers {
			if strings.EqualFold(remoteNs, currentNs) {
				found = true
			}
		}

		if !found {
			remainNameservers = append(remainNameservers, remoteNs)
		}
	}

	if len(remainNameservers) == 1 {
		return diag.Errorf("Unable to proceed with one remaining nameserver, you must have at least 2 nameservers")
	}

	if len(remainNameservers) == 0 {
		_, err := client.DomainsDNS.SetDefaultWithContext(ctx, domain)
		if err != nil {
			return diagFromClientError(err)
		}
		return nil
	}

	_, err = client.DomainsDNS.SetCustomWithContext(ctx, domain, remainNameservers)
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// deleteNameserversOverwrite resets nameservers settings to default (set default Namecheap's nameservers)
func deleteNameserversOverwrite(ctx context.Context, domain string, client *namecheap.Client) diag.Diagnostics {
	_, err := client.DomainsDNS.SetDefaultWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// createRecordsMerge adds records to the ones already on Namecheap. A record
// that is already live is adopted, not refused: see mergeRecords.
func createRecordsMerge(ctx context.Context, domain string, emailType *string, records []interface{}, client *namecheap.Client) diag.Diagnostics {
	remoteRecordsResponse, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if err := validateGetHostsResponse(remoteRecordsResponse); err != nil {
		return diagFromClientError(err)
	}

	// Create removes the default parking records; update and delete keep
	// whatever is live.
	var liveRecords []namecheap.DomainsDNSHostRecordDetailed
	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		liveRecords = *filterDefaultParkingRecords(remoteRecordsResponse.DomainDNSGetHostsResult.Hosts, &domain)
	}

	newDomainRecords, err := mergeRecords(liveRecords, nil, *convertRecordTypeSetToDomainRecords(&records))
	if err != nil {
		return diagFromClientError(err)
	}

	// Without email_type the resource owns records, not the mail mode: resend
	// the zone's mode so it never changes. Omitting EmailType would keep it on
	// the API side too, but the SDK refuses the zone's own MX rows without it.
	if emailType == nil {
		emailType = resendEmailType(remoteRecordsResponse)
	}

	_, err = client.DomainsDNS.SetHostsWithContext(ctx, &namecheap.DomainsDNSSetHostsArgs{
		Domain:    namecheap.String(domain),
		Records:   &newDomainRecords,
		EmailType: emailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// createRecordsOverwrite overwrites existing records with provided new ones.
// priorRecords additionally counts as "consented" for the unmanaged-deletion
// pre-flight below, on top of records itself: pass the prior state's records
// on update so a record the user just deliberately removed from config isn't
// misreported as a surprise deletion (it's still live at pre-flight time,
// but its removal was consented via the config diff, not out-of-band
// drift). Pass nil on create, where there is no prior state to consult.
func createRecordsOverwrite(ctx context.Context, domain string, emailType *string, records []interface{}, priorRecords []interface{}, client *namecheap.Client) diag.Diagnostics {
	domainRecords := convertRecordTypeSetToDomainRecords(&records)

	var diags diag.Diagnostics

	// Pre-flight read: enumerate live records that are about to be wiped by
	// the destructive SetHosts below because they aren't in the incoming
	// config (or, on update, in the prior state either), so the caller can
	// warn before data loss instead of after (#65).
	preflightReference := records
	if len(priorRecords) > 0 {
		preflightReference = append(append([]interface{}{}, records...), priorRecords...)
	}
	unmanaged, zoneEmailType, preflightDiags := unmanagedRecordsOverwrite(ctx, domain, preflightReference, client)
	if preflightDiags.HasError() {
		return preflightDiags
	}
	diags = append(diags, preflightDiags...)
	if len(unmanaged) > 0 {
		diags = append(diags, buildUnmanagedDeletionWarning(domain, unmanaged, "will delete"))
	}

	// Without email_type the mail mode stays as it is, as in MERGE: sending
	// NONE here switched Private Email and Google Workspace domains off.
	if emailType == nil {
		emailType = zoneEmailType
	}

	_, err := client.DomainsDNS.SetHostsWithContext(ctx, &namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   domainRecords,
		EmailType: emailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diagFromClientError(err)
	}

	return diags
}

// readRecordsMerge reads all remote records, return only the currentRecords that are exist in remote records
// NOTE: method has address fix. Refer to getFixedAddressOfRecord
func readRecordsMerge(ctx context.Context, domain string, currentRecords []interface{}, client *namecheap.Client) (*[]map[string]interface{}, *string, diag.Diagnostics) {
	remoteRecordsResponse, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return nil, nil, diagFromClientError(err)
	}

	if err := validateGetHostsResponse(remoteRecordsResponse); err != nil {
		return nil, nil, diagFromClientError(err)
	}

	currentRecordsConverted := convertRecordTypeSetToDomainRecords(&currentRecords)

	var foundRecords []map[string]interface{}

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, currentRecord := range *currentRecordsConverted {
			currentRecordAddressFixed, err := getFixedAddressOfRecord(&currentRecord)
			if err != nil {
				return nil, nil, diagFromClientError(err)
			}

			currentRecordHash := hashRecord(*currentRecord.HostName, *currentRecord.RecordType, *currentRecordAddressFixed)
			for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
				remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)
				if currentRecordHash == remoteRecordHash {
					remoteRecord.Address = currentRecord.Address
					foundRecords = append(foundRecords, *convertDomainRecordDetailedToTypeSetRecord(&remoteRecord))
					break
				}
			}
		}
	}

	return &foundRecords, remoteRecordsResponse.DomainDNSGetHostsResult.EmailType, nil
}

// readRecordsOverwrite returns the records that are exist on Namecheap, plus
// the subset of live records that are unmanaged (not present in
// currentRecords and not a default parking record) - i.e. what OVERWRITE
// mode would delete on the next apply (#65, #250).
// NOTE: method has address fix. Refer to getFixedAddressOfRecord
func readRecordsOverwrite(ctx context.Context, domain string, currentRecords []interface{}, client *namecheap.Client) (*[]map[string]interface{}, *string, []namecheap.DomainsDNSHostRecordDetailed, diag.Diagnostics) {
	remoteRecordsResponse, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return nil, nil, nil, diagFromClientError(err)
	}

	if err := validateGetHostsResponse(remoteRecordsResponse); err != nil {
		return nil, nil, nil, diagFromClientError(err)
	}

	currentRecordsConverted := convertRecordTypeSetToDomainRecords(&currentRecords)

	var remoteRecords []map[string]interface{}
	var unmanagedRecords []namecheap.DomainsDNSHostRecordDetailed

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
			remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)

			managed := false
			for _, currentRecord := range *currentRecordsConverted {
				currentRecordAddressFixed, err := getFixedAddressOfRecord(&currentRecord)
				if err != nil {
					return nil, nil, nil, diagFromClientError(err)
				}

				currentRecordHash := hashRecord(*currentRecord.HostName, *currentRecord.RecordType, *currentRecordAddressFixed)

				if currentRecordHash == remoteRecordHash {
					*remoteRecord.Address = *currentRecord.Address
					managed = true
					break
				}

			}

			// Skip Namecheap's default parking records unless the user explicitly
			// manages them, so they don't surface as spurious drift (issue #260)
			// while still respecting an intentionally-configured record.
			if !managed && isDefaultParkingRecord(&remoteRecord, &domain) {
				continue
			}

			if !managed {
				unmanagedRecords = append(unmanagedRecords, remoteRecord)
			}

			remoteRecords = append(remoteRecords, *convertDomainRecordDetailedToTypeSetRecord(&remoteRecord))
		}
	}

	return &remoteRecords, remoteRecordsResponse.DomainDNSGetHostsResult.EmailType, unmanagedRecords, nil
}

// unmanagedRecordsOverwrite fetches the live DNS records for domain and
// returns the subset that are neither present in referenceRecords (in the
// Terraform config's raw *schema.Set form) nor a default parking record.
// Used as the apply-time pre-flight for OVERWRITE create/update/delete so
// callers can warn before a destructive SetHosts (#65, #250). Callers pass
// the incoming config records for create/update, or the prior-state records
// for delete (state-tracked records are consented deletions; anything else
// live is the surprise worth warning about). It also returns the zone's
// current mail mode from the same read.
func unmanagedRecordsOverwrite(ctx context.Context, domain string, referenceRecords []interface{}, client *namecheap.Client) ([]namecheap.DomainsDNSHostRecordDetailed, *string, diag.Diagnostics) {
	remoteRecordsResponse, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return nil, nil, diagFromClientError(err)
	}

	if err := validateGetHostsResponse(remoteRecordsResponse); err != nil {
		return nil, nil, diagFromClientError(err)
	}

	emailType := resendEmailType(remoteRecordsResponse)
	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts == nil {
		return nil, emailType, nil
	}

	referenceRecordsConverted := convertRecordTypeSetToDomainRecords(&referenceRecords)

	var unmanagedRecords []namecheap.DomainsDNSHostRecordDetailed

	for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
		remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)

		managed := false
		for _, referenceRecord := range *referenceRecordsConverted {
			referenceRecordAddressFixed, err := getFixedAddressOfRecord(&referenceRecord)
			if err != nil {
				return nil, nil, diagFromClientError(err)
			}

			referenceRecordHash := hashRecord(*referenceRecord.HostName, *referenceRecord.RecordType, *referenceRecordAddressFixed)
			if referenceRecordHash == remoteRecordHash {
				managed = true
				break
			}
		}

		if managed || isDefaultParkingRecord(&remoteRecord, &domain) {
			continue
		}

		unmanagedRecords = append(unmanagedRecords, remoteRecord)
	}

	return unmanagedRecords, emailType, nil
}

// updateRecordsMerge replaces previousRecords, the records this resource wrote
// last, with currentRecords and keeps every other live record: see mergeRecords.
// NOTE: method has address fix. Refer to getFixedAddressOfRecord
func updateRecordsMerge(ctx context.Context, domain string, emailType *string, previousRecords []interface{}, currentRecords []interface{}, client *namecheap.Client) diag.Diagnostics {
	remoteRecordsResponse, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if err := validateGetHostsResponse(remoteRecordsResponse); err != nil {
		return diagFromClientError(err)
	}

	var liveRecords []namecheap.DomainsDNSHostRecordDetailed
	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		liveRecords = *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts
	}

	newRecordList, err := mergeRecords(liveRecords, *convertRecordTypeSetToDomainRecords(&previousRecords), *convertRecordTypeSetToDomainRecords(&currentRecords))
	if err != nil {
		return diagFromClientError(err)
	}

	if emailType == nil {
		emailType = resendEmailType(remoteRecordsResponse)
	}

	_, err = client.DomainsDNS.SetHostsWithContext(ctx, &namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &newRecordList,
		EmailType: emailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// deleteRecordsMerge removes only previousRecords from remote records
// NOTE: method has address fix. Refer to internal.GetFixedAddressOfRecord
func deleteRecordsMerge(ctx context.Context, domain string, previousRecords []interface{}, client *namecheap.Client) diag.Diagnostics {
	remoteRecordsResponse, err := client.DomainsDNS.GetHostsWithContext(ctx, domain)
	if err != nil {
		return diagFromClientError(err)
	}

	if err := validateGetHostsResponse(remoteRecordsResponse); err != nil {
		return diagFromClientError(err)
	}

	var remainedRecords []namecheap.DomainsDNSHostRecord
	previousRecordsMapped := convertRecordTypeSetToDomainRecords(&previousRecords)

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
			found, err := isPriorRecord(remoteRecord, *previousRecordsMapped)
			if err != nil {
				return diagFromClientError(err)
			}

			if !found {
				remainedRecords = append(remainedRecords, namecheap.DomainsDNSHostRecord{
					HostName:   remoteRecord.Name,
					RecordType: remoteRecord.Type,
					Address:    remoteRecord.Address,
					MXPref:     namecheap.UInt8(uint8(*remoteRecord.MXPref)),
					TTL:        remoteRecord.TTL,
				})
			}
		}
	}

	_, err = client.DomainsDNS.SetHostsWithContext(ctx, &namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &remainedRecords,
		EmailType: resendEmailType(remoteRecordsResponse),
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diagFromClientError(err)
	}

	return nil
}

// deleteRecordsOverwrite removes all records and keeps the mail mode.
// priorStateRecords is the set of records Terraform had in state for this
// resource (raw *schema.Set form); it is used only to compute the
// unmanaged-deletion pre-flight warning below, not to scope the deletion
// itself - destroy always wipes the whole zone.
func deleteRecordsOverwrite(ctx context.Context, domain string, priorStateRecords []interface{}, client *namecheap.Client) diag.Diagnostics {
	var diags diag.Diagnostics

	// Pre-flight read: anything live that isn't in priorStateRecords is a
	// surprise deletion Terraform never consented to (#65, #250).
	unmanaged, zoneEmailType, preflightDiags := unmanagedRecordsOverwrite(ctx, domain, priorStateRecords, client)
	if preflightDiags.HasError() {
		return preflightDiags
	}
	diags = append(diags, preflightDiags...)
	if len(unmanaged) > 0 {
		diags = append(diags, buildUnmanagedDeletionWarning(domain, unmanaged, "will delete"))
	}

	var records []namecheap.DomainsDNSHostRecord

	// Destroy empties the zone but keeps its mail mode, as MERGE destroy does:
	// a Private Email or Google Workspace domain keeps receiving mail.
	_, err := client.DomainsDNS.SetHostsWithContext(ctx, &namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &records,
		EmailType: zoneEmailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diagFromClientError(err)
	}

	return diags
}

// hashRecord creates a hash for record by hostname, recordType, and address
func hashRecord(hostname string, recordType string, address string) string {
	return fmt.Sprintf("[%s:%s:%s]", hostname, recordType, address)
}

// resendEmailType returns the zone's mail mode from a getHosts response, for a
// write that resends it, or nil when getHosts left the attribute out or empty.
// The SDK rejects "" as an invalid EmailType; nil omits the parameter, which
// keeps the zone's mode just the same.
func resendEmailType(response *namecheap.DomainsDNSGetHostsCommandResponse) *string {
	if response == nil || response.DomainDNSGetHostsResult == nil {
		return nil
	}
	emailType := response.DomainDNSGetHostsResult.EmailType
	if emailType == nil || *emailType == "" {
		return nil
	}
	return emailType
}

// mergeRecords builds the zone a MERGE write sends: every live record, in the
// order getHosts returned it, except the ones this resource owns, followed by
// the configured records. A live record is owned when it is in previous (the
// records this resource wrote last, so a record removed from config is deleted)
// or in configured. A configured record that is already live is the same
// record, matched on hostname, type and address as isPriorRecord matches: it is
// adopted, written once with the configured TTL and MX preference, instead of
// being written a second time next to the live copy. Live records the resource
// never declared, duplicates included, pass through unchanged. The error is
// getFixedAddressOfRecord's, for a CAA value it cannot parse.
func mergeRecords(live []namecheap.DomainsDNSHostRecordDetailed, previous, configured []namecheap.DomainsDNSHostRecord) ([]namecheap.DomainsDNSHostRecord, error) {
	// Check every configured address even when no live record needs matching,
	// so an invalid CAA value fails before the write on an empty zone too.
	for i := range configured {
		if _, err := getFixedAddressOfRecord(&configured[i]); err != nil {
			return nil, err
		}
	}

	var zone []namecheap.DomainsDNSHostRecord
	for _, liveRecord := range live {
		owned, err := isPriorRecord(liveRecord, previous)
		if err != nil {
			return nil, err
		}
		if !owned {
			if owned, err = isPriorRecord(liveRecord, configured); err != nil {
				return nil, err
			}
		}
		if owned {
			continue
		}
		zone = append(zone, namecheap.DomainsDNSHostRecord{
			HostName:   liveRecord.Name,
			RecordType: liveRecord.Type,
			Address:    liveRecord.Address,
			MXPref:     namecheap.UInt8(uint8(*liveRecord.MXPref)),
			TTL:        liveRecord.TTL,
		})
	}
	return append(zone, configured...), nil
}

// isPriorRecord reports whether live, a row read from the zone, is one of prior,
// the records this resource wrote last. Each prior address is normalised with
// getFixedAddressOfRecord first, as the write normalises it, so a CNAME or MX
// configured without its trailing dot still matches the row Namecheap returns
// with one. Host name, type and address compare case-insensitively. The error is
// getFixedAddressOfRecord's, for a prior CAA value it cannot parse.
func isPriorRecord(live namecheap.DomainsDNSHostRecordDetailed, prior []namecheap.DomainsDNSHostRecord) (bool, error) {
	liveHash := hashRecord(derefStr(live.Name), derefStr(live.Type), derefStr(live.Address))
	for i := range prior {
		address, err := getFixedAddressOfRecord(&prior[i])
		if err != nil {
			return false, err
		}
		if strings.EqualFold(liveHash, hashRecord(*prior[i].HostName, *prior[i].RecordType, *address)) {
			return true, nil
		}
	}
	return false, nil
}

func convertRecordTypeSetToDomainRecords(records *[]interface{}) *[]namecheap.DomainsDNSHostRecord {
	var mappedRecords []namecheap.DomainsDNSHostRecord

	for _, _record := range *records {
		record := _record.(map[string]interface{})

		hostNameVal := record["hostname"].(string)
		typeVal := record["type"].(string)
		addressVal := record["address"].(string)
		mxPrefVal := record["mx_pref"].(int)
		ttlVal := record["ttl"].(int)

		domainRecord := namecheap.DomainsDNSHostRecord{
			HostName:   namecheap.String(hostNameVal),
			RecordType: namecheap.String(typeVal),
			Address:    namecheap.String(addressVal),
			MXPref:     namecheap.UInt8(uint8(mxPrefVal)),
			TTL:        namecheap.Int(ttlVal),
		}

		mappedRecords = append(mappedRecords, domainRecord)
	}

	return &mappedRecords
}

func convertDomainRecordDetailedToTypeSetRecord(record *namecheap.DomainsDNSHostRecordDetailed) *map[string]interface{} {
	return &map[string]interface{}{
		"hostname": *record.Name,
		"type":     *record.Type,
		"address":  *record.Address,
		"mx_pref":  *record.MXPref,
		"ttl":      *record.TTL,
	}
}

func convertInterfacesToString(stringsRaw []interface{}) []string {
	var stringList []string
	for _, stringRaw := range stringsRaw {
		stringList = append(stringList, stringRaw.(string))
	}
	return stringList
}

func fixCAAAddressValue(address *string) (*string, error) {
	addressValues := strings.Split(strings.TrimSpace(*address), " ")
	var addressValuesFixed []string

	for _, value := range addressValues {
		fixedValue := strings.TrimSpace(value)
		if len(fixedValue) != 0 {
			addressValuesFixed = append(addressValuesFixed, fixedValue)
		}
	}

	if len(addressValuesFixed) != 3 {
		return nil, fmt.Errorf(`invalid value "%s"`, *address)
	}

	hasPrefixQuote := strings.HasPrefix(addressValuesFixed[2], `"`)
	hasSuffixQuote := strings.HasSuffix(addressValuesFixed[2], `"`)

	if !hasPrefixQuote && !hasSuffixQuote {
		addressValuesFixed[2] = fmt.Sprintf(`"%s"`, addressValuesFixed[2])
	} else if !hasPrefixQuote || !hasSuffixQuote {
		return nil, fmt.Errorf(`invalid value "%s"`, *address)
	}

	addressNew := strings.Join(addressValuesFixed, " ")
	return &addressNew, nil
}

func fixAddressEndWithDot(address *string) *string {
	if !strings.HasSuffix(*address, ".") {
		return namecheap.String(*address + ".")
	}
	return address
}

// getFixedAddressOfRecord check the record type and return the fixed address with either dot suffix or quotes around domain name
// The following addresses should be returned:
// - for CNAME, ALIAS, NS, MX records, if the address has been provided without dot suffix, then it will be added
// - for CAA records, if no quotes wrapping the domain, then the quotes will be added
// - for other cases the method will just return the address equal to input one
func getFixedAddressOfRecord(record *namecheap.DomainsDNSHostRecord) (*string, error) {
	if *record.RecordType == namecheap.RecordTypeCNAME ||
		*record.RecordType == namecheap.RecordTypeAlias ||
		*record.RecordType == namecheap.RecordTypeNS ||
		*record.RecordType == namecheap.RecordTypeMX {
		return fixAddressEndWithDot(record.Address), nil
	}

	if *record.RecordType == namecheap.RecordTypeCAA {
		return fixCAAAddressValue(record.Address)
	}

	return record.Address, nil
}

// isDefaultParkingRecord reports whether the record is one of Namecheap's
// auto-provisioned default parking records (CNAME www -> parkingpage.namecheap.com.,
// URL @ -> http://www.<domain>).
func isDefaultParkingRecord(record *namecheap.DomainsDNSHostRecordDetailed, domain *string) bool {
	return (*record.Type == namecheap.RecordTypeCNAME && *record.Name == "www" && *record.Address == "parkingpage.namecheap.com.") ||
		(*record.Type == namecheap.RecordTypeURL && *record.Name == "@" && strings.HasPrefix(*record.Address, "http://www."+*domain))
}

// filterDefaultParkingRecords filters out default parking records
func filterDefaultParkingRecords(records *[]namecheap.DomainsDNSHostRecordDetailed, domain *string) *[]namecheap.DomainsDNSHostRecordDetailed {
	var filteredRecords []namecheap.DomainsDNSHostRecordDetailed

	for _, record := range *records {
		if isDefaultParkingRecord(&record, domain) {
			continue
		}
		filteredRecords = append(filteredRecords, record)
	}

	return &filteredRecords
}

// buildUnmanagedDeletionWarning builds a single warning diagnostic enumerating
// DNS records that OVERWRITE mode will delete (or has deleted) because they
// are not present in the Terraform configuration. tense is the verb used in
// the summary/detail text (e.g. "will delete", "deleted").
func buildUnmanagedDeletionWarning(domain string, unmanaged []namecheap.DomainsDNSHostRecordDetailed, tense string) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Warning,
		Summary:  fmt.Sprintf("OVERWRITE mode %s %d record(s) not present in the configuration for %s", tense, len(unmanaged), domain),
		Detail: describeRecordsToKeep(unmanaged) +
			"See the OVERWRITE mode safety notes in the namecheap_domain_records resource docs, " +
			"or switch to MERGE mode if this domain's DNS is shared with other tools or Terraform configurations.",
	}
}

// describeRecordsToKeep lists records, one line each, and then the paste-ready
// record blocks that keep them. It ends with a blank line.
func describeRecordsToKeep(records []namecheap.DomainsDNSHostRecordDetailed) string {
	var text strings.Builder

	for _, record := range records {
		text.WriteString(formatRecordSummaryLine(&record))
		text.WriteString("\n")
	}

	text.WriteString("\nTo keep these records, add them to your configuration:\n\n")
	for _, record := range records {
		text.WriteString(formatRecordHCL(&record))
		text.WriteString("\n\n")
	}

	return text.String()
}

// formatRecordSummaryLine renders one human-readable line for a live DNS
// record, used in the unmanaged-deletion warning detail.
func formatRecordSummaryLine(record *namecheap.DomainsDNSHostRecordDetailed) string {
	line := fmt.Sprintf("  %s %s → %s", derefStr(record.Type), derefStr(record.Name), derefStr(record.Address))

	if record.Type != nil && *record.Type == namecheap.RecordTypeMX && record.MXPref != nil {
		line += fmt.Sprintf(" (mx_pref %d)", *record.MXPref)
	}
	if record.TTL != nil {
		line += fmt.Sprintf(" (ttl %d)", *record.TTL)
	}

	return line
}

// formatRecordHCL renders a record as a paste-ready HCL `record { ... }`
// block. Values are emitted verbatim (live TTL, dotted CNAME targets, quoted
// CAA values) so that pasting the block into a namecheap_domain_records
// resource produces an empty diff against this exact live record - the
// address-fix normalization in getFixedAddressOfRecord makes equivalent
// pasted forms hash-equal even when the printed form differs. mx_pref is
// only emitted when it differs from the record schema's default (10), and
// ttl is always emitted because the live default (1799) differs from the
// schema default (1800).
func formatRecordHCL(record *namecheap.DomainsDNSHostRecordDetailed) string {
	var b strings.Builder

	b.WriteString("  record {\n")
	fmt.Fprintf(&b, "    hostname = %q\n", derefStr(record.Name))
	fmt.Fprintf(&b, "    type = %q\n", derefStr(record.Type))
	fmt.Fprintf(&b, "    address = %q\n", derefStr(record.Address))
	if record.Type != nil && *record.Type == namecheap.RecordTypeMX && record.MXPref != nil && *record.MXPref != 10 {
		fmt.Fprintf(&b, "    mx_pref = %d\n", *record.MXPref)
	}
	if record.TTL != nil {
		fmt.Fprintf(&b, "    ttl = %d\n", *record.TTL)
	}
	b.WriteString("  }")

	return b.String()
}

// derefStr safely dereferences a possibly-nil string pointer.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
