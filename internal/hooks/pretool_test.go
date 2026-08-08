// hook 内核与模板测试
// 索引条目: pretool_test.go[TPT8TS]
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/textassets"
)

// buildRepo 造最小 hook 测试仓库
func buildRepo(t *testing.T, strict bool) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "src"), 0755))
	must(os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("A1"), 0644))
	idx := "===段 " + filepath.ToSlash(root) + "/src/===\na.go[X.Y.5.T]: F:甲 | R:- | A:- | S:约束\n"
	must(os.MkdirAll(filepath.Join(root, ".aoci"), 0755))
	// 测试仓索引沿用 .aoci/index.txt 旧路径(经 config.index_path 兼容,顺带锁定兼容性)
	must(os.WriteFile(filepath.Join(root, ".aoci", "index.txt"), []byte(idx), 0644))
	cfg := config.DefaultConfig()
	cfg.IndexPath = ".aoci/index.txt"
	cfg.HookStrict = strict
	must(config.Save(root, cfg))
	snap, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	must(err)
	must(baseline.Save(root, baseline.NewBaseline(snap)))
	return root
}

// TestPretoolMatrix 硬用例矩阵: 默认不阻断/strict+STALE 阻断/逃逸不阻断/未收录提示
func TestPretoolMatrix(t *testing.T) {
	// 默认: 有条目注入,不阻断
	root := buildRepo(t, false)
	res := HandlePreTool(root, "Edit", "src/a.go")
	if res.Block || !strings.Contains(res.Text, "约束") {
		t.Fatalf("默认模式应注入不阻断: %+v", res)
	}
	// 默认 + STALE: 警告但仍不阻断
	os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("A2"), 0644)
	res = HandlePreTool(root, "Edit", "src/a.go")
	if res.Block || !strings.Contains(res.Text, "STALE") {
		t.Fatalf("默认+STALE 应警告不阻断: %+v", res)
	}
	// strict + STALE: 阻断
	root2 := buildRepo(t, true)
	os.WriteFile(filepath.Join(root2, "src", "a.go"), []byte("A2"), 0644)
	res = HandlePreTool(root2, "Edit", "src/a.go")
	if !res.Block {
		t.Fatal("strict+STALE 必须阻断")
	}
	// strict + 未漂移: 放行
	root3 := buildRepo(t, true)
	res = HandlePreTool(root3, "Edit", "src/a.go")
	if res.Block {
		t.Fatal("strict+未漂移不得阻断")
	}
	// 逃逸路径: 不阻断只说明
	res = HandlePreTool(root, "Edit", "../etc/passwd")
	if res.Block || !strings.Contains(res.Text, "不注入") {
		t.Fatalf("逃逸应说明且不阻断: %+v", res)
	}
	// 未收录: 提示补录
	os.WriteFile(filepath.Join(root, "src", "n.go"), []byte("N"), 0644)
	res = HandlePreTool(root, "Write", "src/n.go")
	if res.Block || !strings.Contains(res.Text, "未收录") {
		t.Fatalf("未收录应提示补录: %+v", res)
	}
	// 环境缺失(未 init 的空仓): 静默放行
	res = HandlePreTool(t.TempDir(), "Edit", "x.go")
	if res.Block {
		t.Fatal("环境缺失必须静默放行(hook 容错纪律)")
	}
	_ = afs.WalkOptions{}
}

// TestTemplatesRenderSafety 全部内嵌模板渲染产物过 safety 闸门
func TestTemplatesRenderSafety(t *testing.T) {
	data := TplData{BinPath: "/usr/local/bin/aoci", RepoRoot: "/repo", ProjectName: "repo", RepoRootSlash: "/repo/"}
	agents := loadAgentsAssetForTest(t)
	minimal, err := textassets.Load(textassets.LegacyLocale, textassets.TemplateMinimalIndex)
	if err != nil {
		t.Fatal(err)
	}
	claude, err := textassets.Load(textassets.LegacyLocale, textassets.TemplateClaudePretool)
	if err != nil {
		t.Fatal(err)
	}
	codex, err := textassets.Load(textassets.LegacyLocale, textassets.TemplateCodexMCPConfig)
	if err != nil {
		t.Fatal(err)
	}
	stubs, err := textassets.Load(textassets.LegacyLocale, textassets.TemplateCodexCursorStubs)
	if err != nil {
		t.Fatal(err)
	}
	for name, tpl := range map[string]string{
		"AGENTS.md.tmpl":              agents,
		"claude-pretool.sh.tmpl":      claude,
		"codex-mcp.toml.tmpl":         codex,
		"codex-cursor-stubs.txt.tmpl": stubs,
		"minimal-index.txt.tmpl":      minimal,
	} {
		if _, err := RenderTemplate(name, tpl, data); err != nil {
			t.Fatalf("模板 %s 渲染或 safety 未通过: %v", name, err)
		}
	}
}

// TestEnsureAgentsBlock 区块外内容一个字节不动 + 幂等
func TestEnsureAgentsBlock(t *testing.T) {
	root := t.TempDir()
	user := "# 用户公约\n手写内容不可动。\n"
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(user), 0644)
	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !strings.HasPrefix(string(data), user) {
		t.Fatal("用户既有内容被改动")
	}
	// 幂等: 第二次应报最新跳过
	msg, _ := EnsureAgentsBlock(root)
	if !strings.Contains(msg, "跳过") {
		t.Fatalf("重复执行应跳过: %q", msg)
	}
}
