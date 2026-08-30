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
	root, candidates := buildCoverageReductionFixture(t, machinecontract.ScopeDecisionUnspecified)
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

// The classification must cover the emitted set exactly. A reason that reaches
// interactionRequiredForAuto without an entry is a programming error, and the
// two lists living apart is precisely how 313a3ab's "only bypasser" claim
// decayed into a shipped defect.
func TestEveryAutoBlockerCodeIsClassified(t *testing.T) {
	for _, code := range autoBlockerCodes {
		if _, classified := autoBlockerRatifiableByIndependentReview[code]; !classified {
			t.Errorf("blocker %q is emitted but never classified as ratifiable or not", code)
		}
	}
	if len(autoBlockerRatifiableByIndependentReview) != len(autoBlockerCodes) {
		t.Errorf("classification carries %d entries for %d emitted reasons; a stale entry hides a "+
			"reason that no longer exists and masks the next unclassified one",
			len(autoBlockerRatifiableByIndependentReview), len(autoBlockerCodes))
	}
}

// Pin the judgement itself, so changing which reasons a human may ratify is a
// deliberate edit with a failing test rather than a quiet widening.
func TestSafetyBoundariesAreNotRatifiableByApproval(t *testing.T) {
	unratifiable := []string{
		autoBlockerTransportConstraint,       // never an admissible reduction reason
		autoBlockerRetentionReviewIncomplete, // missing input, not a decision
		autoBlockerCognitionBudgetExceeded,   // enforce mode rejects hard-limit violations
		autoBlockerRecoveryUnavailable,       // no recovery path to approve into
		autoBlockerBusinessSourceWriteSet,    // Scope Change never writes business sources
		autoBlockerBusinessSourcePostimage,
	}
	for _, code := range unratifiable {
		if autoBlockerRatifiableByIndependentReview[code] {
			t.Errorf("%q is a safety boundary; no approval may wave it through", code)
		}
	}
	ratifiable := []string{
		autoBlockerHighRiskContentInclusion, autoBlockerBudgetPolicyRelaxation,
		autoBlockerApprovalPolicyRelaxation, autoBlockerP0OrP1,
		autoBlockerCoverageReduction, autoBlockerExplicitDropWithoutTransfer,
	}
	for _, code := range ratifiable {
		if !autoBlockerRatifiableByIndependentReview[code] {
			t.Errorf("%q is exactly what independent review exists to decide; refusing auto and "+
				"then reporting no reviewer leaves the change with no approver", code)
		}
	}
}

// An unclassified reason must fail the build rather than silently pick a side:
// guessing "not ratifiable" recreates the approver-less dead end, and guessing
// "ratifiable" would let a human approve past a safety boundary.
func TestUnclassifiedAutoBlockerFailsClosed(t *testing.T) {
	root, candidates := buildCoverageReductionFixture(t, machinecontract.ScopeDecisionUnspecified)
	saved, existed := autoBlockerRatifiableByIndependentReview[autoBlockerCoverageReduction]
	delete(autoBlockerRatifiableByIndependentReview, autoBlockerCoverageReduction)
	defer func() {
		if existed {
			autoBlockerRatifiableByIndependentReview[autoBlockerCoverageReduction] = saved
		}
	}()
	_, err := Build(root, authorizationTestTime, candidates)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_auto_blocker_unclassified") {
		t.Fatalf("an unclassified blocker must fail the build, got %v", err)
	}
}

// buildCoverageReductionFixture retires the entry and moves main_test.go
// index -> observe through a user rule, producing a real cognition coverage
// reduction under effective-auto authorization.
func buildCoverageReductionFixture(t *testing.T, decisionBasis string) (string, CandidateSet) {
	t.Helper()
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeAuto)
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
			DecisionBasis: decisionBasis, Source: machinecontract.ScopeRuleUser,
			CreatedBy: "coverage-test", Order: 0, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, candidates
}

// A transport constraint is a safety boundary: the published contract says it
// carries no reviewer and the operator must resolve the condition instead of
// approving past it. But the risk flag is only ever set inside the
// coverage-reduction branch, which also raises a ratifiable blocker, so a plan
// can never carry the transport constraint alone. Deriving
// interaction_required from "any blocker is ratifiable" therefore routed the
// whole plan to a human and let one approval land a change rc5 held closed on
// every path — and the map entry classifying the transport constraint as
// unratifiable could never decide anything.
func TestASafetyBoundaryIsNeverRoutedToAReviewer(t *testing.T) {
	root, candidates := buildCoverageReductionFixture(t, machinecontract.ScopeDecisionTransportConstraint)
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Plan.Risk.TransportConstraintNotAllowed {
		t.Fatalf("fixture precondition: expected a transport-constrained reduction, got %+v", preview.Plan.Risk)
	}
	if !preview.Plan.Risk.CognitionCoverageReduction {
		t.Fatal("fixture precondition: the transport constraint only ever fires together with a coverage reduction, " +
			"which is what makes the mixed case the only reachable one")
	}

	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	blockers := autoAuthorizationBlockers(preview, cfg)
	unratifiable := []string{}
	for _, reason := range blockers {
		if !autoBlockerRatifiableByIndependentReview[reason] {
			unratifiable = append(unratifiable, reason)
		}
	}
	if len(unratifiable) == 0 {
		t.Fatalf("fixture precondition: expected an unratifiable blocker among %v", blockers)
	}

	if preview.Plan.InteractionRequired {
		t.Fatalf("a plan carrying the unratifiable blocker(s) %v was routed to a human reviewer.\n"+
			"The published partition says a safety boundary has no reviewer, so one approval must not be able "+
			"to ratify past it; interaction_required must be false and the operator must resolve the condition.",
			unratifiable)
	}
	if preview.Plan.ConfirmationPhrase != "" {
		t.Fatal("a plan with no reviewer must not mint a confirmation phrase")
	}
}
