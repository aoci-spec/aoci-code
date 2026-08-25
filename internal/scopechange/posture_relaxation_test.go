// 姿态放松在没有其他安全阻断时由显式期望auto策略授权，不进入真人分支。
package scopechange

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// relaxingPreview 建一个收据为 review、期望为 auto 的计划 —— 即"想把姿态放松回去"。
func relaxingPreview(t *testing.T) (string, *Preview) {
	t.Helper()
	root, candidates := buildChangeFixture(t)

	// 先让 review 成为**已收据**的姿态。
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

	// 再把期望改回 auto: 这就是放松。
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

func TestPostureRelaxationUsesPolicyBoundAutoWhenOtherwiseSafe(t *testing.T) {
	root, preview := relaxingPreview(t)

	if preview.Plan.InteractionRequired {
		t.Fatal("a posture-only relaxation under explicit auto policy must not request a human")
	}
	if preview.Plan.ConfirmationPhrase != "" {
		t.Fatal("a non-interactive auto plan must not carry a confirmation phrase")
	}
	receipt, err := NewPolicyBoundApproval(root, preview, authorizationTestTime)
	if err != nil {
		t.Fatalf("posture-only relaxation must receive a policy-bound receipt: %v", err)
	}
	result, err := ApplyAuthorized(root, preview, nil, receipt)
	if err != nil {
		t.Fatalf("policy-bound posture relaxation must apply: %v", err)
	}
	if result.AuthorizationMechanism != machinecontract.ApprovalMechanismPolicyBoundAuto {
		t.Fatalf("unexpected authorization mechanism: %s", result.AuthorizationMechanism)
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
	// 并且在 auto 下人工批准仍然不是权威 —— 分支路由没有被放松改坏。
	approval, err := NewApproval(preview, "fixture-actor", authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAuthorized(root, preview, approval, nil); err == nil ||
		!strings.Contains(err.Error(), "human_approval_not_authoritative_in_auto") {
		t.Fatalf("auto must still refuse a human approval, got %v", err)
	}
}

func TestApplyAuthorizationBranchHonorsExplicitInteraction(t *testing.T) {
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
