package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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
