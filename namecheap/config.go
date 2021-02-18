package namecheap

import (
	"fmt"

	"github.com/adamdecaf/namecheap"
)

// Config contains required namecheap client configuration settings.
type Config struct {
	username   string
	apiUser    string
	token      string
	ip         string
	useSandbox bool
}

// Client returns a new client for accessing Namecheap.
func (c *Config) Client() (*namecheap.Client, error) {
	client, err := namecheap.NewClient(c.username, c.apiUser, c.token, c.ip, c.useSandbox)

	if err != nil {
		return nil, fmt.Errorf("Error setting up client: %s", err)
	}

	return client, nil
}
