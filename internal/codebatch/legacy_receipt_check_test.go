package codebatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 旧二进制写下的收据没有 observed_relations 字段。新二进制必须能原样读它并
// 退化为零知识路径 —— 否则升级会作废宿主已经创作好的候选绑定。
func TestLegacyReceiptWithoutObservedRelationsStillLoads(t *testing.T) {
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
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	// 模拟旧格式: 彻底移除该字段后重新落盘。
	delete(raw, "observed_relations")
	legacy, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "observed_relations") {
		t.Fatal("测试前提不成立: 旧格式样本仍带有该字段")
	}
	if err := os.WriteFile(path, append(legacy, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	receipt, err := LoadReceipt(root, plan.BatchID)
	if err != nil {
		t.Fatalf("新二进制必须能读旧收据: %v", err)
	}
	if len(receipt.ObservedRelations) != 0 {
		t.Fatalf("旧收据应退化为零知识: %+v", receipt.ObservedRelations)
	}
	// 旧收据仍能驱动重排(此时按零知识/学习路径挑批)。
	if _, _, err := ReplanForRelations(root, receipt, []ObservedRelation{
		{SourceObjectRef: receipt.Targets[0].ObjectRef, TargetObjectRefs: []string{receipt.AllTargets[5].ObjectRef}},
	}, nil, 4); err != nil {
		t.Fatalf("旧收据必须仍可重排: %v", err)
	}
}
