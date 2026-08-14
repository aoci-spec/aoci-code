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

// 治理姿态的放松必须按上一份收据记录的模式判定,否则 review→auto 的跃迁会在
// 新模式下自我批准。
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

// 迁移边界: 早于本字段的收据不追溯阻断(保护从第一份记录了模式的收据开始生效),
// 但无法识别的模式属篡改或未来格式,方向不可证,失败关闭。
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

// 放松治理姿态必须挡住 policy_bound_auto,交由真人复核。
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
