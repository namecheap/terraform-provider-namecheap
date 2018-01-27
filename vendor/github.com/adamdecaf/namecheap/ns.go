package namecheap

import (
	"bytes"
	"fmt"
	"strings"
)

func (c *Client) SetNS(domain string, servers []string) ([]string, error) {
	var ret NSSetCustomRepsonse
	var domainSplit = strings.Split(domain, ".")
	var serversJoin = strings.Join(servers, ",")

	if len(domainSplit) != 2 {
		return nil, fmt.Errorf("Domain does not contain SLD and TLD")
	}

	params := map[string]string{
		"Command":     "namecheap.domains.dns.setCustom",
		"SLD":         domainSplit[0],
		"TLD":         domainSplit[1],
		"Nameservers": serversJoin,
	}

	req, err := c.NewRequest(params)
	if err != nil {
		return nil, err
	}
	resp, err := c.Http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	err = c.decode(resp.Body, &ret)
	if err != nil {
		return nil, err
	}
	if ret.CommandResponse.DomainDNSSetCustomResult.Updated == false {
		var errorBuf bytes.Buffer
		for _, responseError := range ret.Errors {
			errorBuf.WriteString("Number: ")
			errorBuf.WriteString(responseError.Number)
			errorBuf.WriteString(" Message: ")
			errorBuf.WriteString(responseError.Message)
			errorBuf.WriteString("\n")
		}
		return nil, fmt.Errorf(errorBuf.String())
	}
	newNS, err := c.GetNS(domain)
	if err != nil {
		return nil, err
	}
	return newNS, nil
}

func (c *Client) GetNS(domain string) ([]string, error) {
	var ret NSListResponse
	var domainSplit = strings.Split(domain, ".")
	params := map[string]string{
		"Command": "namecheap.domains.dns.getList",
		"SLD":     domainSplit[0],
		"TLD":     domainSplit[1],
	}
	req, err := c.NewRequest(params)
	if err != nil {
		return nil, err
	}
	resp, err := c.Http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	err = c.decode(resp.Body, &ret)
	if err != nil {
		return nil, err
	}
	return ret.CommandResponse.DomainDNSGetListResult, nil
}

func (c *Client) ResetNS(domain string) error {
	var ret NSSetDefaultResponse
	var domainSplit = strings.Split(domain, ".")
	params := map[string]string{
		"Command": "namecheap.domains.dns.setDefault",
		"SLD":     domainSplit[0],
		"TLD":     domainSplit[1],
	}
	req, err := c.NewRequest(params)
	if err != nil {
		return err
	}
	resp, err := c.Http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	err = c.decode(resp.Body, &ret)
	if err != nil {
		return err
	}
	if ret.CommandResponse.DomainDNSSetDefaultResult.Updated == false {
		var errorBuf bytes.Buffer
		for _, responseError := range ret.Errors {
			errorBuf.WriteString("Number: ")
			errorBuf.WriteString(responseError.Number)
			errorBuf.WriteString(" Message: ")
			errorBuf.WriteString(responseError.Message)
			errorBuf.WriteString("\n")
		}
		return fmt.Errorf(errorBuf.String())
	}

	return nil
}
