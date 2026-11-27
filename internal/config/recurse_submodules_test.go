package config

import (
	"os"
	"strings"
	"testing"
)

// #107: the submodule switch is team-owned. Legacy defaults leave it off, so
// an old repository keeps its inventory and identity; the local layer can
// neither turn it on nor off, and SaveLocal never writes it.
func TestRecurseSubmodulesIsTeamOwned(t *testing.T) {
	if DefaultConfig().RecurseSubmodules {
		t.Fatal("legacy defaults turned the submodule switch on")
	}
	root := t.TempDir()
	team := DefaultConfig()
	team.RecurseSubmodules = true
	if err := Save(root, team); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalFilePath(root), []byte(`{"recurse_submodules":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.RecurseSubmodules || !merged.WalkOptions().RecurseSubmodules {
		t.Fatal("local configuration overrode the team submodule switch")
	}
	if err := SaveLocal(root, merged); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "recurse_submodules") {
		t.Fatalf("SaveLocal retained the team-only switch: %s", raw)
	}

	off := DefaultConfig()
	if err := Save(root, off); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalFilePath(root), []byte(`{"recurse_submodules":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if merged.RecurseSubmodules {
		t.Fatal("local configuration turned the team submodule switch on")
	}
}
