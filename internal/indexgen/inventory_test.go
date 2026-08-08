// indexgen 包 inventory 表驱动测试
// 索引条目: inventory_test.go(待补录)
//
// 覆盖面: 全对齐零缺失/新文件差集/二进制标注/空文件标注/超大标注/
// 嵌套目录最长前缀归段/未命中段建议新段/排除模式不入清单/输出稳定排序。
// 全用例 t.TempDir 造仓,索引文本经 index.Parse 真实解析(不 mock 段结构)。
package indexgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// buildTestRepo 造一个最小仓: 写入给定文件与索引文本,返回 root/配置/解析后的索引文档。
// 索引文本的段头目录用 root 拼绝对路径(与真实索引格式一致)。
// 事实源: index.Parse(text)→(*Document,[]Warning),收文本不收路径。
func buildTestRepo(t *testing.T, files map[string]string, indexBody func(root string) string) (string, *config.Config, *index.Document) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatalf("写文件失败: %v", err)
		}
	}
	body := indexBody(root)
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(body), 0644); err != nil {
		t.Fatalf("写索引失败: %v", err)
	}
	doc, warns := index.Parse(body)
	if len(warns) != 0 {
		t.Fatalf("测试索引不应有解析警告,实得: %+v", warns)
	}
	return root, config.DefaultConfig(), doc
}

// TestAllAligned 全对齐: 磁盘文件全部有条目 → 清单为空。
func TestAllAligned(t *testing.T) {
	files := map[string]string{
		"main.go":           "package main\n",
		"internal/cli/a.go": "package cli\n",
	}
	root, cfg, doc := buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return "#====测试索引====\n" +
			"===配置索引" + root + "/===\n" +
			"main.go[CRT9T]: F:入口 | R:- | A:- | S:测试\n" +
			"aoci.txt[AIX9T]: F:索引 | R:- | A:- | S:测试\n" +
			"===CLI子命令" + root + "/internal/cli/===\n" +
			"a.go[CIN9T]: F:命令 | R:- | A:- | S:测试\n"
	})

	inv, err := BuildInventory(root, cfg, doc)
	if err != nil {
		t.Fatalf("BuildInventory 失败: %v", err)
	}
	if len(inv.Items) != 0 {
		t.Errorf("全对齐应零缺失,实得 %d 项: %+v", len(inv.Items), inv.Items)
	}
}

// TestMissingAndProfiles 新文件差集 + 各类画像标注 + 归段建议 + 稳定排序。
func TestMissingAndProfiles(t *testing.T) {
	big := strings.Repeat("x", 1<<20+1) // 超过 1MiB
	files := map[string]string{
		"main.go":               "package main\n",
		"internal/cli/a.go":     "package cli\n",
		"internal/cli/new.go":   "package cli\n// 新文件\nfunc f() {}\n", // 未索引,3行
		"internal/cli/sub/x.go": "package sub\n",                      // 嵌套目录,最长前缀应归 CLI 段
		"docs/spec.txt":         "规范\n",                               // 未命中任何段
		"assets/logo.bin":       "PNG\x00\x00binary",                  // 二进制
		"empty.txt":             "",                                   // 空文件
		"huge.txt":              big,                                  // 超大
	}
	// 索引只含 CLI 段(无根段) → 根/docs/assets 下文件应报"建议新段"
	root, cfg, doc := buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return "#====测试索引====\n" +
			"===CLI子命令" + root + "/internal/cli/===\n" +
			"a.go[CIN9T]: F:命令 | R:- | A:- | S:测试\n"
	})

	inv, err := BuildInventory(root, cfg, doc)
	if err != nil {
		t.Fatalf("BuildInventory 失败: %v", err)
	}

	byPath := map[string]Item{}
	for _, it := range inv.Items {
		byPath[it.RelPath] = it
	}

	// 差集正确性: a.go 已索引不应出现
	if _, ok := byPath["internal/cli/a.go"]; ok {
		t.Error("已索引文件不应入清单")
	}
	// 新文件画像
	if it, ok := byPath["internal/cli/new.go"]; !ok {
		t.Error("缺 internal/cli/new.go")
	} else {
		if it.Lines != 3 || it.Ext != ".go" || it.SkipReason != "" {
			t.Errorf("new.go 画像不符: %+v", it)
		}
		if it.SuggestedSection != "CLI子命令" {
			t.Errorf("new.go 应归 CLI子命令,实得 %s", it.SuggestedSection)
		}
	}
	// 嵌套目录最长前缀归段
	if it := byPath["internal/cli/sub/x.go"]; it.SuggestedSection != "CLI子命令" {
		t.Errorf("sub/x.go 应按前缀归 CLI子命令,实得 %s", it.SuggestedSection)
	}
	// 未命中 → 建议新段
	if it := byPath["docs/spec.txt"]; it.SuggestedSection != "建议新段:docs/" {
		t.Errorf("spec.txt 应建议新段 docs/,实得 %s", it.SuggestedSection)
	}
	// 三类跳过标注
	if it := byPath["assets/logo.bin"]; it.SkipReason != "binary" {
		t.Errorf("logo.bin 应标 binary,实得 %q", it.SkipReason)
	}
	if it := byPath["empty.txt"]; it.SkipReason != "empty" {
		t.Errorf("empty.txt 应标 empty,实得 %q", it.SkipReason)
	}
	if it := byPath["huge.txt"]; it.SkipReason != "oversize" || it.Lines != 0 {
		t.Errorf("huge.txt 应标 oversize 且不读行数,实得 %+v", it)
	}
	// 稳定排序
	for i := 1; i < len(inv.Items); i++ {
		if inv.Items[i-1].RelPath >= inv.Items[i].RelPath {
			t.Errorf("输出未按 RelPath 字典序: %s >= %s", inv.Items[i-1].RelPath, inv.Items[i].RelPath)
		}
	}
}

// TestExcludePatternRespected 排除模式命中的文件不入清单(与 scan 同口径)。
func TestExcludePatternRespected(t *testing.T) {
	files := map[string]string{
		"main.go":                 "package main\n",
		"main.go.backup.20260709": "旧备份\n", // 默认排除 *.backup.*
	}
	root, cfg, doc := buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return "#====测试索引====\n" +
			"===配置索引" + root + "/===\n" +
			"main.go[CRT9T]: F:入口 | R:- | A:- | S:测试\n" +
			"aoci.txt[AIX9T]: F:索引 | R:- | A:- | S:测试\n"
	})

	inv, err := BuildInventory(root, cfg, doc)
	if err != nil {
		t.Fatalf("BuildInventory 失败: %v", err)
	}
	for _, it := range inv.Items {
		if strings.Contains(it.RelPath, ".backup.") {
			t.Errorf("备份文件不应入清单: %s", it.RelPath)
		}
	}
}
