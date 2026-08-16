// 已删文件的陈旧 Baseline 记录不是覆盖缩减。
//
// 真实经历: internal/codebatch/relations.go 在拆关系校验时就删了, 但它的 index 角色
// 记录留在 Baseline 里三周。日常 verify/check 不看它, 所以仓库一直报 aligned; 而任何
// 一次 Scope Change 都会重估全部角色, 于是这条陈账必然被判成覆盖缩减(risk=high),
// 自动授权拒绝, 一次纯策略变更被迫要真人批准。
//
// 判据是"当前 Safe Inventory 还看不看得见这个路径": 被排除但仍存在的文件留在评估里,
// 只有真正消失的文件不在。消失的文件既无 Entry 也无字节可失去, 退役它的记录是记账,
// 不是缩减。
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

func TestVanishedSourceIsBookkeepingNotCoverageReduction(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeInherit)

	// 造一条陈账: Baseline 里有 index 角色记录, 但文件已经从工作树消失。
	vanished := "internal/gone/removed.go"
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	state.Files[vanished] = baseline.Fingerprint{
		Role: machinecontract.ScopeRoleIndex, SHA256: strings.Repeat("a", 64),
		NormalizedSHA256: strings.Repeat("a", 64), Size: 1,
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, vanished)); !os.IsNotExist(statErr) {
		t.Fatalf("fixture precondition broken: %s should not exist", vanished)
	}

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}

	for _, reduction := range preview.Plan.CoverageReductions {
		if reduction.Path == vanished {
			t.Fatalf("a deleted file has no Entry and no bytes to lose, so retiring its stale "+
				"Baseline record must not count as a coverage reduction: %+v", reduction)
		}
	}
	if preview.Plan.Risk.CognitionCoverageReduction {
		t.Fatalf("stale bookkeeping must not raise the coverage-reduction risk: %+v", preview.Plan.Risk)
	}
	// 夹具本身会退役一条 Entry, 所以整体 Level 不是 low; 要钉死的是这条陈账没有
	// 贡献任何覆盖缩减计数与 P1。
	if preview.Plan.Risk.CoverageReductionCount != 0 || preview.Plan.Risk.P1 != 0 {
		t.Fatalf("stale bookkeeping must contribute no coverage-reduction count and no P1: %+v", preview.Plan.Risk)
	}

	// 记账仍然发生 —— 陈账确实被清掉, 只是不再需要真人。
	removed := false
	for _, item := range preview.Plan.BaselineRemoved {
		if item.Path == vanished {
			removed = true
		}
	}
	if !removed {
		t.Fatal("the stale Baseline record should still be retired by the transaction")
	}
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err != nil {
		t.Fatalf("policy-bound auto must accept a pure bookkeeping retirement: %v", err)
	}
}

// 反面: 文件仍然存在、只是还没写 Entry —— 那是真的在丢覆盖, 必须照旧拦。
func TestExistingUnauthoredSourceStillCountsAsCoverageReduction(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeInherit)

	// 真实存在但没有 Entry 的源文件。
	present := filepath.Join(root, "unauthored.go")
	if err := os.WriteFile(present, []byte("package main\n\nfunc Unauthored() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(present)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Role = machinecontract.ScopeRoleIndex
	state.Files["unauthored.go"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{
			RuleID: "drop-unauthored", Action: machinecontract.ScopeRoleExclude,
			Pattern: "unauthored.go", PatternKind: machinecontract.ScopePatternFile,
			Reason: "test-only fixture", Source: machinecontract.ScopeRuleUser,
			CreatedBy: "stale-baseline-test", Order: 0, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, reduction := range preview.Plan.CoverageReductions {
		if reduction.Path == "unauthored.go" {
			found = true
			if reduction.AuthoringState != "missing" {
				t.Fatalf("an existing unauthored source is missing authoring debt, got %q", reduction.AuthoringState)
			}
		}
	}
	if !found {
		t.Fatal("dropping an existing source that still owes an Entry is a real coverage reduction and must stay reported")
	}
	if !preview.Plan.Risk.CognitionCoverageReduction {
		t.Fatal("a real coverage reduction must still raise its risk")
	}
}
