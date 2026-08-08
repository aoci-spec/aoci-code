// Missing 三分唯一事实源测试。
package indexgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
)

func TestClassifyMissingThreeWayPartition(t *testing.T) {
	cfg := &config.Config{
		CurationExclude: []string{
			"docs",
			"empty-curated.txt",
		},
	}
	inventory := &Inventory{
		Items: []Item{
			{RelPath: "binary.bin", SkipReason: "binary"},
			{RelPath: "empty.txt", SkipReason: "empty"},
			{RelPath: "empty-curated.txt", SkipReason: "empty"},
			{RelPath: "huge.txt", SkipReason: "oversize"},
		},
	}
	raw := []string{
		"src/main.go",
		"huge.txt",
		"docs/guide.md",
		"empty.txt",
		"binary.bin",
		"empty-curated.txt",
		"unknown.txt",
	}

	got := ClassifyMissing(cfg, raw, inventory)

	wantMissing := []string{
		"binary.bin",
		"docs/guide.md",
		"empty-curated.txt",
		"empty.txt",
		"huge.txt",
		"src/main.go",
		"unknown.txt",
	}
	if strings.Join(got.Missing, ",") != strings.Join(wantMissing, ",") {
		t.Fatalf("原始 Missing 排序不符: got=%v want=%v", got.Missing, wantMissing)
	}

	wantActionable := []string{"src/main.go", "unknown.txt"}
	if strings.Join(got.Actionable, ",") != strings.Join(wantActionable, ",") {
		t.Fatalf("Actionable 分类不符: got=%v want=%v", got.Actionable, wantActionable)
	}

	wantCuration := []string{"docs/guide.md", "empty-curated.txt"}
	if strings.Join(got.CurationExcluded, ",") != strings.Join(wantCuration, ",") {
		t.Fatalf("Curation 分类不符: got=%v want=%v", got.CurationExcluded, wantCuration)
	}

	if len(got.Skipped) != 3 {
		t.Fatalf("Skipped 应为3项: %+v", got.Skipped)
	}
	if got.Skipped[0].Path != "binary.bin" ||
		got.Skipped[0].Reason != "binary" ||
		got.Skipped[1].Path != "empty.txt" ||
		got.Skipped[1].Reason != "empty" ||
		got.Skipped[2].Path != "huge.txt" ||
		got.Skipped[2].Reason != "oversize" {
		t.Fatalf("Skipped 内容或顺序不符: %+v", got.Skipped)
	}

	if len(got.Actionable)+len(got.CurationExcluded)+len(got.Skipped) != len(got.Missing) {
		t.Fatalf("Missing 三分数量不守恒: %+v", got)
	}
}

func TestClassifyMissingWithoutInventoryIsConservative(t *testing.T) {
	got := ClassifyMissing(
		&config.Config{},
		[]string{"z.go", "a.go"},
		nil,
	)

	if got.Missing == nil ||
		got.Actionable == nil ||
		got.CurationExcluded == nil ||
		got.Skipped == nil {
		t.Fatalf("所有切片必须非 nil: %+v", got)
	}
	if strings.Join(got.Actionable, ",") != "a.go,z.go" {
		t.Fatalf("画像缺失时必须保守归入 Actionable: %+v", got)
	}
	if len(got.CurationExcluded) != 0 || len(got.Skipped) != 0 {
		t.Fatalf("画像缺失不得臆造排除或跳过: %+v", got)
	}
}

func TestBuildMissingClassificationProfilesOnlyMissing(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("aoci.txt", "#测试\n===代码"+rootSlash+"/===\naoci.txt[XRT9T]: F:- | R:- | A:- | S:-\n")
	write("empty.txt", "")
	write("binary.bin", "abc\x00def")
	write("src/main.go", "package main\n")

	data, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	doc, warnings := index.Parse(string(data))
	if len(warnings) != 0 {
		t.Fatalf("测试索引不应有解析警告: %+v", warnings)
	}

	classification, inventory, err := BuildMissingClassification(
		root,
		&config.Config{},
		doc,
		[]string{"src/main.go", "empty.txt", "binary.bin"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(inventory.Items) != 3 {
		t.Fatalf("只应画像三项原始 Missing: %+v", inventory.Items)
	}
	if len(classification.Actionable) != 1 ||
		classification.Actionable[0] != "src/main.go" {
		t.Fatalf("普通源码应为 Actionable: %+v", classification)
	}
	if len(classification.Skipped) != 2 {
		t.Fatalf("空文件和二进制应进入 Skipped: %+v", classification)
	}
}
