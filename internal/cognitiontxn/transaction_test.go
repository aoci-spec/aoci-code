package cognitiontxn

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestImmutableFallbacksRejectExactSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available to unprivileged Windows tests")
	}
	expected := []byte("exact intent\n")

	t.Run("save", func(t *testing.T) {
		root := t.TempDir()
		referent := filepath.Join(root, "referent.json")
		target := filepath.Join(root, "intent.json")
		if err := os.WriteFile(referent, expected, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(referent, target); err != nil {
			t.Fatal(err)
		}
		if err := SaveImmutable(target, expected); err == nil {
			t.Fatal("SaveImmutable accepted an exact symlink")
		}
	})

	t.Run("archive", func(t *testing.T) {
		root := t.TempDir()
		active := filepath.Join(root, "active.json")
		archive := filepath.Join(root, "archive.json")
		if err := os.WriteFile(active, expected, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(active, archive); err != nil {
			t.Fatal(err)
		}
		if err := ArchiveImmutable(active, archive, expected); err == nil {
			t.Fatal("ArchiveImmutable accepted an exact symlink")
		}
		if info, err := os.Lstat(active); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("ArchiveImmutable removed the active regular file: %v", err)
		}
	})
}
