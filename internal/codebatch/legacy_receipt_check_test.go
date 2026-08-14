package codebatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 机器不再从 R 推导任何东西, 但升级前签发的收据里带着 observed_relations,
// 而收据加载拒绝未知字段。这条测试钉死升级不会把进行中的计划卡死: 旧字段被
// 读进来后即丢弃, 收据照常驱动提交校验。
func TestReceiptCarryingLegacyObservedRelationsStillLoads(t *testing.T) {
	root := t.TempDir()
	plan, err := BuildPlan(root, strings.Repeat("a", 64), strings.Repeat("b", 64), "aoci.code.txt",
		strings.Repeat("c", 64), codeCandidates(6), 4)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".aoci", "drafts", "code-cognition", "candidate-"+plan.BatchID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "observed_relations") {
		t.Fatal("新收据不应再写出关系事实")
	}
	// 模拟升级前的收据: 原样注入当时会写出的字段。
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["observed_relations"] = []any{
		map[string]any{"source_object_ref": plan.Candidates[0].ObjectRef,
			"target_object_refs": []any{plan.Candidates[1].ObjectRef}},
	}
	legacy, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(legacy, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	receipt, err := LoadReceipt(root, plan.BatchID)
	if err != nil {
		t.Fatalf("新二进制必须能读旧收据: %v", err)
	}
	if len(receipt.Targets) != 4 || len(receipt.AllTargets) != 6 {
		t.Fatalf("旧收据的批次内容必须原样保留: %+v", receipt)
	}
	// 关键: 旧字段不参与任何身份推导, 收据仍能校验宿主按原绑定提交的候选。
	submissions := make([]Submission, 0, len(receipt.Targets))
	for index, target := range receipt.Targets {
		submissions = append(submissions, Submission{CandidateIndex: index + 1, ObjectRef: target.ObjectRef,
			CandidateID: target.CandidateID, SourceSHA256: target.SourceSHA256})
	}
	if _, err := ValidateSubmission(root, receipt.BatchID, receipt.CompositeIdentity, receipt.ScopePolicyIdentity,
		receipt.CodeVolumePath, receipt.CodeVolumeSHA256, submissions, false); err != nil {
		t.Fatalf("旧收据必须仍能校验提交: %v", err)
	}
}
