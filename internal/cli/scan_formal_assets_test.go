// A formal cognition asset hidden from Git never enters the Baseline. Nothing
// reported that: the repository only failed much later on a blocked Guide whose
// finding named neither the ignore rule nor the file carrying it. These tests
// pin the refusal, and the line-ending protection that keeps a Windows checkout
// from producing the same class of failure without any ignore rule at all.
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runScan(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	oldRepo, oldQuiet, oldJSON := flagRepo, flagQuiet, flagJSON
	flagRepo, flagQuiet, flagJSON = root, true, false
	t.Cleanup(func() { flagRepo, flagQuiet, flagJSON = oldRepo, oldQuiet, oldJSON })

	cmd := findRegisteredCommand(t, "scan")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	// The registered command is package-level, so a boolean flag set by an
	// earlier case survives into this one; runInit guards the same hazard.
	for _, name := range []string{"dry-run", "force"} {
		if err := cmd.Flags().Set(name, "false"); err != nil {
			t.Fatalf("reset --%s: %v", name, err)
		}
	}
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), err
}

func commitFixture(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "fixture"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := command.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
}

func hideFromGit(t *testing.T, root string, paths ...string) {
	t.Helper()
	excludePath := filepath.Join(root, ".git", "info", "exclude")
	existing, _ := os.ReadFile(excludePath)
	if err := os.WriteFile(excludePath,
		append(existing, []byte("\n"+strings.Join(paths, "\n")+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanRefusesToBaselineWithoutTheVolumesItGoverns(t *testing.T) {
	root := initGitRepo(t)
	commitFixture(t, root)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	hideFromGit(t, root, "aoci.txt", "aoci.meta.txt", "aoci.code.txt")

	output, err := runScan(t, root)
	if err == nil {
		t.Fatalf("scan published a Baseline that omits the Volumes it governs:\n%s", output)
	}
	message := err.Error()
	// The operator must not have to guess which rule did it: the recorded case
	// cost a capable agent a dozen rounds to trace one line in .git/info/exclude.
	for _, required := range []string{"aoci.code.txt", "exclude"} {
		if !strings.Contains(message, required) {
			t.Fatalf("refusal must name the asset and the rule source, missing %q in %q", required, message)
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, ".aoci", "baseline.json")); statErr == nil {
		t.Fatal("a refused scan still wrote a Baseline")
	}
}

func TestScanDryRunReportsTheSameRefusal(t *testing.T) {
	root := initGitRepo(t)
	commitFixture(t, root)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	hideFromGit(t, root, "aoci.code.txt")
	// A dry run that promises a scan the real run would refuse is worse than no
	// dry run at all.
	if _, err := runScan(t, root, "--dry-run"); err == nil {
		t.Fatal("--dry-run reported success for a scan that cannot succeed")
	}
}

func TestScanStaysSilentOnAVisibleCognitionLayer(t *testing.T) {
	root := initGitRepo(t)
	commitFixture(t, root)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if _, err := runScan(t, root); err != nil {
		t.Fatalf("an ordinary repository must scan cleanly: %v", err)
	}
	// The guard is only worth having if it is silent on the happy path, and only
	// meaningful if the assets really did land.
	data, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		if !strings.Contains(string(data), required) {
			t.Fatalf("Baseline is missing %s", required)
		}
	}
}

func TestInitGivesNewRepositoriesTheLineEndingProtectionAOCIUsesItself(t *testing.T) {
	root := initGitRepo(t)
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf("init did not write .gitattributes: %v", err)
	}
	// core.autocrlf=true is the Git for Windows default and rewrites every line
	// ending on checkout. This repository protects itself with exactly this line;
	// user repositories were left without it.
	if !strings.Contains(string(data), "text=auto eol=lf") {
		t.Fatalf(".gitattributes does not normalize line endings: %q", string(data))
	}
}

func TestInitNeverRewritesAMaintainerGitattributes(t *testing.T) {
	root := initGitRepo(t)
	original := "# mine\n*.bin binary\n"
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	// Never rewriting a maintainer file outranks this protection: a repository
	// that already has .gitattributes has its own rules and AOCI cannot know
	// which of them should yield.
	if string(data) != original {
		t.Fatalf("init rewrote a maintainer .gitattributes: %q", string(data))
	}
}
