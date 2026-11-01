package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

// The terminal confirmation of 2026-11-01: a person reads what approving does
// and answers yes. The 64-character digest phrase it replaced proved nothing
// the digest-bound approval record does not, and nobody new to a terminal
// could make sense of it.
func TestTerminalApprovalIsPlainSummaryAndYes(t *testing.T) {
	pinDefaultLocale(t)
	root := buildScopeCLIRepo(t)
	previewPath, preview := reviewPreviewFile(t, root)
	grantTTY(t)
	recorder := capturePrompt(t)

	// The retired phrase is no longer an approval, and the person is told
	// nothing changed.
	_, stderr, err := runScopeSplit(t, root, strings.NewReader(preview.Plan.ConfirmationPhrase+"\n"), true,
		"approve", "--preview-file", previewPath, "--out-file", filepath.Join(t.TempDir(), "phrase.json"))
	if err == nil {
		t.Fatal("the retired digest phrase was accepted as an approval")
	}
	screen := recorder.String()
	if strings.Contains(screen, "Type exactly") || strings.Contains(screen, preview.Plan.ConfirmationPhrase) {
		t.Fatalf("the prompt still asks for the digest phrase: %q", screen)
	}
	for _, want := range []string{preview.Plan.PlanID, "Type yes to approve", "Not approved, so nothing was changed"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the prompt is missing %q:\n%s\n%s", want, screen, stderr)
		}
	}
	recorder.Reset()

	// Case and surrounding spaces do not matter, and --actor has a default,
	// so the printed command runs as printed.
	outPath := filepath.Join(t.TempDir(), "approval.json")
	if _, _, err := runScopeSplit(t, root, strings.NewReader("  YES \n"), true,
		"approve", "--preview-file", previewPath, "--out-file", outPath); err != nil {
		t.Fatalf("yes was not accepted: %v", err)
	}
	written, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := scopechange.DecodeApproval(written)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Actor != defaultApprovalActor || approval.Mechanism != machinecontract.ApprovalMechanismInteractiveDigestConfirmation {
		t.Fatalf("the approval does not record the default actor and the terminal mechanism: %+v", approval)
	}
}

// Without a terminal the contract still names yes as the answer, so a host
// that relays the exact command tells the person the right thing.
func TestInteractionContractNamesYesAsTheAnswer(t *testing.T) {
	pinDefaultLocale(t)
	root := buildScopeCLIRepo(t)
	previewPath, _ := reviewPreviewFile(t, root)
	// go test's stdin can be a character device (/dev/null is one), so the
	// no-terminal branch is selected explicitly.
	previous := stdinIsCharDevice
	stdinIsCharDevice = func() bool { return false }
	t.Cleanup(func() { stdinIsCharDevice = previous })
	stdout, stderr, err := runScopeSplit(t, root, nil, true, "approve", "--preview-file", previewPath)
	if err == nil {
		t.Fatalf("approve without a terminal did not stop: %s", stdout)
	}
	merged := stdout.String() + stderr.String()
	if !strings.Contains(merged, `"confirmation_phrase":"yes"`) && !strings.Contains(merged, `"confirmation_phrase": "yes"`) {
		t.Fatalf("the interaction contract does not name yes as the answer:\n%s", merged)
	}
	if strings.Contains(merged, "<id>") {
		t.Fatalf("the interaction contract still carries a placeholder:\n%s", merged)
	}
}
