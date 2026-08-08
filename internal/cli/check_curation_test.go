// check对CurationExcludedMissing的不阻断语义测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestCheckCurationMissingDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

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
	if err := os.MkdirAll(
		filepath.Join(root, "docs"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "docs", "x.md"),
		[]byte("# doc\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.CurationExclude = []string{"docs"}
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
		t.Fatalf("测试快照不应产生警告: %v", warnings)
	}
	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	out, err := runCheck(t, root)
	if err != nil {
		t.Fatalf(
			"仅有CurationExcludedMissing时check应exit=0: %v\n%s",
			err,
			out,
		)
	}
	if !strings.Contains(out, "✓ 可提交") {
		t.Fatalf("仅有CurationExcludedMissing时应报可提交:\n%s", out)
	}
	if !strings.Contains(out, "RawMissing 1") {
		t.Fatalf("check应保留原始Missing事实:\n%s", out)
	}
	if !strings.Contains(out, "CurationExcludedMissing 1") {
		t.Fatalf("check应明确展示策展排除数量:\n%s", out)
	}
	if !strings.Contains(out, "策展排除 1 项") ||
		!strings.Contains(out, "不阻断提交") {
		t.Fatalf("check应解释负空间放行语义:\n%s", out)
	}
}

func TestCheckMixedCurationAndActionableMissingBlocks(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

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
	if err := os.MkdirAll(
		filepath.Join(root, "docs"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "docs", "x.md"),
		[]byte("# doc\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, "src"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "src", "main.go"),
		[]byte("package main\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.CurationExclude = []string{"docs"}
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
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

	out, err := runCheck(t, root)
	exitError, ok := err.(*ExitError)
	if !ok || exitError.Code != ExitDrift {
		t.Fatalf(
			"存在ActionableMissing时应ExitDrift: %v\n%s",
			err,
			out,
		)
	}
	if !strings.Contains(out, "RawMissing 2") {
		t.Fatalf(
			"原始事实应包含两个Missing:\n%s",
			out,
		)
	}
	if !strings.Contains(out, "ActionableMissing 1") {
		t.Fatalf(
			"阻断数量应只计算ActionableMissing:\n%s",
			out,
		)
	}
	if !strings.Contains(out, "CurationExcludedMissing 1") {
		t.Fatalf(
			"同时应展示CurationExcludedMissing:\n%s",
			out,
		)
	}
}
