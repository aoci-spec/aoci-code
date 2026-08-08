package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadOnlyDoesNotMaterializeLegacyLocale(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte("# legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != "zh-CN" {
		t.Fatalf("legacy Locale = %q", cfg.Locale)
	}
	if _, err := os.Lstat(FilePath(root)); !os.IsNotExist(err) {
		t.Fatalf("read-only load materialized config: %v", err)
	}

	path := FilePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\"version\":1,\"index_path\":\"aoci.txt\"}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReadOnly(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("read-only load rewrote a pre-Locale config")
	}
}
