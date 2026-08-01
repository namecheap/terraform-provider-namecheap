package namecheap_provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the account-balance and TLD-pricing ReadContext functions
// directly against an in-process httptest server driving the real
// go-namecheap-sdk client, in the same style as data_source_read_test.go. They
// are ordinary unit tests (no build tag), so the money-formatting and
// error-surface paths are covered by the standard `make test` run.

// --- XML builders (mirror the real API envelopes the SDK parses) ------------

func xmlGetBalances(currency, available, account, earned, withdrawable, autoRenew string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.users.getBalances">
    <UserGetBalancesResult Currency="%s" AvailableBalance="%s" AccountBalance="%s" EarnedAmount="%s" WithdrawableAmount="%s" FundsRequiredForAutoRenew="%s" />
  </CommandResponse>
</ApiResponse>`, currency, available, account, earned, withdrawable, autoRenew)
}

// dsPriceTier is a single <Price> node for building a getPricing response. An
// empty string field is rendered as an omitted attribute, so a test can model
// the live API's habit of dropping Currency or PromotionPrice entirely.
type dsPriceTier struct {
	Duration                                            int
	DurationType                                        string
	Price, RegularPrice, YourPrice, Currency, Promotion string
}

func (p dsPriceTier) attrs() string {
	attrs := []string{
		fmt.Sprintf(`Duration="%d"`, p.Duration),
		fmt.Sprintf(`DurationType="%s"`, p.DurationType),
	}
	for _, a := range []struct{ name, value string }{
		{"Price", p.Price}, {"RegularPrice", p.RegularPrice}, {"YourPrice", p.YourPrice},
	} {
		attrs = append(attrs, fmt.Sprintf(`%s="%s"`, a.name, a.value))
	}
	if p.Currency != "" {
		attrs = append(attrs, fmt.Sprintf(`Currency="%s"`, p.Currency))
	}
	if p.Promotion != "" {
		attrs = append(attrs, fmt.Sprintf(`PromotionPrice="%s"`, p.Promotion))
	}
	return strings.Join(attrs, " ")
}

func xmlGetPricing(category, product string, tiers ...dsPriceTier) string {
	var prices []string
	for _, t := range tiers {
		prices = append(prices, fmt.Sprintf(`<Price %s />`, t.attrs()))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.users.getPricing">
    <UserGetPricingResult>
      <ProductType Name="domains">
        <ProductCategory Name="%s">
          <Product Name="%s">
            %s
          </Product>
        </ProductCategory>
      </ProductType>
    </UserGetPricingResult>
  </CommandResponse>
</ApiResponse>`, category, product, strings.Join(prices, "\n            "))
}

// --- namecheap_account_balance ----------------------------------------------

func TestDataSourceAccountBalanceRead_Success(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command == "namecheap.users.getBalances" {
			return xmlGetBalances("USD", "123.45", "123.45", "15.00", "100.00", "42.50")
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapAccountBalance().Schema, map[string]interface{}{})
	diags := dataSourceNamecheapAccountBalanceRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	assert.Equal(t, "USD", d.Get("currency"))
	// Money keeps the server's exact decimal string — no float round-trip, and
	// no reformatting of trailing zeros.
	assert.Equal(t, "123.45", d.Get("available_balance"))
	assert.Equal(t, "123.45", d.Get("account_balance"))
	assert.Equal(t, "15.00", d.Get("earned_amount"))
	assert.Equal(t, "100.00", d.Get("withdrawable_amount"))
	assert.Equal(t, "42.50", d.Get("funds_required_for_auto_renew"))
	assert.Equal(t, "account_balance", d.Id(), "the ID is part of the data source's contract; changing it moves every consumer's state address")
}

// TestDataSourceAccountBalanceRead_PrecisionPreserved pins the decimal-safety
// contract with values a float64 round-trip would visibly damage.
func TestDataSourceAccountBalanceRead_PrecisionPreserved(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command == "namecheap.users.getBalances" {
			return xmlGetBalances("EUR", "10.87", "0.00", "1234567.89", "0.10", "8.881")
		}
		return apiErrorXML("1010101", "unexpected command "+command)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapAccountBalance().Schema, map[string]interface{}{})
	diags := dataSourceNamecheapAccountBalanceRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	assert.Equal(t, "EUR", d.Get("currency"))
	assert.Equal(t, "10.87", d.Get("available_balance"))
	assert.Equal(t, "0.00", d.Get("account_balance"), "a zero balance must stay \"0.00\", not become \"0\"")
	assert.Equal(t, "1234567.89", d.Get("earned_amount"))
	assert.Equal(t, "0.10", d.Get("withdrawable_amount"), "trailing zero must survive")
	assert.Equal(t, "8.881", d.Get("funds_required_for_auto_renew"), "third decimal must survive")
}

func TestDataSourceAccountBalanceRead_APIError(t *testing.T) {
	client := startDataSourceServer(t, func(string, *http.Request) string {
		return apiErrorXML("1011102", "API Key is invalid or API access has not been enabled")
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapAccountBalance().Schema, map[string]interface{}{})
	diags := dataSourceNamecheapAccountBalanceRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "an API error should surface")
	assert.Empty(t, d.Id(), "a failed read must not set an ID")
}

// TestDataSourceAccountBalanceRead_MalformedEnvelope covers a Status=OK response
// whose CommandResponse carries no result element. The SDK reports no error for
// it, so the provider's own guard is what stops a nil dereference — and what
// stops an apparently-successful read of an all-empty balance.
func TestDataSourceAccountBalanceRead_MalformedEnvelope(t *testing.T) {
	client := startDataSourceServer(t, func(string, *http.Request) string {
		return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.users.getBalances"></CommandResponse>
</ApiResponse>`
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapAccountBalance().Schema, map[string]interface{}{})
	diags := dataSourceNamecheapAccountBalanceRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "a result-less response must not read as a zero balance")
	assert.Equal(t, "", d.Get("available_balance"))
	assert.Empty(t, d.Id(), "a failed read must not set an ID")
}

// --- namecheap_tld_pricing ---------------------------------------------------

func TestDataSourceTldPricingRead_Success(t *testing.T) {
	var calls int32
	client := startDataSourceServer(t, func(command string, r *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		atomic.AddInt32(&calls, 1)
		// The request must be narrowed server-side rather than fetching the whole
		// sheet and filtering client-side.
		if got := r.FormValue("ProductType"); got != "DOMAIN" {
			return apiErrorXML("1010101", "unexpected ProductType "+got)
		}
		if got := r.FormValue("ActionName"); got != "REGISTER" {
			return apiErrorXML("1010101", "unexpected ActionName "+got)
		}
		if got := r.FormValue("ProductName"); got != "com" {
			return apiErrorXML("1010101", "unexpected ProductName "+got)
		}
		return xmlGetPricing("register", "com",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "8.88", RegularPrice: "10.87", YourPrice: "9.99", Currency: "USD"},
			dsPriceTier{Duration: 2, DurationType: "YEAR", Price: "17.76", RegularPrice: "21.74", YourPrice: "19.98", Currency: "USD"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "com", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	assert.Equal(t, "8.88", d.Get("price"))
	assert.Equal(t, "10.87", d.Get("regular_price"))
	assert.Equal(t, "9.99", d.Get("your_price"))
	assert.Equal(t, "", d.Get("promo_price"), "no promotion in this sheet")
	assert.Equal(t, "USD", d.Get("currency"))
	assert.Equal(t, "YEAR", d.Get("duration_type"))
	assert.Equal(t, "pricing:com:REGISTER:1", d.Id())
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "exactly one getPricing call per read")
}

func TestDataSourceTldPricingRead_MultiYearTier(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		return xmlGetPricing("register", "com",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "8.88", RegularPrice: "10.87", YourPrice: "9.99", Currency: "USD"},
			dsPriceTier{Duration: 2, DurationType: "YEAR", Price: "17.76", RegularPrice: "21.74", YourPrice: "19.98", Currency: "USD"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "com", "action": "REGISTER", "years": 2,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)
	assert.Equal(t, "17.76", d.Get("price"))
	assert.Equal(t, "pricing:com:REGISTER:2", d.Id())
}

// TestDataSourceTldPricingRead_Promotion covers the attributes that only exist
// once the SDK parses them: a promotional tier exports promo_price and
// still reports the server-resolved price as what is charged.
func TestDataSourceTldPricingRead_Promotion(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		return xmlGetPricing("register", "shop",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "1.16", RegularPrice: "19.99", YourPrice: "1.16", Currency: "EUR", Promotion: "1.16"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "shop", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	assert.Equal(t, "1.16", d.Get("price"))
	assert.Equal(t, "19.99", d.Get("regular_price"))
	assert.Equal(t, "1.16", d.Get("promo_price"))
	assert.Equal(t, "EUR", d.Get("currency"))
}

// TestDataSourceTldPricingRead_OptionalAttributesAbsent covers the live API's
// habit of omitting Currency and PromotionPrice: the read must succeed and
// export empty strings rather than failing or inventing values.
func TestDataSourceTldPricingRead_OptionalAttributesAbsent(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		return xmlGetPricing("renew", "org",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "0.00", RegularPrice: "9.18", YourPrice: "0.00"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "org", "action": "RENEW", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	// Price and YourPrice are zero, so the effective price falls through to the
	// regular price — the same precedence the SDK documents.
	assert.Equal(t, "9.18", d.Get("price"))
	assert.Equal(t, "", d.Get("currency"))
	assert.Equal(t, "", d.Get("promo_price"))
}

// TestDataSourceTldPricingRead_ZeroPromotionIsNoPromotion pins the behaviour the
// live API forced: it answers un-promoted tiers with PromotionPrice="0.0"
// rather than omitting the attribute (observed for .com REGISTER/RENEW/TRANSFER,
// all with price == regular_price), so a non-positive promotional price must
// export as empty. Without this, promo_price would report a promotion on
// essentially every lookup.
func TestDataSourceTldPricingRead_ZeroPromotionIsNoPromotion(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		return xmlGetPricing("register", "net",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "12.00", RegularPrice: "12.00", YourPrice: "12.00", Currency: "USD", Promotion: "0.0"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "net", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)
	assert.Equal(t, "", d.Get("promo_price"), "a non-positive promotion is not a promotion")
	assert.Equal(t, "12.00", d.Get("price"), "the charged price is unaffected by the promotion attribute")
}

// TestDataSourceTldPricingRead_PrecedenceRungs walks each rung of the three-level
// precedence the docs promise. The middle rung — falling through a zero Price to
// YourPrice — is the one a two-level implementation would still pass without.
func TestDataSourceTldPricingRead_PrecedenceRungs(t *testing.T) {
	tests := []struct {
		name      string
		tier      dsPriceTier
		wantPrice string
	}{
		{
			name:      "server price wins",
			tier:      dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "8.88", RegularPrice: "10.87", YourPrice: "9.99"},
			wantPrice: "8.88",
		},
		{
			name:      "zero server price falls through to your price",
			tier:      dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "0.00", RegularPrice: "10.87", YourPrice: "9.99"},
			wantPrice: "9.99",
		},
		{
			name:      "zero server and account price fall through to regular",
			tier:      dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "0.00", RegularPrice: "10.87", YourPrice: "0.00"},
			wantPrice: "10.87",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tier := tc.tier
			client := startDataSourceServer(t, func(command string, _ *http.Request) string {
				if command != "namecheap.users.getPricing" {
					return apiErrorXML("1010101", "unexpected command "+command)
				}
				return xmlGetPricing("register", "com", tier)
			})

			d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
				"tld": "com", "action": "REGISTER", "years": 1,
			})
			diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
			require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)
			assert.Equal(t, tc.wantPrice, d.Get("price"))
		})
	}
}

// TestDataSourceTldPricingRead_DurationTypeVerbatim asserts duration_type is the
// server's own value rather than a hardcoded "YEAR". The SDK matches the tier
// case-insensitively, so a server that answers "Year" must export "Year".
func TestDataSourceTldPricingRead_DurationTypeVerbatim(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		return xmlGetPricing("register", "com",
			dsPriceTier{Duration: 1, DurationType: "Year", Price: "8.88", RegularPrice: "10.87", YourPrice: "8.88", Currency: "USD"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "com", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)
	assert.Equal(t, "Year", d.Get("duration_type"), "duration_type must be the server's value, not a constant")
}

// TestDataSourceTldPricingRead_MultiLabelTld covers the dotted product name the
// docs advertise (co.uk). Nothing else in the suite proves a multi-label TLD
// resolves, so without this the docs' central example is an untested claim.
func TestDataSourceTldPricingRead_MultiLabelTld(t *testing.T) {
	var sentProduct string
	client := startDataSourceServer(t, func(command string, r *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		sentProduct = r.FormValue("ProductName")
		return xmlGetPricing("register", "co.uk",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "7.48", RegularPrice: "9.98", YourPrice: "7.48", Currency: "GBP"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "co.uk", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)
	assert.Equal(t, "co.uk", sentProduct)
	assert.Equal(t, "7.48", d.Get("price"))
	assert.Equal(t, "GBP", d.Get("currency"))
	assert.Equal(t, "pricing:co.uk:REGISTER:1", d.Id())
}

// TestDataSourceTldPricingRead_BlankPromotionIsAbsent covers a promotional
// attribute the server sends as whitespace. It must read as absent rather than
// being exported as a blank-looking price, which is the one behaviour that
// distinguishes going through Promo() from reading the raw field.
func TestDataSourceTldPricingRead_BlankPromotionIsAbsent(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		return xmlGetPricing("register", "info",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "3.98", RegularPrice: "3.98", YourPrice: "3.98", Currency: "USD", Promotion: "   "},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "info", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)
	assert.Equal(t, "", d.Get("promo_price"), "a whitespace-only promotion is not a promotion")
	assert.Equal(t, "3.98", d.Get("price"))
}

// TestDataSourceTldPricingRead_MalformedEnvelope covers a Status=OK response
// whose CommandResponse carries no result element. The SDK reports no error for
// it, so without the provider's own guard the read would dereference a nil
// result; the guard must turn it into a diagnostic instead.
func TestDataSourceTldPricingRead_MalformedEnvelope(t *testing.T) {
	client := startDataSourceServer(t, func(string, *http.Request) string {
		return `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors />
  <CommandResponse Type="namecheap.users.getPricing"></CommandResponse>
</ApiResponse>`
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "com", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "a result-less response must not read as a valid price")
	assert.Contains(t, diags[0].Summary, "com")
	assert.Empty(t, d.Id(), "a failed read must not set an ID")
}

// TestDataSourceTldPricingRead_CaseNormalization proves the request is
// normalized before it reaches the API: an uppercase TLD and a lowercase action
// resolve, and the ID is built from the normalized values. The lowercase action
// is reachable from a real config because the validator ignores case — see
// TestAccMockDataSourceTldPricingLowercaseAction, which drives it through
// Terraform rather than around the validator as this test does.
func TestDataSourceTldPricingRead_CaseNormalization(t *testing.T) {
	var sentProduct, sentAction string
	client := startDataSourceServer(t, func(command string, r *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		sentProduct = r.FormValue("ProductName")
		sentAction = r.FormValue("ActionName")
		return xmlGetPricing("transfer", "com",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "9.06", RegularPrice: "10.87", YourPrice: "9.06", Currency: "USD"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "COM", "action": "transfer", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.False(t, diags.HasError(), "unexpected diagnostics: %+v", diags)

	assert.Equal(t, "com", sentProduct, "TLD should be lower-cased before the request")
	assert.Equal(t, "TRANSFER", sentAction, "action should be upper-cased before the request")
	assert.Equal(t, "9.06", d.Get("price"))
	assert.Equal(t, "pricing:com:TRANSFER:1", d.Id())
}

// TestDataSourceTldPricingRead_TierNotFound covers a TLD the API answers for but
// publishes no matching tier: the diagnostic must name the TLD, action and term
// rather than surfacing a nil-pointer or an empty price.
func TestDataSourceTldPricingRead_TierNotFound(t *testing.T) {
	client := startDataSourceServer(t, func(command string, _ *http.Request) string {
		if command != "namecheap.users.getPricing" {
			return apiErrorXML("1010101", "unexpected command "+command)
		}
		return xmlGetPricing("register", "com",
			dsPriceTier{Duration: 1, DurationType: "YEAR", Price: "8.88", RegularPrice: "10.87", YourPrice: "9.99", Currency: "USD"},
		)
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "com", "action": "REGISTER", "years": 9,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "a missing tier should be an error, not an empty price")
	assert.Contains(t, diags[0].Summary, "com")
	assert.Contains(t, diags[0].Summary, "9 year")
	assert.Empty(t, d.Id(), "a failed read must not set an ID")
}

func TestDataSourceTldPricingRead_APIError(t *testing.T) {
	client := startDataSourceServer(t, func(string, *http.Request) string {
		return apiErrorXML("2011170", "Promotion code is invalid")
	})

	d := schema.TestResourceDataRaw(t, dataSourceNamecheapTldPricing().Schema, map[string]interface{}{
		"tld": "xyz", "action": "REGISTER", "years": 1,
	})
	diags := dataSourceNamecheapTldPricingRead(context.Background(), d, client)
	require.True(t, diags.HasError(), "an API error should surface")
	assert.Contains(t, diags[0].Summary, "xyz", "diagnostic should name the TLD")
	assert.Contains(t, diags[0].Summary, "REGISTER", "diagnostic should name the action")
}

// TestValidateTld pins the plan-time input validation, which is what keeps a
// mistyped TLD from becoming a confusing empty-response error at apply time.
func TestValidateTld(t *testing.T) {
	tests := []struct {
		name    string
		tld     string
		wantErr bool
	}{
		{"simple tld", "com", false},
		{"multi-label tld", "co.uk", false},
		{"idn-style tld", "xn--p1ai", false},
		{"empty", "", true},
		{"leading dot", ".com", true},
		{"leading whitespace", " com", true},
		{"trailing whitespace", "com ", true},
		{"url path", "example.com/path", true},
		{"trailing dot", "com.", true},
		{"internal newline", "co\nuk", true},
		// A bare domain name cannot be rejected: it is structurally identical to a
		// legitimate multi-label TLD, so it is accepted here and fails later as
		// "no published price".
		{"bare domain name is accepted", "example.com", false},
		{"url", "https://example.com", true},
		{"space inside", "co uk", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := validateTld(tc.tld, "tld")
			if tc.wantErr {
				assert.NotEmpty(t, errs, "validateTld(%q) = no error, want error", tc.tld)
				return
			}
			assert.Empty(t, errs, "validateTld(%q) = %v, want no error", tc.tld, errs)
		})
	}
}

// TestDataSourcePricingSchemasAreReadOnly guards the compatibility contract of
// this change: both data sources are read-only, so every attribute other than
// the documented inputs must be Computed and none may be Required except tld.
// A future edit that makes an attribute writable would change plan behaviour for
// existing configurations, and this test fails first.
func TestDataSourcePricingSchemasAreReadOnly(t *testing.T) {
	inputs := map[string]bool{"tld": true, "action": true, "years": true}

	for name, ds := range map[string]*schema.Resource{
		"namecheap_account_balance": dataSourceNamecheapAccountBalance(),
		"namecheap_tld_pricing":     dataSourceNamecheapTldPricing(),
	} {
		for attr, s := range ds.Schema {
			if inputs[attr] {
				continue
			}
			assert.True(t, s.Computed, "%s.%s must be Computed", name, attr)
			assert.False(t, s.Required, "%s.%s must not be Required", name, attr)
			assert.False(t, s.Optional, "%s.%s must not be Optional", name, attr)
		}
		assert.Nil(t, ds.CreateContext, "%s must not define a create", name)
		assert.Nil(t, ds.UpdateContext, "%s must not define an update", name)
		assert.Nil(t, ds.DeleteContext, "%s must not define a delete", name)
	}
}
