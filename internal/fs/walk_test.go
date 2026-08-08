// walk.go 的防回归测试
// 索引条目: walk_test.go(待补录)
//
// 立测背景(2026-07-10 httpx 实弹缺陷): fs 包此前零测试,WalkRepo 的内置排除
// 边界("到底内置排除哪些目录")必须由机器断言锁定。Safe Inventory v2 将
// .git/.aoci、秘密、运行目录和生成资产提升为不可静默越过的安全边界；项目
// 配置仍可增加普通治理排除，但不能移除这些硬边界。
package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// mustWrite 在 t.TempDir 造的仓库内写一个文件(自动建父目录),失败即测试失败。
func mustWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("建目录失败 %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败 %s: %v", rel, err)
	}
}

// walkSet 执行 WalkRepo 并把结果转为集合便于断言。
func walkSet(t *testing.T, root string, opt WalkOptions) map[string]bool {
	t.Helper()
	list, err := WalkRepo(root, opt)
	if err != nil {
		t.Fatalf("WalkRepo 失败: %v", err)
	}
	set := make(map[string]bool, len(list))
	for _, rel := range list {
		set[rel] = true
	}
	return set
}

// TestWalkRepoBuiltinExcludes 锁定内置无条件排除的真实边界:
// .git、.aoci 与生成目录在【零配置】下也必须被安全排除。
func TestWalkRepoBuiltinExcludes(t *testing.T) {
	root := t.TempDir()
	// 正常业务文件
	mustWrite(t, root, "src/main.py", "print('ok')\n")
	mustWrite(t, root, "pyproject.toml", "[project]\n")
	// 真实 VCS 内部目录 —— 必须被内置排除(httpx 实弹缺陷靶)
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, ".git/hooks/pre-commit.sample", "#!/bin/sh\n")
	// AOCI 状态目录 —— 必须被内置排除(防自吞,既有语义)
	mustWrite(t, root, ".aoci/config.json", "{}\n")
	// node_modules —— Safe Inventory v2 的生成资产硬边界
	mustWrite(t, root, "node_modules/pkg/index.js", "x\n")

	got := walkSet(t, root, WalkOptions{})

	if !got["src/main.py"] || !got["pyproject.toml"] {
		t.Fatalf("正常文件应在遍历结果中, got=%v", got)
	}
	for rel := range got {
		if rel == ".git/HEAD" || rel == ".git/hooks/pre-commit.sample" {
			t.Fatalf(".git 内文件泄入遍历结果(内置排除失效,httpx 实弹缺陷回归): %s", rel)
		}
		if rel == ".aoci/config.json" {
			t.Fatalf(".aoci 内文件泄入遍历结果(防自吞失效): %s", rel)
		}
	}
	if got["node_modules/pkg/index.js"] {
		t.Fatalf("node_modules 生成资产泄入 Safe Inventory v2")
	}
}

// TestWalkRepoConfiguredExcludes 锁定配置注入的排除语义:
// ExcludeDirs 按基名整棵剪枝;ExcludeFiles 按模式匹配(后缀形态抽查一种,
// MatchExcludePattern 的四种模式形态由下方专项用例覆盖)。
func TestWalkRepoConfiguredExcludes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "src/app.go", "package app\n")
	mustWrite(t, root, "docs/guide.md", "# g\n")
	mustWrite(t, root, "src/readme.md", "# r\n")
	mustWrite(t, root, "src/app.go.backup.20260710", "old\n")

	got := walkSet(t, root, WalkOptions{
		ExcludeDirs:  []string{"docs"},
		ExcludeFiles: []string{"*.md", "*.backup.*"},
	})

	if !got["src/app.go"] {
		t.Fatalf("src/app.go 应在结果中")
	}
	if got["docs/guide.md"] {
		t.Fatalf("docs 目录应被配置排除剪枝")
	}
	if got["src/readme.md"] {
		t.Fatalf("*.md 后缀模式应排除 src/readme.md")
	}
	if got["src/app.go.backup.20260710"] {
		t.Fatalf("*.backup.* 双端模式应排除备份文件")
	}
}

// TestMatchExcludePattern 锁定四种模式形态与无 * 精确匹配的判定语义
// (语义对齐平台 matchExcludePattern,是 R6 三方同步面的一部分)。
func TestMatchExcludePattern(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		pats []string
		want bool
	}{
		{"后缀形态", "a/b.md", []string{"*.md"}, true},
		{"后缀不命中", "a/b.mdx", []string{"*.md"}, false},
		{"前缀形态", "a/backup_x", []string{"backup_*"}, true},
		{"双端包含", "a/f.backup.123", []string{"*.backup.*"}, true},
		{"中置形态", "a/backup_2026.sql", []string{"backup_*.sql"}, true},
		{"中置不命中", "a/backup_2026.txt", []string{"backup_*.sql"}, false},
		{"无星基名精确", "sub/py.typed", []string{"py.typed"}, true},
		{"无星相对路径精确", "sub/only.txt", []string{"sub/only.txt"}, true},
		{"无星不命中", "sub/other.txt", []string{"py.typed"}, false},
		{"空模式忽略", "a/b.go", []string{"", "  "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MatchExcludePattern(c.rel, c.pats); got != c.want {
				t.Fatalf("MatchExcludePattern(%q, %v)=%v, want %v", c.rel, c.pats, got, c.want)
			}
		})
	}
}
