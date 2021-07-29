package namecheap

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap/internal/syncretry"
	"github.com/weppos/publicsuffix-go/publicsuffix"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

const (
	namecheapProductionApiUrl = "https://api.namecheap.com/xml.response"
	namecheapSandboxApiUrl    = "https://api.sandbox.namecheap.com/xml.response"
)

type ClientOptions struct {
	UserName   string
	ApiUser    string
	ApiKey     string
	ClientIp   string
	UseSandbox bool
}

type Client struct {
	http   *http.Client
	common service
	sr     *syncretry.SyncRetry

	ClientOptions *ClientOptions
	BaseURL       string

	Domains    *DomainsService
	DomainsDNS *DomainsDNSService
}

type service struct {
	client *Client
}

// NewClient returns a new Namecheap API Client
func NewClient(options *ClientOptions) *Client {
	client := &Client{
		ClientOptions: options,
		http:          cleanhttp.DefaultClient(),
		sr:            syncretry.NewSyncRetry(&syncretry.Options{Delays: []int{1, 5, 15, 30, 50}}),
	}

	if options.UseSandbox {
		client.BaseURL = namecheapSandboxApiUrl
	} else {
		client.BaseURL = namecheapProductionApiUrl
	}

	client.common.client = client
	client.Domains = (*DomainsService)(&client.common)
	client.DomainsDNS = (*DomainsDNSService)(&client.common)

	return client
}

// NewRequest creates a new request with the params
func (c *Client) NewRequest(body map[string]string) (*http.Request, error) {
	u, err := url.Parse(c.BaseURL)

	if err != nil {
		return nil, fmt.Errorf("Error parsing base URL: %s", err)
	}

	body["Username"] = c.ClientOptions.UserName
	body["ApiKey"] = c.ClientOptions.ApiKey
	body["ApiUser"] = c.ClientOptions.ApiUser
	body["ClientIp"] = c.ClientOptions.ClientIp

	rBody := encodeBody(body)

	// Build the request
	req, err := http.NewRequest("POST", u.String(), bytes.NewBufferString(rBody))

	if err != nil {
		return nil, fmt.Errorf("Error creating request: %s", err)
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(rBody)))

	return req, nil
}

func (c *Client) DoXML(body map[string]string, obj interface{}) (*http.Response, error) {
	var requestResponse *http.Response
	err := c.sr.Do(func() error {
		request, err := c.NewRequest(body)
		if err != nil {
			return err
		}

		response, err := c.http.Do(request)
		if err != nil {
			return err
		}

		if response.StatusCode == 405 {
			return syncretry.RetryError
		}

		requestResponse = response
		defer response.Body.Close()

		err = decodeBody(response.Body, obj)
		return err
	})

	if err != nil && errors.Is(err, syncretry.RetryAttemptsError) {
		return nil, fmt.Errorf("API retry limit exceeded")
	}

	return requestResponse, err
}

// decodeBody decodes the interface from received XML
func decodeBody(reader io.Reader, obj interface{}) error {
	decoder := xml.NewDecoder(reader)
	err := decoder.Decode(&obj)
	if err != nil {
		return fmt.Errorf("unable to parse server response: %s", err)
	}
	return nil
}

// encodeBody converts the map into query string
func encodeBody(body map[string]string) string {
	data := url.Values{}
	for key, val := range body {
		data.Set(key, val)
	}
	return data.Encode()
}

// ParseDomain is a wrapper around publicsuffix.Parse to throw the correct error
func ParseDomain(domain string) (*publicsuffix.DomainName, error) {
	const regDomainString = `^([\-a-zA-Z0-9]+\.+){1,}[a-zA-Z0-9]+$`
	regDomain, err := regexp.Compile(regDomainString)
	if err != nil {
		return nil, err
	}

	if !regDomain.MatchString(domain) {
		return nil, fmt.Errorf("invalid domain: incorrect format")
	}

	parsedDomain, err := publicsuffix.Parse(domain)
	if err != nil {
		return nil, fmt.Errorf("invalid domain: %v", err)
	}

	return parsedDomain, nil
}

// Bool is a helper routine that allocates a new bool value
// to store v and returns a pointer to it.
func Bool(v bool) *bool { return &v }

// Int is a helper routine that allocates a new int value
// to store v and returns a pointer to it.
func Int(v int) *int { return &v }

// String is a helper routine that allocates a new string value
// to store v and returns a pointer to it.
func String(v string) *string { return &v }

// UInt8 is a helper routine that allocates a new uint8 value
// to store v and returns a pointer to it.
func UInt8(v uint8) *uint8 { return &v }
