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

	var allDomains []namecheap.Domain
	for page := 1; ; page++ {
		args := &namecheap.DomainsGetListArgs{
			ListType: namecheap.String(listType),
			Page:     namecheap.Int(page),
			PageSize: namecheap.Int(domainsPageSize),
		}
		if searchTerm != "" {
			args.SearchTerm = namecheap.String(searchTerm)
		}

		resp, err := client.Domains.GetListWithContext(ctx, args)
		if err != nil {
			return diagFromClientError(err)
		}
		if resp == nil {
			return diag.Errorf("Namecheap returned an empty response while listing domains (page %d)", page)
		}

		if resp.Domains != nil {
			allDomains = append(allDomains, *resp.Domains...)
		}

		// Stop when the paging block indicates every item has been fetched.
		// When paging is absent or degenerate, stop after the current page so a
		// malformed response cannot spin an unbounded loop.
		if resp.Paging == nil || resp.Paging.TotalItems == nil || resp.Paging.PageSize == nil || *resp.Paging.PageSize <= 0 {
			break
		}
		if page*(*resp.Paging.PageSize) >= *resp.Paging.TotalItems {
			break
		}
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
