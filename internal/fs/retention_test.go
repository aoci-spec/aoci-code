package fs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneAgedRecordsRemovesOnlyOldMatchingRegularFiles(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	write := func(name string, aged bool) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if aged {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("scope-old.json", true)
	write("scope-new.json", false)
	write("scope-kept.json", true)
	write("migration-old.json", true)
	write("scope-old.snapshot", true)
	if err := os.Mkdir(filepath.Join(dir, "scope-dir.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	PruneAgedRecords(dir, "scope-", 24*time.Hour, "scope-kept.json")
	PruneAgedRecords(filepath.Join(dir, "absent"), "scope-", 24*time.Hour, "")
	for name, want := range map[string]bool{"scope-old.json": false, "scope-new.json": true, "scope-kept.json": true,
		"migration-old.json": true, "scope-old.snapshot": true, "scope-dir.json": true} {
		if _, err := os.Lstat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s: present=%t, want %t", name, err == nil, want)
		}
	}
}
