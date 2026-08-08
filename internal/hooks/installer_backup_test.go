package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupThenWritePreservesDistinctSameSecondPreimages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.txt")
	original := []byte("original\n")
	firstUpdate := []byte("first update\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := BackupThenWrite(path, firstUpdate); err != nil {
		t.Fatal(err)
	}
	if err := BackupThenWrite(path, []byte("second update\n")); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(path + ".backup.*")
	if err != nil || len(backups) != 2 {
		t.Fatalf("同秒不同preimage必须各有内容身份备份: backups=%v err=%v", backups, err)
	}
	seenOriginal := false
	seenFirst := false
	for _, backup := range backups {
		data, readErr := os.ReadFile(backup)
		if readErr != nil {
			t.Fatal(readErr)
		}
		seenOriginal = seenOriginal || string(data) == string(original)
		seenFirst = seenFirst || string(data) == string(firstUpdate)
	}
	if !seenOriginal || !seenFirst {
		t.Fatalf("备份内容不完整: original=%v first=%v", seenOriginal, seenFirst)
	}
}
