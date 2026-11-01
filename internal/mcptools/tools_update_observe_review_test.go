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

// pendingObserveRepoWithWork is pendingObserveRepo plus one source the Index
// role still has to author, so Maintain has a batch to issue beside the
// pending review.
func pendingObserveRepoWithWork(t *testing.T, automation string) string {
	t.Helper()
	root := pendingObserveRepo(t, automation)
	if err := os.WriteFile(filepath.Join(root, "extra.go"), []byte("package sample\n\nfunc Extra() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func maintainResultOf(t *testing.T, root string) volumeMaintainResult {
	t.Helper()
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(resText(t, handleMaintainInput(root, "test", maintainIn{}, nil))), &maintain); err != nil {
		t.Fatal(err)
	}
	return maintain
}

// The Observe review of 2026-11-01: under a posture that lets the model
// acknowledge, pending Observe evidence no longer stops a Maintain that has
// work to issue. The listing rides beside the batch, the batch confirms it
// through its own observe_review, and one call records the review and
// writes the Entries. Until then the model called observe_review, then
// Maintain again, then the batch.
func TestObserveReviewRidesWithTheBatchUnderAuto(t *testing.T) {
	root := pendingObserveRepoWithWork(t, config.AutomationModeAuto)
	maintain := maintainResultOf(t, root)
	if maintain.Status != autoStatusRepairRequired || maintain.Result != volumegovernance.ResultAuthoringRequired ||
		maintain.NextAction != "author_complete_current_machine_batch" || len(maintain.Candidates) != 1 ||
		maintain.ObserveReview == nil || maintain.ObserveReview.Confirm == nil || maintain.ObserveReview.Confirm.Tool != "aoci_update_entry" {
		t.Fatalf("pending observe evidence beside work must ride with the batch: status=%s result=%s next=%s candidates=%d review=%#v",
			maintain.Status, maintain.Result, maintain.NextAction, len(maintain.Candidates), maintain.ObserveReview)
	}
	if !strings.Contains(strings.Join(maintain.Instructions, "\n"), maintain.ObserveReview.ReviewDigest) {
		t.Fatalf("the instructions do not name the digest to confirm: %v", maintain.Instructions)
	}
	session := connectMCPClient(t, root)
	// A wrong digest is a zero-write repair finding with the current listing.
	arguments := codeBatchArguments(wireCodePlan(maintain))
	arguments["observe_review"] = map[string]any{"review_digest": strings.Repeat("0", 64), "reviewed_by": "codex-agent"}
	stale := applyVolumeBatch(t, session, arguments)
	if stale.Status != autoStatusRepairRequired || stale.Applied != 0 || stale.FormalWritesStarted ||
		len(stale.Findings) != 1 || stale.Findings[0].RuleCode != "observe_review_stale" ||
		stale.ObserveReviewCurrent == nil || stale.ObserveReviewCurrent.ReviewDigest != maintain.ObserveReview.ReviewDigest {
		t.Fatalf("a stale digest must be a zero-write finding with the current listing: %#v", stale)
	}
	// The right digest records the review and writes the Entries in one call.
	arguments["observe_review"] = map[string]any{"review_digest": maintain.ObserveReview.ReviewDigest, "reviewed_by": "codex-agent"}
	applied := applyVolumeBatch(t, session, arguments)
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 1 ||
		applied.ObserveReview == nil || applied.ObserveReview.Status != machinecontract.ObserveReviewStatusAcknowledged ||
		applied.ObserveReview.Acknowledged != 1 || applied.ObserveReview.TransactionID == "" ||
		applied.TerminalProof == nil || !applied.TerminalProof.Aligned {
		t.Fatalf("the batch did not record the review and reach aligned: %#v review=%#v", applied, applied.ObserveReview)
	}
	if after := maintainResultOf(t, root); after.ObserveReview != nil || !after.Aligned {
		t.Fatalf("the reviewed evidence still waits after the batch: %#v", after)
	}
	// A spent digest on an aligned repository is a no-op, never a fault.
	if again := applyVolumeBatch(t, session, arguments); again.ObserveReview == nil || again.ObserveReview.Status != machinecontract.ObserveReviewStatusNotPending {
		t.Fatalf("a spent review is not reported as not_pending: %#v", again.ObserveReview)
	}
}

// A posture that needs a person keeps the review a stop: the batch is not
// issued over it, and the person answers in the dialog or the terminal.
func TestObserveReviewStillStopsUnderAReviewPosture(t *testing.T) {
	root := pendingObserveRepoWithWork(t, config.AutomationModeReview)
	maintain := maintainResultOf(t, root)
	if maintain.Status != autoStatusStopped || maintain.NextAction != machinecontract.ActionReviewObservedEvidence ||
		maintain.ObserveReview == nil || maintain.ObserveReview.Confirm == nil || maintain.ObserveReview.Confirm.Tool != "aoci_maintain" {
		t.Fatalf("a review posture must keep the stop: status=%s next=%s review=%#v", maintain.Status, maintain.NextAction, maintain.ObserveReview)
	}
}
