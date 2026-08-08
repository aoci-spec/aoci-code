// build --missing 策展排除生产路径测试。
// 本文件锁定两项行为:
//  1. 自动起草队列过滤策展项,但显式目标选择辅助不改变 inventory 顺序;
//  2. 当全部缺口均被策展排除时,命令在构造 AI client 之前成功返回,
//     无需配置模型端点,也不产生草稿。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
)

func TestSelectBuildMissingTargets(t *testing.T) {
	inv := &indexgen.Inventory{Items: []indexgen.Item{
		{RelPath: "docs/a.md"},
		{RelPath: "src/main.go"},
		{RelPath: "empty.txt", SkipReason: "empty"},
		{RelPath: "README.md"},
	}}
	cfg := &config.Config{
		CurationExclude: []string{"docs", "README.md"},
	}

	targets, skipped := selectBuildMissingTargets(inv, cfg)
	if skipped != 2 {
		t.Fatalf("策展跳过应为2,实得 %d", skipped)
	}
	if len(targets) != 1 || targets[0] != "src/main.go" {
		t.Fatalf("实际目标应仅保留 src/main.go 且顺序不变,实得 %v", targets)
	}
}

func TestIndexBuildMissingAllCurationReturnsBeforeAI(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: T测试\n" +
		"#C重要度: 5常规\n" +
		"#E规模: T微<100\n" +
		"===配置索引" + rootSlash + "/===\n" +
		"aoci.txt[XT5T]: F:索引 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(root, "aoci.txt"),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
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

	// AI 保持默认关闭: 若命令在目标过滤前构造 client,本用例会失败。
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, false, false
	t.Cleanup(func() {
		flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet
	})

	cmd := newIndexBuildCmd()
	if err := cmd.Flags().Set("missing", "true"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("全部缺口被策展排除时应在 AI 前成功返回,实得: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "策展排除 1 条未进入 AI 起草队列") {
		t.Fatalf("输出应明确显示策展跳过计数:\n%s", got)
	}
	if !strings.Contains(got, "无待索引文件") {
		t.Fatalf("输出应给出零实际目标结论:\n%s", got)
	}

	if _, err := os.Stat(filepath.Join(root, ".aoci", "drafts")); !os.IsNotExist(err) {
		t.Fatalf("零实际目标不得产生草稿目录,stat err=%v", err)
	}
}
