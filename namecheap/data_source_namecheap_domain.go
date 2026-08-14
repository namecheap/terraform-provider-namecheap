package namecheap_provider

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

// dataSourceNamecheapDomain exposes read-only information about a single domain.
//
// Almost everything comes from one namecheap.domains.getInfo call: DNS details,
// dates, privacy (Whoisguard), ownership, and modification rights. The
// namecheap.domains.getList portfolio listing still supplies is_locked and
// auto_renew (getInfo does not return those two booleans) and the authoritative
// is_expired flag — the API's own verdict, which accounts for renewal grace
// periods that date arithmetic cannot see. On a portfolio miss, is_expired
// falls back to a value derived from the expiry date and domain status.
func dataSourceNamecheapDomain() *schema.Resource {
	return &schema.Resource{
		Description: "Reads a single domain on the account: its DNS provider and nameservers, plus lifecycle fields such as expiry, registrar lock and auto-renew.",
		ReadContext: dataSourceNamecheapDomainRead,
		Schema: map[string]*schema.Schema{
			"domain": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "The domain name to look up (e.g. example.com). Must be a registered root domain, not a subdomain.",
				ValidateFunc: validateDomainIsNotSubdomain,
			},
			"dns_provider_type": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The DNS provider currently serving the domain (e.g. NAMECHEAP, FreeDNS, Custom).",
			},
			"is_our_dns": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain is using Namecheap's DNS.",
			},
			"is_premium": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain is a premium domain.",
			},
			"is_premium_dns": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain has an active PremiumDNS subscription.",
			},
			"nameservers": {
				Type:        schema.TypeList,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "The nameservers currently configured for the domain.",
			},
			"created": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Registration date as an RFC3339 timestamp (UTC).",
			},
			"expires": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Expiration date as an RFC3339 timestamp (UTC).",
			},
			"expires_in_days": {
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "Whole calendar days until the domain expires (negative if already expired). 0 with an empty expires means the API did not report an expiry date.",
			},
			"is_expired": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the domain has expired, as reported by the portfolio listing (accounts for renewal grace periods); derived from the expiry date and status when the domain is missing from the listing.",
			},
			"is_locked": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the registrar lock is enabled.",
			},
			"auto_renew": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether auto-renew is enabled.",
			},
			"whois_guard": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "WhoisGuard status (e.g. ENABLED, DISABLED, NOTPRESENT).",
			},
			"whois_guard_expires": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Expiration date of the domain privacy protection as an RFC3339 timestamp (UTC); empty when privacy is not allotted.",
			},
			"whois_guard_email": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The generated privacy-protection email address on the Whois record.",
			},
			"whois_guard_forwarded_to": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The real address the privacy-protection email forwards to. Note that this address is stored in the Terraform state.",
			},
			"status": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Domain status as reported by the API (e.g. Ok, Locked, Expired); empty when the API omits it.",
			},
			"owner_name": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The user account under which the domain is registered.",
			},
			"is_owner": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the API user is the domain owner.",
			},
			"premium_dns_auto_renew": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the PremiumDNS subscription auto-renews. This is the subscription's own flag, distinct from the domain's auto_renew.",
			},
			"premium_dns_expires": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Expiration date of the PremiumDNS subscription as an RFC3339 timestamp (UTC); empty when there is no subscription or the API reports no expiry.",
			},
			"email_type": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The email service configured for the domain (e.g. FWD, MXE, OX, No Email Service).",
			},
			"host_count": {
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "Number of DNS host records configured for the domain.",
			},
			"dynamic_dns_status": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether dynamic DNS is enabled for the domain.",
			},
			"is_failover": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether DNS failover is enabled for the domain.",
			},
			"modification_rights_all": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the API user holds all modification rights on the domain.",
			},
			"modification_rights": {
				Type:        schema.TypeMap,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Granular per-feature modification rights (e.g. dns, rlock, autorenew mapped to OK), populated for shared/permissioned domains.",
			},
		},
	}
}

func dataSourceNamecheapDomainRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)
	domain := strings.ToLower(data.Get("domain").(string))

	resp, err := client.Domains.GetInfoWithContext(ctx, domain)
	if err != nil {
		return dataSourceDomainReadError(domain, err)
	}

	if resp == nil || resp.Result() == nil {
		return diag.Errorf("Namecheap returned no information for domain %q; it may not exist or may not be associated with this account", domain)
	}

	info := resp.Result()

	if info.IsPremium != nil {
		_ = data.Set("is_premium", *info.IsPremium)
	}
	if info.Status != nil {
		_ = data.Set("status", *info.Status)
	}
	if info.OwnerName != nil {
		_ = data.Set("owner_name", *info.OwnerName)
	}
	if info.IsOwner != nil {
		_ = data.Set("is_owner", *info.IsOwner)
	}

	expired := isStatusExpired(info.Status)
	if dd := info.DomainDetails; dd != nil {
		_ = data.Set("created", formatDateTime(dd.CreatedDate))
		_ = data.Set("expires", formatDateTime(dd.ExpiredDate))
		if dd.ExpiredDate != nil && !dd.ExpiredDate.IsZero() {
			days := daysUntil(dd.ExpiredDate.Time, time.Now().UTC())
			_ = data.Set("expires_in_days", days)
			expired = expired || days < 0
		}
	}
	// Fallback only: when the domain is found in the portfolio listing,
	// setDomainLifecycleFromList below overwrites is_expired with the API's own
	// flag, which stays authoritative (it accounts for renewal grace periods).
	_ = data.Set("is_expired", expired)

	if wg := info.WhoisGuard; wg != nil {
		if wg.Enabled != nil {
			_ = data.Set("whois_guard", mapGetInfoWhoisGuard(*wg.Enabled))
		}
		_ = data.Set("whois_guard_expires", formatDateTime(wg.ExpiredDate))
		if ed := wg.EmailDetails; ed != nil {
			if ed.WhoisGuardEmail != nil {
				_ = data.Set("whois_guard_email", *ed.WhoisGuardEmail)
			}
			if ed.ForwardedTo != nil {
				_ = data.Set("whois_guard_forwarded_to", *ed.ForwardedTo)
			}
		}
	}

	if sub := info.PremiumDnsSubscription; sub != nil {
		if sub.IsActive != nil {
			_ = data.Set("is_premium_dns", *sub.IsActive)
		}
		// SubscriptionId -1 is the API's "no subscription" sentinel; its
		// UseAutoRenew and sentinel dates carry no information in that case.
		if sub.SubscriptionID != nil && *sub.SubscriptionID != -1 {
			if sub.UseAutoRenew != nil {
				_ = data.Set("premium_dns_auto_renew", *sub.UseAutoRenew)
			}
			if sub.ExpirationDate != nil {
				_ = data.Set("premium_dns_expires", formatPremiumDNSDate(*sub.ExpirationDate))
			}
		}
	}

	if dns := info.DnsDetails; dns != nil {
		if dns.ProviderType != nil {
			_ = data.Set("dns_provider_type", *dns.ProviderType)
		}
		if dns.IsUsingOurDNS != nil {
			_ = data.Set("is_our_dns", *dns.IsUsingOurDNS)
		}
		if dns.Nameservers != nil {
			_ = data.Set("nameservers", *dns.Nameservers)
		}
		if dns.EmailType != nil {
			_ = data.Set("email_type", *dns.EmailType)
		}
		if dns.HostCount != nil {
			_ = data.Set("host_count", *dns.HostCount)
		}
		if dns.DynamicDNSStatus != nil {
			_ = data.Set("dynamic_dns_status", *dns.DynamicDNSStatus)
		}
		if dns.IsFailover != nil {
			_ = data.Set("is_failover", *dns.IsFailover)
		}
	}

	if mr := info.ModificationRights; mr != nil {
		if mr.All != nil {
			_ = data.Set("modification_rights_all", *mr.All)
		}
		if mr.Rights != nil {
			rights := make(map[string]interface{}, len(*mr.Rights))
			for _, r := range *mr.Rights {
				if r.Type != nil {
					// Value is XML chardata; trim in case the API pretty-prints.
					rights[*r.Type] = strings.TrimSpace(derefString(r.Value))
				}
			}
			_ = data.Set("modification_rights", rights)
		}
	}

	// getInfo does not carry the registrar-lock and domain auto-renew booleans,
	// and its is_expired can only be derived. Fetch those three from the account
	// portfolio listing. getInfo above has already confirmed the domain exists,
	// so a portfolio miss leaves the booleans at their zero values and is_expired
	// at the derived fallback (with a warning) rather than failing the lookup.
	lifecycleDiags := setDomainLifecycleFromList(ctx, client, data, domain)
	if lifecycleDiags.HasError() {
		return lifecycleDiags
	}

	data.SetId(domain)
	// Propagate any non-error diagnostics (e.g. the portfolio-miss warning)
	// without discarding the successful read.
	return lifecycleDiags
}

// isStatusExpired reports whether getInfo's Status attribute says the domain is
// expired. A nil or unrecognized status reports false.
func isStatusExpired(status *string) bool {
	return status != nil && strings.EqualFold(*status, "Expired")
}

// mapGetInfoWhoisGuard converts getInfo's Whoisguard Enabled vocabulary
// ("True"/"False"/"NotAlloted") onto the whois_guard attribute's documented
// getList vocabulary (ENABLED/DISABLED/NOTPRESENT), so the attribute's values
// stay stable regardless of which API command sourced them. Unknown values pass
// through uppercased rather than being dropped.
func mapGetInfoWhoisGuard(enabled string) string {
	switch strings.ToLower(enabled) {
	case "true":
		return "ENABLED"
	case "false":
		return "DISABLED"
	case "notalloted", "notallotted":
		return "NOTPRESENT"
	default:
		return strings.ToUpper(enabled)
	}
}
