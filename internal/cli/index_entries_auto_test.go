// R65/R65-03 Entries Auto共享收口内核测试。
//
// 本文件直接验证共享内核，不经过Host-Agent Stage或Endpoint模型调用，确保：
//   - Check、Diff、Apply与Application使用同一草稿摘要；
//   - Host-Agent来源在四类Ledger事件中保持agent；
//   - Check拒绝后返回repair_required、草稿保留、正式索引零写入；
//   - 共享内核仍返回ExitInvalid，由Host-Agent外层选择是否自动恢复。
package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

// r65LoadEntriesAutoState读取测试仓当前正式索引，不修改任何资产。
func r65LoadEntriesAutoState(
	t *testing.T,
	root string,
) (*config.Config, *index.Document) {
	t.Helper()

	cfg, err := config.Load(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	data, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	doc, warnings := index.Parse(
		string(data),
	)
	if doc == nil {
		t.Fatal(
			"测试仓正式索引解析结果为空",
		)
	}
	if len(warnings) > 0 {
		t.Fatalf(
			"测试仓正式索引不应带解析告警: %+v",
			warnings,
		)
	}

	index.ResolveRelPaths(
		doc,
		root,
	)

	return cfg, doc
}

func TestEntriesAutoFinalizeAppliesWithAgentAudit(
	t *testing.T,
) {
	root, runID := buildManualAtomicEntriesRepo(
		t,
	)

	cfg, doc := r65LoadEntriesAutoState(
		t,
		root,
	)

	beforeManifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer

	result, err := runEntriesAutoFinalize(
		root,
		cfg,
		doc,
		runID,
		len(beforeManifest.Entries),
		ledger.SourceAgent,
		&output,
	)
	if err != nil {
		t.Fatalf(
			"共享Auto全净批次应成功: %v\n%s",
			err,
			output.String(),
		)
	}

	if result.Status != entriesAutoStatusApplied ||
		result.FailedStep != "" ||
		result.Checked != len(beforeManifest.Entries) ||
		result.Passed != len(beforeManifest.Entries) ||
		result.Rejected != 0 ||
		result.Skipped != 0 ||
		result.DiffReviewed != len(beforeManifest.Entries) ||
		result.Attempted != len(beforeManifest.Entries) ||
		result.Applied != len(beforeManifest.Entries) ||
		!result.AssetWritten ||
		!result.AuditRecorded ||
		result.DraftHash == "" ||
		result.Recovery != "" {
		t.Fatalf(
			"共享Auto成功结果不符: %+v",
			result,
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Reviews) != 2 ||
		len(manifest.Applications) != 1 {
		t.Fatalf(
			"Check/Diff/Application审计不完整: %+v",
			manifest,
		)
	}

	checkReview := manifest.Reviews[0]
	diffReview := manifest.Reviews[1]
	application := manifest.Applications[0]

	if checkReview.Action != draft.ReviewActionCheck ||
		diffReview.Action != draft.ReviewActionDiff ||
		checkReview.DraftHash == "" ||
		diffReview.DraftHash != checkReview.DraftHash ||
		application.DraftHash != diffReview.DraftHash ||
		application.Applied != len(beforeManifest.Entries) ||
		application.Rejected != 0 ||
		manifest.AppliedAt != application.At {
		t.Fatalf(
			"共享Auto三阶段摘要不一致: %+v",
			manifest,
		)
	}

	requiredOps := map[string]bool{
		"entries_check":        false,
		"entries_diff":         false,
		"update_entries_batch": false,
		"entries_apply":        false,
	}

	events, _ := ledger.Recent(
		root,
		100,
	)

	for _, event := range events {
		if _, tracked := requiredOps[event.Op]; !tracked {
			continue
		}

		if event.Source != ledger.SourceAgent {
			t.Fatalf(
				"Host-Agent Auto事件来源错误: %+v",
				event,
			)
		}

		requiredOps[event.Op] = true
	}

	for operation, found := range requiredOps {
		if !found {
			t.Fatalf(
				"Host-Agent Auto缺少Ledger事件: %s events=%+v",
				operation,
				events,
			)
		}
	}

	for _, anchor := range []string{
		"已完成Diff审计",
		"已原子应用",
		"check/diff/apply draft_hash=",
	} {
		if !strings.Contains(
			output.String(),
			anchor,
		) {
			t.Fatalf(
				"共享Auto输出缺少%q: %s",
				anchor,
				output.String(),
			)
		}
	}
}

func TestEntriesAutoCompletedRunRetryIsZeroWrite(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	beforeManifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, _ := ledger.Recent(root, 100)
	retry, retryErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, io.Discard,
	)
	if retryErr != nil || retry.Status != entriesAutoStatusApplied || retry.Applied != 0 ||
		!retry.AuditRecorded || !strings.Contains(retry.Recovery, "零Apply") {
		t.Fatalf("已完成run重复收尾必须幂等: err=%v result=%+v", retryErr, retry)
	}
	afterManifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	afterEvents, _ := ledger.Recent(root, 100)
	if len(afterManifest.Reviews) != len(beforeManifest.Reviews) ||
		len(afterManifest.Applications) != len(beforeManifest.Applications) ||
		len(afterEvents) != len(beforeEvents) {
		t.Fatalf("重复run不得追加Review/Application/Ledger: before=%+v after=%+v events=%d/%d",
			beforeManifest, afterManifest, len(beforeEvents), len(afterEvents))
	}
}

func TestEntriesAutoFinalizeCheckRequiresRepairWithoutApply(
	t *testing.T,
) {
	root, runID := buildManualAtomicEntriesRepo(
		t,
	)

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal(
			"测试草稿没有Entries目标",
		)
	}

	target := manifest.Entries[0]
	draftName := entryDraftFileName(
		target.Path,
	)

	originalDraft, err := draft.ReadFile(
		root,
		runID,
		draftName,
	)
	if err != nil {
		t.Fatal(err)
	}

	bracket := strings.Index(
		string(originalDraft),
		"[",
	)
	if bracket < 0 {
		t.Fatalf(
			"测试草稿缺少标签起点: %s",
			originalDraft,
		)
	}

	invalidDraft :=
		"wrong.go" +
			string(originalDraft[bracket:])

	if err := draft.WriteFile(
		root,
		runID,
		draftName,
		[]byte(invalidDraft),
	); err != nil {
		t.Fatal(err)
	}

	cfg, doc := r65LoadEntriesAutoState(
		t,
		root,
	)

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	indexBefore, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer

	result, runErr := runEntriesAutoFinalize(
		root,
		cfg,
		doc,
		runID,
		len(manifest.Entries),
		ledger.SourceAgent,
		&output,
	)

	var exitErr *ExitError
	if !errors.As(
		runErr,
		&exitErr,
	) ||
		exitErr.Code != ExitInvalid {
		t.Fatalf(
			"共享内核Check拒绝应返回ExitInvalid: %v",
			runErr,
		)
	}

	if result == nil ||
		result.Status !=
			entriesAutoStatusRepairRequired ||
		result.FailedStep != entriesAutoStepCheck ||
		result.AssetWritten ||
		result.AuditRecorded ||
		result.Applied != 0 ||
		result.Rejected == 0 ||
		len(result.Findings) == 0 ||
		!strings.Contains(
			result.Recovery,
			"零写入",
		) ||
		!strings.Contains(
			result.Recovery,
			"再次提交同一完整批次",
		) {
		t.Fatalf(
			"Check repair_required结果不符: %+v",
			result,
		)
	}

	indexAfter, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(indexAfter) != string(indexBefore) {
		t.Fatal(
			"Check repair_required后正式索引必须零写入",
		)
	}

	afterManifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(afterManifest.Reviews) != 1 ||
		afterManifest.Reviews[0].Action !=
			draft.ReviewActionCheck ||
		afterManifest.Reviews[0].Rejected == 0 {
		t.Fatalf(
			"Check拒绝审计不符: %+v",
			afterManifest.Reviews,
		)
	}

	if len(afterManifest.Applications) != 0 ||
		afterManifest.AppliedAt != "" {
		t.Fatalf(
			"Check repair_required不得形成Application: %+v",
			afterManifest,
		)
	}

	events, _ := ledger.Recent(
		root,
		100,
	)

	for _, event := range events {
		if event.Op == "entries_diff" ||
			event.Op == "entries_apply" ||
			event.Op == "update_entries_batch" {
			t.Fatalf(
				"Check repair_required不得进入Diff或Apply: %+v",
				event,
			)
		}
	}

	if !strings.Contains(
		output.String(),
		"机器预检未通过",
	) {
		t.Fatalf(
			"Check输出缺修复说明: %s",
			output.String(),
		)
	}
}

func TestEntriesAutoFindingsKeepShuffledFullBatchPositions(t *testing.T) {
	paths := []string{
		"src/item09.go", "src/item02.go", "src/item13.go", "src/item00.go",
		"src/item05.go", "src/item01.go", "src/item11.go", "src/item04.go",
		"src/item08.go", "src/item06.go", "src/item10.go", "src/item03.go",
		"src/item12.go", "src/item07.go",
	}
	items := make([]entriesCheckItem, 0, len(paths))
	for _, path := range paths {
		items = append(items, entriesCheckItem{Path: path, Outcome: "passed"})
	}
	items[3].Outcome = "rejected"
	items[3].Errors = []entriesFinding{{
		Code: "impact_candidate_fras_invalid", Field: "A", RuleCode: "fras_a_too_many_items",
		Expected: "max_items=6", Actual: "item_count=7", Cause: "A exceeds its item limit",
	}}
	items[10].Outcome = "rejected"
	items[10].Errors = []entriesFinding{{
		Code: "impact_candidate_fras_invalid", Field: "R", RuleCode: "fras_r_too_many_items",
		Expected: "max_items=8", Actual: "item_count=9", Cause: "R exceeds its item limit",
	}}

	findings := entriesAutoRejectedFindings(items)
	if len(findings) != 2 ||
		findings[0].CandidateIndex != 4 || findings[0].Path != "src/item00.go" || findings[0].Field != "A" ||
		findings[0].RuleCode != "fras_a_too_many_items" || findings[0].Expected != "max_items=6" || findings[0].Actual != "item_count=7" ||
		findings[1].CandidateIndex != 11 || findings[1].Path != "src/item10.go" || findings[1].Field != "R" ||
		findings[1].RuleCode != "fras_r_too_many_items" || findings[1].Expected != "max_items=8" || findings[1].Actual != "item_count=9" {
		t.Fatalf("Auto finalizer renumbered the shuffled complete batch: %+v", findings)
	}
}

func TestEntriesAutoSortsFindingsIndependentOfInputOrder(t *testing.T) {
	items := []entriesCheckItem{
		{
			Path: "z.go", Outcome: "rejected",
			Errors: []entriesFinding{{Code: "impact_candidate_fras_invalid", Field: "A", RuleCode: "fras_a_too_many_items", Expected: "max_items=6", Actual: "item_count=7"}},
		},
		{
			Path: "a.go", Outcome: "rejected",
			Errors: []entriesFinding{{Code: "impact_candidate_fras_invalid", Field: "A", RuleCode: "fras_a_too_many_items", Expected: "max_items=6", Actual: "item_count=7"}},
		},
	}

	findings := entriesAutoRejectedFindings(items)
	if len(findings) != 2 || findings[0].Path != "a.go" || findings[0].CandidateIndex != 2 ||
		findings[1].Path != "z.go" || findings[1].CandidateIndex != 1 {
		t.Fatalf("Auto retained input order instead of the shared Finding order: %+v", findings)
	}
}

func TestCLIMCPAndAutoRepairFindingsMatchFieldForField(t *testing.T) {
	raw := []cognition.RepairFinding{
		{
			CandidateIndex: 1, Path: "z.go", CanonicalObjectIdentity: "code:z.go", Domain: "code",
			Field: "A", RuleCode: "fras_a_too_many_items", Expected: "max_items=6", Actual: "item_count=7",
			Code: "impact_candidate_fras_invalid",
		},
		{
			CandidateIndex: 2, Path: "a.go", CanonicalObjectIdentity: "code:a.go", Domain: "code",
			Field: "A", RuleCode: "fras_a_too_many_items", Expected: "max_items=6", Actual: "item_count=7",
			Code: "impact_candidate_fras_invalid",
		},
	}
	items := []entriesCheckItem{
		{Path: "z.go", Outcome: "rejected", Errors: []entriesFinding{raw[0]}},
		{Path: "a.go", Outcome: "rejected", Errors: []entriesFinding{raw[1]}},
	}

	mcpFindings := mcptools.LocalizeRepairFindings(raw)
	cliFindings := updateEntryRepairFindings(raw)
	autoFindings := entriesAutoRejectedFindings(items)
	if !reflect.DeepEqual(cliFindings, mcpFindings) || !reflect.DeepEqual(autoFindings, mcpFindings) {
		t.Fatalf("CLI/MCP/Auto Findings diverged:\nCLI=%+v\nMCP=%+v\nAuto=%+v", cliFindings, mcpFindings, autoFindings)
	}
}

func TestEntriesAutoBaselineFailureStopsAndReplayRecovers(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	unblockBaseline := blockBaselineBackupReplacement(t, root)
	var output bytes.Buffer
	result, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, &output,
	)
	var exitErr *ExitError
	if !errors.As(runErr, &exitErr) || exitErr.Code != ExitInternal || result == nil ||
		result.Status != entriesAutoStatusStopped || !result.AssetWritten ||
		result.Applied != len(manifest.Entries) || !result.AuditRecorded {
		t.Fatalf("Auto Baseline故障必须返回可恢复stopped: err=%v result=%+v\n%s", runErr, result, output.String())
	}
	afterFailure, err := draft.LoadManifest(root, runID)
	if err != nil || afterFailure.AppliedAt != "" || len(afterFailure.Applications) != 1 ||
		afterFailure.Applications[0].RejectKinds != "baseline_incomplete" {
		t.Fatalf("Auto Baseline故障不得标记Run已应用: err=%v manifest=%+v", err, afterFailure)
	}
	unblockBaseline()
	cfg, doc = r65LoadEntriesAutoState(t, root)
	output.Reset()
	recovered, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, &output,
	)
	if runErr != nil || recovered.Status != entriesAutoStatusApplied ||
		!recovered.AuditRecorded || recovered.Applied != 0 || recovered.AssetWritten {
		t.Fatalf("Auto受绑定重放应只恢复Baseline并完成: err=%v result=%+v\n%s", runErr, recovered, output.String())
	}
	afterRecovery, err := draft.LoadManifest(root, runID)
	if err != nil || afterRecovery.AppliedAt == "" || len(afterRecovery.Applications) != 2 ||
		afterRecovery.Applications[1].Applied != 0 ||
		afterRecovery.Applications[1].Recovered != len(manifest.Entries) {
		t.Fatalf("Auto重放未恢复Run终态: err=%v manifest=%+v", err, afterRecovery)
	}
	events, _ := ledger.Recent(root, 100)
	initialApplied, replayZero := false, false
	for _, event := range events {
		if event.Op != "entries_apply" || event.DraftRunID != runID {
			continue
		}
		if event.RejectKinds == "baseline_incomplete" && event.AppliedCount == len(manifest.Entries) {
			initialApplied = true
		}
		if event.Result == ledger.ResultOK && event.AppliedCount == 0 &&
			event.RecoveredCount == len(manifest.Entries) {
			replayZero = true
		}
	}
	if !initialApplied || !replayZero {
		t.Fatalf("Auto恢复Ledger必须区分首次写入与零写入重放: %+v", events)
	}
}

func TestEntriesAutoReplayPersistsRecoveredCountAfterPreAuditCrash(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	before, err := draft.LoadManifest(root, runID)
	if err != nil || len(before.Applications) != 0 {
		t.Fatalf("夹具必须从未应用Host-Agent Manifest开始: err=%v manifest=%+v", err, before)
	}
	fingerprintA, err := baseline.HashFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	fingerprintB, err := baseline.HashFile(filepath.Join(root, "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	first, fail := mcptools.ApplyUpdateEntriesAtomicBound(root, []mcptools.AtomicUpdateItem{
		{Path: "a.go", NewEntry: manualAtomicNewA, SourceSHA256: fingerprintA.SHA256},
		{Path: "b.go", NewEntry: manualAtomicNewB, SourceSHA256: fingerprintB.SHA256},
	}, ledger.SourceAgent, false, before.IndexSHA256)
	if fail != nil || first == nil || first.AppliedCount != 2 || !first.BaselineComplete {
		t.Fatalf("模拟审计前崩溃的底层Apply失败: fail=%+v outcome=%+v", fail, first)
	}
	before, err = draft.LoadManifest(root, runID)
	if err != nil || len(before.Applications) != 0 {
		t.Fatalf("夹具必须停在Application前: err=%v manifest=%+v", err, before)
	}

	cfg, doc := r65LoadEntriesAutoState(t, root)
	var output bytes.Buffer
	recovered, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(before.Entries), ledger.SourceAgent, &output,
	)
	if runErr != nil || recovered.Status != entriesAutoStatusApplied || recovered.Applied != 0 ||
		recovered.Recovered != len(before.Entries) || recovered.AssetWritten {
		t.Fatalf("崩溃重放必须区分零新写入与已恢复目标: err=%v result=%+v", runErr, recovered)
	}
	after, err := draft.LoadManifest(root, runID)
	if err != nil || len(after.Applications) != 1 ||
		after.Applications[0].Recovered != len(before.Entries) ||
		after.Applications[0].Applied != 0 {
		t.Fatalf("Application必须持久记录恢复数量: err=%v manifest=%+v", err, after)
	}
	events, _ := ledger.Recent(root, 100)
	found := false
	for _, event := range events {
		if event.Op == "update_entries_batch_recover" &&
			event.RecoveredCount == len(before.Entries) && event.DuplicateApplies == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("底层Ledger必须持久记录崩溃重放恢复数量: %+v", events)
	}
}

func TestEntriesAutoReportsBaselineAndApplicationAuditFailureTogether(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	_ = blockBaselineBackupReplacement(t, root)
	previous := appendEntriesAutoApplication
	appendEntriesAutoApplication = func(string, string, draft.ApplicationRecord, bool) error {
		return errors.New("injected application audit failure")
	}
	t.Cleanup(func() { appendEntriesAutoApplication = previous })
	var output bytes.Buffer
	result, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, &output,
	)
	if runErr == nil || result == nil || result.FailedStep != entriesAutoStepAudit ||
		result.AuditRecorded || !strings.Contains(runErr.Error(), "Application审计失败") ||
		!strings.Contains(result.Recovery, "同时Application审计保存失败") ||
		result.RejectKinds != "baseline_incomplete,application_audit" {
		t.Fatalf("Baseline与Application双重失败必须完整归因: err=%v result=%+v", runErr, result)
	}
	events, _ := ledger.Recent(root, 100)
	found := false
	for _, event := range events {
		if event.Op == "entries_apply" && event.DraftRunID == runID &&
			event.Result == ledger.ResultError &&
			event.RejectKinds == "baseline_incomplete,application_audit" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Baseline与Application双重失败必须形成error Ledger: %+v", events)
	}
}

func TestEntriesAutoApplyAndApplicationAuditDoubleFailureIsLedgerError(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	if err := os.WriteFile(
		filepath.Join(root, "a.go"),
		[]byte("package demo\n\nvar Drift = true\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	previous := appendEntriesAutoApplication
	appendEntriesAutoApplication = func(string, string, draft.ApplicationRecord, bool) error {
		return errors.New("injected application audit failure")
	}
	t.Cleanup(func() { appendEntriesAutoApplication = previous })
	var output bytes.Buffer
	result, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, &output,
	)
	if runErr == nil || result == nil || result.FailedStep != entriesAutoStepAudit ||
		result.RejectKinds != "conflict,application_audit" || result.AuditRecorded {
		t.Fatalf("Apply冲突与Application双重失败必须完整归因: err=%v result=%+v", runErr, result)
	}
	events, _ := ledger.Recent(root, 100)
	found := false
	for _, event := range events {
		if event.Op == "entries_apply" && event.DraftRunID == runID &&
			event.Result == ledger.ResultError &&
			event.RejectKinds == "conflict,application_audit" &&
			event.RejectedCount == len(manifest.Entries) {
			found = true
		}
	}
	if !found {
		t.Fatalf("Apply与Application双重失败不得记成普通conflict: %+v", events)
	}
}

func TestEntriesAutoRecordsAppliedCountWhenApplicationAuditFails(t *testing.T) {
	root, runID := buildManualAtomicEntriesRepo(t)
	cfg, doc := r65LoadEntriesAutoState(t, root)
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	previous := appendEntriesAutoApplication
	appendEntriesAutoApplication = func(string, string, draft.ApplicationRecord, bool) error {
		return errors.New("injected application audit failure")
	}
	t.Cleanup(func() { appendEntriesAutoApplication = previous })
	var output bytes.Buffer
	result, runErr := runEntriesAutoFinalize(
		root, cfg, doc, runID, len(manifest.Entries), ledger.SourceAgent, &output,
	)
	if runErr == nil || result == nil || result.FailedStep != entriesAutoStepAudit ||
		result.Applied != len(manifest.Entries) || !result.AssetWritten {
		t.Fatalf("Application审计失败仍须保留首次真实写入计数: err=%v result=%+v", runErr, result)
	}
	events, _ := ledger.Recent(root, 100)
	found := false
	for _, event := range events {
		if event.Op == "entries_apply" && event.DraftRunID == runID &&
			event.Result == ledger.ResultError && event.RejectKinds == "application_audit" &&
			event.AppliedCount == len(manifest.Entries) {
			found = true
		}
	}
	if !found {
		t.Fatalf("Application审计失败不得漏记首次真实Apply: %+v", events)
	}
}
