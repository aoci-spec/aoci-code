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
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
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

func TestInitIgnoresAgentsBackupBeforeFirstScanAndReportsBackupFile(t *testing.T) {
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

			backups, err := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("expected one AGENTS.md backup file, got %v (err=%v)", backups, err)
			}
			backupName := filepath.Base(backups[0])
			if saved, readErr := os.ReadFile(backups[0]); readErr != nil || string(saved) != original {
				t.Fatalf("backup content mismatch: err=%v got=%q", readErr, string(saved))
			}
			if !strings.Contains(printed, tc.marker+backupName) {
				t.Fatalf("init output must report backup filename %q:\n%s", backupName, printed)
			}

			ignore := readGitignore(t, root)
			if !strings.Contains(ignore, hostConfigIgnoreMarker) || !strings.Contains(ignore, agentsBackupIgnorePattern) {
				t.Fatalf(".gitignore must include %s inside init ignore block:\n%s", agentsBackupIgnorePattern, ignore)
			}
			for _, line := range strings.Split(ignore, "\n") {
				if strings.TrimSpace(line) == "AGENTS.md" {
					t.Fatalf(".gitignore must never ignore AGENTS.md itself:\n%s", ignore)
				}
			}
			if ignored, rule := afs.PathIgnoredByGit(root, backupName); !ignored {
				t.Fatalf("backup %q must be ignored by Git: ignored=%t rule=%q", backupName, ignored, rule)
			}
			if ignored, rule := afs.PathIgnoredByGit(root, "AGENTS.md"); ignored {
				t.Fatalf("AGENTS.md must remain unignored by Git: ignored=%t rule=%q", ignored, rule)
			}

			// First scan must exclude the backup while keeping AGENTS.md and .gitignore indexed.
			var out, errOut bytes.Buffer
			if scanCode := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); scanCode != ExitOK {
				t.Fatalf("scan failed: %d %s %s", scanCode, out.String(), errOut.String())
			}
			state, exists, err := baseline.Load(root)
			if err != nil || !exists {
				t.Fatalf("load baseline: exists=%t err=%v", exists, err)
			}
			if _, present := state.Files[backupName]; present {
				t.Fatalf("AGENTS.md backup %q must not enter Managed Scope", backupName)
			}
			if state.Files["AGENTS.md"].Role != machinecontract.ScopeRoleIndex {
				t.Fatalf("AGENTS.md role = %q, want index", state.Files["AGENTS.md"].Role)
			}
			if state.Files[".gitignore"].Role != machinecontract.ScopeRoleIndex {
				t.Fatalf(".gitignore role = %q, want index", state.Files[".gitignore"].Role)
			}
		})
	}
}

func TestInitWithAgentIncludesAgentsBackupPatternBeforeFirstScan(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	ignore := readGitignore(t, root)
	if !strings.Contains(ignore, agentsBackupIgnorePattern) {
		t.Fatalf("init --agent=codex must add %s before first scan:\n%s", agentsBackupIgnorePattern, ignore)
	}
}

func TestInitDoesNotModifyExistingRepoGitignoreOrBaselineRolesForAgentsBackup(t *testing.T) {
	root := initGitRepo(t)
	// Simulate an existing repository that already completed its first scan
	// without AGENTS.md.backup.* in .gitignore, with AGENTS.md in observe role.
	testkit.WriteFile(t, root, ".gitignore", "# existing repo\nbuild/\n")
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(root, "AGENTS.md")
	currentAgents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	staleAgents := strings.Replace(string(currentAgents), "<!-- aoci:end -->", "# stale\n<!-- aoci:end -->", 1)
	if err := os.WriteFile(agentsPath, []byte(staleAgents), 0o644); err != nil {
		t.Fatal(err)
	}
	agentsFP, err := baseline.HashFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	agentsFP.Role = machinecontract.ScopeRoleObserve
	gitignoreFP, err := baseline.HashFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	gitignoreFP.Role = machinecontract.ScopeRoleIndex
	state := baseline.NewBaseline(map[string]baseline.Fingerprint{
		"AGENTS.md":  agentsFP,
		".gitignore": gitignoreFP,
	})
	state.ManagedScope = &baseline.ManagedScopeState{
		Version:             machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity:      strings.Repeat("b", 64),
		ObserveChangePolicy: machinecontract.ObserveChangeInformational,
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	beforeGitignore := readGitignore(t, root)

	printed, _, stderr, code := runCLIPrinted(t, root, "init", "--agent=", "--hooks=false", "--here=false")
	if code != ExitOK {
		t.Fatalf("re-init failed: code=%d stderr=%s", code, stderr)
	}
	backups, err := filepath.Glob(filepath.Join(root, "AGENTS.md.backup.*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected backup on stale AGENTS.md rewrite, got %v (err=%v)", backups, err)
	}
	if !strings.Contains(printed, filepath.Base(backups[0])) {
		t.Fatalf("re-init output must still report backup file %q:\n%s", filepath.Base(backups[0]), printed)
	}
	if afterGitignore := readGitignore(t, root); afterGitignore != beforeGitignore {
		t.Fatalf("existing repo .gitignore must not be modified after Baseline exists:\nbefore:\n%s\nafter:\n%s", beforeGitignore, afterGitignore)
	}
	afterBaseline, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load baseline: exists=%t err=%v", exists, err)
	}
	if afterBaseline.Files["AGENTS.md"].Role != machinecontract.ScopeRoleObserve {
		t.Fatalf("existing repo AGENTS.md role changed: %+v", afterBaseline.Files["AGENTS.md"])
	}
	if afterBaseline.Files[".gitignore"] != gitignoreFP {
		t.Fatalf("existing repo .gitignore baseline entry changed: got=%+v want=%+v", afterBaseline.Files[".gitignore"], gitignoreFP)
	}
}
