// get_entries换行宽容消费级测试。
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

// TestGetEntriesLineEndingTolerance验证默认宽容、团队严格与真实变化。
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

	// 先以LF内容重新建立带NormalizedSHA256的Baseline。
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

	// 只把LF改成CRLF。
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

	// 团队显式严格后，同一现场必须恢复STALE。
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

	// 恢复宽容，但制造真实内容变化，仍必须STALE。
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
