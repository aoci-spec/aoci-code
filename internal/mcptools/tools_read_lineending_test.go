// Consumer-level tests of get_entries line-ending tolerance.
package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// TestGetEntriesLineEndingTolerance verifies the default tolerance, team strictness, and a real change.
func TestGetEntriesLineEndingTolerance(
	t *testing.T,
) {
	root := buildRepo(t)

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}

	sourcePath := filepath.Join(
		root,
		"src",
		"a.go",
	)

	// First re-establish a Baseline with NormalizedSHA256 from LF content.
	if err := os.WriteFile(
		sourcePath,
		[]byte("A1\n"),
		0o644,
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

	// Change only LF to CRLF.
	if err := os.WriteFile(
		sourcePath,
		[]byte("A1\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output := resText(
		t,
		handleGetEntries(
			root,
			"test-version",
			getEntriesIn{
				Paths: []string{
					"src/a.go",
				},
			},
			nil,
		),
	)

	if strings.Contains(
		output,
		"STALE",
	) {
		t.Fatalf(
			"默认宽容时纯换行差异不得注入假STALE:\n%s",
			output,
		)
	}

	if !strings.Contains(
		output,
		"改前必读",
	) {
		t.Fatalf(
			"宽容时仍应正常返回条目:\n%s",
			output,
		)
	}

	events, _ := ledger.Recent(
		root,
		10,
	)

	foundCleanEvent := false

	for _, event := range events {
		if event.Op == "get_entries" {
			foundCleanEvent = true

			if event.DriftWarned {
				t.Fatalf(
					"纯换行差异不得把Ledger记为DriftWarned: %+v",
					event,
				)
			}
		}
	}

	if !foundCleanEvent {
		t.Fatal("未找到get_entries Ledger事件")
	}

	// After the team is explicitly strict, the same scene must return to STALE.
	cfg.LineEndingTolerance = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	strictOutput := resText(
		t,
		handleGetEntries(
			root,
			"test-version",
			getEntriesIn{
				Paths: []string{
					"src/a.go",
				},
			},
			nil,
		),
	)

	if !strings.Contains(
		strictOutput,
		"⚠ STALE",
	) {
		t.Fatalf(
			"团队严格模式必须恢复STALE:\n%s",
			strictOutput,
		)
	}

	// Restore tolerance but manufacture a real content change: it must still be STALE.
	cfg.LineEndingTolerance = true

	if err := config.Save(
		root,
		cfg,
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

	changedOutput := resText(
		t,
		handleGetEntries(
			root,
			"test-version",
			getEntriesIn{
				Paths: []string{
					"src/a.go",
				},
			},
			nil,
		),
	)

	if !strings.Contains(
		changedOutput,
		"⚠ STALE",
	) {
		t.Fatalf(
			"真实内容变化不得被换行宽容吞掉:\n%s",
			changedOutput,
		)
	}
}
