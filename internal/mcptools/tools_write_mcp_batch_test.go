// MCP原生批量回写、紧凑终态与重复Apply防重测试。
package mcptools

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestMCPBatchAppliesOnceAndReturnsAligned(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	writeBatchSource(t, root, "src/c.go")
	input := []updateEntryItemIn{
		{
			Path:         "src/a.go",
			NewEntry:     "a.go[X.Y.5.T]: F:批量替换 | R:- | A:- | S:模型逐文件阅读后生成",
			SourceSHA256: sourceSHA256(t, root, "src/a.go"),
		},
		{
			Path:         "src/b.go",
			NewEntry:     "b.go[X.Y.5.T]: F:批量新增乙 | R:- | A:- | S:模型逐文件阅读后生成",
			SourceSHA256: sourceSHA256(t, root, "src/b.go"),
		},
		{
			Path:         "src/c.go",
			NewEntry:     "c.go[X.Y.5.T]: F:批量新增丙 | R:- | A:- | S:模型逐文件阅读后生成",
			SourceSHA256: sourceSHA256(t, root, "src/c.go"),
		},
	}
	firstResult := handleMCPUpdateBatch(root, "test-version", input)
	firstText := maintainResultText(t, firstResult)
	first := decodeAutoResult(t, firstResult)
	assertAutoMachineCounts(t, firstText, 3, 3, 0, 3, 0)
	if first.Status != autoStatusApplied || !first.Aligned || first.Attempted != 3 || first.Applied != 3 ||
		first.Metrics.AOCIToolCalls != 1 || first.Metrics.ShellAOCICalls != 0 ||
		first.Audit == nil || first.Audit.DiffFiles != 3 || len(first.Audit.P23ContentSHA256) != 64 {
		t.Fatalf("MCP批量闭环结果不符: %+v", first)
	}
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineBeforeRetry, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	indexBeforeRetry := sha256.Sum256([]byte(readBatchIndex(t, root)))
	baselineHashBeforeRetry := sha256.Sum256(baselineBeforeRetry)
	applicationsBeforeRetry := successfulBatchApplications(t, root)

	secondResult := handleMCPUpdateBatch(root, "test-version", input)
	secondText := maintainResultText(t, secondResult)
	second := decodeAutoResult(t, secondResult)
	assertAutoMachineCounts(t, secondText, 3, 0, 1, 3, 0)
	baselineAfterRetry, _ := os.ReadFile(baselinePath)
	indexAfterRetry := sha256.Sum256([]byte(readBatchIndex(t, root)))
	baselineHashAfterRetry := sha256.Sum256(baselineAfterRetry)
	applicationsAfterRetry := successfulBatchApplications(t, root)
	if second.Attempted != 3 || second.Applied != 0 || second.Metrics.DuplicateApplies != 1 ||
		indexBeforeRetry != indexAfterRetry ||
		baselineHashBeforeRetry != baselineHashAfterRetry ||
		applicationsBeforeRetry != 1 || applicationsAfterRetry != applicationsBeforeRetry {
		t.Fatalf("重复调用必须零写入且可观测: %+v", second)
	}
	if !strings.Contains(second.NextAction, "请求处理成功，但正式写入为0") ||
		strings.Contains(second.NextAction, "正式写入为3") || strings.Contains(second.NextAction, "Aggregate Check") {
		t.Fatalf("模型可见说明必须与Applied=0同源: %q", second.NextAction)
	}
}

func assertAutoMachineCounts(
	t *testing.T,
	raw string,
	attempted,
	applied,
	duplicateApplies,
	semanticFiles,
	formatOnlyFiles int,
) {
	t.Helper()
	var document struct {
		Attempted *int `json:"attempted"`
		Applied   *int `json:"applied"`
		Remaining *int `json:"remaining"`
		Metrics   struct {
			DuplicateApplies *int `json:"duplicate_applies"`
			SemanticFiles    *int `json:"semantic_files"`
			FormatOnlyFiles  *int `json:"format_only_files"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatalf("机器终态必须是单一合法JSON对象: %v\n%s", err, raw)
	}
	if document.Attempted == nil || *document.Attempted != attempted ||
		document.Applied == nil || *document.Applied != applied ||
		document.Remaining == nil || *document.Remaining != 0 ||
		document.Metrics.DuplicateApplies == nil || *document.Metrics.DuplicateApplies != duplicateApplies ||
		document.Metrics.SemanticFiles == nil || *document.Metrics.SemanticFiles != semanticFiles ||
		document.Metrics.FormatOnlyFiles == nil || *document.Metrics.FormatOnlyFiles != formatOnlyFiles {
		t.Fatalf("机器终态关键计数缺失、类型错误或数值不符: %+v\n%s", document, raw)
	}
}

func successfulBatchApplications(t *testing.T, root string) int {
	t.Helper()
	events, corrupt := ledger.Recent(root, 100)
	if corrupt != 0 {
		t.Fatalf("Ledger不应存在损坏行: %d", corrupt)
	}
	count := 0
	for _, event := range events {
		if event.Op == "update_entries_batch" &&
			event.Result == ledger.ResultOK &&
			event.AppliedCount > 0 {
			count++
		}
	}
	return count
}

func TestMCPBatchRetryRepairsInterruptedBaselineOnly(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	input := []updateEntryItemIn{{
		Path:         "src/b.go",
		NewEntry:     "b.go[X.Y.5.T]: F:恢复基线 | R:- | A:- | S:模型逐文件阅读后生成",
		SourceSHA256: sourceSHA256(t, root, "src/b.go"),
	}}
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if first.Applied != 1 || !first.Aligned {
		t.Fatalf("首次批次应完整应用: %+v", first)
	}

	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("读取Baseline失败: exists=%v err=%v", exists, err)
	}
	state.Files["src/b.go"] = baseline.Fingerprint{SHA256: "interrupted"}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}

	retry := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if retry.Applied != 0 || retry.Metrics.DuplicateApplies != 1 || !retry.Aligned {
		t.Fatalf("重试应只补齐Baseline并恢复aligned: %+v", retry)
	}
	repaired, _, err := baseline.Load(root)
	if err != nil || repaired.Files["src/b.go"].SHA256 == "interrupted" {
		t.Fatalf("中断Baseline未修复: err=%v fp=%+v", err, repaired.Files["src/b.go"])
	}
}

func TestMCPBatchRetryDoesNotWashNewSourceDrift(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	input := []updateEntryItemIn{{
		Path:         "src/b.go",
		NewEntry:     "b.go[X.Y.5.T]: F:防漂移洗白 | R:- | A:- | S:模型逐文件阅读后生成",
		SourceSHA256: sourceSHA256(t, root, "src/b.go"),
	}}
	first := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if first.Applied != 1 || !first.Aligned {
		t.Fatalf("首次批次应完整应用: %+v", first)
	}
	writeBatchSource(t, root, "src/b.go")
	if err := os.WriteFile(filepath.Join(root, "src", "b.go"), []byte("package sample\n\nvar Changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	retry := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if retry.Status != autoStatusStopped || retry.Aligned || retry.Metrics.DuplicateApplies != 0 {
		t.Fatalf("旧请求重放必须保留新源码漂移: %+v", retry)
	}
}

func TestMCPBatchReplayRechecksSourceBindingInsideLockBeforeBaselineFastPath(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	item := AtomicUpdateItem{
		Path:         "src/b.go",
		NewEntry:     "b.go[X.Y.5.T]: F:锁内绑定 | R:- | A:- | S:-",
		SourceSHA256: sourceSHA256(t, root, "src/b.go"),
	}
	first, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	if fail != nil || first.AppliedCount != 1 {
		t.Fatalf("首次应用失败: fail=%+v outcome=%+v", fail, first)
	}
	plan, fail := planUpdateEntriesAtomic(root, []AtomicUpdateItem{item})
	if fail != nil || plan.finalText != plan.rc.text {
		t.Fatalf("重放规划应形成已应用快路径: fail=%+v plan=%+v", fail, plan)
	}
	if err := os.WriteFile(
		filepath.Join(root, "src", "b.go"),
		[]byte("package demo\n\nvar ChangedInsideWindow = true\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("读取Baseline失败: exists=%v err=%v", exists, err)
	}
	current, err := baseline.HashFile(filepath.Join(root, "src", "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	baseline.UpdateOne(state, "src/b.go", current)
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	if _, _, reconcileFail := reconcileAlreadyAppliedBaseline(root, ledger.SourceAgent, plan); reconcileFail == nil || reconcileFail.Code != errWriteConflict {
		t.Fatalf("Baseline已前移也不得绕过锁内source_sha256 CAS: %+v", reconcileFail)
	}
}

func TestMCPBatchReturnsRelationWarningsInAudit(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path:         "src/b.go",
		SourceSHA256: sourceSHA256(t, root, "src/b.go"),
		NewEntry: "b.go[X.Y.5.T]: F:批量R审计 | " +
			"R:src/missing.go | A:- | S:-",
	}}))
	if result.Status != autoStatusApplied || result.Audit == nil || len(result.Audit.Warnings) != 1 {
		t.Fatalf("关系Warning必须保留在紧凑审计摘要: %+v", result)
	}
}

func TestMCPBatchRepairRequiredWritesNothing(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	indexBefore := readBatchIndex(t, root)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path:         "src/b.go",
		NewEntry:     "wrong.go[X.Y.5.T]: F:错误候选 | R:- | A:- | S:-",
		SourceSHA256: sourceSHA256(t, root, "src/b.go"),
	}}))
	if result.Status != autoStatusRepairRequired || result.Aligned || len(result.Findings) == 0 {
		t.Fatalf("候选机器拒绝应返回repair_required: %+v", result)
	}
	if readBatchIndex(t, root) != indexBefore {
		t.Fatal("repair_required必须正式索引零写入")
	}
}

func TestMCPBatchRequiresMaintainSourceBinding(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	indexBefore := readBatchIndex(t, root)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path:     "src/b.go",
		NewEntry: "b.go[X.Y.5.T]: F:缺绑定 | R:- | A:- | S:-",
	}}))
	if result.Status != autoStatusRepairRequired || result.Aligned || len(result.Findings) == 0 {
		t.Fatalf("无源码指纹的MCP批次必须零写入拒绝: %+v", result)
	}
	if readBatchIndex(t, root) != indexBefore {
		t.Fatal("缺少source_sha256不得写入索引")
	}
}

func TestMCPLegacySingleUsesSameSourceBindingAndReplayGuards(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	entry := "b.go[X.Y.5.T]: F:单条兼容 | R:- | A:- | S:受源码指纹绑定"
	missing := decodeAutoResult(t, handleMCPUpdateSingle(root, "test-version", updateEntryIn{
		Path: "src/b.go", NewEntry: entry,
	}))
	if missing.Status != autoStatusRepairRequired || missing.Aligned {
		t.Fatalf("兼容单条缺少source_sha256必须零写入拒绝: %+v", missing)
	}
	binding := sourceSHA256(t, root, "src/b.go")
	first := decodeAutoResult(t, handleMCPUpdateSingle(root, "test-version", updateEntryIn{
		Path: "src/b.go", NewEntry: entry, SourceSHA256: binding,
	}))
	if first.Status != autoStatusApplied || !first.Aligned || first.Applied != 1 {
		t.Fatalf("受绑定兼容单条应进入同一原子闭环: %+v", first)
	}
	if err := os.WriteFile(
		filepath.Join(root, "src", "b.go"),
		[]byte("package sample\n\nvar SingleChanged = true\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	retry := decodeAutoResult(t, handleMCPUpdateSingle(root, "test-version", updateEntryIn{
		Path: "src/b.go", NewEntry: entry, SourceSHA256: binding,
	}))
	if retry.Status != autoStatusStopped || retry.Aligned || retry.Metrics.DuplicateApplies != 0 {
		t.Fatalf("旧版单条重放不得洗白新源码漂移: %+v", retry)
	}
}

func TestMCPBatchBaselineFailureStopsAndBoundReplayRecovers(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	input := []updateEntryItemIn{{
		Path:         "src/b.go",
		NewEntry:     "b.go[X.Y.5.T]: F:基线恢复 | R:- | A:- | S:受指纹绑定",
		SourceSHA256: sourceSHA256(t, root, "src/b.go"),
	}}
	previousSave := saveAtomicBaseline
	saveAtomicBaseline = func(string, *baseline.Baseline) error {
		return errors.New("injected cross-platform Baseline write failure")
	}
	t.Cleanup(func() { saveAtomicBaseline = previousSave })

	stopped := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if stopped.Status != autoStatusStopped || stopped.Aligned || stopped.Applied != 1 ||
		len(stopped.Findings) == 0 || stopped.Audit == nil {
		t.Fatalf("Baseline写失败必须报告已写索引与可恢复停点: %+v", stopped)
	}
	if !strings.Contains(readBatchIndex(t, root), "F:基线恢复") {
		t.Fatal("stopped必须如实保留已原子写入的索引")
	}
	saveAtomicBaseline = previousSave

	recovered := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", input))
	if recovered.Status != autoStatusApplied || !recovered.Aligned || recovered.Applied != 0 ||
		recovered.Metrics.DuplicateApplies != 1 {
		t.Fatalf("受绑定批次重放应只恢复Baseline: %+v", recovered)
	}
}
