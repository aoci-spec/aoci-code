package hooks

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestBackupThenWriteKeepsHostConfigurationBackupsPrivate(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			original := []byte("token = \"synthetic-config-value\"\n")
			if err := os.WriteFile(path, original, mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			if err := BackupThenWrite(path, []byte("updated\n")); err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(path + ".backup.*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups=%v err=%v", backups, err)
			}
			data, err := os.ReadFile(backups[0])
			if err != nil || string(data) != string(original) {
				t.Fatalf("backup lost: %v", err)
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(backups[0])
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("backup must be owner-only: %v", info)
				}
				info, err = os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					t.Fatalf("original mode changed: %v", info)
				}
			}
		})
	}
}
