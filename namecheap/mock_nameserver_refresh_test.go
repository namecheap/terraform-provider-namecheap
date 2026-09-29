//go:build testacc

package namecheap_provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccMockNameserversRefreshCallCount proves a refresh of custom
// nameservers costs one getList per domain in both MERGE and OVERWRITE mode:
// Read reuses the response it fetched for the IsUsingOurDNS check instead of
// requesting the same list again. Every call counts against the account's API
// rate limit, so a doubled read doubles the wall time of a large refresh.
func TestAccMockNameserversRefreshCallCount(t *testing.T) {
	for _, mode := range []string{"MERGE", "OVERWRITE"} {
		t.Run(mode, func(t *testing.T) {
			m := newNamecheapMock(t)
			config := mockNameserversConfig(mode, "dns1.namecheaphosting.com", "dns2.namecheaphosting.com")

			var getListBeforePlan, getHostsBeforePlan int

			resource.Test(t, resource.TestCase{
				PreCheck:          func() { mockPreCheck(t, m) },
				ProviderFactories: mockProviderFactories(),
				CheckDestroy:      mockCheckNameserversDefault(m, mockScenarioDomain),
				Steps: []resource.TestStep{
					{Config: config},
					{
						// Check does not run for PlanOnly steps, so the count is
						// asserted in PostApplyPostRefresh against a baseline
						// snapshotted in PreConfig.
						PreConfig: func() {
							getListBeforePlan = m.commandCount("namecheap.domains.dns.getList")
							getHostsBeforePlan = m.commandCount("namecheap.domains.dns.getHosts")
						},
						Config:   config,
						PlanOnly: true,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PostApplyPostRefresh: []plancheck.PlanCheck{
								planCheckFunc(func(_ context.Context, _ plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
									if got := m.commandCount("namecheap.domains.dns.getList") - getListBeforePlan; got != 1 {
										resp.Error = fmt.Errorf("refresh issued %d getList call(s), want exactly 1", got)
										return
									}
									if got := m.commandCount("namecheap.domains.dns.getHosts") - getHostsBeforePlan; got != 0 {
										resp.Error = fmt.Errorf("refresh issued %d getHosts call(s), want 0 for a domain on custom nameservers", got)
									}
								}),
							},
						},
					},
				},
			})
		})
	}
}
