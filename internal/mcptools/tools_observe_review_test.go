package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// pendingObserveRepo is a governed Volumes repository whose one Observe file
// changed after the Baseline, so Maintain waits for its review.
func pendingObserveRepo(t *testing.T, automation string) string {
	t.Helper()
	root := buildObservedVolumeRepo(t)
	if automation != "" {
		cfg, err := config.LoadBase(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.SetAutomationMode(automation); err != nil {
			t.Fatal(err)
		}
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("observe-only release notes, revised\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func blockedObserveListing(t *testing.T, root string) *observeReviewListing {
	t.Helper()
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(resText(t, handleMaintainInput(root, "test", maintainIn{}, nil))), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.Status != autoStatusStopped || maintain.NextAction != machinecontract.ActionReviewObservedEvidence ||
		maintain.ObserveReview == nil {
		t.Fatalf("a pending observe review is not listed: %#v", maintain)
	}
	return maintain.ObserveReview
}

func confirmObserveReview(t *testing.T, root, digest, reviewer string) (observeReviewResult, string, bool) {
	t.Helper()
	response := handleMaintainInput(root, "test", maintainIn{Intent: machinecontract.MaintainIntentObserveReview,
		ObserveReview: &observeReviewRequest{ReviewDigest: digest, ReviewedBy: reviewer}}, nil)
	text := resText(t, response)
	var result observeReviewResult
	if !response.IsError {
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			t.Fatalf("observe review result: %v %s", err, text)
		}
	}
	return result, text, response.IsError
}

func TestObserveReviewListsTheEvidenceAndConfirmsIt(t *testing.T) {
	root := pendingObserveRepo(t, config.AutomationModeAuto)
	listing := blockedObserveListing(t, root)
	if listing.Total != 1 || len(listing.Evidence) != 1 || listing.Evidence[0].Path != "notes.txt" ||
		listing.Evidence[0].Change != volumegovernance.ObservedChangeChanged || len(listing.Evidence[0].SHA256) != 64 ||
		len(listing.ReviewDigest) != 64 || listing.Truncated || listing.Confirm == nil || listing.Confirm.Tool != "aoci_maintain" ||
		listing.Confirm.Arguments["intent"] != machinecontract.MaintainIntentObserveReview {
		t.Fatalf("listing: %#v", listing)
	}
	result, text, isError := confirmObserveReview(t, root, listing.ReviewDigest, "codex-agent")
	if isError || result.Status != machinecontract.ObserveReviewStatusAcknowledged || !result.FormalWritesStarted ||
		result.ReviewDigest != listing.ReviewDigest || result.Acknowledged != 1 ||
		len(result.AcknowledgedPaths) != 1 || result.AcknowledgedPaths[0] != "notes.txt" || result.TransactionID == "" ||
		result.NextAction != machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch {
		t.Fatalf("confirmation: %s", text)
	}
	var after volumeMaintainResult
	if err := json.Unmarshal([]byte(resText(t, handleMaintainInput(root, "test", maintainIn{}, nil))), &after); err != nil {
		t.Fatal(err)
	}
	if after.ObserveReview != nil || after.NextAction == machinecontract.ActionReviewObservedEvidence {
		t.Fatalf("the reviewed evidence still blocks Maintain: %#v", after)
	}
	if again, _, _ := confirmObserveReview(t, root, listing.ReviewDigest, "codex-agent"); again.Status != machinecontract.ObserveReviewStatusNotPending {
		t.Fatalf("a spent review was accepted again: %#v", again)
	}
}

func TestObserveReviewRefusesEvidenceThatMovedAfterTheListing(t *testing.T) {
	root := pendingObserveRepo(t, config.AutomationModeAuto)
	listing := blockedObserveListing(t, root)
	baselineBefore, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("observe-only release notes, revised twice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, text, isError := confirmObserveReview(t, root, listing.ReviewDigest, "codex-agent")
	if isError || result.Status != machinecontract.ObserveReviewStatusStale || result.FormalWritesStarted ||
		result.ObserveReview == nil || result.ObserveReview.ReviewDigest == listing.ReviewDigest ||
		result.NextAction != machinecontract.ObserveReviewActionReviewCurrent {
		t.Fatalf("moved evidence was not refused: %s", text)
	}
	baselineAfter, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil || string(baselineAfter) != string(baselineBefore) {
		t.Fatalf("a stale review wrote the Baseline: %v", err)
	}
	// The fresh listing confirms.
	if confirmed, _, _ := confirmObserveReview(t, root, result.ObserveReview.ReviewDigest, "codex-agent"); confirmed.Status != machinecontract.ObserveReviewStatusAcknowledged {
		t.Fatalf("the fresh listing did not confirm: %#v", confirmed)
	}
}

func TestObserveReviewSendsAListLongerThanTheListingToTheTerminal(t *testing.T) {
	root := pendingObserveRepo(t, config.AutomationModeAuto)
	saved := observeReviewListLimit
	observeReviewListLimit = 0
	t.Cleanup(func() { observeReviewListLimit = saved })
	listing := blockedObserveListing(t, root)
	if !listing.Truncated || listing.Total != 1 || len(listing.Evidence) != 0 || listing.Confirm != nil {
		t.Fatalf("a list Maintain cannot show in full offered a confirmation: %#v", listing)
	}
	baselineBefore, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, text, isError := confirmObserveReview(t, root, listing.ReviewDigest, "codex-agent")
	if !isError || !strings.Contains(text, "observe_review_list_exceeds_mcp_listing") || !strings.Contains(text, "scope acknowledge") {
		t.Fatalf("evidence the Host was not shown was confirmed: %s", text)
	}
	baselineAfter, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil || string(baselineAfter) != string(baselineBefore) {
		t.Fatalf("a refused review wrote the Baseline: %v", err)
	}
}

func TestObserveReviewHandsAReviewPostureToAPerson(t *testing.T) {
	root := pendingObserveRepo(t, config.AutomationModeReview)
	listing := blockedObserveListing(t, root)
	result, text, isError := confirmObserveReview(t, root, listing.ReviewDigest, "codex-agent")
	if isError || result.Status != machinecontract.ObserveReviewStatusHumanApprovalRequired || result.FormalWritesStarted ||
		!strings.HasPrefix(result.PreviewFile, ".aoci/scope-change/observe-") || result.HostDialogRequest == nil ||
		result.HostDialogRequest.Arguments["intent"] != machinecontract.MaintainIntentHumanApproval || len(result.NextCommands) != 2 ||
		result.ReviewDigest != listing.ReviewDigest || result.Acknowledged != 0 {
		t.Fatalf("a review posture did not ask for a person: %s", text)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.PreviewFile))); err != nil {
		t.Fatalf("preview not saved: %v", err)
	}
}

func TestObserveReviewRequestShape(t *testing.T) {
	root := pendingObserveRepo(t, config.AutomationModeAuto)
	digest := blockedObserveListing(t, root).ReviewDigest
	for _, input := range []maintainIn{
		{Intent: machinecontract.MaintainIntentObserveReview},
		{Intent: machinecontract.MaintainIntentObserveReview, ObserveReview: &observeReviewRequest{ReviewDigest: "short", ReviewedBy: "codex"}},
		{Intent: machinecontract.MaintainIntentObserveReview, ObserveReview: &observeReviewRequest{ReviewDigest: digest, ReviewedBy: "not an actor!"}},
		{Intent: machinecontract.MaintainIntentObserveReview, Scope: "code", ObserveReview: &observeReviewRequest{ReviewDigest: digest, ReviewedBy: "codex"}},
		{ObserveReview: &observeReviewRequest{ReviewDigest: digest, ReviewedBy: "codex"}},
	} {
		if result := handleMaintainInput(root, "test", input, nil); !result.IsError {
			t.Fatalf("malformed request accepted: %#v", input)
		}
	}
	if result := handleMaintainInput(buildRepo(t), "test", maintainIn{Intent: machinecontract.MaintainIntentObserveReview,
		ObserveReview: &observeReviewRequest{ReviewDigest: digest, ReviewedBy: "codex"}}, nil); !result.IsError ||
		!strings.Contains(resText(t, result), machinecontract.StatusLegacyReadOnlyMigrateRequired) {
		t.Fatalf("a Legacy layout accepted an observe review: %s", resText(t, result))
	}
}
