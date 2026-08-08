// build --missing 对确定性跳过的生产路径测试。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
)

func TestSelectBuildMissingTargetsDetailedThreeWay(t *testing.T) {
	inventory := &indexgen.Inventory{
		Items: []indexgen.Item{
			{RelPath: "docs/a.md"},
			{RelPath: "empty.txt", SkipReason: "empty"},
			{RelPath: "src/main.go"},
		},
	}
	cfg := &config.Config{
		CurationExclude: []string{"docs"},
	}

	targets, curationCount, skipped :=
		selectBuildMissingTargetsDetailed(
			inventory,
			cfg,
		)

	if len(targets) != 1 ||
		targets[0] != "src/main.go" ||
		curationCount != 1 ||
		len(skipped) != 1 ||
		skipped[0].Path != "empty.txt" ||
		skipped[0].Reason != "empty" {
		t.Fatalf(
			"build Missing三分不符: targets=%v curation=%d skipped=%+v",
			targets,
			curationCount,
			skipped,
		)
	}
}

func TestIndexBuildMissingAllSkippedReturnsBeforeAI(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心\n" +
		"#E规模: T微<100\n" +
		"===配置索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"

	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.AI.Enabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, false, false
	t.Cleanup(func() {
		flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet
	})

	command := newIndexBuildCmd()
	if err := command.Flags().Set("missing", "true"); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)

	if err := command.RunE(command, nil); err != nil {
		t.Fatalf("全部为Skipped时应在AI前成功返回: %v\n%s", err, output.String())
	}

	if !strings.Contains(output.String(), "确定性跳过 1 条") ||
		!strings.Contains(output.String(), "ActionableMissing=0") {
		t.Fatalf("build应明确说明确定性跳过:\n%s", output.String())
	}

	runIDs, err := draft.ListRunIDs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(runIDs) != 0 {
		t.Fatalf("零Actionable目标不得产生草稿: %+v", runIDs)
	}
}
