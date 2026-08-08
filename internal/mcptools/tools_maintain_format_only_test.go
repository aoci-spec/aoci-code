// maintain的format-only正例、反例、混合批次与防重测试。
package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func buildFormatOnlyRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	maintainWriteFile(t, root, "format.go", "package sample\n\nfunc Format( )int{return 1}\n")
	maintainWriteFile(t, root, "semantic.go", "package sample\n\nfunc Semantic() int { return 1 }\n")
	indexText := maintainHeader(true) + "\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"format.go[CRT7T]: F:格式靶 | R:- | A:Format | S:语义保持\n" +
		"semantic.go[CRT7T]: F:语义靶 | R:- | A:Semantic | S:返回值属于契约\n"
	maintainWriteFile(t, root, "aoci.txt", indexText)
	cfg := legacyTestConfig()
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, warnings, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("建立Baseline失败: warnings=%v err=%v", warnings, err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMaintainAppliesFormatOnlyWithoutRewritingEntry(t *testing.T) {
	root := buildFormatOnlyRepo(t)
	indexBefore, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	maintainWriteFile(t, root, "format.go", "package sample\n\nfunc Format() int { return 1 }\n")

	result := decodeAutoResult(t, handleMaintainWithVersion(root, "test-version"))
	if result.Status != autoStatusApplied || !result.Aligned ||
		result.Metrics.FormatOnlyFiles != 1 || len(result.Candidates) != 0 {
		t.Fatalf("纯格式变化应一次无语义收口: %+v", result)
	}
	if len(result.FormatOnlyApplied) != 1 || result.FormatOnlyApplied[0] != "format.go" {
		t.Fatalf("format-only目标不符: %+v", result.FormatOnlyApplied)
	}
	indexAfter, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if string(indexAfter) != string(indexBefore) {
		t.Fatal("format-only不得重写Entry或索引")
	}
	baselineAfterFirst, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	second := decodeAutoResult(t, handleMaintainWithVersion(root, "test-version"))
	baselineAfterSecond, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if second.Metrics.FormatOnlyFiles != 0 || second.Metrics.RepeatedMaintains != 1 ||
		string(baselineAfterFirst) != string(baselineAfterSecond) {
		t.Fatal("重复maintain不得重复前移format-only Baseline")
	}
	events, _ := ledger.Recent(root, 50)
	formatEvents := 0
	for _, event := range events {
		if event.Op == "format_only" {
			formatEvents++
		}
	}
	if formatEvents != 1 {
		t.Fatalf("format_only审计事件应且仅应一条: %+v", events)
	}
}

func TestMaintainFormatOnlyRejectsSemanticAndHandlesMixedBatch(t *testing.T) {
	root := buildFormatOnlyRepo(t)
	maintainWriteFile(t, root, "format.go", "package sample\n\nfunc Format() int { return 1 }\n")
	maintainWriteFile(t, root, "semantic.go", "package sample\n\nfunc Semantic() int { return 2 }\n")

	result := decodeAutoResult(t, handleMaintain(root))
	paths := candidatePaths(result)
	if result.Status != autoStatusRepairRequired || result.Aligned ||
		result.Metrics.FormatOnlyFiles != 1 || result.Metrics.SemanticFiles != 1 {
		t.Fatalf("混合批次状态不符: %+v", result)
	}
	if paths["format.go"] || !paths["semantic.go"] {
		t.Fatalf("只应把真实语义变化交给模型: %+v", result.Candidates)
	}
}

func TestMaintainInvalidCurationStopsBeforeFormatOnlyWrite(t *testing.T) {
	root := buildFormatOnlyRepo(t)
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineBefore, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	maintainWriteFile(t, root, "format.go", "package sample\n\nfunc Format() int { return 1 }\n")
	if err := os.WriteFile(filepath.Join(root, ".aoci", "curation.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := decodeAutoResult(t, handleMaintainWithVersion(root, "test-version"))
	baselineAfter, readErr := os.ReadFile(baselinePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if result.Status != autoStatusStopped || result.Metrics.FormatOnlyFiles != 0 ||
		len(result.FormatOnlyApplied) != 0 || string(baselineBefore) != string(baselineAfter) {
		t.Fatalf("策展停点必须发生在format-only写入前: %+v", result)
	}
}

func TestMaintainFormatOnlyDriftDuringBaselineSaveCannotReportAligned(t *testing.T) {
	root := buildFormatOnlyRepo(t)
	maintainWriteFile(t, root, "format.go", "package sample\n\nfunc Format() int { return 1 }\n")
	previousSave := saveFormatOnlyBaseline
	saveFormatOnlyBaseline = func(root string, state *baseline.Baseline) error {
		if err := previousSave(root, state); err != nil {
			return err
		}
		return os.WriteFile(
			filepath.Join(root, "format.go"),
			[]byte("package sample\n\nfunc Format() int { return 2 }\n"),
			0o644,
		)
	}
	t.Cleanup(func() { saveFormatOnlyBaseline = previousSave })

	result := decodeAutoResult(t, handleMaintainWithVersion(root, "test-version"))
	if result.Status != autoStatusStopped || result.Aligned ||
		len(result.Findings) == 0 {
		t.Fatalf("保存期漂移不得从旧快照误报aligned: %+v", result)
	}
	state, exists, err := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, "format.go"))
	if err != nil || hashErr != nil || !exists || state.Files["format.go"] == current {
		t.Fatalf("保存期漂移必须保持Stale: exists=%v load=%v hash=%v state=%+v current=%+v", exists, err, hashErr, state, current)
	}
}
