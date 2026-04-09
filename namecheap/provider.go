package namecheap_provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
	"github.com/namecheap/terraform-provider-namecheap/namecheap/internal/mutexkv"
)

func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"user_name": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "A registered user name for namecheap",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_USER_NAME", nil),
			},

			"api_user": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "A registered api user for namecheap",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_API_USER", nil),
			},

			"api_key": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "The namecheap API key",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_API_KEY", nil),
			},

			"client_ip": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Client IP address",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_CLIENT_IP", nil),
				Default:     "0.0.0.0",
			},

			"use_sandbox": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "Use sandbox API endpoints",
				DefaultFunc: schema.EnvDefaultFunc("NAMECHEAP_USE_SANDBOX", false),
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"namecheap_domain_records": resourceNamecheapDomainRecords(),
		},
		ConfigureContextFunc: configureContext,
	}
}

func configureContext(ctx context.Context, data *schema.ResourceData) (interface{}, diag.Diagnostics) {
	userName := strings.TrimSpace(data.Get("user_name").(string))
	apiUser := strings.TrimSpace(data.Get("api_user").(string))
	apiKey := strings.TrimSpace(data.Get("api_key").(string))
	clientIp := data.Get("client_ip").(string)
	useSandbox := data.Get("use_sandbox").(bool)

	var missing []string
	if userName == "" {
		missing = append(missing, "user_name (NAMECHEAP_USER_NAME)")
	}
	if apiUser == "" {
		missing = append(missing, "api_user (NAMECHEAP_API_USER)")
	}
	if apiKey == "" {
		missing = append(missing, "api_key (NAMECHEAP_API_KEY)")
	}
	if len(missing) > 0 {
		return nil, diag.Diagnostics{
			diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "Missing required provider configuration",
				Detail:   "The following provider attributes must be set either in the configuration or via environment variables: " + strings.Join(missing, ", "),
			},
		}
	}

	client := namecheap.NewClient(&namecheap.ClientOptions{
		UserName:   userName,
		ApiUser:    apiUser,
		ApiKey:     apiKey,
		ClientIp:   clientIp,
		UseSandbox: useSandbox,
	})

	return client, diag.Diagnostics{}
}

var ncMutexKV = mutexkv.NewMutexKV()
