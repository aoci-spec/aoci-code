package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// Issue #57: the update classifier counted an item as part of the current
// optimization batch when its batch id equalled the checkpoint's current batch
// id. Optimization clears that id when it completes, or between two batches,
// and leaves the checkpoint on disk for good, so an item without a batch id,
// the documented direct and compatibility update path, matched the empty id
// and was refused as cognition_optimization_batch_mixed. Maintain-issued
// batches carry ids and never saw it. The same comparison against the
// last-completed id closed a third window: while the first batch is still
// pending, that id is empty too. These tests pin all three windows.
func TestCognitionOptimizationCompletedCheckpointDoesNotRefuseBatchlessUpdates(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	session := connectMCPClient(t, root)
	first := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, first, 3, 3, 0)
	done := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(first, nil))
	assertOptimizationProgress(t, done.Optimization, "complete", 3, 3, 3, 0, 0, false)
	checkpoint := readOptimizationCheckpoint(t, root)
	if !checkpoint.Completed || checkpoint.CurrentBatchID != "" {
		t.Fatalf("fixture precondition: a completed checkpoint with an empty current batch id: %+v", checkpoint)
	}
	assertBatchlessUpdateApplies(t, session, root, first.Candidates[0])
}

func TestCognitionOptimizationPendingFirstBatchDoesNotRefuseBatchlessUpdates(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 201)
	session := connectMCPClient(t, root)
	first := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, first, 201, 200, 1)
	checkpoint := readOptimizationCheckpoint(t, root)
	if checkpoint.Completed || checkpoint.CurrentBatchID == "" || checkpoint.LastCompletedBatchID != "" {
		t.Fatalf("fixture precondition: a pending first batch with no completed batch yet: %+v", checkpoint)
	}
	assertBatchlessUpdateApplies(t, session, root, first.Candidates[0])
	// The optimization is not wedged by the ordinary write: the next request
	// rebinds a batch for review under the same optimization identity.
	second := callCognitionOptimizationMaintain(t, session, nil)
	if second.Optimization == nil || second.Optimization.OptimizationID != first.Optimization.OptimizationID ||
		second.Optimization.State != "review_required" || second.Optimization.CurrentBatchID == "" ||
		second.Optimization.TotalTargets != 201 {
		t.Fatalf("optimization did not continue after a batch-less update: %+v", second.Optimization)
	}
}

func TestCognitionOptimizationBetweenBatchesDoesNotRefuseBatchlessUpdates(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 201)
	session := connectMCPClient(t, root)
	first := callCognitionOptimizationMaintain(t, session, nil)
	assertOptimizationReviewBatch(t, first, 201, 200, 1)
	update := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(first, nil))
	assertOptimizationProgress(t, update.Optimization, "in_progress", 201, 200, 200, 0, 1, true)
	checkpoint := readOptimizationCheckpoint(t, root)
	if checkpoint.Completed || checkpoint.CurrentBatchID != "" || checkpoint.LastCompletedBatchID == "" {
		t.Fatalf("fixture precondition: between batches, no current id and one completed batch: %+v", checkpoint)
	}
	assertBatchlessUpdateApplies(t, session, root, first.Candidates[0])
}

type optimizationCheckpointIDs struct {
	Completed            bool   `json:"completed"`
	CurrentBatchID       string `json:"current_batch_id"`
	LastCompletedBatchID string `json:"last_completed_batch_id"`
}

func readOptimizationCheckpoint(t *testing.T, root string) optimizationCheckpointIDs {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(optimizationCheckpointRelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint optimizationCheckpointIDs
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

func assertBatchlessUpdateApplies(t *testing.T, session *mcp.ClientSession, root string, target optimizationCandidatePayload) {
	t.Helper()
	fieldStart := strings.Index(target.ExistingEntry, "F:")
	if fieldStart < 0 {
		t.Fatalf("fixture Entry carries no F field: %q", target.ExistingEntry)
	}
	revised := target.ExistingEntry[:fieldStart+2] + "Revised: " + target.ExistingEntry[fieldStart+2:]
	result := applyVolumeBatch(t, session, map[string]any{"entries": []map[string]any{{
		"path": target.Path, "source_sha256": target.SourceSHA256, "new_entry": revised}}})
	if result.Status != autoStatusApplied || result.Applied != 1 {
		t.Fatalf("a batch-less update must apply through the ordinary path while an optimization checkpoint "+
			"holds an empty current batch id: status=%s applied=%d findings=%#v", result.Status, result.Applied, result.Findings)
	}
	if got := cognitionEntryLines(t, root, cognition.ScopeCode)[target.ObjectRef]; got != revised {
		t.Fatalf("the direct update did not reach the Code Volume: got %q want %q", got, revised)
	}
}
