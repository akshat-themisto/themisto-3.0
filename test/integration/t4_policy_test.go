package integration

import (
	"testing"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/policy"
	"github.com/themisto/agent/test/integration/testutil"
)

// T4.1 — Initial policy load.
func TestT4_1_InitialPolicyLoad(t *testing.T) {
	engine := policy.NewEngine(domain.DecisionForward)

	payload := testutil.TestPolicy("v1")
	err := engine.Update(payload)
	testutil.AssertNoError(t, err, "update engine with policy v1")
	testutil.AssertEqual(t, engine.Version(), "v1", "policy version")

	// All three rules should be active.
	d, rid, err := engine.Apply(&domain.RequestContext{Host: "foo.example.com"})
	testutil.AssertNoError(t, err, "apply allow")
	testutil.AssertEqual(t, d, domain.DecisionForward, "forward for example.com")
	testutil.AssertEqual(t, rid, "rule-allow", "rule ID")

	d, rid, _ = engine.Apply(&domain.RequestContext{Host: "app.blocked.test"})
	testutil.AssertEqual(t, d, domain.DecisionBlock, "block for blocked.test")
	testutil.AssertEqual(t, rid, "rule-block", "rule ID")

	d, rid, _ = engine.Apply(&domain.RequestContext{Host: "internal.bypass.test"})
	testutil.AssertEqual(t, d, domain.DecisionBypass, "bypass for bypass.test")
	testutil.AssertEqual(t, rid, "rule-bypass", "rule ID")
}

// T4.2 — Policy update during runtime replaces rules atomically.
func TestT4_2_PolicyUpdateRuntime(t *testing.T) {
	engine := policy.NewEngine(domain.DecisionForward)
	engine.Update(testutil.TestPolicy("v1"))

	// Verify v1 behavior.
	d, _, _ := engine.Apply(&domain.RequestContext{Host: "app.newblocked.test"})
	testutil.AssertEqual(t, d, domain.DecisionForward, "v1: newblocked.test not blocked yet")

	// Update to v2 with a new block rule.
	v2 := testutil.TestPolicy("v2")
	v2.Rules = append(v2.Rules, domain.PolicyRule{
		ID:       "rule-newblock",
		Priority: 15,
		Decision: domain.DecisionBlock,
		Conditions: []domain.RuleCondition{
			{Field: "host", Operator: "suffix", Value: ".newblocked.test"},
		},
	})
	err := engine.Update(v2)
	testutil.AssertNoError(t, err, "update to v2")
	testutil.AssertEqual(t, engine.Version(), "v2", "version is v2")

	// Verify v2 behavior: newblocked.test is now blocked.
	d, rid, _ := engine.Apply(&domain.RequestContext{Host: "app.newblocked.test"})
	testutil.AssertEqual(t, d, domain.DecisionBlock, "v2: newblocked.test is blocked")
	testutil.AssertEqual(t, rid, "rule-newblock", "matched new rule")

	// Existing rules still work.
	d, _, _ = engine.Apply(&domain.RequestContext{Host: "foo.example.com"})
	testutil.AssertEqual(t, d, domain.DecisionForward, "v2: example.com still forwarded")
}

// T4.3 — Policy fetch failure: engine keeps last-known-good policy.
func TestT4_3_PolicyFetchFailureKeepsOld(t *testing.T) {
	engine := policy.NewEngine(domain.DecisionForward)

	// Load v1 successfully.
	engine.Update(testutil.TestPolicy("v1"))

	// Attempt a bad update (nil payload).
	err := engine.Update(nil)
	testutil.AssertError(t, err, "nil payload should fail")

	// Engine still has v1.
	testutil.AssertEqual(t, engine.Version(), "v1", "version still v1 after failed update")
	d, _, _ := engine.Apply(&domain.RequestContext{Host: "app.blocked.test"})
	testutil.AssertEqual(t, d, domain.DecisionBlock, "v1 rules still active")
}

// Test default decision when no rules match.
func TestT4_DefaultDecision(t *testing.T) {
	engine := policy.NewEngine(domain.DecisionBypass)
	engine.Update(testutil.TestPolicy("v1"))

	// Host that matches no rule.
	d, rid, err := engine.Apply(&domain.RequestContext{Host: "unknown.host.org"})
	testutil.AssertNoError(t, err, "apply unmatched")
	testutil.AssertEqual(t, d, domain.DecisionBypass, "default decision applied")
	testutil.AssertEqual(t, rid, "", "no rule matched")
}
