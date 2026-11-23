package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Inventory and curation exclusions and installed Host integrations are
// team-owned: the local layer neither changes them nor keeps them on SaveLocal.
func TestLocalLayerCannotOverrideTeamOwnedLists(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(FilePath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	team := `{"version":2,"locale":"en-US","exclude_dirs":["vendor"],"exclude_files":["a.lock"],"exclude_exts":[".bin"],"curation_exclude":["gen/"],"installed_agents":["claude"]}` + "\n"
	local := `{"version":2,"exclude_dirs":["src"],"exclude_files":["main.go"],"exclude_exts":[".go"],"curation_exclude":["internal/"],"installed_agents":["codex"],"code_cognition_parallel_batches":9,"safe_inventory_high_risk_opt_in":[".env"]}` + "\n"
	if err := os.WriteFile(FilePath(root), []byte(team), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalFilePath(root), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2][]string{
		"exclude_dirs":     {cfg.ExcludeDirs, {"vendor"}},
		"exclude_files":    {cfg.ExcludeFiles, {"a.lock"}},
		"exclude_exts":     {cfg.ExcludeExts, {".bin"}},
		"curation_exclude": {cfg.CurationExclude, {"gen/"}},
		"installed_agents": {cfg.InstalledAgents, {"claude"}},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s = %v, want team value %v", name, pair[0], pair[1])
		}
	}
	if err := SaveLocal(root, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"exclude_dirs", "exclude_files", "exclude_exts", "curation_exclude", "installed_agents", "code_cognition_parallel_batches", "safe_inventory_high_risk_opt_in"} {
		if _, kept := saved[key]; kept {
			t.Errorf("SaveLocal kept team-owned key %s", key)
		}
	}
}
