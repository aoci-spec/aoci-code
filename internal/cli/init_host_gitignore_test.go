// The host configuration init writes carries local absolute paths; once committed, someone else's init judges only by whether the key exists,
// returns early as idempotent, and silently spins. The harder constraint is timing: the roles of the Managed Scope are fixed at the first
// scan, after which scan --force cannot advance them, and removal goes through the coverage-reduction Scope Change approval; so
// the ignore must be written by init before scan. Locked here: the write rules and that timing.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/testkit"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	root := testkit.GitRepo(t)
	testkit.WriteFile(t, root, "src/a.go", "package src\n\nfunc A() int { return 1 }\n")
	return root
}

func readGitignore(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func TestInitIgnoresTheHostConfigItJustWrote(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	ignore := readGitignore(t, root)
	if !strings.Contains(ignore, hostConfigIgnoreMarker) {
		t.Fatalf("init must open a marked block it owns:\n%s", ignore)
	}
	if !strings.Contains(ignore, ".codex/config.toml") {
		t.Fatalf("the written host config must be ignored:\n%s", ignore)
	}

	// The timing is the point: after scan it must not be a managed object.
	var out, errOut bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); code != ExitOK {
		t.Fatalf("scan failed: %d %s %s", code, out.String(), errOut.String())
	}
	var baselineDoc struct {
		Files map[string]struct {
			Role string `json:"role"`
		} `json:"files"`
	}
	raw, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &baselineDoc); err != nil {
		t.Fatal(err)
	}
	if _, present := baselineDoc.Files[".codex/config.toml"]; present {
		t.Fatal("a machine-bound host config must never enter Managed Scope")
	}
	if role := baselineDoc.Files[".gitignore"].Role; role != "index" {
		t.Fatalf(".gitignore is ordinary tracked project content, got role %q", role)
	}
}

func TestInitAppendsToAnExistingGitignoreWithoutTouchingIt(t *testing.T) {
	root := initGitRepo(t)
	original := "# maintainer owned\nbuild/\n*.log\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	ignore := readGitignore(t, root)
	if !strings.HasPrefix(ignore, original) {
		t.Fatalf("maintainer content must be preserved byte-for-byte:\n%s", ignore)
	}
	if !strings.Contains(ignore, ".codex/config.toml") {
		t.Fatalf("the host config must still be appended:\n%s", ignore)
	}
}

func TestInitNeverOverridesAnAlreadyTrackedHostConfig(t *testing.T) {
	root := initGitRepo(t)
	// The maintainer has explicitly decided to commit this file: init does not change their mind.
	if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex", "config.toml"), []byte("# theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, root, "add", ".codex/config.toml")
	testkit.Git(t, root, "commit", "-qm", "keep")
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readGitignore(t, root), ".codex/config.toml") {
		t.Fatal("a tracked path must not be ignored behind the maintainer's back")
	}
}

func TestInitHostGitignoreIsIdempotentAndRespectsExistingCoverage(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	first := readGitignore(t, root)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if second := readGitignore(t, root); second != first {
		t.Fatalf("a repeated init must not rewrite the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	// Counted as exact lines, not as a substring: the host block also carries a
	// distinct `.codex/config.toml.backup.*` pattern, which contains the path but
	// is not a duplicate entry for it. The property under test is that one path
	// is never written twice.
	exact := 0
	for _, line := range strings.Split(first, "\n") {
		if strings.TrimSpace(line) == ".codex/config.toml" {
			exact++
		}
	}
	if exact != 1 {
		t.Fatalf("the path must appear exactly once, got %d:\n%s", exact, first)
	}
	if !strings.Contains(first, ".codex/config.toml.backup.*") {
		t.Fatalf("the host block does not cover the machine-bound backup the installer writes:\n%s", first)
	}

	// An existing cover in directory form counts too, and is not written again.
	other := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(other, ".gitignore"), []byte(".codex/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, other, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if ignore := readGitignore(t, other); strings.Contains(ignore, ".codex/config.toml") {
		t.Fatalf("an existing directory rule already covers it:\n%s", ignore)
	}
}

func TestInitWithoutAgentWritesNoHostIgnore(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if ignore := readGitignore(t, root); strings.Contains(ignore, hostConfigIgnoreMarker) {
		t.Fatalf("no host configuration was written, so nothing may be ignored:\n%s", ignore)
	}
}

func TestInitAgentsBackupStoredUnderAOCIAndUnstageable(t *testing.T) {
	for _, tc := range []struct {
		locale string
		marker string
	}{
		{"en-US", "original backed up to "},
		{"zh-CN", "原文件已备份至 "},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			root := initGitRepo(t)
			original := "# Maintainer AGENTS rules\nKeep this section.\n"
			testkit.WriteFile(t, root, "AGENTS.md", original)
			testkit.Git(t, root, "add", "AGENTS.md")
			testkit.Git(t, root, "commit", "-qm", "add existing AGENTS.md")

			printed, _, stderr, code := runCLIPrinted(t, root, "init", "--locale", tc.locale, "--agent=", "--hooks=false", "--here=false")
			if code != ExitOK {
				t.Fatalf("init failed: code=%d stderr=%s", code, stderr)
			}

			if rootBackups, _ := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*")); len(rootBackups) != 0 {
				t.Fatalf("AGENTS.md backup must not be created at repo root: %v", rootBackups)
			}
			backups, err := filepath.Glob(filepath.Join(root, ".aoci", "backups", "AGENTS.md.backup.*"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("expected one backup under .aoci/backups/, got %v (err=%v)", backups, err)
			}
			backupRel := ".aoci/backups/" + filepath.Base(backups[0])
			if saved, readErr := os.ReadFile(backups[0]); readErr != nil || string(saved) != original {
				t.Fatalf("backup content mismatch: err=%v got=%q", readErr, string(saved))
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(backups[0])
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("backup mode = %v (err=%v), want 0600", info, err)
				}
			}
			if !strings.Contains(printed, tc.marker+backupRel) {
				t.Fatalf("init output must report backup path %q:\n%s", backupRel, printed)
			}
			if ignore := readGitignore(t, root); ignore != "" {
				t.Fatalf("init without --agent must not create or alter team .gitignore:\n%s", ignore)
			}
			if ignored, rule := afs.PathIgnoredByGit(root, backupRel); !ignored {
				t.Fatalf("backup %q must be ignored by .aoci/.gitignore: ignored=%t rule=%q", backupRel, ignored, rule)
			}
			if status := testkit.Git(t, root, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "AGENTS.md.backup.") {
				t.Fatalf("backup must not be stageable in git status:\n%s", status)
			}

			var out, errOut bytes.Buffer
			if scanCode := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); scanCode != ExitOK {
				t.Fatalf("scan failed: %d %s %s", scanCode, out.String(), errOut.String())
			}
			state, exists, err := baseline.Load(root)
			if err != nil || !exists {
				t.Fatalf("load baseline: exists=%t err=%v", exists, err)
			}
			if _, present := state.Files[backupRel]; present {
				t.Fatalf("AGENTS.md backup %q must not enter Baseline", backupRel)
			}
			if state.Files["AGENTS.md"].Role != machinecontract.ScopeRoleIndex {
				t.Fatalf("AGENTS.md role = %q, want index", state.Files["AGENTS.md"].Role)
			}
		})
	}
}

func TestInitAgentsBackupProtectedAcrossScannedRepoRetryAndNoGitCustomExclude(t *testing.T) {
	t.Run("existing_scanned_repo", func(t *testing.T) {
		root := initGitRepo(t)
		teamIgnore := "# team-owned\nbuild/\n"
		testkit.WriteFile(t, root, ".gitignore", teamIgnore)
		testkit.Git(t, root, "add", ".gitignore", "src/a.go")
		testkit.Git(t, root, "commit", "-qm", "seed repo")

		if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); code != ExitOK {
			t.Fatalf("initial scan failed: %d %s %s", code, out.String(), errOut.String())
		}

		// Record initial policy identity and switch AGENTS.md to observe role,
		// then make AGENTS.md's managed block stale so re-init rewrites it.
		agentsPath := filepath.Join(root, "AGENTS.md")
		currentAgents, err := os.ReadFile(agentsPath)
		if err != nil {
			t.Fatal(err)
		}
		staleAgents := strings.Replace(string(currentAgents), "<!-- aoci:end -->", "# stale\n<!-- aoci:end -->", 1)
		if err := os.WriteFile(agentsPath, []byte(staleAgents), 0o644); err != nil {
			t.Fatal(err)
		}
		beforeBaseline, exists, err := baseline.Load(root)
		if err != nil || !exists || beforeBaseline.ManagedScope == nil {
			t.Fatalf("load initial baseline: exists=%t err=%v state=%+v", exists, err, beforeBaseline)
		}
		staleFP, err := baseline.HashFile(agentsPath)
		if err != nil {
			t.Fatal(err)
		}
		staleFP.Role = machinecontract.ScopeRoleObserve
		beforeBaseline.Files["AGENTS.md"] = staleFP
		if err := baseline.Save(root, beforeBaseline); err != nil {
			t.Fatal(err)
		}
		wantPolicyID := beforeBaseline.ManagedScope.PolicyIdentity
		wantIgnoreFP := beforeBaseline.Files[".gitignore"]

		printed, _, stderr, code := runCLIPrinted(t, root, "init", "--agent=", "--hooks=false", "--here=false")
		if code != ExitOK {
			t.Fatalf("re-init failed: code=%d stderr=%s", code, stderr)
		}
		if rootBackups, _ := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*")); len(rootBackups) != 0 {
			t.Fatalf("re-init created root backup: %v", rootBackups)
		}
		backups, err := filepath.Glob(filepath.Join(root, ".aoci", "backups", "AGENTS.md.backup.*"))
		if err != nil || len(backups) != 1 {
			t.Fatalf("expected one backup under .aoci/backups/, got %v (err=%v)", backups, err)
		}
		backupRel := ".aoci/backups/" + filepath.Base(backups[0])
		if !strings.Contains(printed, backupRel) {
			t.Fatalf("re-init output must report backup path %q:\n%s", backupRel, printed)
		}
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(backups[0]); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("backup mode = %v (err=%v), want 0600", info, err)
			}
		}
		if ignored, _ := afs.PathIgnoredByGit(root, backupRel); !ignored {
			t.Fatalf("backup %q must be ignored by Git on already-scanned repo", backupRel)
		}
		if status := testkit.Git(t, root, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "AGENTS.md.backup.") {
			t.Fatalf("backup must not be stageable:\n%s", status)
		}
		if readGitignore(t, root) != teamIgnore {
			t.Fatalf("team .gitignore was altered:\n%s", readGitignore(t, root))
		}
		afterBaseline, exists, err := baseline.Load(root)
		if err != nil || !exists || afterBaseline.ManagedScope == nil {
			t.Fatalf("reload baseline: exists=%t err=%v", exists, err)
		}
		if _, present := afterBaseline.Files[backupRel]; present {
			t.Fatalf("backup entered Baseline: %q", backupRel)
		}
		if afterBaseline.Files["AGENTS.md"].Role != machinecontract.ScopeRoleObserve {
			t.Fatalf("AGENTS.md role changed: %+v", afterBaseline.Files["AGENTS.md"])
		}
		if afterBaseline.Files[".gitignore"] != wantIgnoreFP {
			t.Fatalf(".gitignore baseline entry changed: got=%+v want=%+v", afterBaseline.Files[".gitignore"], wantIgnoreFP)
		}
		if afterBaseline.ManagedScope.PolicyIdentity != wantPolicyID {
			t.Fatalf("ManagedScope.PolicyIdentity changed: got=%s want=%s", afterBaseline.ManagedScope.PolicyIdentity, wantPolicyID)
		}
	})

	t.Run("mid_init_failure_then_retry", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires elevated privileges on Windows")
		}
		root := initGitRepo(t)
		testkit.WriteFile(t, root, "AGENTS.md", "# Pre-existing AGENTS\n")
		testkit.Git(t, root, "add", "AGENTS.md", "src/a.go")
		testkit.Git(t, root, "commit", "-qm", "seed repo with AGENTS.md")

		// Plant a symlinked .mcp.json so init --agent=claude fails AFTER
		// writing .aoci/.gitignore and backing up AGENTS.md.
		linkPath := filepath.Join(root, ".mcp.json")
		if err := os.Symlink(filepath.Join(root, "src", "a.go"), linkPath); err != nil {
			t.Fatal(err)
		}
		if _, err := runInit(t, root, "--agent=claude", "--hooks=false"); err == nil {
			t.Fatal("init --agent=claude must fail on symlinked .mcp.json")
		}
		if rootBackups, _ := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*")); len(rootBackups) != 0 {
			t.Fatalf("aborted init left root backup: %v", rootBackups)
		}
		backups, err := filepath.Glob(filepath.Join(root, ".aoci", "backups", "AGENTS.md.backup.*"))
		if err != nil || len(backups) != 1 {
			t.Fatalf("expected backup under .aoci/backups/ after aborted init, got %v (err=%v)", backups, err)
		}
		backupRel := ".aoci/backups/" + filepath.Base(backups[0])
		if ignored, _ := afs.PathIgnoredByGit(root, backupRel); !ignored {
			t.Fatalf("backup %q must already be ignored after mid-init failure", backupRel)
		}
		if status := testkit.Git(t, root, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "AGENTS.md.backup.") {
			t.Fatalf("backup must not be stageable after mid-init failure:\n%s", status)
		}

		// Remove the symlink, retry init without an agent, then run first scan.
		if err := os.Remove(linkPath); err != nil {
			t.Fatal(err)
		}
		if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
			t.Fatalf("retry init failed: %v", err)
		}
		var out, errOut bytes.Buffer
		if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); code != ExitOK {
			t.Fatalf("scan after retry failed: %d %s %s", code, out.String(), errOut.String())
		}
		if status := testkit.Git(t, root, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "AGENTS.md.backup.") {
			t.Fatalf("backup became stageable after retry + scan:\n%s", status)
		}
		state, exists, err := baseline.Load(root)
		if err != nil || !exists {
			t.Fatalf("load baseline after retry: exists=%t err=%v", exists, err)
		}
		if _, present := state.Files[backupRel]; present {
			t.Fatalf("backup %q entered Baseline after retry + scan", backupRel)
		}
		if state.Files["AGENTS.md"].Role != machinecontract.ScopeRoleIndex {
			t.Fatalf("AGENTS.md role = %q, want index", state.Files["AGENTS.md"].Role)
		}
		if readGitignore(t, root) != "" {
			t.Fatalf("retry without --agent should not create team .gitignore:\n%s", readGitignore(t, root))
		}
	})

	t.Run("no_git_custom_exclude_files", func(t *testing.T) {
		root := t.TempDir()
		testkit.WriteFile(t, root, "src/a.go", "package src\n\nfunc A() int { return 1 }\n")
		testkit.WriteFile(t, root, "AGENTS.md", "# Non-Git repo AGENTS\n")

		// Pre-create config.json with Managed Scope enabled and custom exclude_files
		// that omits *.backup.*.
		cfg := config.DefaultConfig()
		if err := cfg.SetNewProjectGovernance("production"); err != nil {
			t.Fatal(err)
		}
		cfg.ExcludeFiles = []string{"*.tmp"}
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}

		printed, _, stderr, code := runCLIPrinted(t, root, "init", "--agent=", "--hooks=false", "--here=false")
		if code != ExitOK {
			t.Fatalf("non-Git init failed: code=%d stderr=%s", code, stderr)
		}
		if rootBackups, _ := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*")); len(rootBackups) != 0 {
			t.Fatalf("non-Git init wrote root backup: %v", rootBackups)
		}
		backups, err := filepath.Glob(filepath.Join(root, ".aoci", "backups", "AGENTS.md.backup.*"))
		if err != nil || len(backups) != 1 {
			t.Fatalf("expected backup under .aoci/backups/, got %v (err=%v)", backups, err)
		}
		backupRel := ".aoci/backups/" + filepath.Base(backups[0])
		if !strings.Contains(printed, backupRel) {
			t.Fatalf("non-Git init output must report backup path %q:\n%s", backupRel, printed)
		}
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(backups[0]); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("backup mode = %v (err=%v), want 0600", info, err)
			}
		}

		var out, errOut bytes.Buffer
		if scanCode := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); scanCode != ExitOK {
			t.Fatalf("non-Git scan failed: %d %s %s", scanCode, out.String(), errOut.String())
		}
		state, exists, err := baseline.Load(root)
		if err != nil || !exists {
			t.Fatalf("load non-Git baseline: exists=%t err=%v", exists, err)
		}
		for rel := range state.Files {
			if strings.Contains(rel, "AGENTS.md.backup.") || strings.HasPrefix(rel, ".aoci/") {
				t.Fatalf("backup or .aoci asset entered non-Git Baseline with custom exclude_files: %q", rel)
			}
		}
		if state.Files["AGENTS.md"].Role != machinecontract.ScopeRoleIndex {
			t.Fatalf("AGENTS.md role = %q, want index", state.Files["AGENTS.md"].Role)
		}
	})
}

func TestInitRefusesSymlinkOrFileBackupDirAndPreservesAgentsBytes(t *testing.T) {
	original := "# Project instructions\nDo not overwrite when backup directory is unsafe.\n"

	for _, locale := range []struct {
		code   string
		marker string
	}{
		{"en-US", "refused to back up or modify AGENTS.md"},
		{"zh-CN", "拒绝备份或改写 AGENTS.md"},
	} {
		t.Run(locale.code, func(t *testing.T) {
			cases := []struct {
				name       string
				needLink   bool
				wantTarget string
				setup      func(t *testing.T, root, outside string) string
			}{
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
					name:       "backups_is_regular_file",
					wantTarget: ".aoci/backups",
					setup: func(t *testing.T, root, _ string) string {
						if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(root, ".aoci", "backups"), []byte("not a directory\n"), 0o644); err != nil {
							t.Fatal(err)
						}
						return ""
					},
				},
				{
					name:       "aoci_symlink_outside_repo",
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
					root := initGitRepo(t)
					outside := t.TempDir()
					testkit.WriteFile(t, root, "AGENTS.md", original)
					testkit.Git(t, root, "add", "AGENTS.md", "src/a.go")
					testkit.Git(t, root, "commit", "-qm", "seed repo")
					redirectDir := tc.setup(t, root, outside)

					stdout, stderr, code := runCLI(t, root, "init", "--locale", locale.code, "--agent=", "--hooks=false", "--here=false")
					if code == ExitOK {
						t.Fatalf("init succeeded despite unsafe %s: stdout=%s stderr=%s", tc.wantTarget, stdout, stderr)
					}
					combined := stdout + stderr
					if !strings.Contains(combined, locale.marker) || !strings.Contains(combined, tc.wantTarget) {
						t.Fatalf("init refusal missing localized reason for %s: %s", tc.wantTarget, combined)
					}
					after, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
					if err != nil || string(after) != original {
						t.Fatalf("AGENTS.md bytes changed after refused init: err=%v got=%q", err, string(after))
					}
					if redirectDir != "" {
						entries, err := os.ReadDir(redirectDir)
						if err != nil {
							t.Fatal(err)
						}
						if len(entries) != 0 {
							t.Fatalf("refused init wrote into redirected dir %s: %v", redirectDir, entries)
						}
					}
				})
			}
		})
	}
}
