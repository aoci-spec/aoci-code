// aoci_maintain换行宽容消费级测试。
package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func buildMaintainLineEndingRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"keep.go",
		"package main\n\nvar Value = 1\n",
	)

	indexText := maintainHeader(true) +
		"\n===代码索引" +
		filepath.ToSlash(root) +
		"/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"keep.go[CRT7T]: F:对齐靶 | R:- | A:- | S:保留约束\n"

	maintainWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = true

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, warnings, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(warnings) != 0 {
		t.Fatalf(
			"测试快照不应有警告: %v",
			warnings,
		)
	}

	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	return root
}

// latestMaintainEvent返回最近一次maintain事件。
func latestMaintainEvent(
	t *testing.T,
	root string,
) ledger.Event {
	t.Helper()

	events, _ := ledger.Recent(
		root,
		20,
	)

	for position := len(events) - 1; position >= 0; position-- {
		if events[position].Op == "maintain" {
			return events[position]
		}
	}

	t.Fatalf(
		"Ledger未找到maintain事件: %+v",
		events,
	)

	return ledger.Event{}
}

// TestMaintainLineEndingToleranceProducesNoTask验证默认宽容下零任务、零漂移账。
func TestMaintainLineEndingToleranceProducesNoTask(
	t *testing.T,
) {
	root := buildMaintainLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"keep.go",
		),
		[]byte(
			"package main\r\n\r\nvar Value = 1\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusApplied || !result.Aligned || len(result.Candidates) != 0 {
		t.Fatalf("宽容模式的纯换行差异不得派发: %+v", result)
	}

	event := latestMaintainEvent(
		t,
		root,
	)

	if event.PathsCount != 0 ||
		event.DriftWarned {
		t.Fatalf(
			"纯换行差异Ledger应为零任务且不告警: %+v",
			event,
		)
	}
}

// TestMaintainLineEndingStrictModeDispatchesUpdate验证团队严格模式。
func TestMaintainLineEndingStrictModeDispatchesUpdate(
	t *testing.T,
) {
	root := buildMaintainLineEndingRepo(t)

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}

	cfg.LineEndingTolerance = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"keep.go",
		),
		[]byte(
			"package main\r\n\r\nvar Value = 1\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusRepairRequired || !candidatePaths(result)["keep.go"] {
		t.Fatalf("团队严格模式应派发更新: %+v", result)
	}

	event := latestMaintainEvent(
		t,
		root,
	)

	if event.PathsCount != 1 ||
		!event.DriftWarned {
		t.Fatalf(
			"严格模式Ledger应记录一个漂移任务: %+v",
			event,
		)
	}
}

// TestMaintainLineEndingToleranceDoesNotHideRealChange验证真实变化。
func TestMaintainLineEndingToleranceDoesNotHideRealChange(
	t *testing.T,
) {
	root := buildMaintainLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"keep.go",
		),
		[]byte(
			"package main\r\n\r\nvar Value = 2\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusRepairRequired || !candidatePaths(result)["keep.go"] {
		t.Fatalf("真实内容变化必须派发更新: %+v", result)
	}

	event := latestMaintainEvent(
		t,
		root,
	)

	if event.PathsCount != 1 ||
		!event.DriftWarned {
		t.Fatalf(
			"真实变化Ledger应记录漂移任务: %+v",
			event,
		)
	}
}
