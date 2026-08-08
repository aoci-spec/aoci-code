// 单条与原子批量回写对共享E规模路径判据的消费测试。
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

const (
	runtimeLedgerEntry = "ledger.jsonl[ALG7M]: F:台账 | R:- | A:- | S:测试"
	staticBigEntry     = "big.go[XCR7M]: F:源码 | R:- | A:- | S:测试"
)

// buildWriteEScalePathRepo 构造一个治理运行时资产和一个静态源码均超过400行的仓。
func buildWriteEScalePathRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, directory := range []string{
		".aoci",
		"src",
	} {
		if err := os.MkdirAll(
			filepath.Join(root, directory),
			0o755,
		); err != nil {
			t.Fatal(err)
		}
	}

	longFile := strings.Repeat(
		"x\n",
		401,
	)
	if err := os.WriteFile(
		filepath.Join(
			root,
			".aoci",
			"ledger.jsonl",
		),
		[]byte(longFile),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(
			root,
			"src",
			"big.go",
		),
		[]byte(longFile),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)
	indexText := "#A层级: A-运行时 X-源码\n" +
		"#B模块: LG台账 CR核心\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		"===治理 " + rootSlash + "/.aoci/===\n" +
		runtimeLedgerEntry + "\n" +
		"===源码 " + rootSlash + "/src/===\n" +
		staticBigEntry + "\n"

	if err := os.WriteFile(
		filepath.Join(root, "aoci.txt"),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, _, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	return root
}

func hasWriteEScaleWarning(
	warnings []string,
) bool {
	for _, warning := range warnings {
		if strings.Contains(
			warning,
			"E规模档位错配",
		) {
			return true
		}
	}
	return false
}

// TestApplyUpdateEntryEScalePathBoundary 锁定单条回写同判。
func TestApplyUpdateEntryEScalePathBoundary(t *testing.T) {
	root := buildWriteEScalePathRepo(t)

	runtimeOutcome, runtimeFail := ApplyUpdateEntry(
		root,
		".aoci/ledger.jsonl",
		runtimeLedgerEntry,
		ledger.SourceHuman,
		true,
	)
	if runtimeFail != nil {
		t.Fatalf(
			"运行时资产预览失败: %+v",
			runtimeFail,
		)
	}
	if hasWriteEScaleWarning(
		runtimeOutcome.Warnings,
	) {
		t.Fatalf(
			".aoci运行时资产不得产生E档位告警: %+v",
			runtimeOutcome.Warnings,
		)
	}

	staticOutcome, staticFail := ApplyUpdateEntry(
		root,
		"src/big.go",
		staticBigEntry,
		ledger.SourceHuman,
		true,
	)
	if staticFail != nil {
		t.Fatalf(
			"静态源码预览失败: %+v",
			staticFail,
		)
	}
	if !hasWriteEScaleWarning(
		staticOutcome.Warnings,
	) {
		t.Fatalf(
			"普通静态源码仍应产生E档位告警: %+v",
			staticOutcome.Warnings,
		)
	}
}

// TestAtomicBatchEScalePathBoundary 锁定原子批量通过同一规划内核继承口径。
func TestAtomicBatchEScalePathBoundary(t *testing.T) {
	root := buildWriteEScalePathRepo(t)

	outcome, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path:     ".aoci/ledger.jsonl",
				NewEntry: runtimeLedgerEntry,
			},
			{
				Path:     "src/big.go",
				NewEntry: staticBigEntry,
			},
		},
		ledger.SourceCLIAI,
		true,
	)
	if fail != nil {
		t.Fatalf(
			"原子批量预览失败: %+v",
			fail,
		)
	}
	if outcome == nil ||
		len(outcome.Items) != 2 {
		t.Fatalf(
			"原子批量结果不完整: %+v",
			outcome,
		)
	}
	if hasWriteEScaleWarning(
		outcome.Items[0].Warnings,
	) {
		t.Fatalf(
			"批量中的.aoci资产不得产生E档位告警: %+v",
			outcome.Items[0].Warnings,
		)
	}
	if !hasWriteEScaleWarning(
		outcome.Items[1].Warnings,
	) {
		t.Fatalf(
			"批量中的普通静态源码仍应告警: %+v",
			outcome.Items[1].Warnings,
		)
	}
}
