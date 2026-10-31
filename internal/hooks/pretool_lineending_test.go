// Consumer-level tests of line-ending tolerance in PreTool strict mode.
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func buildPretoolLineEndingRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()

	if err := os.MkdirAll(
		filepath.Join(
			root,
			"src",
		),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(
		filepath.Join(
			root,
			".aoci",
		),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"src",
			"a.go",
		),
		[]byte("A1\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	indexText :=
		"===段 " +
			filepath.ToSlash(root) +
			"/src/===\n" +
			"a.go[X.Y.5.T]: F:测试文件 | R:- | A:- | S:必须保留的约束\n"

	if err := os.WriteFile(
		filepath.Join(
			root,
			".aoci",
			"index.txt",
		),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	configValue := config.DefaultConfig()
	configValue.IndexPath = ".aoci/index.txt"
	configValue.HookStrict = true
	configValue.LedgerEnabled = true

	if err := config.Save(
		root,
		configValue,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, warnings, err := baseline.Snapshot(
		root,
		configValue.WalkOptions(),
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

func latestPretoolEvent(
	t *testing.T,
	root string,
) ledger.Event {
	t.Helper()

	events, _ := ledger.Recent(
		root,
		20,
	)

	for position := len(events) - 1; position >= 0; position-- {
		if events[position].Op ==
			"hook_trigger" {
			return events[position]
		}
	}

	t.Fatalf(
		"Ledger未找到hook_trigger事件: %+v",
		events,
	)

	return ledger.Event{}
}

// TestPretoolLineEndingTolerance verifies:
// the default tolerance does not block; team strictness restores blocking; a real change always blocks.
func TestPretoolLineEndingTolerance(
	t *testing.T,
) {
	root := buildPretoolLineEndingRepo(t)

	sourcePath := filepath.Join(
		root,
		"src",
		"a.go",
	)

	// Change only LF to CRLF.
	if err := os.WriteFile(
		sourcePath,
		[]byte("A1\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	tolerantResult := HandlePreTool(
		root,
		"Edit",
		"src/a.go",
	)

	if tolerantResult.Block {
		t.Fatalf(
			"默认宽容时纯换行差异不得触发strict阻断: %+v",
			tolerantResult,
		)
	}

	if strings.Contains(
		tolerantResult.Text,
		"STALE",
	) {
		t.Fatalf(
			"默认宽容时不得注入假STALE:\n%s",
			tolerantResult.Text,
		)
	}

	if !strings.Contains(
		tolerantResult.Text,
		"必须保留的约束",
	) {
		t.Fatalf(
			"宽容时仍须正常注入条目:\n%s",
			tolerantResult.Text,
		)
	}

	tolerantEvent := latestPretoolEvent(
		t,
		root,
	)

	if tolerantEvent.DriftWarned {
		t.Fatalf(
			"纯换行差异不得把Ledger记为DriftWarned: %+v",
			tolerantEvent,
		)
	}

	// When the team is explicitly strict, the same scene must restore the STALE block.
	configValue, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}

	configValue.LineEndingTolerance = false

	if err := config.Save(
		root,
		configValue,
	); err != nil {
		t.Fatal(err)
	}

	strictResult := HandlePreTool(
		root,
		"Edit",
		"src/a.go",
	)

	if !strictResult.Block ||
		!strings.Contains(
			strictResult.Text,
			"STALE",
		) {
		t.Fatalf(
			"团队严格模式必须恢复STALE阻断: %+v",
			strictResult,
		)
	}

	strictEvent := latestPretoolEvent(
		t,
		root,
	)

	if !strictEvent.DriftWarned {
		t.Fatalf(
			"严格模式应记录DriftWarned: %+v",
			strictEvent,
		)
	}

	// Restore tolerance but manufacture a real content change: it must still block.
	configValue.LineEndingTolerance = true

	if err := config.Save(
		root,
		configValue,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		sourcePath,
		[]byte("A2\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	changedResult := HandlePreTool(
		root,
		"Write",
		"src/a.go",
	)

	if !changedResult.Block ||
		!strings.Contains(
			changedResult.Text,
			"STALE",
		) {
		t.Fatalf(
			"真实内容变化不得被换行宽容吞掉: %+v",
			changedResult,
		)
	}

	changedEvent := latestPretoolEvent(
		t,
		root,
	)

	if !changedEvent.DriftWarned {
		t.Fatalf(
			"真实变化必须记录DriftWarned: %+v",
			changedEvent,
		)
	}
}
