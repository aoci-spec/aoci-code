// Hook kernel and template tests
// Index entry: pretool_test.go[TPT8TS]
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

// buildRepo builds a minimal hook test repository
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
	// The test repository's index keeps the old .aoci/index.txt path (through the config.index_path compatibility, which this also locks)
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

// TestPretoolMatrix, the hard case matrix: default does not block / strict+STALE blocks / an escape does not block / unrecorded prompts
func TestPretoolMatrix(t *testing.T) {
	// Default: an entry is injected, no block
	root := buildRepo(t, false)
	res := HandlePreTool(root, "Edit", "src/a.go")
	if res.Block || !strings.Contains(res.Text, "约束") {
		t.Fatalf("默认模式应注入不阻断: %+v", res)
	}
	// Default + STALE: a warning but still no block
	os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("A2"), 0644)
	res = HandlePreTool(root, "Edit", "src/a.go")
	if res.Block || !strings.Contains(res.Text, "STALE") {
		t.Fatalf("默认+STALE 应警告不阻断: %+v", res)
	}
	// strict + STALE: block
	root2 := buildRepo(t, true)
	os.WriteFile(filepath.Join(root2, "src", "a.go"), []byte("A2"), 0644)
	res = HandlePreTool(root2, "Edit", "src/a.go")
	if !res.Block {
		t.Fatal("strict+STALE 必须阻断")
	}
	// strict + no drift: pass
	root3 := buildRepo(t, true)
	res = HandlePreTool(root3, "Edit", "src/a.go")
	if res.Block {
		t.Fatal("strict+未漂移不得阻断")
	}
	// An escaping path: no block, only an explanation
	res = HandlePreTool(root, "Edit", "../etc/passwd")
	if res.Block || !strings.Contains(res.Text, "不注入") {
		t.Fatalf("逃逸应说明且不阻断: %+v", res)
	}
	// Unrecorded: a prompt to record
	os.WriteFile(filepath.Join(root, "src", "n.go"), []byte("N"), 0644)
	res = HandlePreTool(root, "Write", "src/n.go")
	if res.Block || !strings.Contains(res.Text, "未收录") {
		t.Fatalf("未收录应提示补录: %+v", res)
	}
	// Missing environment (an empty repository without init): pass silently
	res = HandlePreTool(t.TempDir(), "Edit", "x.go")
	if res.Block {
		t.Fatal("环境缺失必须静默放行(hook 容错纪律)")
	}
	_ = afs.WalkOptions{}
}

// TestTemplatesRenderSafety: every embedded template's rendering passes the safety gate
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

// TestEnsureAgentsBlock: content outside the block keeps every byte, plus idempotence
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
	// Idempotence: the second run should report up to date and skipped
	msg, _ := EnsureAgentsBlock(root)
	if !strings.Contains(msg, "跳过") {
		t.Fatalf("重复执行应跳过: %q", msg)
	}
}
