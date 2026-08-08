// check对PendingCurationMissing与ActionableMissing的提交裁决测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func buildCheckSkippedRepo(
	t *testing.T,
	withActionable bool,
) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心\n" +
		"#E规模: T微<100\n" +
		"===配置索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(root, "aoci.txt"),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "empty.txt"),
		[]byte{},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if withActionable {
		if err := os.WriteFile(
			filepath.Join(root, "new.go"),
			[]byte("package main\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false

	if err := config.Save(root, cfg); err != nil {
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

func TestCheckPendingCurationMissingBlocks(
	t *testing.T,
) {
	root := buildCheckSkippedRepo(
		t,
		false,
	)

	output, err := runCheck(
		t,
		root,
	)
	exitError, ok := err.(*ExitError)
	if !ok ||
		exitError.Code != ExitDrift {
		t.Fatalf(
			"只有PendingCuration时check也应ExitDrift: %v\n%s",
			err,
			output,
		)
	}

	for _, anchor := range []string{
		"PendingCurationMissing 1 项",
		"RawMissing 1",
		"ActionableMissing 0",
		"SkippedMissing 1(其中PendingCurationMissing 1)",
		"待策展Missing 1 项",
		"阻断提交",
	} {
		if !strings.Contains(
			output,
			anchor,
		) {
			t.Fatalf(
				"check缺少Pending裁决锚点%q:\n%s",
				anchor,
				output,
			)
		}
	}
}

func TestCheckActionableAndPendingBothBlock(
	t *testing.T,
) {
	root := buildCheckSkippedRepo(
		t,
		true,
	)

	output, err := runCheck(
		t,
		root,
	)
	exitError, ok := err.(*ExitError)
	if !ok ||
		exitError.Code != ExitDrift {
		t.Fatalf(
			"Actionable与Pending并存应ExitDrift: %v\n%s",
			err,
			output,
		)
	}

	for _, anchor := range []string{
		"Entries治理: ActionableMissing 1,Stale 0",
		"PendingCurationMissing 1 项",
		"RawMissing 2",
		"ActionableMissing 1",
		"SkippedMissing 1(其中PendingCurationMissing 1)",
	} {
		if !strings.Contains(
			output,
			anchor,
		) {
			t.Fatalf(
				"check混合裁决缺少锚点%q:\n%s",
				anchor,
				output,
			)
		}
	}
}
