package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// The viewer observes every repository it lists, including discovered ones;
// a pre-Locale config must come back byte-identical.
func TestSnapshotLeavesLegacyConfigUnchanged(t *testing.T) {
	root := t.TempDir()
	path := config.FilePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\"version\":1,\"index_path\":\"aoci.txt\"}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	buildSnapshot(root, Options{}, nil)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("snapshot rewrote configuration:\n%s", after)
	}
}
