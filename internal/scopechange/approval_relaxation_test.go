package scopechange

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func receiptWithMode(mode string) *baseline.Baseline {
	return &baseline.Baseline{ManagedScope: &baseline.ManagedScopeState{
		Version: machinecontract.ManagedScopeBaselineV1, ApplyAuthorizationMode: mode}}
}

// A relaxation of the governance posture must be judged by the mode the previous receipt recorded, or the review→auto transition would
// approve itself under the new mode.
func TestApprovalRelaxationDirectionUsesReceiptedMode(t *testing.T) {
	cases := []struct {
		name    string
		receipt string
		current string
		relaxed bool
	}{
		{"review 降为 auto 是放松", machinecontract.ApplyAuthorizationReview, machinecontract.ApplyAuthorizationAuto, true},
		{"review 降为 legacy 是放松", machinecontract.ApplyAuthorizationReview, machinecontract.ApplyAuthorizationLegacy, true},
		{"legacy 降为 auto 是放松", machinecontract.ApplyAuthorizationLegacy, machinecontract.ApplyAuthorizationAuto, true},
		{"off 降为 review 是放松", machinecontract.ApplyAuthorizationOff, machinecontract.ApplyAuthorizationReview, true},
		{"auto 保持 auto 不是放松", machinecontract.ApplyAuthorizationAuto, machinecontract.ApplyAuthorizationAuto, false},
		{"auto 升为 review 是收紧", machinecontract.ApplyAuthorizationAuto, machinecontract.ApplyAuthorizationReview, false},
		{"review 保持 review 不是放松", machinecontract.ApplyAuthorizationReview, machinecontract.ApplyAuthorizationReview, false},
	}
	for _, item := range cases {
		if got := approvalRelaxed(receiptWithMode(item.receipt), item.current); got != item.relaxed {
			t.Fatalf("%s: 期望 relaxed=%v, 实际 %v", item.name, item.relaxed, got)
		}
	}
}

// Migration boundary: a receipt older than this field is not blocked retroactively (the protection takes effect from the first receipt that recorded the mode),
// but an unrecognizable mode is tampering or a future format, the direction cannot be proven, and it fails closed.
func TestApprovalRelaxationMigrationBoundaryAndUnprovableDirection(t *testing.T) {
	if approvalRelaxed(receiptWithMode(""), machinecontract.ApplyAuthorizationAuto) {
		t.Fatal("早于本字段的收据不应追溯阻断存量仓库")
	}
	if !approvalRelaxed(receiptWithMode("unknown-mode"), machinecontract.ApplyAuthorizationAuto) {
		t.Fatal("无法识别的收据模式必须失败关闭")
	}
	if !approvalRelaxed(receiptWithMode(machinecontract.ApplyAuthorizationReview), "unknown-mode") {
		t.Fatal("无法识别的当前模式必须失败关闭")
	}
	if approvalRelaxed(nil, machinecontract.ApplyAuthorizationAuto) {
		t.Fatal("无 Baseline 时不应判定为放松")
	}
	if approvalRelaxed(&baseline.Baseline{}, machinecontract.ApplyAuthorizationAuto) {
		t.Fatal("尚未建立受管收据时不应判定为放松")
	}
}

// Relaxing the governance posture must block policy_bound_auto and hand over to a human review.
func TestApprovalPolicyRelaxationBlocksAutoAuthorization(t *testing.T) {
	root, _, preview := buildAutoPreview(t)
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if reasons := autoAuthorizationBlockers(preview, cfg); len(reasons) != 0 {
		t.Fatalf("基线用例不应有阻断: %v", reasons)
	}
	relaxed := *preview
	relaxed.Plan.Risk.ApprovalPolicyRelaxation = true
	reasons := strings.Join(autoAuthorizationBlockers(&relaxed, cfg), ",")
	if !strings.Contains(reasons, "approval_policy_relaxation") {
		t.Fatalf("放松治理姿态必须阻断自动授权,实际阻断: %s", reasons)
	}
}
