// A posture relaxation must have an approver.
//
// autoAuthorizationBlockers refusing approval_policy_relaxation is right: a relaxation must never approve itself.
// But the refusal is only half of the contract: the Spec's pattern is "a blocked fact goes to independent review", not a dead end.
// Before the fix, InteractionRequired was derived entirely from the EXPECTED mode, so once auto was expected it judged "no
// review needed", and the block had no approver: auto became an absorbing state with an exit and no entrance, and review a one-way door.
//
// Locked here is the repaired semantics: with a review receipt and an auto expectation, the plan must demand a human confirmation and take
// the interactive branch; while an ordinary auto plan (no relaxation) stays fully automatic and refuses a human approval.
package scopechange

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// relaxingPreview builds a plan whose receipt is review and expectation auto, that is, "wanting to relax the posture back".
func relaxingPreview(t *testing.T) (string, *Preview) {
	t.Helper()
	root, candidates := buildChangeFixture(t)

	// First make review the RECEIPTED posture.
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeReview)
	reviewed, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := NewApproval(reviewed, "fixture-actor", authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAuthorized(root, reviewed, approval, nil); err != nil {
		t.Fatalf("could not receipt the review posture: %v", err)
	}

	// Then change the expectation back to auto: that is the relaxation.
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeAuto)
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Plan.Risk.ApprovalPolicyRelaxation {
		t.Fatalf("fixture did not produce a posture relaxation: %+v", preview.Plan.Risk)
	}
	return root, preview
}

func TestPostureRelaxationDemandsAHumanReviewer(t *testing.T) {
	root, preview := relaxingPreview(t)

	if !preview.Plan.InteractionRequired {
		t.Fatal("a relaxation planned under the weaker desired mode still needs a reviewer; " +
			"without this the auto blocker has no approver and review becomes a one-way door")
	}
	if preview.Plan.ConfirmationPhrase == "" {
		t.Fatal("an interactive plan must carry the phrase that binds the approval to it")
	}

	// Automatic authorization must still refuse: a relaxation cannot approve itself.
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err == nil ||
		!strings.Contains(err.Error(), "approval_policy_relaxation") {
		t.Fatalf("policy-bound auto must still refuse a relaxation, got %v", err)
	}

	// And a human approval can now land.
	approval, err := NewApproval(preview, "fixture-actor", authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAuthorized(root, preview, approval, nil); err != nil {
		t.Fatalf("an interactively approved relaxation must apply: %v", err)
	}
}

func TestOrdinaryAutoPlanStaysFullyAutomatic(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeAuto)
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.Risk.ApprovalPolicyRelaxation {
		t.Fatalf("fixture unexpectedly relaxes the posture: %+v", preview.Plan.Risk)
	}
	if preview.Plan.InteractionRequired {
		t.Fatal("an ordinary auto plan must never ask a human")
	}
	// And under auto a human approval is still not the authority: the branch routing was not broken by the relaxation.
	approval, err := NewApproval(preview, "fixture-actor", authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAuthorized(root, preview, approval, nil); err == nil ||
		!strings.Contains(err.Error(), "human_approval_not_authoritative_in_auto") {
		t.Fatalf("auto must still refuse a human approval, got %v", err)
	}
}

func TestApplyAuthorizationBranchRoutesRelaxationToReview(t *testing.T) {
	cases := []struct {
		mode        string
		interaction bool
		want        string
	}{
		{machinecontract.ApplyAuthorizationAuto, false, machinecontract.ApplyAuthorizationAuto},
		{machinecontract.ApplyAuthorizationAuto, true, machinecontract.ApplyAuthorizationReview},
		{machinecontract.ApplyAuthorizationReview, true, machinecontract.ApplyAuthorizationReview},
		{machinecontract.ApplyAuthorizationLegacy, false, machinecontract.ApplyAuthorizationLegacy},
		{machinecontract.ApplyAuthorizationOff, false, machinecontract.ApplyAuthorizationOff},
	}
	for _, item := range cases {
		if got := applyAuthorizationBranch(item.mode, item.interaction); got != item.want {
			t.Errorf("branch(%s, interaction=%v) = %s, want %s", item.mode, item.interaction, got, item.want)
		}
	}
}
