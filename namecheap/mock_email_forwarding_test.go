//go:build testacc

package namecheap_provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const mockEmailForwardingDomain = "mock-forwarding-example.com"

// planCheckFunc adapts a plain func to plancheck.PlanCheck, so a call-count
// assertion can run via ConfigPlanChecks - the mechanism that actually
// executes for a PlanOnly step. TestStep.Check does not: it is only invoked
// for non-PlanOnly steps (terraform-plugin-testing v1.16.0,
// helper/resource/testing_new_config.go), so a Check on a PlanOnly step is
// silently skipped and never asserts anything.
type planCheckFunc func(context.Context, plancheck.CheckPlanRequest, *plancheck.CheckPlanResponse)

func (f planCheckFunc) CheckPlan(ctx context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	f(ctx, req, resp)
}

// mockCheckForward asserts the mock persisted the given destination for a
// mailbox alias on the domain.
func mockCheckForward(m *namecheapMock, domain, mailbox, wantDest string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return fmt.Errorf("mock has no state for %q", domain)
		}
		got, ok := st.forwards[mailbox]
		if !ok {
			return fmt.Errorf("mock state for %q missing forward %q (have %+v)", domain, mailbox, st.forwards)
		}
		if got != wantDest {
			return fmt.Errorf("mock forward %s/%s = %q, want %q", domain, mailbox, got, wantDest)
		}
		return nil
	}
}

// mockCheckForwardCount asserts the mock persisted exactly n forwarding rules
// for the domain.
func mockCheckForwardCount(m *namecheapMock, domain string, n int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st == nil {
			return fmt.Errorf("mock has no state for %q", domain)
		}
		if len(st.forwards) != n {
			return fmt.Errorf("mock forward count for %q = %d, want %d (have %+v)", domain, len(st.forwards), n, st.forwards)
		}
		return nil
	}
}

// mockCheckForwardsCleared is a CheckDestroy that asserts the domain's
// forwarding table was cleared when the resource was destroyed.
func mockCheckForwardsCleared(m *namecheapMock, domain string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		st := m.state(domain)
		if st != nil && len(st.forwards) != 0 {
			return fmt.Errorf("expected forwards for %q to be cleared on destroy, mock still has %+v", domain, st.forwards)
		}
		return nil
	}
}

func emailForwardingConfig(domain string, forwards map[string]string) string {
	var lines string
	for mailbox, dest := range forwards {
		lines += fmt.Sprintf("    %q = %q\n", mailbox, dest)
	}
	return fmt.Sprintf(`
resource "namecheap_email_forwarding" "test" {
  domain = %q

  forwards = {
%s  }
}
`, domain, lines)
}

// TestAccMockEmailForwardingLifecycle drives create -> update (add/change/
// remove entries) -> import -> destroy (table cleared) against the stateful
// mock, asserting both Terraform state and the mock backend at each step.
func TestAccMockEmailForwardingLifecycle(t *testing.T) {
	m := newNamecheapMock(t)
	const resourceName = "namecheap_email_forwarding.test"

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckForwardsCleared(m, mockEmailForwardingDomain),
		Steps: []resource.TestStep{
			{
				Config: emailForwardingConfig(mockEmailForwardingDomain, map[string]string{
					"info": "info-dest@example.com",
				}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "forwards.%", "1"),
					resource.TestCheckResourceAttr(resourceName, "forwards.info", "info-dest@example.com"),
					mockCheckForwardCount(m, mockEmailForwardingDomain, 1),
					mockCheckForward(m, mockEmailForwardingDomain, "info", "info-dest@example.com"),
				),
			},
			{
				// Add a new alias, change an existing destination.
				Config: emailForwardingConfig(mockEmailForwardingDomain, map[string]string{
					"info":  "changed@example.com",
					"sales": "sales-dest@example.com",
				}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "forwards.%", "2"),
					mockCheckForwardCount(m, mockEmailForwardingDomain, 2),
					mockCheckForward(m, mockEmailForwardingDomain, "info", "changed@example.com"),
					mockCheckForward(m, mockEmailForwardingDomain, "sales", "sales-dest@example.com"),
				),
			},
			{
				// Remove "sales" - full-table replace must drop it.
				Config: emailForwardingConfig(mockEmailForwardingDomain, map[string]string{
					"info": "changed@example.com",
				}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "forwards.%", "1"),
					mockCheckForwardCount(m, mockEmailForwardingDomain, 1),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateId:     mockEmailForwardingDomain,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccMockEmailForwardingFullOwnershipAndDrift covers the full-ownership
// contract: create replaces any pre-seeded (out-of-band) rules entirely, and
// a rule added out-of-band after that surfaces as drift on the next refresh
// rather than being silently ignored or merged.
func TestAccMockEmailForwardingFullOwnershipAndDrift(t *testing.T) {
	m := newNamecheapMock(t)
	const resourceName = "namecheap_email_forwarding.test"

	// Pre-seed a rule as if it were created out-of-band (e.g. via the
	// dashboard) before Terraform ever manages this domain.
	m.seedForwards(mockEmailForwardingDomain, map[string]string{
		"legacy": "legacy-dest@example.com",
	})

	config := emailForwardingConfig(mockEmailForwardingDomain, map[string]string{
		"info": "info-dest@example.com",
	})

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckForwardsCleared(m, mockEmailForwardingDomain),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "forwards.%", "1"),
					mockCheckForwardCount(m, mockEmailForwardingDomain, 1),
					mockCheckForward(m, mockEmailForwardingDomain, "info", "info-dest@example.com"),
				),
			},
			{
				// An out-of-band rule appears after Terraform has taken
				// ownership - the next refresh must surface it as drift.
				PreConfig: func() {
					m.addForward(mockEmailForwardingDomain, "extra", "extra-dest@example.com")
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestAccMockEmailForwardingCallCounts covers the "no extra API calls" AC:
// refresh/plan issues exactly one getEmailForwarding and nothing else; create
// issues exactly one setEmailForwarding and one getHosts (the conflict check).
func TestAccMockEmailForwardingCallCounts(t *testing.T) {
	m := newNamecheapMock(t)
	config := emailForwardingConfig(mockEmailForwardingDomain, map[string]string{
		"info": "info-dest@example.com",
	})

	var getEmailForwardingBeforePlan, getHostsBeforePlan int

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckForwardsCleared(m, mockEmailForwardingDomain),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(*terraform.State) error {
					if got := m.commandCount("namecheap.domains.dns.setEmailForwarding"); got != 1 {
						return fmt.Errorf("create issued %d setEmailForwarding call(s), want exactly 1", got)
					}
					if got := m.commandCount("namecheap.domains.dns.getHosts"); got != 1 {
						return fmt.Errorf("create issued %d getHosts call(s) (the conflict check), want exactly 1", got)
					}
					return nil
				},
			},
			{
				// A plan-only step against the unchanged config must issue
				// exactly one more getEmailForwarding (the refresh read) and
				// no getHosts at all - the conflict check only runs on
				// apply. Asserted via ConfigPlanChecks.PostApplyPostRefresh
				// (which does run for PlanOnly steps) against a baseline
				// snapshotted in PreConfig, rather than Check (which does
				// not run for PlanOnly steps at all).
				PreConfig: func() {
					getEmailForwardingBeforePlan = m.commandCount("namecheap.domains.dns.getEmailForwarding")
					getHostsBeforePlan = m.commandCount("namecheap.domains.dns.getHosts")
				},
				Config:   config,
				PlanOnly: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{
						planCheckFunc(func(_ context.Context, _ plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
							if got := m.commandCount("namecheap.domains.dns.getEmailForwarding") - getEmailForwardingBeforePlan; got != 1 {
								resp.Error = fmt.Errorf("plan issued %d getEmailForwarding call(s), want exactly 1", got)
								return
							}
							if got := m.commandCount("namecheap.domains.dns.getHosts") - getHostsBeforePlan; got != 0 {
								resp.Error = fmt.Errorf("plan added %d getHosts call(s), want exactly 0 - the conflict check must only run on apply", got)
							}
						}),
					},
				},
			},
		},
	})
}

// TestAccMockEmailForwardingSucceedsDespiteConflict proves apply is never
// blocked by the DNS-mode/email_type mismatch the conflict check warns
// about - only a warning diagnostic is emitted (content is unit-tested in
// namecheap_email_forwarding_test.go); the forwards are still persisted.
func TestAccMockEmailForwardingSucceedsDespiteConflict(t *testing.T) {
	m := newNamecheapMock(t)
	const resourceName = "namecheap_email_forwarding.test"

	// Seed a non-FWD email type so the conflict check's second warning path
	// fires; apply must still succeed and persist the forwards.
	m.seed(mockEmailForwardingDomain, nil, "MX", nil)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		CheckDestroy:      mockCheckForwardsCleared(m, mockEmailForwardingDomain),
		Steps: []resource.TestStep{
			{
				Config: emailForwardingConfig(mockEmailForwardingDomain, map[string]string{
					"info": "info-dest@example.com",
				}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "forwards.%", "1"),
					mockCheckForward(m, mockEmailForwardingDomain, "info", "info-dest@example.com"),
				),
			},
		},
	})
}

// TestAccMockEmailForwardingSetAPIError covers the negative path: a
// setEmailForwarding failure must surface as an error and must not persist
// any state.
func TestAccMockEmailForwardingSetAPIError(t *testing.T) {
	m := newNamecheapMock(t)
	m.failOn("namecheap.domains.dns.setEmailForwarding", "2019166", "Domain not found")

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { mockPreCheck(t, m) },
		ProviderFactories: mockProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: emailForwardingConfig(mockEmailForwardingDomain, map[string]string{
					"info": "info-dest@example.com",
				}),
				ExpectError: regexp.MustCompile(`Domain not found`),
			},
		},
	})
}
