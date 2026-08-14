// 预算 enforce 违规的结构化修复契约测试。
//
// 背景(真实事故): zh-CN 索引里 S 按 UTF-8 字节/3 计的 token 数会先于 rune 配额
// 撞上重要度档位上限, 而该拒绝路径曾把 int 传给 %s 模板, 严格资产渲染失败触发
// writeMessage panic —— 候选可修的预算违规被升级成不可定位的内部错误, 宿主被迫
// 在隔离副本里二分定位。这里钉死: 该路径永远返回带完整定位的 repair_required。
package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// activateEnforceBudget 给 Volumes 夹具装上受管范围与 enforce 预算收据 ——
// 缺省夹具落在 LegacyPolicy(observe), 永远走不到 enforce 拒绝路径。
func activateEnforceBudget(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := managedscope.Normalize(managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	budget, err := cognitionbudget.Normalize(cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope, cfg.CognitionBudget = &policy, &budget
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("加载夹具 Baseline 失败: exists=%t err=%v", exists, err)
	}
	budgetIdentity, err := cognitionbudget.Identity(budget)
	if err != nil {
		t.Fatal(err)
	}
	state.Files = files
	state.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: &budget}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetEnforceViolationReturnsStructuredRepair(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	activateEnforceBudget(t, root)
	indexBefore := volumeFileText(t, root, "aoci.code.txt")
	// C9 档 S 上限 200 tokens(600 字节); 210 个汉字 = 630 字节 = 210 tokens 超档,
	// 但 210 runes 远低于 C9 的 600 rune 配额 —— 只有 token 预算这一层会拒绝。
	line := "main.go[CD9S]: F:run the fixture | R:- | A:main | S:" + strings.Repeat("约", 210)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "main.go", SourceSHA256: sourceSHA256(t, root, "main.go"), NewEntry: line,
	}}))
	if result.Status != autoStatusRepairRequired || result.Applied != 0 {
		t.Fatalf("预算违规必须是零写入的 repair_required, 不是内部错误: %#v", result)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("必须恰好一条定位 Finding: %#v", result.Findings)
	}
	finding := result.Findings[0]
	if finding.RuleCode != "entry_field_budget_exceeded" || finding.Field != "S" ||
		finding.CandidateIndex != 1 || finding.CanonicalObjectIdentity != "code:main.go" ||
		finding.Expected != "max_tokens=200" || finding.Actual != "actual_tokens=210" ||
		finding.Cause == "" || finding.SafeRepairAction == "" {
		t.Fatalf("Finding 缺定位或精确 token 事实: %+v", finding)
	}
	if len(result.RetryScope) != 1 || result.RetryScope[0] != "code:main.go" {
		t.Fatalf("retry_scope 必须只指向违规候选: %#v", result.RetryScope)
	}
	if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
		t.Fatal("拒绝必须零正式写入")
	}
}

// 拆分逻辑: 命中本批候选的字段违规可修; whole-index 超限与批外条目的历史违规
// 永远归入不可修, 由调用方保持批级停止。
func TestBudgetRepairFindingsSplitsCandidateAndBatchLevel(t *testing.T) {
	normalized := []normalizedAtomicItem{
		{rel: "src/a.go", originalCandidateIndex: 3},
		{objectRef: "database://primary/public/users", originalCandidateIndex: 7},
	}
	violations := []cognitionbudget.Violation{
		{Code: "entry_field_budget_exceeded", Path: "code:src/a.go", Field: "S", Importance: 5, Actual: 82, Maximum: 80},
		{Code: "entry_field_budget_exceeded", Path: "database://primary/public/users", Field: "R", Importance: 6, Actual: 101, Maximum: 90},
		{Code: "entry_field_budget_exceeded", Path: "code:not/in/batch.go", Field: "S", Actual: 99, Maximum: 80},
		{Code: "whole_index_budget_exceeded", Actual: 250000, Maximum: 240000},
	}
	findings, unmatched := budgetRepairFindings(violations, normalized)
	if len(findings) != 2 || len(unmatched) != 2 {
		t.Fatalf("拆分不对: findings=%d unmatched=%d", len(findings), len(unmatched))
	}
	if findings[0].CandidateIndex != 3 || findings[0].Domain != "code" || findings[0].Field != "S" {
		t.Fatalf("code 候选映射不对: %+v", findings[0])
	}
	if findings[1].CandidateIndex != 7 || findings[1].Domain != "database" ||
		findings[1].CanonicalObjectIdentity != "database://primary/public/users" {
		t.Fatalf("database 候选映射不对: %+v", findings[1])
	}
	detail := budgetViolationDetail(unmatched[1])
	if !strings.Contains(detail, "whole_index_budget_exceeded") || !strings.Contains(detail, "250000") {
		t.Fatalf("批级违规事实串不完整: %q", detail)
	}
}
