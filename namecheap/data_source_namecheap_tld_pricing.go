package namecheap_provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

const (
	// pricingProductType is the only product type this data source reads. SSL and
	// WhoisGuard pricing live under different product types; exposing them would
	// need a product_type argument and a different attribute shape, so they are
	// deliberately out of scope here.
	pricingProductType = "DOMAIN"

	pricingActionRegister = "REGISTER"
	pricingActionRenew    = "RENEW"
	pricingActionTransfer = "TRANSFER"

	// pricingMaxYears is the longest term the data source will ask about. The
	// API publishes annual tiers well below this; the bound exists so a typo
	// (years = 100) fails at plan time instead of returning "no such tier".
	pricingMaxYears = 10
)

// dataSourceNamecheapTldPricing looks up the published price of one TLD for one
// action and term, so a configuration can compare TLDs or assert a cost ceiling
// before a charge-bearing apply.
//
// Every monetary attribute is exported as a *string* holding the exact decimal
// the API returned, never a number: Terraform numbers are IEEE-754 floats and a
// price that round-trips through a float can silently change. Use tonumber() at
// the comparison site when a numeric check is needed.
func dataSourceNamecheapTldPricing() *schema.Resource {
	return &schema.Resource{
		Description: "Reads Namecheap's published price for one TLD, action and term, so a configuration can compare TLDs or assert a cost ceiling.",
		ReadContext: dataSourceNamecheapTldPricingRead,
		Schema: map[string]*schema.Schema{
			"tld": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "The top-level domain to price, without a leading dot (e.g. com, co.uk).",
				ValidateFunc: validateTld,
			},
			"action": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     pricingActionRegister,
				Description: "The action to price: REGISTER (default), RENEW or TRANSFER.",
				// ignoreCase: "renew" is accepted and normalized to RENEW before the
				// request, so a config is not rejected over letter case.
				ValidateFunc: validation.StringInSlice([]string{pricingActionRegister, pricingActionRenew, pricingActionTransfer}, true),
			},
			"years": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      1,
				Description:  fmt.Sprintf("The term length in years to price (1-%d). Defaults to 1.", pricingMaxYears),
				ValidateFunc: validation.IntBetween(1, pricingMaxYears),
			},
			"price": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The price actually charged for this tier, as an exact decimal string. It resolves the documented precedence: the server's final price (which already reflects any promotion or special), then your account price, then the regular price.",
			},
			"regular_price": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The public list price for this tier, as an exact decimal string.",
			},
			"your_price": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The account-specific price for this tier, as an exact decimal string. Empty when the API does not return one.",
			},
			"promo_price": {
				Type:     schema.TypeString,
				Computed: true,
				Description: "The promotional price for this tier, as an exact decimal string, or empty when no promotion applies. " +
					"Namecheap reports an un-promoted tier as \"0.0\" rather than omitting the value, so a non-positive promotional price is exported as empty here and `promo_price != \"\"` is a meaningful test for \"is this TLD on promotion\". " +
					"An active promotion is already reflected in price, so this attribute identifies the discount rather than adding to it.",
			},
			"currency": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The currency the prices are denominated in (e.g. USD). Empty when the API omits it for this tier; namecheap_account_balance always reports the account currency.",
			},
			"duration_type": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The unit the term is expressed in, as returned by the API (always YEAR for the tiers this data source matches).",
			},
		},
	}
}

func dataSourceNamecheapTldPricingRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	// validateTld has already rejected a leading dot and surrounding whitespace,
	// so case is the only normalization left to do.
	tld := strings.ToLower(data.Get("tld").(string))
	action := strings.ToUpper(data.Get("action").(string))
	years := data.Get("years").(int)

	// Narrow the request server-side. The full price sheet is large; asking for
	// one product and one action keeps the response (and the parse) proportional
	// to the question, and makes the call count exactly one per read.
	resp, err := client.Users.GetPricingWithContext(ctx, &namecheap.UsersGetPricingArgs{
		ProductType: namecheap.String(pricingProductType),
		ActionName:  namecheap.String(action),
		ProductName: namecheap.String(tld),
	})
	if err != nil {
		return dataSourcePricingReadError(tld, action, err)
	}
	if resp == nil || resp.UserGetPricingResult == nil {
		return diag.Errorf("Namecheap returned no pricing information for .%s (%s)", tld, action)
	}

	price, ok := resp.UserGetPricingResult.PriceFor(action, tld, years)
	if !ok {
		return diag.Diagnostics{{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("No %s price published for .%s over %d year(s)", action, tld, years),
			Detail: fmt.Sprintf("Namecheap answered the pricing request but the response contains no %d-year %s tier for .%s. "+
				"The TLD may not be offered, may not support this action, or may not be sold for this term "+
				"(some TLDs have a multi-year minimum, and some are registry-only). Check the TLD spelling, "+
				"then try a different years value.", years, action, tld),
		}}
	}

	// The live API sends PromotionPrice="0.0" on tiers with no promotion, so
	// presence is not the question — IsPositive is. A non-positive promotional
	// price is exported as empty so that promo_price != "" means what a reader
	// assumes it means.
	promo := namecheap.Amount("")
	if price.PromotionPrice.IsPositive() {
		promo = price.PromotionPrice
	}

	_ = data.Set("price", price.EffectivePrice().String())
	_ = data.Set("regular_price", price.RegularPrice.String())
	_ = data.Set("your_price", price.YourPrice.String())
	_ = data.Set("promo_price", promo.String())
	_ = data.Set("currency", price.Currency)
	_ = data.Set("duration_type", price.DurationType)

	data.SetId(fmt.Sprintf("pricing:%s:%s:%d", tld, action, years))
	return nil
}

// validateTld rejects the shapes that would otherwise reach the API as a silent
// "no such product": an empty value, a leading or trailing dot, surrounding
// whitespace, and anything carrying URL punctuation.
//
// It cannot reject a full domain name: "example.com" is structurally identical
// to the legitimate multi-label TLD "co.uk", so that mistake surfaces as the
// read's "no published price" diagnostic instead.
func validateTld(val interface{}, key string) (warns []string, errs []error) {
	tld, _ := val.(string)
	if strings.TrimSpace(tld) != tld {
		errs = append(errs, fmt.Errorf("%s must not have leading or trailing whitespace, got %q", key, tld))
		return
	}
	if tld == "" {
		errs = append(errs, fmt.Errorf("%s must not be empty", key))
		return
	}
	if strings.HasPrefix(tld, ".") {
		errs = append(errs, fmt.Errorf("%s must be written without a leading dot (use \"com\", not \".com\"), got %q", key, tld))
		return
	}
	if strings.HasSuffix(tld, ".") {
		errs = append(errs, fmt.Errorf("%s must not end with a dot, got %q", key, tld))
		return
	}
	if strings.ContainsAny(tld, " /:@\n\t") {
		errs = append(errs, fmt.Errorf("%s must be a top-level domain such as \"com\" or \"co.uk\", not a domain name or URL, got %q", key, tld))
	}
	return
}

// dataSourcePricingReadError converts an SDK error from a pricing lookup into
// diagnostics naming the TLD and action, so a failed lookup in a for_each over
// several TLDs identifies which one failed. It reuses diagFromClientError so
// known Namecheap error codes keep their remediation text.
func dataSourcePricingReadError(tld, action string, err error) diag.Diagnostics {
	diags := diagFromClientError(err)
	for i := range diags {
		diags[i].Summary = fmt.Sprintf("%s (.%s, %s)", diags[i].Summary, tld, action)
	}
	return diags
}
