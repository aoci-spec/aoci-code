// Entries Check对共享E规模路径判据的消费者级测试。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

const (
	checkIndexEntry   = "aoci.txt[XCR7M]: F:索引本体 | R:- | A:- | S:测试"
	checkRuntimeEntry = "ledger.jsonl[ALG7M]: F:台账 | R:- | A:- | S:测试"
	checkStaticEntry  = "big.go[XCR7M]: F:源码 | R:- | A:- | S:测试"
)

// buildEntriesCheckEScalePathRepo构造索引本体、运行时资产和静态源码草稿。
func buildEntriesCheckEScalePathRepo(
	t *testing.T,
) (string, string) {
	t.Helper()

	root := t.TempDir()

	for _, directory := range []string{
		".aoci",
		"src",
	} {
		if err := os.MkdirAll(
			filepath.Join(
				root,
				directory,
			),
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
		"#C重要度: 7业务\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		strings.Repeat(
			"#索引规模填充\n",
			401,
		) +
		"===配置 " +
		rootSlash +
		"/===\n" +
		checkIndexEntry +
		"\n" +
		"===治理 " +
		rootSlash +
		"/.aoci/===\n" +
		checkRuntimeEntry +
		"\n" +
		"===源码 " +
		rootSlash +
		"/src/===\n" +
		checkStaticEntry +
		"\n"

	if err := os.WriteFile(
		filepath.Join(
			root,
			"aoci.txt",
		),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	runID := "20260718T010000Z"

	statuses := []draft.EntryStatus{
		{
			Path:   "aoci.txt",
			Status: "drafted",
		},
		{
			Path:   ".aoci/ledger.jsonl",
			Status: "drafted",
		},
		{
			Path:   "src/big.go",
			Status: "drafted",
		},
	}

	for _, item := range []struct {
		rel  string
		line string
	}{
		{
			rel:  "aoci.txt",
			line: checkIndexEntry,
		},
		{
			rel:  ".aoci/ledger.jsonl",
			line: checkRuntimeEntry,
		},
		{
			rel:  "src/big.go",
			line: checkStaticEntry,
		},
	} {
		if err := draft.WriteFile(
			root,
			runID,
			entryDraftFileName(
				item.rel,
			),
			[]byte(item.line+"\n"),
		); err != nil {
			t.Fatal(err)
		}
	}

	if err := draft.SaveManifest(
		root,
		&draft.Manifest{
			RunID:   runID,
			Kind:    draft.KindEntries,
			Entries: statuses,
		},
	); err != nil {
		t.Fatal(err)
	}

	return root, runID
}

// TestEntriesCheckEScalePathBoundary锁定三个消费者路径边界。
func TestEntriesCheckEScalePathBoundary(
	t *testing.T,
) {
	root, runID :=
		buildEntriesCheckEScalePathRepo(t)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer

	result, err := runEntriesCheckCore(
		root,
		runID,
		cfg,
		loadEntriesCheckCoreDoc(
			t,
			root,
		),
		&output,
		ledger.SourceHuman,
	)
	if err != nil {
		t.Fatalf(
			"Entries Check应成功: %v\n%s",
			err,
			output.String(),
		)
	}

	if result == nil {
		t.Fatal(
			"Entries Check结果为空",
		)
	}

	if result.Review.Passed != 3 ||
		result.Review.Warned != 1 ||
		result.Review.Rejected != 0 ||
		result.Review.Skipped != 0 {
		t.Fatalf(
			"审阅摘要不符: %+v",
			result.Review,
		)
	}

	text := output.String()

	for _, excluded := range []string{
		"aoci.txt",
		".aoci/ledger.jsonl",
	} {
		if strings.Contains(
			text,
			"⚠ "+excluded,
		) ||
			strings.Contains(
				text,
				excluded+
					" —— E规模档位错配",
			) {
			t.Fatalf(
				"AOCI治理资产不得产生E档位告警%s:\n%s",
				excluded,
				text,
			)
		}

		if !strings.Contains(
			text,
			"✓ "+excluded,
		) {
			t.Fatalf(
				"AOCI治理资产应作为无警告通过项展示%s:\n%s",
				excluded,
				text,
			)
		}
	}

	if !strings.Contains(
		text,
		"⚠ src/big.go",
	) ||
		!strings.Contains(
			text,
			"E规模档位错配",
		) {
		t.Fatalf(
			"普通静态源码仍应产生E档位告警:\n%s",
			text,
		)
	}
}
