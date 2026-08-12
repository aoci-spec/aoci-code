// Auto无变化、小变化与重试路径的正式计量报告。
package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestRenderAutoResultKeepsCriticalZeroCounts(t *testing.T) {
	raw := renderAutoResult(autoResult{
		Version:    1,
		Status:     autoStatusApplied,
		Aligned:    true,
		Receipt:    cognitionReceipt{},
		Metrics:    autoMetrics{},
		NextAction: autoApplyNextAction(false, true, 0, 0),
	})
	assertAutoMachineCounts(t, raw, 0, 0, 0, 0, 0)
	for _, field := range []string{
		`"attempted":0`,
		`"applied":0`,
		`"remaining":0`,
		`"semantic_files":0`,
		`"format_only_files":0`,
		`"duplicate_applies":0`,
	} {
		if !strings.Contains(raw, field) {
			t.Fatalf("关键零值字段不得从机器终态省略: field=%s raw=%s", field, raw)
		}
	}
}

func TestAutoPerformanceReport(t *testing.T) {
	alignedRoot := buildFormatOnlyRepo(t)
	aligned := decodeAutoResult(t, handleMaintainWithVersion(alignedRoot, "metrics-test"))
	if !aligned.Aligned || aligned.Metrics.AOCIToolCalls != 1 ||
		aligned.Metrics.ShellAOCICalls != 0 || aligned.Metrics.OverviewReads != 0 ||
		aligned.Metrics.LocalRecalls != 0 || aligned.Metrics.SemanticFiles != 0 ||
		aligned.Metrics.FormatOnlyFiles != 0 || aligned.Metrics.DeterministicMs > 1000 {
		t.Fatalf("无变化Auto路径未保持低开销: %+v", aligned)
	}
	formatRoot := buildFormatOnlyRepo(t)
	maintainWriteFile(t, formatRoot, "format.go", "package sample\n\nfunc Format() int { return 1 }\n")
	formatOnly := decodeAutoResult(t, handleMaintainWithVersion(formatRoot, "metrics-test"))
	if !formatOnly.Aligned || formatOnly.Metrics.FormatOnlyFiles != 1 ||
		formatOnly.Metrics.SemanticFiles != 0 || formatOnly.Metrics.AOCIToolCalls != 1 {
		t.Fatalf("format-only低开销路径计量不符: %+v", formatOnly)
	}

	semanticRoot := buildRepo(t)
	writeBatchSource(t, semanticRoot, "src/b.go")
	input := []updateEntryItemIn{{
		Path:         "src/b.go",
		NewEntry:     "b.go[X.Y.5.T]: F:计量新增 | R:- | A:- | S:模型逐文件阅读",
		SourceSHA256: sourceSHA256(t, semanticRoot, "src/b.go"),
	}}
	applied := decodeAutoResult(t, handleMCPUpdateBatch(semanticRoot, "metrics-test", input))
	retry := decodeAutoResult(t, handleMCPUpdateBatch(semanticRoot, "metrics-test", input))
	if !applied.Aligned || applied.Applied != 1 || applied.Metrics.AOCIToolCalls != 1 ||
		applied.Metrics.SemanticFiles != 1 || retry.Applied != 0 ||
		retry.Metrics.DuplicateApplies != 1 || retry.Metrics.ShellAOCICalls != 0 {
		t.Fatalf("小范围语义批次或防重计量不符: applied=%+v retry=%+v", applied, retry)
	}

	t.Logf(
		"AUTO_METRICS aligned_ms=%d aligned_calls=%d shell_calls=%d overview_reads=%d local_recalls=%d semantic_files=%d format_only_files=%d apply_ms=%d duplicate_applies=%d",
		aligned.Metrics.DeterministicMs,
		aligned.Metrics.AOCIToolCalls,
		aligned.Metrics.ShellAOCICalls,
		aligned.Metrics.OverviewReads,
		aligned.Metrics.LocalRecalls,
		applied.Metrics.SemanticFiles,
		formatOnly.Metrics.FormatOnlyFiles,
		applied.Metrics.DeterministicMs,
		retry.Metrics.DuplicateApplies,
	)
}

func TestMaintainCarriesSemanticThresholdThroughAlignedApply(t *testing.T) {
	root := buildFormatOnlyRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CognitionRefreshThreshold = 1
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	clean, fail := inspectSemanticChanges(root, repository)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	session := newCognitionRefreshSession()
	current := newCognitionReceipt(root, "refresh-test", repository.text, cognitionScopeRepositoryFull)
	initial := session.evaluate(overviewIn{}, current, clean, nil, "")
	if initial.RefreshStatus != machinecontract.RefreshStatusReadyForOverview {
		t.Fatalf("initial refresh = %+v", initial)
	}
	session.markDeliveryAttempt(session.deliveredReceipt(current, ""))

	maintainWriteFile(t, root, "semantic.go", "package sample\n\nfunc Semantic() int { return 2 }\n")
	maintain := decodeAutoResult(t, handleMaintainWithVersion(root, "refresh-test", session))
	if maintain.Status != autoStatusRepairRequired || maintain.RefreshStatus != machinecontract.RefreshStatusRequired ||
		!containsRefreshReason(maintain.RefreshReasons, machinecontract.RefreshReasonSemanticThreshold) ||
		!containsRefreshReason(maintain.Receipt.PendingRefreshReasons, machinecontract.RefreshReasonSemanticThreshold) ||
		len(maintain.Candidates) != 1 {
		t.Fatalf("Maintain未保留阈值刷新事实: %+v", maintain)
	}

	applied := decodeAutoResult(t, handleMCPUpdateBatch(
		root,
		"refresh-test",
		[]updateEntryItemIn{{
			Path:         "semantic.go",
			SourceSHA256: maintain.Candidates[0].SourceSHA256,
			NewEntry:     "semantic.go[CRT7T]: F:语义靶 | R:- | A:Semantic | S:返回值变更为2",
		}},
		session,
	))
	if !applied.Aligned || applied.RefreshStatus != machinecontract.RefreshStatusReadyForOverview ||
		!containsRefreshReason(applied.RefreshReasons, machinecontract.RefreshReasonSemanticThreshold) ||
		applied.NextAction != refreshNextAction(machinecontract.RefreshStatusReadyForOverview) {
		t.Fatalf("aligned Apply未交付一次Overview的机器下一步: %+v", applied)
	}
	for _, token := range []string{"Verify", "Aggregate Check", "Guide"} {
		if !strings.Contains(applied.NextAction, token) {
			t.Fatalf("刷新动作不得覆盖Volumes终态证明步骤%q: %q", token, applied.NextAction)
		}
	}

	repository, fail = loadRepoCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	clean, fail = inspectSemanticChanges(root, repository)
	if fail != nil || !clean.GovernanceAligned {
		t.Fatalf("Apply后未aligned: facts=%+v fail=%+v", clean, fail)
	}
	current = newCognitionReceipt(root, "refresh-test", repository.text, cognitionScopeRepositoryFull)
	ready := session.evaluate(overviewIn{}, current, clean, nil, "")
	if ready.RefreshStatus != machinecontract.RefreshStatusReadyForOverview ||
		!containsRefreshReason(ready.RefreshReasons, machinecontract.RefreshReasonSemanticThreshold) {
		t.Fatalf("阈值事件在正文交付前丢失: %+v", ready)
	}
	session.markDeliveryAttempt(session.deliveredReceipt(current, ""))
	repeated := session.evaluate(overviewIn{}, current, clean, nil, "")
	if repeated.RefreshStatus != machinecontract.RefreshStatusNotRequired {
		t.Fatalf("阈值事件消费后重复正文: %+v", repeated)
	}
}

func TestMaintainBelowThresholdKeepsOrdinaryTerminalResult(t *testing.T) {
	root := buildFormatOnlyRepo(t)
	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	clean, fail := inspectSemanticChanges(root, repository)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	session := newCognitionRefreshSession()
	current := newCognitionReceipt(root, "refresh-test", repository.text, cognitionScopeRepositoryFull)
	session.evaluate(overviewIn{}, current, clean, nil, "")
	session.markDeliveryAttempt(session.deliveredReceipt(current, ""))

	maintainWriteFile(t, root, "semantic.go", "package sample\n\nfunc Semantic() int { return 2 }\n")
	maintain := decodeAutoResult(t, handleMaintainWithVersion(root, "refresh-test", session))
	if maintain.RefreshStatus != "" || len(maintain.RefreshReasons) != 0 || len(maintain.Candidates) != 1 {
		t.Fatalf("低于阈值的普通维护不应创建刷新: %+v", maintain)
	}
	applied := decodeAutoResult(t, handleMCPUpdateBatch(
		root,
		"refresh-test",
		[]updateEntryItemIn{{
			Path:         "semantic.go",
			SourceSHA256: maintain.Candidates[0].SourceSHA256,
			NewEntry:     "semantic.go[CRT7T]: F:语义靶 | R:- | A:Semantic | S:返回值变更为2",
		}},
		session,
	))
	if !applied.Aligned || applied.RefreshStatus != "" || len(applied.RefreshReasons) != 0 ||
		applied.NextAction != autoApplyNextAction(false, true, 1, 0) {
		t.Fatalf("低于阈值的Apply不应要求Overview: %+v", applied)
	}
}
