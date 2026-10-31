// Table-driven tests of the indexgen package inventory
// Index entry: inventory_test.go (to be recorded)
//
// Coverage: fully aligned with zero missing / the new-file difference / binary marking / empty-file marking / oversize marking /
// longest-prefix section assignment of nested directories / a new section suggested when no section hits / excluded patterns stay out of the inventory / stable output order.
// Every case builds a repository in t.TempDir, and the index text is really parsed by index.Parse (no mocked section structure).
package indexgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// buildTestRepo builds a minimal repository: it writes the given files and index text and returns the root, the configuration, and the parsed index document.
// The section header directories of the index text are absolute paths built from root (matching the real index format).
// Source of truth: index.Parse(text)→(*Document,[]Warning), which takes text, not a path.
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

// TestAllAligned, fully aligned: every file on disk has an entry → the inventory is empty.
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

// TestMissingAndProfiles: the new-file difference + every profile marking + section suggestion + stable order.
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
	// The index holds only the CLI section (no root section) → files under root/docs/assets should report "suggest a new section"
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

	// Difference correctness: the indexed a.go must not appear
	if _, ok := byPath["internal/cli/a.go"]; ok {
		t.Error("已索引文件不应入清单")
	}
	// New-file profiles
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
	// Longest-prefix section assignment of nested directories
	if it := byPath["internal/cli/sub/x.go"]; it.SuggestedSection != "CLI子命令" {
		t.Errorf("sub/x.go 应按前缀归 CLI子命令,实得 %s", it.SuggestedSection)
	}
	// No hit → suggest a new section
	if it := byPath["docs/spec.txt"]; it.SuggestedSection != "建议新段:docs/" {
		t.Errorf("spec.txt 应建议新段 docs/,实得 %s", it.SuggestedSection)
	}
	// The three skip markings
	if it := byPath["assets/logo.bin"]; it.SkipReason != "binary" {
		t.Errorf("logo.bin 应标 binary,实得 %q", it.SkipReason)
	}
	if it := byPath["empty.txt"]; it.SkipReason != "empty" {
		t.Errorf("empty.txt 应标 empty,实得 %q", it.SkipReason)
	}
	if it := byPath["huge.txt"]; it.SkipReason != "oversize" || it.Lines != 0 {
		t.Errorf("huge.txt 应标 oversize 且不读行数,实得 %+v", it)
	}
	// Stable order
	for i := 1; i < len(inv.Items); i++ {
		if inv.Items[i-1].RelPath >= inv.Items[i].RelPath {
			t.Errorf("输出未按 RelPath 字典序: %s >= %s", inv.Items[i-1].RelPath, inv.Items[i].RelPath)
		}
	}
}

// TestExcludePatternRespected: a file hit by an exclusion pattern stays out of the inventory (the same reading as scan).
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
