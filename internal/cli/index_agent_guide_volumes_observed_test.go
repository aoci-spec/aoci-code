// observe_change_policy defaults to review_required, so changing one Observe-role file (usually a
// test) blocks authoring. The Legacy Plan had long had agentPlanStageObservedReview wiring this state to
// scope acknowledge, while Volumes only reported a bare observed_pending finding without a command:
// the same dead end as baseline_missing before the issue #8 fix, and reproduced far more often.
// Locked here: a Volumes Guide with Observe evidence pending review must carry the scope_acknowledge command and a
// structured stop; after acknowledge the same repository no longer blocks.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestVolumeGuideWithPendingObserveCarriesAcknowledgeRemediation(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/a.go", "package src\n\nfunc A() int { return 1 }\n")
	// The Observe role: the companion test of production code.
	write("src/a_test.go", "package src\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { _ = A() }\n")
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	var scanOut, scanErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &scanOut, &scanErr); code != ExitOK {
		t.Fatalf("scan failed: code=%d stdout=%s stderr=%s", code, scanOut.String(), scanErr.String())
	}

	// Change the Observe file: governance enters observed_pending.
	write("src/a_test.go", "package src\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"drift\")\n\t}\n}\n")

	guide := volumeGuideJSON(t, root)
	if guide.Stage != "blocked" || guide.NextAction != machinecontract.ActionReviewObservedEvidence || guide.Complete {
		t.Fatalf("the write remains blocked while the Agent must continue review: %+v", guide)
	}
	wantInstructions, err := textassets.ObservedReviewInstructions(textassets.DefaultLocale)
	if err != nil {
		t.Fatal(err)
	}
	if len(guide.Instructions) < len(wantInstructions) ||
		strings.Join(guide.Instructions[:len(wantInstructions)], "\n") != strings.Join(wantInstructions, "\n") {
		t.Fatalf("review steps must precede general guidance: %v", guide.Instructions)
	}
	var wire, wireErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "index", "agent", "guide", "--agent", "codex", "--json"}, &wire, &wireErr); code != ExitOK {
		t.Fatalf("guide failed: %s", wireErr.String())
	}
	for _, key := range []string{"instructions", "commands", "stop"} {
		position := strings.Index(wire.String(), `"`+key+`"`)
		if position < 0 || position > strings.Index(wire.String(), `"findings"`) || position > 8192 {
			t.Fatalf("%s was buried behind findings in the CLI response", key)
		}
	}
	hasObserved := false
	for _, finding := range guide.Findings {
		if finding.Code == "observed_pending" {
			hasObserved = true
		}
	}
	if !hasObserved {
		t.Fatalf("changing an observe-role file must raise observed_pending: %+v", guide.Findings)
	}
	if guide.Commands.ScopeAcknowledge == "" || !strings.Contains(guide.Commands.ScopeAcknowledge, "scope acknowledge") {
		t.Fatalf("a pending-observe Guide must carry the acknowledge command: %+v", guide.Commands)
	}
	if guide.Commands.ScopeStatus == "" {
		t.Fatalf("a pending-observe Guide must expose scope status for the evidence review: %+v", guide.Commands)
	}
	if guide.Stop == nil || guide.Stop.RuleCode != "observed_pending" ||
		guide.Stop.Cause == "" || guide.Stop.SafeNextAction == "" {
		t.Fatalf("a pending-observe Guide must carry a structured observed_pending stop: %+v", guide.Stop)
	}
	if len(guide.Instructions) == 0 {
		t.Fatal("the Guide must tell the model to review the reported observe evidence before acknowledging")
	}

	// After acknowledge the same repository no longer blocks on Observe evidence.
	var ackOut, ackErr bytes.Buffer
	if code := executeCLI([]string{
		"--repo", root, "--quiet", "scope", "acknowledge", "--reviewed-by", "conformance-test",
	}, &ackOut, &ackErr); code != ExitOK {
		t.Fatalf("acknowledge failed: code=%d stdout=%s stderr=%s", code, ackOut.String(), ackErr.String())
	}
	guide = volumeGuideJSON(t, root)
	for _, finding := range guide.Findings {
		if finding.Code == "observed_pending" {
			t.Fatalf("acknowledge must clear observed_pending: %+v", guide.Findings)
		}
	}
	if guide.Commands.ScopeAcknowledge != "" || (guide.Stop != nil && guide.Stop.RuleCode == "observed_pending") {
		t.Fatalf("the remediation must disappear once nothing is pending: cmd=%q stop=%+v",
			guide.Commands.ScopeAcknowledge, guide.Stop)
	}
}

func TestObservedReviewGuidancePreservesAuthorizationBoundary(t *testing.T) {
	for _, mode := range []string{config.AutomationModeReview, config.AutomationModeOff} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			for rel, body := range map[string]string{"main.go": "package main\n", "main_test.go": "package main\n// initial evidence\n"} {
				if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := executeCLI([]string{"--repo", root, "scan"}, &stdout, &stderr); code != ExitOK {
				t.Fatalf("scan failed: %s", stderr.String())
			}
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.SetAutomationMode(mode); err != nil {
				t.Fatal(err)
			}
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package main\n// changed evidence\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			guide := volumeGuideJSON(t, root)
			if guide.NextAction != machinecontract.ActionReviewObservedEvidence {
				t.Fatalf("read-only evidence review should remain available: %s", guide.NextAction)
			}
			before := map[string][]byte{}
			for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"} {
				body, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					t.Fatal(err)
				}
				before[rel] = body
			}
			stdout.Reset()
			stderr.Reset()
			if code := executeCLI([]string{"--repo", root, "scope", "acknowledge", "--reviewed-by", "test-agent", "--json"}, &stdout, &stderr); code == ExitOK {
				t.Fatalf("reviewer label bypassed %s authorization: %s", mode, stdout.String())
			}
			for rel, body := range before {
				after, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil || !bytes.Equal(body, after) {
					t.Fatalf("unauthorized acknowledgement changed %s: %v", rel, err)
				}
			}
		})
	}
}
