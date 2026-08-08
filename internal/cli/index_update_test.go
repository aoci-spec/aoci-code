// aoci index update 命令测试(dry-run 纯离线路径,不触碰 AI 层)。
//
// 覆盖:
//   - Stale→changed / Missing→new;
//   - 索引自身 Stale 单独提示,不进 changed;
//   - 零漂移早退;
//   - curation_exclude 只过滤 Missing,单列报告且不计入 ledger PathsCount;
//   - 命中策展清单的已收录文件若 Stale,仍按条目优先进入 changed。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// buildUpdateRepo 造最小全对齐仓库。
func buildUpdateRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

	if err := os.WriteFile(
		filepath.Join(root, "f.go"),
		[]byte("package f\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	indexText := "#头部第一行\n#头部第二行\n" +
		"===段" + rootSlash + "/===\n" +
		"f.go[XC5T]: F:x | R:- | A:- | S:-\n" +
		"aoci.txt[XC9T]: F:索引本体 | R:- | A:- | S:-\n"
	if err := os.WriteFile(
		filepath.Join(root, "aoci.txt"),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, warnings, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatalf("快照失败: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("快照不应有警告: %v", warnings)
	}
	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatalf("建线失败: %v", err)
	}
	return root
}

// runUpdateDry 执行 `update --dry-run`。
func runUpdateDry(t *testing.T, root string) (string, error) {
	t.Helper()
	oldRepo := flagRepo
	flagRepo = root
	t.Cleanup(func() { flagRepo = oldRepo })

	cmd := newIndexUpdateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--dry-run"})
	err := cmd.Execute()
	return out.String(), err
}

// assertUpdateLedger 断言最近的 index_update 事件 PathsCount。
func assertUpdateLedger(t *testing.T, root string, wantPaths int) {
	t.Helper()
	events, _ := ledger.Recent(root, 10)
	for _, event := range events {
		if event.Op == "index_update" && event.Source == ledger.SourceHuman {
			if event.PathsCount != wantPaths {
				t.Fatalf(
					"index_update PathsCount 应为 %d,得到 %+v",
					wantPaths,
					event,
				)
			}
			return
		}
	}
	t.Fatalf("ledger 未见 index_update 事件: %+v", events)
}

func TestUpdateDryRunClassification(t *testing.T) {
	root := buildUpdateRepo(t)
	if err := os.WriteFile(
		filepath.Join(root, "f.go"),
		[]byte("package f\n// 已修改\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "new.go"),
		[]byte("package n\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	out, err := runUpdateDry(t, root)
	if err != nil {
		t.Fatalf("dry-run 应成功: %v\n%s", err, out)
	}
	if !strings.Contains(out, "changed(待更新):") ||
		!strings.Contains(out, "f.go") {
		t.Fatalf("changed 分类缺失: %s", out)
	}
	if !strings.Contains(out, "new(待新增):") ||
		!strings.Contains(out, "new.go") {
		t.Fatalf("new 分类缺失: %s", out)
	}
	if !strings.Contains(out, "未调用端点") {
		t.Fatalf("dry-run 应声明未调用端点: %s", out)
	}
	if !strings.Contains(out, "changed 1 / new 1") {
		t.Fatalf("候选目标计数不符: %s", out)
	}
	assertUpdateLedger(t, root, 2)
}

func TestUpdateIndexSelfStaleExcluded(t *testing.T) {
	root := buildUpdateRepo(t)
	file, err := os.OpenFile(
		filepath.Join(root, "aoci.txt"),
		os.O_APPEND|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("#建线后追加的注释\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	out, err := runUpdateDry(t, root)
	if err != nil {
		t.Fatalf("dry-run 应成功: %v\n%s", err, out)
	}
	if !strings.Contains(out, "索引自身漂移: aoci.txt") {
		t.Fatalf("应有索引自身漂移提示: %s", out)
	}
	if !strings.Contains(out, "changed(待更新):") ||
		!strings.Contains(out, "无可执行 changed/new 目标") {
		t.Fatalf("索引自身不得进入起草目标: %s", out)
	}
	assertUpdateLedger(t, root, 0)
}

func TestUpdateZeroDriftEarlyExit(t *testing.T) {
	root := buildUpdateRepo(t)
	out, err := runUpdateDry(t, root)
	if err != nil {
		t.Fatalf("dry-run 应成功: %v\n%s", err, out)
	}
	if !strings.Contains(out, "无可执行 changed/new 目标") {
		t.Fatalf("全对齐仓应报无需起草: %s", out)
	}
	assertUpdateLedger(t, root, 0)
}

func TestUpdateCurationFiltersMissing(t *testing.T) {
	root := buildUpdateRepo(t)
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
	if err := os.WriteFile(
		filepath.Join(root, "new.go"),
		[]byte("package n\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CurationExclude = []string{"docs"}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	out, err := runUpdateDry(t, root)
	if err != nil {
		t.Fatalf("dry-run 应成功: %v\n%s", err, out)
	}
	if !strings.Contains(out, "new(待新增):") ||
		!strings.Contains(out, "new.go") {
		t.Fatalf("普通 Missing 应进入 new: %s", out)
	}
	if !strings.Contains(out, "curation_excluded(不派发):") ||
		!strings.Contains(out, "docs/x.md") {
		t.Fatalf("策展 Missing 应单列不派发: %s", out)
	}
	if !strings.Contains(
		out,
		"可执行目标 1 个(changed 0 / new 1),策展排除 1 个",
	) {
		t.Fatalf("行动计数不符: %s", out)
	}
	assertUpdateLedger(t, root, 1)
}

func TestUpdateCurationDoesNotFilterChanged(t *testing.T) {
	root := buildUpdateRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CurationExclude = []string{"f.go"}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "f.go"),
		[]byte("package f\n// changed\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	out, err := runUpdateDry(t, root)
	if err != nil {
		t.Fatalf("dry-run 应成功: %v\n%s", err, out)
	}
	if !strings.Contains(out, "changed(待更新):") ||
		!strings.Contains(out, "f.go") {
		t.Fatalf("已收录文件命中策展后仍应派发 changed: %s", out)
	}
	if !strings.Contains(out, "changed 1 / new 0") {
		t.Fatalf("changed 行动计数不符: %s", out)
	}
	assertUpdateLedger(t, root, 1)
}

func TestClassifyUpdateTargetsPartition(t *testing.T) {
	cfg := &config.Config{
		IndexPath:       "aoci.txt",
		CurationExclude: []string{"docs"},
	}
	result := &baseline.DetectResult{
		Stale:   []string{"aoci.txt", "src/old.go"},
		Missing: []string{"docs/x.md", "src/new.go"},
	}
	classified := classifyUpdateTargets(cfg, result)

	if !classified.IndexSelfStale {
		t.Fatal("索引自身 Stale 应被单独标记")
	}
	if len(classified.Changed) != 1 ||
		classified.Changed[0] != "src/old.go" {
		t.Fatalf("changed 分类不符: %+v", classified)
	}
	if len(classified.NewFiles) != 1 ||
		classified.NewFiles[0] != "src/new.go" {
		t.Fatalf("new 分类不符: %+v", classified)
	}
	if len(classified.CurationExcludedNew) != 1 ||
		classified.CurationExcludedNew[0] != "docs/x.md" {
		t.Fatalf("策展分类不符: %+v", classified)
	}
}
