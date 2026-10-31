// Verdict tests of the A2 existing-repository exclusion missing-item hint (at the pure-function level of missingTechnicalExcludeDirs).
//
// The verdict:
//  1. an explicit short list → the missing list holds the new default technical artifact items;
//  2. the exclude_dirs key not declared → zero missing (Load fills the defaults; the defence against the misreport a direct comparison would give);
//  3. an explicit list already holding every default item → zero missing;
//  4. config.json missing → zero missing (no duplicate report).
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func writeDoctorHintConfig(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(
		filepath.Join(root, ".aoci"), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		config.FilePath(root), []byte(content), 0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestMissingTechnicalExcludeDirs(t *testing.T) {
	// Verdict 1: an explicit short list → missing items reported
	root := t.TempDir()
	writeDoctorHintConfig(t, root,
		`{"version":1,"exclude_dirs":["node_modules"]}`+"\n")
	missing := missingTechnicalExcludeDirs(root)
	found := map[string]bool{}
	for _, m := range missing {
		found[m] = true
	}
	for _, want := range []string{".ruff_cache", ".tox"} {
		if !found[want] {
			t.Fatalf("显式短清单应报缺项 %s: %v", want, missing)
		}
	}

	// Verdict 2: the key not declared → zero missing (the misreport defence)
	root2 := t.TempDir()
	writeDoctorHintConfig(t, root2, `{"version":1}`+"\n")
	if got := missingTechnicalExcludeDirs(root2); len(got) != 0 {
		t.Fatalf("键未声明时不得报缺项(Load 已回填默认): %v", got)
	}

	// Verdict 3: the explicit list already holds every default item → zero missing
	root3 := t.TempDir()
	cfg := legacyTestConfig()
	if err := os.MkdirAll(
		filepath.Join(root3, ".aoci"), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root3, cfg); err != nil {
		t.Fatal(err)
	}
	if got := missingTechnicalExcludeDirs(root3); len(got) != 0 {
		t.Fatalf("全量默认落盘不得报缺项: %v", got)
	}

	rootNew := t.TempDir()
	newCfg := config.DefaultConfig()
	newCfg.SetNewProjectArtifactExclusions()
	if err := config.Save(rootNew, newCfg); err != nil {
		t.Fatal(err)
	}
	if got := missingTechnicalExcludeDirs(rootNew); len(got) != 0 {
		t.Fatalf("new-project root exclusions must satisfy the doctor hint: %v", got)
	}

	// Verdict 4: config.json missing → zero missing
	root4 := t.TempDir()
	if got := missingTechnicalExcludeDirs(root4); len(got) != 0 {
		t.Fatalf("无配置文件不得报缺项: %v", got)
	}
}
