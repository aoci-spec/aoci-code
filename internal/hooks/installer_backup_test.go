package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
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

func TestEnsureAgentsBlockWithBackupReportsExactBackupFile(t *testing.T) {
	for _, tc := range []struct {
		locale string
		marker string
	}{
		{textassets.DefaultLocale, "original backed up to "},
		{textassets.LegacyLocale, "原文件已备份至 "},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			previous := textassets.ActiveLocale()
			if err := textassets.SetActiveLocale(tc.locale); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

			// Fresh directory: created with no backup.
			fresh := t.TempDir()
			msg, bak, err := EnsureAgentsBlockWithBackup(fresh)
			if err != nil {
				t.Fatal(err)
			}
			if bak != "" || strings.Contains(msg, ".backup.") {
				t.Fatalf("fresh AGENTS.md must not report a backup: msg=%q bak=%q", msg, bak)
			}

			// Existing AGENTS.md without markers: appended, backup reported.
			root := t.TempDir()
			original := "# Project rules\nKeep this untouched.\n"
			if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			msg, bak, err = EnsureAgentsBlockWithBackup(root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(bak, "AGENTS.md.backup.") || !strings.Contains(msg, tc.marker+bak) {
				t.Fatalf("append must report backup file: msg=%q bak=%q", msg, bak)
			}
			saved, err := os.ReadFile(filepath.Join(root, bak))
			if err != nil || string(saved) != original {
				t.Fatalf("backup content mismatch: err=%v got=%q", err, string(saved))
			}

			// Idempotent re-run: no backup reported.
			msg, bak, err = EnsureAgentsBlockWithBackup(root)
			if err != nil {
				t.Fatal(err)
			}
			if bak != "" || strings.Contains(msg, ".backup.") {
				t.Fatalf("idempotent run must not create a backup: msg=%q bak=%q", msg, bak)
			}

			// Existing AGENTS.md with stale managed block: updated, backup reported.
			stale := "# Header\n\n" + agentsBegin + "\nstale\n" + agentsEnd + "\n"
			if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(stale), 0o644); err != nil {
				t.Fatal(err)
			}
			msg, bak, err = EnsureAgentsBlockWithBackup(root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(bak, "AGENTS.md.backup.") || !strings.Contains(msg, tc.marker+bak) {
				t.Fatalf("update must report backup file: msg=%q bak=%q", msg, bak)
			}
			saved, err = os.ReadFile(filepath.Join(root, bak))
			if err != nil || string(saved) != stale {
				t.Fatalf("updated backup content mismatch: err=%v got=%q", err, string(saved))
			}
		})
	}
}
