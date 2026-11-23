package config

import (
	"os"
	"strings"
	"testing"
)

// index_path is a plain path inside the repository: saving or loading one
// that leaves it is refused (audit G4-01).
func TestIndexPathMustStayInsideTheRepository(t *testing.T) {
	for _, bad := range []string{"../outside.txt", "/etc/aoci.txt", "a/../../b.txt", "./aoci.txt"} {
		root := t.TempDir()
		cfg := DefaultConfig()
		cfg.IndexPath = bad
		if err := Save(root, cfg); err == nil || !strings.Contains(err.Error(), "index_path_invalid") {
			t.Fatalf("%q: save must refuse, got %v", bad, err)
		}
		if err := os.MkdirAll(root+"/.aoci", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(FilePath(root), []byte(`{"index_path":"`+bad+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadReadOnly(root); err == nil {
			t.Fatalf("%q: load must refuse a hand-edited escape", bad)
		}
	}
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.IndexPath = "docs/aoci.txt"
	if err := Save(root, cfg); err != nil {
		t.Fatalf("a nested path inside the repository stays allowed: %v", err)
	}
}
