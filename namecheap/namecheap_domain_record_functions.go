package namecheap_provider

import (
	"fmt"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"strings"
)

// createNameserversMerge has the following behaviour:
// - if nameservers have been set manually, then this method merge the provided ones with manually set
// - else this is overwriting existent ones
func createNameserversMerge(domain string, nameservers []string, client *namecheap.Client) diag.Diagnostics {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	if *nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		_, err := client.DomainsDNS.SetCustom(domain, nameservers)
		if err != nil {
			return diag.FromErr(err)
		}
	} else {
		var newNameservers []string
		if nsResponse.DomainDNSGetListResult.Nameservers != nil {
			newNameservers = append(newNameservers, *nsResponse.DomainDNSGetListResult.Nameservers...)
		}

		for index, nameserver := range nameservers {
			if nsResponse.DomainDNSGetListResult.Nameservers != nil {
				for _, remoteNameserver := range *nsResponse.DomainDNSGetListResult.Nameservers {
					if strings.EqualFold(nameserver, remoteNameserver) {
						return diag.Diagnostics{diag.Diagnostic{
							Severity: diag.Error,
							Summary:  "Duplicate nameserver",
							Detail:   fmt.Sprintf("Nameserver %s is already exist!", nameserver),
							AttributePath: cty.Path{
								cty.GetAttrStep{Name: "nameservers"},
								cty.IndexStep{Key: cty.NumberIntVal(int64(index))},
							},
						}}
					}
				}
			}

			newNameservers = append(newNameservers, nameserver)
		}

		_, err := client.DomainsDNS.SetCustom(domain, newNameservers)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	return nil
}

// createNameserversOverwrite force overwrites the nameservers
func createNameserversOverwrite(domain string, nameservers []string, client *namecheap.Client) diag.Diagnostics {
	_, err := client.DomainsDNS.SetCustom(domain, nameservers)
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// readNameserversMerge read real nameservers, check whether there's available the current ones, return only
// the records from currentNameservers argument that are really exist
func readNameserversMerge(domain string, currentNameservers []string, client *namecheap.Client) (*[]string, diag.Diagnostics) {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return nil, diag.FromErr(err)
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

// readNameserversOverwrite returns remote real nameservers
func readNameserversOverwrite(domain string, client *namecheap.Client) (*[]string, diag.Diagnostics) {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return nil, diag.FromErr(err)
	}

	if *nsResponse.DomainDNSGetListResult.IsUsingOurDNS || nsResponse.DomainDNSGetListResult.Nameservers == nil {
		return &[]string{}, nil
	} else {
		return nsResponse.DomainDNSGetListResult.Nameservers, nil
	}
}

// readNameserversOverwrite fetches real nameservers from API, remove previousNameservers records, insert currentNameservers
// thus, we have a merge between manually set ones via Namecheap Domain Control Panel and via terraform
func updateNameserversMerge(domain string, previousNameservers []string, currentNameservers []string, client *namecheap.Client) diag.Diagnostics {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return diag.FromErr(err)
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

	newNameservers = append(newNameservers, currentNameservers...)

	if len(newNameservers) == 1 {
		return diag.Errorf("Unable to proceed with one remained nameserver, you must have at least 2 nameservers")
	}

	if len(newNameservers) == 0 {
		_, err := client.DomainsDNS.SetDefault(domain)
		if err != nil {
			return diag.FromErr(err)
		}
		return nil
	}

	_, err = client.DomainsDNS.SetCustom(domain, newNameservers)
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// deleteNameserversMerge deletes the only nameservers that have been set in terraform file
// NOTE: be sure that after executing this method at least 2 nameservers should remain otherwise you will have a error
// NOTE: if there's remained 0 nameservers, the default ones will be set
func deleteNameserversMerge(domain string, previousNameservers []string, client *namecheap.Client) diag.Diagnostics {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return diag.FromErr(err)
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
		return diag.Errorf("Unable to proceed with one remained nameserver, you must have at least 2 nameservers")
	}

	if len(remainNameservers) == 0 {
		_, err := client.DomainsDNS.SetDefault(domain)
		if err != nil {
			return diag.FromErr(err)
		}
		return nil
	}

	_, err = client.DomainsDNS.SetCustom(domain, remainNameservers)
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// deleteNameserversOverwrite resets nameservers settings to default (set default Namecheap's nameservers)
func deleteNameserversOverwrite(domain string, client *namecheap.Client) diag.Diagnostics {
	_, err := client.DomainsDNS.SetDefault(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// createRecordsMerge merges new records with already existing ones on Namecheap
func createRecordsMerge(domain string, emailType *string, records []interface{}, client *namecheap.Client) diag.Diagnostics {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	recordsConverted := convertRecordTypeSetToDomainRecords(&records)
	newRecordsMap := make(map[string]*namecheap.DomainsDNSHostRecord)
	var newDomainRecords []namecheap.DomainsDNSHostRecord

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		filteredRemoteRecords := filterDefaultParkingRecords(remoteRecordsResponse.DomainDNSGetHostsResult.Hosts, &domain)
		for _, remoteRecord := range *filteredRemoteRecords {
			remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)
			domainRecord := namecheap.DomainsDNSHostRecord{
				HostName:   remoteRecord.Name,
				RecordType: remoteRecord.Type,
				Address:    remoteRecord.Address,
				MXPref:     namecheap.UInt8(uint8(*remoteRecord.MXPref)),
				TTL:        remoteRecord.TTL,
			}

			newRecordsMap[remoteRecordHash] = &domainRecord
		}
	}

	for _, record := range *recordsConverted {
		fixedAddress, err := getFixedAddressOfRecord(&record)
		if err != nil {
			return diag.FromErr(err)
		}
		recordHash := hashRecord(*record.HostName, *record.RecordType, *fixedAddress)

		if newRecordsMap[recordHash] != nil {
			return diag.Diagnostics{
				diag.Diagnostic{
					Severity: diag.Error,
					Summary:  "Duplicate record",
					Detail:   fmt.Sprintf("Record %s is already exist!", stringifyNCRecord(&record)),
				},
			}
		}

		newRecord := record
		newRecordsMap[recordHash] = &newRecord
	}

	for _, record := range newRecordsMap {
		newDomainRecords = append(newDomainRecords, *record)
	}

	if emailType == nil {
		emailType = resolveEmailType(&newDomainRecords, remoteRecordsResponse.DomainDNSGetHostsResult.EmailType)
	}

	_, err = client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    namecheap.String(domain),
		Records:   &newDomainRecords,
		EmailType: emailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// createRecordsOverwrite overwrites existing records with provided new ones
func createRecordsOverwrite(domain string, emailType *string, records []interface{}, client *namecheap.Client) diag.Diagnostics {
	domainRecords := convertRecordTypeSetToDomainRecords(&records)

	emailTypeValue := namecheap.String(namecheap.EmailTypeNone)
	if emailType != nil {
		emailTypeValue = emailType
	}

	_, err := client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   domainRecords,
		EmailType: emailTypeValue,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	return diag.Diagnostics{}
}

// readRecordsMerge reads all remote records, return only the currentRecords that are exist in remote records
// NOTE: method has address fix. Refer to getFixedAddressOfRecord
func readRecordsMerge(domain string, currentRecords []interface{}, client *namecheap.Client) (*[]map[string]interface{}, *string, diag.Diagnostics) {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return nil, nil, diag.FromErr(err)
	}

	currentRecordsConverted := convertRecordTypeSetToDomainRecords(&currentRecords)

	var foundRecords []map[string]interface{}

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, currentRecord := range *currentRecordsConverted {
			currentRecordAddressFixed, err := getFixedAddressOfRecord(&currentRecord)
			if err != nil {
				return nil, nil, diag.FromErr(err)
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

// readRecordsOverwrite returns the records that are exist on Namecheap
// NOTE: method has address fix. Refer to getFixedAddressOfRecord
func readRecordsOverwrite(domain string, currentRecords []interface{}, client *namecheap.Client) (*[]map[string]interface{}, *string, diag.Diagnostics) {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return nil, nil, diag.FromErr(err)
	}

	currentRecordsConverted := convertRecordTypeSetToDomainRecords(&currentRecords)

	var remoteRecords []map[string]interface{}

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
			remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)

			for _, currentRecord := range *currentRecordsConverted {
				currentRecordAddressFixed, err := getFixedAddressOfRecord(&currentRecord)
				if err != nil {
					return nil, nil, diag.FromErr(err)
				}

				currentRecordHash := hashRecord(*currentRecord.HostName, *currentRecord.RecordType, *currentRecordAddressFixed)

				if currentRecordHash == remoteRecordHash {
					*remoteRecord.Address = *currentRecord.Address
					break
				}

			}

			remoteRecords = append(remoteRecords, *convertDomainRecordDetailedToTypeSetRecord(&remoteRecord))
		}
	}

	return &remoteRecords, remoteRecordsResponse.DomainDNSGetHostsResult.EmailType, nil
}

// updateRecordsMerge fetches remote records, remove previousRecords from remote, add currentRecords and return the final list
// NOTE: method has address fix. Refer to getFixedAddressOfRecord
func updateRecordsMerge(domain string, emailType *string, previousRecords []interface{}, currentRecords []interface{}, client *namecheap.Client) diag.Diagnostics {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	var newRecordList []namecheap.DomainsDNSHostRecord
	previousRecordsMapped := convertRecordTypeSetToDomainRecords(&previousRecords)
	currentRecordsMapped := convertRecordTypeSetToDomainRecords(&currentRecords)

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
			remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)
			found := false

			for _, prevRecord := range *previousRecordsMapped {
				prevRecordAddressFixed, err := getFixedAddressOfRecord(&prevRecord)
				if err != nil {
					return diag.FromErr(err)
				}
				prevRecordHash := hashRecord(*prevRecord.HostName, *prevRecord.RecordType, *prevRecordAddressFixed)
				if strings.EqualFold(remoteRecordHash, prevRecordHash) {
					found = true
					break
				}
			}

			if !found {
				newRecordList = append(newRecordList, namecheap.DomainsDNSHostRecord{
					HostName:   remoteRecord.Name,
					RecordType: remoteRecord.Type,
					Address:    remoteRecord.Address,
					MXPref:     namecheap.UInt8(uint8(*remoteRecord.MXPref)),
					TTL:        remoteRecord.TTL,
				})
			}
		}
	}

	newRecordList = append(newRecordList, *currentRecordsMapped...)

	if emailType == nil {
		emailType = resolveEmailType(&newRecordList, remoteRecordsResponse.DomainDNSGetHostsResult.EmailType)
	}

	_, err = client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &newRecordList,
		EmailType: emailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// deleteRecordsMerge removes only previousRecords from remote records
// NOTE: method has address fix. Refer to internal.GetFixedAddressOfRecord
func deleteRecordsMerge(domain string, previousRecords []interface{}, client *namecheap.Client) diag.Diagnostics {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return diag.FromErr(err)
	}

	var remainedRecords []namecheap.DomainsDNSHostRecord
	previousRecordsMapped := convertRecordTypeSetToDomainRecords(&previousRecords)

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
			remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)
			found := false

			for _, prevRecord := range *previousRecordsMapped {
				prevRecordAddressFixed, err := getFixedAddressOfRecord(&prevRecord)
				if err != nil {
					return diag.FromErr(err)
				}
				prevRecordHash := hashRecord(*prevRecord.HostName, *prevRecord.RecordType, *prevRecordAddressFixed)
				if strings.EqualFold(remoteRecordHash, prevRecordHash) {
					found = true
					break
				}
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

	resolvedEmailType := resolveEmailType(&remainedRecords, remoteRecordsResponse.DomainDNSGetHostsResult.EmailType)

	_, err = client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &remainedRecords,
		EmailType: resolvedEmailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// deleteRecordsOverwrite removes all records
func deleteRecordsOverwrite(domain string, client *namecheap.Client) diag.Diagnostics {
	var records []namecheap.DomainsDNSHostRecord

	_, err := client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &records,
		EmailType: namecheap.String(namecheap.EmailTypeNone),
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

// hashRecord creates a hash for record by hostname, recordType, and address
func hashRecord(hostname string, recordType string, address string) string {
	return fmt.Sprintf("[%s:%s:%s]", hostname, recordType, address)
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
		return nil, fmt.Errorf(`Invalid value "%s"`, *address)
	}

	hasPrefixQuote := strings.HasPrefix(addressValuesFixed[2], `"`)
	hasSuffixQuote := strings.HasSuffix(addressValuesFixed[2], `"`)

	if !hasPrefixQuote && !hasSuffixQuote {
		addressValuesFixed[2] = fmt.Sprintf(`"%s"`, addressValuesFixed[2])
	} else if !hasPrefixQuote || !hasSuffixQuote {
		return nil, fmt.Errorf(`Invalid value "%s"`, *address)
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

// filterDefaultParkingRecords filters out default parking records
func filterDefaultParkingRecords(records *[]namecheap.DomainsDNSHostRecordDetailed, domain *string) *[]namecheap.DomainsDNSHostRecordDetailed {
	var filteredRecords []namecheap.DomainsDNSHostRecordDetailed

	for _, record := range *records {
		if (*record.Type == namecheap.RecordTypeCNAME && *record.Name == "www" && *record.Address == "parkingpage.namecheap.com.") ||
			(*record.Type == namecheap.RecordTypeURL && *record.Name == "@" && strings.HasPrefix(*record.Address, "http://www."+*domain)) {
			continue
		}
		filteredRecords = append(filteredRecords, record)
	}

	return &filteredRecords
}

// stringifyNCRecord returns a string with hostname, record type and address of the record
// This function mostly serves to print error details for user
func stringifyNCRecord(record *namecheap.DomainsDNSHostRecord) string {
	return fmt.Sprintf("{hostname = %s, type = %s, address = %s}", *record.HostName, *record.RecordType, *record.Address)
}

// resolveEmailType resolves an emailType for the case when no emailType provided by terraform configuration,
// but we have an old emailType value extracted from read response
// The main purpose is to prevent set up MX/MXE email type when after manipulation no MX/MXE records available
// This function resolves a bug when we have removed MX/MXE record without reset of emailType, then trying to remove non-MX* record
func resolveEmailType(records *[]namecheap.DomainsDNSHostRecord, emailType *string) *string {
	if *emailType != namecheap.EmailTypeMXE && *emailType != namecheap.EmailTypeMX {
		return emailType
	}

	foundMX := false
	foundMXE := false

	for _, record := range *records {
		if *record.RecordType == namecheap.RecordTypeMX {
			foundMX = true
		} else if *record.RecordType == namecheap.RecordTypeMXE {
			foundMXE = true
		}
	}

	if *emailType == namecheap.EmailTypeMX && !foundMX ||
		*emailType == namecheap.EmailTypeMXE && !foundMXE {
		return namecheap.String(namecheap.EmailTypeNone)
	}

	return emailType
}
