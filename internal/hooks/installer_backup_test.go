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

			// Existing AGENTS.md without markers: appended, backup under .aoci/backups/ with 0600.
			root := t.TempDir()
			original := "# Project rules\nKeep this untouched.\n"
			if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			msg, bak, err = EnsureAgentsBlockWithBackup(root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(bak, ".aoci/backups/AGENTS.md.backup.") || !strings.Contains(msg, tc.marker+bak) {
				t.Fatalf("append must report backup under .aoci/backups/: msg=%q bak=%q", msg, bak)
			}
			if rootBackups, _ := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*")); len(rootBackups) != 0 {
				t.Fatalf("AGENTS.md backup must not be written at repository root: %v", rootBackups)
			}
			bakPath := filepath.Join(root, filepath.FromSlash(bak))
			saved, err := os.ReadFile(bakPath)
			if err != nil || string(saved) != original {
				t.Fatalf("backup content mismatch: err=%v got=%q", err, string(saved))
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(bakPath)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("AGENTS.md backup must be mode 0600: %v %v", info, err)
				}
			}

			// Idempotent re-run: no backup reported.
			msg, bak, err = EnsureAgentsBlockWithBackup(root)
			if err != nil {
				t.Fatal(err)
			}
			if bak != "" || strings.Contains(msg, ".backup.") {
				t.Fatalf("idempotent run must not create a backup: msg=%q bak=%q", msg, bak)
			}

			// Existing AGENTS.md with stale managed block: updated, backup under .aoci/backups/ with 0600.
			stale := "# Header\n\n" + agentsBegin + "\nstale\n" + agentsEnd + "\n"
			if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(stale), 0o644); err != nil {
				t.Fatal(err)
			}
			msg, bak, err = EnsureAgentsBlockWithBackup(root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(bak, ".aoci/backups/AGENTS.md.backup.") || !strings.Contains(msg, tc.marker+bak) {
				t.Fatalf("update must report backup under .aoci/backups/: msg=%q bak=%q", msg, bak)
			}
			if rootBackups, _ := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*")); len(rootBackups) != 0 {
				t.Fatalf("updated AGENTS.md backup must not be written at repository root: %v", rootBackups)
			}
			bakPath = filepath.Join(root, filepath.FromSlash(bak))
			saved, err = os.ReadFile(bakPath)
			if err != nil || string(saved) != stale {
				t.Fatalf("updated backup content mismatch: err=%v got=%q", err, string(saved))
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(bakPath)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("updated AGENTS.md backup must be mode 0600: %v %v", info, err)
				}
			}
		})
	}
}

func TestEnsureAgentsBlockRefusesUnsafeBackupDirWithoutModifyingAgents(t *testing.T) {
	original := "# Existing AGENTS.md\nMust stay byte-for-byte unchanged.\n"

	for _, locale := range []struct {
		code   string
		marker string
	}{
		{textassets.DefaultLocale, "refused to back up or modify AGENTS.md"},
		{textassets.LegacyLocale, "拒绝备份或改写 AGENTS.md"},
	} {
		t.Run(locale.code, func(t *testing.T) {
			previous := textassets.ActiveLocale()
			if err := textassets.SetActiveLocale(locale.code); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

			cases := []struct {
				name       string
				needLink   bool
				wantTarget string
				setup      func(t *testing.T, root, outside string) string
			}{
				{
					name:       "backups_is_regular_file",
					wantTarget: ".aoci/backups",
					setup: func(t *testing.T, root, _ string) string {
						if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(root, ".aoci", "backups"), []byte("blocker\n"), 0o644); err != nil {
							t.Fatal(err)
						}
						return ""
					},
				},
				{
					name:       "backups_symlink_outside_repo",
					needLink:   true,
					wantTarget: ".aoci/backups",
					setup: func(t *testing.T, root, outside string) string {
						if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(outside, filepath.Join(root, ".aoci", "backups")); err != nil {
							t.Fatal(err)
						}
						return outside
					},
				},
				{
					name:       "backups_symlink_inside_repo",
					needLink:   true,
					wantTarget: ".aoci/backups",
					setup: func(t *testing.T, root, _ string) string {
						inside := filepath.Join(root, "elsewhere")
						if err := os.MkdirAll(inside, 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(inside, filepath.Join(root, ".aoci", "backups")); err != nil {
							t.Fatal(err)
						}
						return inside
					},
				},
				{
					name:       "aoci_parent_symlink_outside_repo",
					needLink:   true,
					wantTarget: ".aoci",
					setup: func(t *testing.T, root, outside string) string {
						if err := os.Symlink(outside, filepath.Join(root, ".aoci")); err != nil {
							t.Fatal(err)
						}
						return outside
					},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					if tc.needLink && runtime.GOOS == "windows" {
						t.Skip("symlink creation requires elevated privileges on Windows")
					}
					root := t.TempDir()
					outside := t.TempDir()
					agentsPath := filepath.Join(root, "AGENTS.md")
					if err := os.WriteFile(agentsPath, []byte(original), 0o644); err != nil {
						t.Fatal(err)
					}
					redirectDir := tc.setup(t, root, outside)

					_, bak, err := EnsureAgentsBlockWithBackup(root)
					if err == nil {
						t.Fatalf("expected refusal for %s, got nil (bak=%q)", tc.name, bak)
					}
					if !strings.Contains(err.Error(), locale.marker) || !strings.Contains(err.Error(), tc.wantTarget) {
						t.Fatalf("unexpected refusal message: %v", err)
					}
					after, readErr := os.ReadFile(agentsPath)
					if readErr != nil || string(after) != original {
						t.Fatalf("AGENTS.md was modified on refused backup: err=%v got=%q", readErr, string(after))
					}
					if redirectDir != "" {
						entries, readDirErr := os.ReadDir(redirectDir)
						if readDirErr != nil {
							t.Fatal(readDirErr)
						}
						if len(entries) != 0 {
							t.Fatalf("backup leaked into redirected dir %s: %v", redirectDir, entries)
						}
					}
				})
			}
		})
	}
}
