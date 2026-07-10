package namecheap_provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/namecheap/go-namecheap-sdk/v2/namecheap"
)

const (
	domainsListTypeAll      = "ALL"
	domainsListTypeExpiring = "EXPIRING"
	domainsListTypeExpired  = "EXPIRED"

	// domainsPageSize is the per-request page size used while paginating
	// getList. The Namecheap API caps PageSize at 100.
	domainsPageSize = 100
)

// dataSourceNamecheapDomains lists the account's domain portfolio via the
// namecheap.domains.getList API command, auto-paginating across all pages so
// the returned domains attribute always reflects the complete result set for
// the given filters.
func dataSourceNamecheapDomains() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceNamecheapDomainsRead,
		Schema: map[string]*schema.Schema{
			"search_term": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional keyword to filter the returned domains (maps to the getList SearchTerm parameter).",
			},
			"list_type": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      domainsListTypeAll,
				ValidateFunc: validation.StringInSlice([]string{domainsListTypeAll, domainsListTypeExpiring, domainsListTypeExpired}, false),
				Description:  "Which subset of the account's domains to return. Possible values: ALL (default), EXPIRING, EXPIRED (maps to the getList ListType parameter).",
			},
			"domains": {
				Type:        schema.TypeList,
				Computed:    true,
				Description: "The domains in the account matching the filters.",
				Elem: &schema.Resource{
					Schema: domainPortfolioElemSchema(),
				},
			},
		},
	}
}

func dataSourceNamecheapDomainsRead(ctx context.Context, data *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*namecheap.Client)

	listType := data.Get("list_type").(string)
	searchTerm := data.Get("search_term").(string)

	// fetchAllDomains walks every page of the filtered listing (shared with the
	// single-domain lifecycle lookup) so the returned set is always complete.
	allDomains, err := fetchAllDomains(ctx, client, listType, searchTerm)
	if err != nil {
		return diagFromClientError(err)
	}

	now := time.Now().UTC()
	result := make([]map[string]interface{}, 0, len(allDomains))
	for i := range allDomains {
		result = append(result, flattenPortfolioDomain(&allDomains[i], now))
	}

	if err := data.Set("domains", result); err != nil {
		return diag.FromErr(err)
	}

	data.SetId(fmt.Sprintf("domains:%s:%s", listType, searchTerm))
	return nil
}
