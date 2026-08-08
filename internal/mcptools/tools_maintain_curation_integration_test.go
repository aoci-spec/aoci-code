// handleMaintain 策展排除路径的集成测试(P-19)
// 索引条目: tools_maintain_curation_integration_test.go(待补录,随本批入册)
//
// 夹具自建带 curation 前缀(R42: 不改动被六个既有测试共享的 mkMaintainRepo);
// 结果提取直读 mcp.TextContent(与既有 maintainText 同形态但独立实现,防夹具耦合);
// 建基线经 baseline 包真实 API(Snapshot→NewBaseline→Save,签名经 go doc 机读确认)。
package mcptools

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// mkCurationRepo 造仓: 带最小字典头部的索引(a.go 已收录)+ 磁盘上
// a.go(已收录)/free.go(未收录)/docs/x.md(未收录),curation 写入 config.json。
// 不建基线 —— Missing 判定不依赖基线;需要 Stale 的用例自行调 curationBuildBaseline。
func mkCurationRepo(t *testing.T, curation []string) string {
	t.Helper()
	root := t.TempDir()

	idx := fmt.Sprintf(`#测试仓
#【标签ABCDE】
#A层级: X测试
#B模块: A5T测试
#C重要度: 5常规
#E规模: T微<100
===代码%s/===
a.go[XA5T]: F:已收录 | R:- | A:- | S:-
`, filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(idx), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "free.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "x.md"), []byte("# doc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.CurationExclude = curation
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

// curationBuildBaseline 以 baseline 包真实 API 为当前磁盘态建基线
// (Snapshot→NewBaseline→Save;walk 选项取仓库生效配置,与 handleMaintain 同口径)
func curationBuildBaseline(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	snap, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatalf("磁盘快照失败: %v", err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snap)); err != nil {
		t.Fatalf("基线落盘失败: %v", err)
	}
}

// TestMaintainCurationFiltersMissing 策展命中项不派发，未命中照发。
func TestMaintainCurationFiltersMissing(t *testing.T) {
	root := mkCurationRepo(t, []string{"docs"})
	result := decodeAutoResult(t, handleMaintain(root))
	paths := candidatePaths(result)
	if paths["docs/x.md"] || !paths["free.go"] {
		t.Fatalf("策展过滤候选不符: %+v", result)
	}
}

// TestMaintainCurationEmptyListNoEffect 空清单零影响: docs 项照常派发,无策展信息行
func TestMaintainCurationEmptyListNoEffect(t *testing.T) {
	root := mkCurationRepo(t, nil)
	result := decodeAutoResult(t, handleMaintain(root))
	paths := candidatePaths(result)
	if !paths["docs/x.md"] || !paths["free.go"] {
		t.Fatalf("空策展清单应保留两个候选: %+v", result)
	}
}

// TestMaintainCurationSingleFile 单文件项恰等命中: free.go 不派发,docs/x.md 照常派发。
func TestMaintainCurationSingleFile(t *testing.T) {
	root := mkCurationRepo(t, []string{"free.go"})
	result := decodeAutoResult(t, handleMaintain(root))
	paths := candidatePaths(result)
	if paths["free.go"] || !paths["docs/x.md"] {
		t.Fatalf("单文件策展过滤不符: %+v", result)
	}
}

// TestMaintainCurationStalePriority 条目优先规则: 已收录文件的精确路径命中策展
// 清单,过期后仍应派发[更新]任务 —— 策展过滤仅作用于 Missing。
// 前置状态真实性(R43): 先真实建基线,再改 a.go 内容制造真 Stale。
func TestMaintainCurationStalePriority(t *testing.T) {
	root := mkCurationRepo(t, []string{"a.go", "docs"})
	curationBuildBaseline(t, root)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main // 已变更\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := decodeAutoResult(t, handleMaintain(root))
	paths := candidatePaths(result)
	if !paths["a.go"] || !paths["free.go"] || paths["docs/x.md"] {
		t.Fatalf("条目优先与Missing策展过滤不符: %+v", result)
	}
}
