// 姿态放松必须有审批人。
//
// autoAuthorizationBlockers 拒绝 approval_policy_relaxation 是对的 —— 放松绝不能自我
// 批准。但拒绝只是契约的一半: Spec 的模式是"被阻断的事实交给独立复核", 不是死路。
// 修复前, InteractionRequired 完全由**期望**模式推导, 于是一旦期望 auto 就判定"不需要
// 复核", 阻断因此没有审批人 —— auto 成了能出不能进的吸收态, 而 review 是单向门。
//
// 这里钉死修好的语义: 收据是 review、期望是 auto 时, 计划必须要求人工确认, 并且走
// 交互分支; 而普通的 auto 计划(无放松)仍然完全自动、且拒收人工批准。
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

func TestPostureRelaxationDemandsAHumanReviewer(t *testing.T) {
	root, preview := relaxingPreview(t)

	if !preview.Plan.InteractionRequired {
		t.Fatal("a relaxation planned under the weaker desired mode still needs a reviewer; " +
			"without this the auto blocker has no approver and review becomes a one-way door")
	}
	if preview.Plan.ConfirmationPhrase == "" {
		t.Fatal("an interactive plan must carry the phrase that binds the approval to it")
	}

	// 自动授权仍然必须拒绝 —— 放松不能自我批准。
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err == nil ||
		!strings.Contains(err.Error(), "approval_policy_relaxation") {
		t.Fatalf("policy-bound auto must still refuse a relaxation, got %v", err)
	}

	// 而真人批准现在能落地。
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
