// Regression tests for walk.go
// Index entry: walk_test.go (pending)
//
// Why these tests exist (the 2026-07-10 httpx live defect): the fs package had no tests, and the built-in exclusion
// boundary of WalkRepo ("which directories are excluded by default") must be locked by machine assertion. Safe
// Inventory v2 raised .git/.aoci, secrets, runtime directories, and generated assets to safety boundaries that cannot
// be crossed silently; project configuration may still add ordinary governance exclusions but cannot remove these hard boundaries.
package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// mustWrite writes one file inside a repository built in t.TempDir (creating parent directories); a failure fails the test.
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

// walkSet runs WalkRepo and turns the result into a set for assertions.
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

// TestWalkRepoBuiltinExcludes locks the real boundary of the unconditional built-in exclusions:
// .git, .aoci, and generated directories must be excluded safely even with zero configuration.
func TestWalkRepoBuiltinExcludes(t *testing.T) {
	root := t.TempDir()
	// Ordinary business files
	mustWrite(t, root, "src/main.py", "print('ok')\n")
	mustWrite(t, root, "pyproject.toml", "[project]\n")
	// A real VCS internal directory, which the built-in exclusion must catch (the httpx live-defect target)
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, ".git/hooks/pre-commit.sample", "#!/bin/sh\n")
	// The AOCI state directory, which the built-in exclusion must catch (against self-ingestion, existing semantics)
	mustWrite(t, root, ".aoci/config.json", "{}\n")
	// node_modules, Safe Inventory v2's generated-asset hard boundary
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

// TestWalkRepoConfiguredExcludes locks the exclusion semantics injected by configuration:
// ExcludeDirs prunes the whole tree by base name; ExcludeFiles matches by pattern (one suffix form is spot-checked here,
// the four pattern forms of MatchExcludePattern have their own case below).
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

// TestMatchExcludePattern locks the four pattern forms and the exact match without * (the semantics match the
// platform's matchExcludePattern, part of the R6 three-way sync surface).
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
