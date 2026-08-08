// A2 技术产物默认排除判决测试。
//
// 判决:
//  1. 新增四目录与 .coverage 文件在 DefaultConfig 中在场(HTTPX 实弹缺口);
//  2. 显式清单冻结语义不变: 已落盘的旧短清单不被新默认静默扩充(A2 已知
//     代价的防回归锁定 —— 若未来有人改 applyFallbacks 做静默合并,本用例红);
//  3. DefaultTechnicalExcludeDirs 与 DefaultConfig.ExcludeDirs 同源一致。
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTechnicalExcludes(t *testing.T) {
	cfg := DefaultConfig()

	dirSet := map[string]bool{}
	for _, d := range cfg.ExcludeDirs {
		dirSet[d] = true
	}
	for _, want := range []string{
		".ruff_cache", ".tox", ".nox", "htmlcov",
	} {
		if !dirSet[want] {
			t.Fatalf("DefaultConfig.ExcludeDirs 缺少技术产物目录 %s", want)
		}
	}

	fileSet := map[string]bool{}
	for _, f := range cfg.ExcludeFiles {
		fileSet[f] = true
	}
	if !fileSet[".coverage"] {
		t.Fatal("DefaultConfig.ExcludeFiles 缺少 .coverage")
	}
}

func TestExplicitExcludeListFreezesDefaults(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(
		filepath.Join(root, ".aoci"), 0o755,
	); err != nil {
		t.Fatal(err)
	}

	// 模拟旧默认值时代落盘的显式短清单
	old := []byte(`{"version":1,"exclude_dirs":["node_modules"]}` + "\n")
	if err := os.WriteFile(
		FilePath(root), old, 0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ExcludeDirs) != 1 || cfg.ExcludeDirs[0] != "node_modules" {
		t.Fatalf("显式清单必须冻结旧值不被新默认静默扩充: %+v",
			cfg.ExcludeDirs)
	}
}

func TestDefaultTechnicalExcludeDirsConsistent(t *testing.T) {
	exported := DefaultTechnicalExcludeDirs()
	inline := DefaultConfig().ExcludeDirs
	if len(exported) != len(inline) {
		t.Fatalf("导出清单与默认清单长度不一致: %d vs %d",
			len(exported), len(inline))
	}
	for i := range exported {
		if exported[i] != inline[i] {
			t.Fatalf("第%d项不一致: %s vs %s",
				i, exported[i], inline[i])
		}
	}
}
