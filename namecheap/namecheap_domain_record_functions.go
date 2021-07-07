package namecheap_provider

import (
	"fmt"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"strings"
)

// createNameserversMerge has the following behaviour:
// - if nameservers have been set manually, then this method merge the provided ones with manually set
// - else this is overwriting existent ones
func createNameserversMerge(domain string, nameservers []string, client *namecheap.Client) error {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return err
	}

	if *nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		_, err := client.DomainsDNS.SetCustom(domain, nameservers)
		if err != nil {
			return err
		}
	} else {
		var newNameservers []string
		if nsResponse.DomainDNSGetListResult.Nameservers != nil {
			newNameservers = append(newNameservers, *nsResponse.DomainDNSGetListResult.Nameservers...)
		}

		newNameservers = append(newNameservers, nameservers...)

		_, err := client.DomainsDNS.SetCustom(domain, newNameservers)
		if err != nil {
			return err
		}
	}

	return nil
}

// createNameserversOverwrite force overwrites the nameservers
func createNameserversOverwrite(domain string, nameservers []string, client *namecheap.Client) error {
	_, err := client.DomainsDNS.SetCustom(domain, nameservers)
	if err != nil {
		return err
	}

	return nil
}

// readNameserversMerge read real nameservers, check whether there's available the current ones, return only
// the records from currentNameservers argument that are really exist
func readNameserversMerge(domain string, currentNameservers []string, client *namecheap.Client) (*[]string, error) {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return nil, err
	}

	var foundNameservers []string

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS && nsResponse.DomainDNSGetListResult.Nameservers != nil {
		for _, currentNs := range currentNameservers {
			for _, remoteNs := range *nsResponse.DomainDNSGetListResult.Nameservers {
				if currentNs == remoteNs {
					foundNameservers = append(foundNameservers, currentNs)
					break
				}
			}
		}
	}

	return &foundNameservers, nil
}

// readNameserversOverwrite returns remote real nameservers
func readNameserversOverwrite(domain string, client *namecheap.Client) (*[]string, error) {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return nil, err
	}

	if *nsResponse.DomainDNSGetListResult.IsUsingOurDNS || nsResponse.DomainDNSGetListResult.Nameservers == nil {
		return &[]string{}, nil
	} else {
		return nsResponse.DomainDNSGetListResult.Nameservers, nil
	}
}

// readNameserversOverwrite fetches real nameservers from API, remove previousNameservers records, insert currentNameservers
// thus, we have a merge between manually set ones via Namecheap Domain Control Panel and via terraform
func updateNameserversMerge(domain string, previousNameservers []string, currentNameservers []string, client *namecheap.Client) error {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return err
	}

	var newNameservers []string

	if !*nsResponse.DomainDNSGetListResult.IsUsingOurDNS && nsResponse.DomainDNSGetListResult.Nameservers != nil {
		for _, remoteNs := range *nsResponse.DomainDNSGetListResult.Nameservers {
			found := false

			for _, prevNs := range previousNameservers {
				if prevNs == remoteNs {
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
		return fmt.Errorf("unable to proceed with one remained nameserver, you must have at least 2 nameservers")
	}

	if len(newNameservers) == 0 {
		_, err := client.DomainsDNS.SetDefault(domain)
		if err != nil {
			return nil
		}
		return nil
	}

	_, err = client.DomainsDNS.SetCustom(domain, newNameservers)
	if err != nil {
		return err
	}

	return nil
}

// deleteNameserversMerge deletes the only nameservers that have been set in terraform file
// NOTE: be sure that after executing this method at least 2 nameservers should remain otherwise you will have a error
// NOTE: if there's remained 0 nameservers, the default ones will be set
func deleteNameserversMerge(domain string, previousNameservers []string, client *namecheap.Client) error {
	nsResponse, err := client.DomainsDNS.GetList(domain)
	if err != nil {
		return err
	}

	if *nsResponse.DomainDNSGetListResult.IsUsingOurDNS {
		// if upon removing you don't have custom nameservers, then nothing to remove
		return nil
	}

	if nsResponse.DomainDNSGetListResult.Nameservers == nil {
		return fmt.Errorf("invalid nameservers response (this is internal error, please report us about it)")
	}

	var remainNameservers []string

	for _, remoteNs := range *nsResponse.DomainDNSGetListResult.Nameservers {
		found := false

		for _, currentNs := range previousNameservers {
			if remoteNs == currentNs {
				found = true
			}
		}

		if !found {
			remainNameservers = append(remainNameservers, remoteNs)
		}
	}

	if len(remainNameservers) == 1 {
		return fmt.Errorf("unable to proceed with one remained nameserver, you must have at least 2 nameservers")
	}

	if len(remainNameservers) == 0 {
		_, err := client.DomainsDNS.SetDefault(domain)
		if err != nil {
			return nil
		}
		return nil
	}

	_, err = client.DomainsDNS.SetCustom(domain, remainNameservers)
	if err != nil {
		return err
	}

	return nil
}

// deleteNameserversOverwrite resets nameservers settings to default (set default Namecheap's nameservers)
func deleteNameserversOverwrite(domain string, client *namecheap.Client) error {
	_, err := client.DomainsDNS.SetDefault(domain)
	if err != nil {
		return nil
	}

	return nil
}

// createRecordsMerge merges new records with already existing ones on Namecheap
func createRecordsMerge(domain string, emailType *string, records []interface{}, client *namecheap.Client) error {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return err
	}

	var newDomainRecords []namecheap.DomainsDNSHostRecord

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
			domainRecord := namecheap.DomainsDNSHostRecord{
				HostName:   remoteRecord.Name,
				RecordType: remoteRecord.Type,
				Address:    remoteRecord.Address,
				MXPref:     namecheap.UInt8(uint8(*remoteRecord.MXPref)),
				TTL:        remoteRecord.TTL,
			}

			newDomainRecords = append(newDomainRecords, domainRecord)
		}
	}

	recordsConverted := convertRecordTypeSetToDomainRecords(&records)

	newDomainRecords = append(newDomainRecords, *recordsConverted...)

	_, err = client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    namecheap.String(domain),
		Records:   &newDomainRecords,
		EmailType: emailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return err
	}

	return nil
}

// createRecordsOverwrite overwrites existing records with provided new ones
func createRecordsOverwrite(domain string, emailType *string, records []interface{}, client *namecheap.Client) error {
	domainRecords := convertRecordTypeSetToDomainRecords(&records)

	emailTypeValue := "NONE"
	if emailType != nil {
		emailTypeValue = *emailType
	}

	_, err := client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   domainRecords,
		EmailType: &emailTypeValue,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return err
	}

	return nil
}

// readRecordsMerge reads all remote records, return only the currentRecords that are exist in remote records
// NOTE: method has address fix. Refer to getFixedAddressOfRecord
func readRecordsMerge(domain string, currentRecords []interface{}, client *namecheap.Client) (*[]map[string]interface{}, *string, error) {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return nil, nil, err
	}

	currentRecordsConverted := convertRecordTypeSetToDomainRecords(&currentRecords)

	var foundRecords []map[string]interface{}

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, currentRecord := range *currentRecordsConverted {
			currentRecordAddressFixed, err := getFixedAddressOfRecord(&currentRecord)
			if err != nil {
				return nil, nil, err
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
func readRecordsOverwrite(domain string, currentRecords []interface{}, client *namecheap.Client) (*[]map[string]interface{}, *string, error) {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return nil, nil, err
	}

	currentRecordsConverted := convertRecordTypeSetToDomainRecords(&currentRecords)

	var remoteRecords []map[string]interface{}

	if remoteRecordsResponse.DomainDNSGetHostsResult.Hosts != nil {
		for _, remoteRecord := range *remoteRecordsResponse.DomainDNSGetHostsResult.Hosts {
			remoteRecordHash := hashRecord(*remoteRecord.Name, *remoteRecord.Type, *remoteRecord.Address)

			for _, currentRecord := range *currentRecordsConverted {
				currentRecordAddressFixed, err := getFixedAddressOfRecord(&currentRecord)
				if err != nil {
					return nil, nil, err
				}

				currentRecordHash := hashRecord(*currentRecord.HostName, *currentRecord.RecordType, *currentRecordAddressFixed)

				if currentRecordHash == remoteRecordHash {
					remoteRecord.Address = currentRecord.Address
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
func updateRecordsMerge(domain string, emailType *string, previousRecords []interface{}, currentRecords []interface{}, client *namecheap.Client) error {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return err
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
					return err
				}
				prevRecordHash := hashRecord(*prevRecord.HostName, *prevRecord.RecordType, *prevRecordAddressFixed)
				if remoteRecordHash == prevRecordHash {
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

	_, err = client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &newRecordList,
		EmailType: emailType,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return err
	}

	return nil
}

// deleteRecordsMerge removes only previousRecords from remote records
// NOTE: method has address fix. Refer to internal.GetFixedAddressOfRecord
func deleteRecordsMerge(domain string, previousRecords []interface{}, client *namecheap.Client) error {
	remoteRecordsResponse, err := client.DomainsDNS.GetHosts(domain)
	if err != nil {
		return err
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
					return err
				}
				prevRecordHash := hashRecord(*prevRecord.HostName, *prevRecord.RecordType, *prevRecordAddressFixed)
				if remoteRecordHash == prevRecordHash {
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

	_, err = client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &remainedRecords,
		EmailType: nil,
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return err
	}

	return nil
}

// deleteRecordsOverwrite removes all records
func deleteRecordsOverwrite(domain string, client *namecheap.Client) error {
	var records []namecheap.DomainsDNSHostRecord

	_, err := client.DomainsDNS.SetHosts(&namecheap.DomainsDNSSetHostsArgs{
		Domain:    &domain,
		Records:   &records,
		EmailType: namecheap.String("NONE"),
		Flag:      nil,
		Tag:       nil,
	})
	if err != nil {
		return err
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

func fixCAAIodefAddressValue(address *string) (*string, error) {
	addressValues := strings.Split(strings.TrimSpace(*address), " ")
	var addressValuesFixed []string

	for _, value := range addressValues {
		fixedValue := strings.TrimSpace(value)
		if len(fixedValue) != 0 {
			addressValuesFixed = append(addressValuesFixed, fixedValue)
		}
	}

	if len(addressValuesFixed) != 3 {
		return nil, fmt.Errorf("invalid value \"%s\"", *address)
	}

	addressValuesFixed[2] = fmt.Sprintf(`"%s"`, addressValuesFixed[2])

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
// - for CAA records with iodef key word, if no quotes wrapping the domain, then the quotes will be added
// - for other cases the method will just return the address equal to input one
func getFixedAddressOfRecord(record *namecheap.DomainsDNSHostRecord) (*string, error) {
	if *record.RecordType == "CNAME" || *record.RecordType == "ALIAS" || *record.RecordType == "NS" || *record.RecordType == "MX" {
		return fixAddressEndWithDot(record.Address), nil
	}

	if *record.RecordType == "CAA" && strings.Contains(*record.Address, "iodef") {
		return fixCAAIodefAddressValue(record.Address)
	}

	return record.Address, nil
}
