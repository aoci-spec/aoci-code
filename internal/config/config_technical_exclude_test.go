// Tests of the A2 technical artifact default exclusion verdict.
//
// The verdict:
//  1. the four added directories and the .coverage file are present in DefaultConfig (the HTTPX live-run gap);
//  2. the explicit-list freeze semantics are unchanged: an old short list already on disk is not silently extended by the new defaults (the
//     regression lock of A2's known cost; if anyone changes applyFallbacks to merge silently, this case goes red);
//  3. DefaultTechnicalExcludeDirs and DefaultConfig.ExcludeDirs agree from one source.
package config

import (
	"os"
	"path/filepath"
	"strings"
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

	// Simulate an explicit short list written in the days of the old defaults
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

func TestNewProjectArtifactExclusionsLeaveLegacyDefaultsAlone(t *testing.T) {
	legacy := DefaultConfig()
	if len(legacy.ExcludeRootDirs) != 0 {
		t.Fatalf("legacy defaults gained a root policy: %v", legacy.ExcludeRootDirs)
	}
	newProject := DefaultConfig()
	newProject.SetNewProjectArtifactExclusions()
	root := map[string]bool{}
	for _, dir := range newProject.ExcludeRootDirs {
		root[dir] = true
	}
	for _, name := range []string{"backup", "backups", "build", "cache", "coverage", "dist", "target", "tmp", "uploads", "artifacts", "vendor", "logs", "run"} {
		if !root[name] {
			t.Errorf("new project root policy is missing %s", name)
		}
		for _, anywhere := range newProject.ExcludeDirs {
			if anywhere == name {
				t.Errorf("%s still excludes nested source", name)
			}
		}
	}
	if len(newProject.ExcludeDirs) >= len(legacy.ExcludeDirs) {
		t.Fatalf("the new-project policy must move names out of exclude_dirs: new=%v legacy=%v", newProject.ExcludeDirs, legacy.ExcludeDirs)
	}
}

func TestRootExclusionsAreTeamOwned(t *testing.T) {
	root := t.TempDir()
	base := DefaultConfig()
	base.SetNewProjectArtifactExclusions()
	if err := Save(root, base); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalFilePath(root), []byte(`{"exclude_root_dirs":["local-only"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.ExcludeRootDirs) != len(base.ExcludeRootDirs) || merged.ExcludeRootDirs[0] != base.ExcludeRootDirs[0] {
		t.Fatalf("local configuration overrode team root exclusions: %v", merged.ExcludeRootDirs)
	}
	if err := SaveLocal(root, merged); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "exclude_root_dirs") {
		t.Fatalf("SaveLocal retained the team-only root policy: %s", raw)
	}
}
