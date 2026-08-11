package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

func buildActiveFreshCLIRouteRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Start(root, cfg.Locale, time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Next(root, 1, 1024*1024); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGuideRedirectsActiveFreshWhileScanAndPlanStopReadOnly(t *testing.T) {
	root := buildActiveFreshCLIRouteRepo(t)
	activePath := onboarding.SessionPath(root)
	before, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "index", "agent", "guide", "--agent", "claude"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Guide did not redirect active Fresh: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var guide onboardingRedirectGuide
	if err := json.Unmarshal(stdout.Bytes(), &guide); err != nil || guide.Mode != "redirect" || guide.Stage != "onboarding" ||
		guide.Complete || guide.Route == nil || guide.Route.Status != "onboarding_in_progress" {
		t.Fatalf("invalid onboarding Guide redirect: guide=%#v err=%v output=%s", guide, err, stdout.String())
	}

	for _, args := range [][]string{
		{"--repo", root, "--json", "scan"},
		{"--repo", root, "--json", "cognition", "plan", "bootstrap"},
	} {
		stdout.Reset()
		stderr.Reset()
		code = executeCLI(args, &stdout, &stderr)
		if code != ExitInvalid {
			t.Fatalf("wrong Fresh command was not stopped: args=%v code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
		var envelope cliJSONErrorEnvelope
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.ErrorCode != errOnboardingInProgress {
			t.Fatalf("wrong command omitted onboarding route error: args=%v envelope=%#v err=%v", args, envelope, err)
		}
		details, err := json.Marshal(envelope.Details)
		if err != nil || !bytes.Contains(details, []byte(`"status":"onboarding_in_progress"`)) ||
			!bytes.Contains(details, []byte(`"formal_writes_started":false`)) {
			t.Fatalf("error details omitted route facts: %s err=%v", details, err)
		}
	}

	after, err := os.ReadFile(activePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("Guide/scan/plan routing changed active Session: err=%v", err)
	}
	for _, path := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", filepath.Join(".aoci", "baseline.json")} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("routing created formal asset %s: %v", path, err)
		}
	}
}

func TestGuideReportsRecoveryBeforeActiveFreshRoute(t *testing.T) {
	root := buildActiveFreshCLIRouteRepo(t)
	transactions := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(transactions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transactions, "bootstrap-route-test.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "index", "agent", "guide", "--agent", "claude"}, &stdout, &stderr)
	if code != ExitInvalid {
		t.Fatalf("Guide did not preserve Recovery priority: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var envelope cliJSONErrorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.ErrorCode != "cognition_recovery_pending" {
		t.Fatalf("Guide mislabeled pending Recovery as onboarding: envelope=%#v err=%v", envelope, err)
	}
}

func TestGuideAndCommandsKeepApplyPendingFinalizationAheadOfPublishedRoot(t *testing.T) {
	root := buildActiveFreshCLIRouteRepo(t)
	activePath := onboarding.SessionPath(root)
	data, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted onboarding.Session
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	persisted.TransactionState = "apply_pending"
	persisted.ApprovalArtifact = ".aoci/onboarding/test/approval.json"
	persisted.EnvelopeArtifact = ".aoci/onboarding/test/apply-envelope.json"
	persisted.NextAction = "human_tty_digest_confirmation"
	persisted.Revision++
	data, err = json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(activePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte("formal Root is already published\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "index", "agent", "guide", "--agent", "claude"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Guide bypassed terminal Fresh route: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var guide onboardingRedirectGuide
	if err := json.Unmarshal(stdout.Bytes(), &guide); err != nil || guide.Route == nil ||
		!guide.Route.FormalIndexAvailable || !guide.Route.FormalWritesStarted ||
		guide.Route.NextActionContract == nil || guide.Route.NextActionContract.Action != "resume" {
		t.Fatalf("Guide omitted apply-pending finalization route: guide=%#v err=%v", guide, err)
	}

	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "--json", "scan"}, &stdout, &stderr)
	if code != ExitInvalid {
		t.Fatalf("scan bypassed terminal Fresh route: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var envelope cliJSONErrorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.ErrorCode != errOnboardingInProgress {
		t.Fatalf("scan omitted terminal Fresh route: envelope=%#v err=%v", envelope, err)
	}
	after, err := os.ReadFile(activePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("CLI finalization route changed active Session: err=%v", err)
	}
}
