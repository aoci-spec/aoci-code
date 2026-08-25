package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestCognitionPlanCLIJSONAndLocales(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "cognition", "plan", "bootstrap"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Bootstrap JSON CLI failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var plan cognitionplan.Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Layout != "uninitialized" || plan.Status != "model_authoring_required" || plan.NetworkAccessed || !plan.FormalAssetProof.FormalAssetsUnchanged {
		t.Fatalf("unexpected machine JSON: %#v", plan)
	}
	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "cognition", "plan", "bootstrap", "--locale", "zh-CN"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "认知规划") {
		t.Fatalf("explicit Planner Locale was not activated: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	cfg := config.DefaultConfig()
	cfg.Locale = "zh-CN"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "cognition", "plan", "bootstrap"}, &stdout, &stderr)
	if code != ExitOK || !strings.Contains(stdout.String(), "认知规划") || !strings.Contains(stdout.String(), "下一步") {
		t.Fatalf("zh-CN Planner output invalid: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func initCLITestGitRepository(t *testing.T, root string) {
	t.Helper()
	command := exec.Command("git", "-C", root, "init", "-q")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, output)
	}
}

func TestCognitionPlanCommandDoesNotReplaceInit(t *testing.T) {
	root := newRootCmd()
	initCommand, _, initErr := root.Find([]string{"init"})
	plannerCommand, _, plannerErr := root.Find([]string{"cognition", "plan", "bootstrap"})
	if initErr != nil || plannerErr != nil || initCommand.CommandPath() == plannerCommand.CommandPath() {
		t.Fatalf("Planner changed or replaced aoci init: init=%v planner=%v", initErr, plannerErr)
	}
}

func TestCognitionPlanDiffReadsCompleteTargetWithoutFormalWrites(t *testing.T) {
	root := cognitionSystemCLIRepo(t)
	formalPaths := []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", "aoci.database.txt", ".aoci/baseline.json"}
	formalBefore := make(map[string]string, len(formalPaths))
	for _, rel := range formalPaths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		formalBefore[rel] = string(data)
	}
	target := strings.Replace(formalBefore["aoci.code.txt"], "coordinate database access", "coordinate planned module access", 1)
	targetPath := filepath.Join(root, "target.aoci.code.txt")
	if err := os.WriteFile(targetPath, []byte(target), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "cognition", "plan", "diff", "--target-index", targetPath}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("target Diff CLI failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report cognitionplan.CodeTargetDiff
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Updated != 1 || len(report.Changes) != 1 || report.Changes[0].ObjectRef != "code:main.go" ||
		report.Authoritative || report.SourceBound || report.ApplyAllowed || report.FormalWritesStarted || report.NetworkAccessed {
		t.Fatalf("unexpected target Diff report: %#v", report)
	}
	for _, rel := range formalPaths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || string(data) != formalBefore[rel] {
			t.Fatalf("target Diff changed formal state %s: %v", rel, err)
		}
	}
}

func TestCognitionPlanDiffRejectsSymlinkTarget(t *testing.T) {
	root := cognitionSystemCLIRepo(t)
	target := filepath.Join(root, "target.aoci.code.txt")
	if err := os.Symlink(filepath.Join(root, "aoci.code.txt"), target); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--json", "cognition", "plan", "diff", "--target-index", target}, &stdout, &stderr); code == ExitOK {
		t.Fatalf("symlink target was accepted: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}
