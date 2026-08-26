package scopechange

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// A cognition coverage reduction (index -> observe) planned under inherit/auto
// must still have a reviewer. Before the fix, InteractionRequired was derived
// from the weaker desired mode, so an effective-auto plan reported
// interaction_required=false: `scope approve` answered
// managed_scope_approval_not_required while `scope apply`/`scope authorize`
// were blocked by managed_scope_auto_authorization_blocked
// (p0_or_p1,cognition_coverage_reduction_requires_independent_review). The
// blocked change had no approver and the repository was stuck. The block is a
// fact handed to independent review, not a dead end, so the plan must carry
// interaction_required and the interactive approval must be able to land it.
func TestCoverageReductionDemandsAHumanReviewer(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeAuto)

	// Retire the entry and move main_test.go index -> observe through an
	// ordinary (non-transport) user rule: a real cognition coverage reduction.
	indexPath := filepath.Join(root, "aoci.txt")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "main_test.go[T.RT.5.T]: F:test | R:main.go | A:- | S:-\n", ""))
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Role = machinecontract.ScopeRoleIndex
	state.Files["aoci.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	candidates.Dispositions = nil
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "coverage-test-observe", Action: machinecontract.ScopeRoleObserve,
			Pattern: "main_test.go", PatternKind: machinecontract.ScopePatternFile, Reason: "test-only fixture",
			DecisionBasis: machinecontract.ScopeDecisionUnspecified, Source: machinecontract.ScopeRuleUser,
			CreatedBy: "coverage-test", Order: 0, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Plan.Risk.CognitionCoverageReduction || preview.Plan.Risk.P1 == 0 {
		t.Fatalf("fixture did not produce a coverage reduction: %+v", preview.Plan.Risk)
	}
	if preview.Plan.Risk.TransportConstraintNotAllowed {
		t.Fatalf("fixture unexpectedly transport-constrained: %+v", preview.Plan.Risk)
	}
	if !preview.Plan.InteractionRequired {
		t.Fatal("a coverage reduction planned under auto still needs a reviewer; " +
			"without this the auto blocker has no approver and the repository is stuck")
	}
	if preview.Plan.ConfirmationPhrase == "" {
		t.Fatal("an interactive plan must carry the phrase that binds the approval to it")
	}

	// Policy-bound auto authorization must still refuse a coverage reduction.
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err == nil ||
		!strings.Contains(err.Error(), "coverage_reduction") {
		t.Fatalf("policy-bound auto must still refuse a coverage reduction, got %v", err)
	}

	// A real human approval now lands through the interactive branch.
	approval, err := NewApproval(preview, "fixture-actor", authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAuthorized(root, preview, approval, nil); err != nil {
		t.Fatalf("an interactively approved coverage reduction must apply: %v", err)
	}
}
