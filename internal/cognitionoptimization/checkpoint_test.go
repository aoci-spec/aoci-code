package cognitionoptimization

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointCreateLoadAndAdvance(t *testing.T) {
	root := t.TempDir()
	optimizationID := strings.Repeat("a", 64)
	firstBatch := strings.Repeat("b", 64)
	created, err := Create(root, CreateInput{OptimizationID: optimizationID, CurrentBatchID: firstBatch,
		RemainingObjectRefs: []string{"code:a.go", "code:b.go", "code:c.go"}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SHA256 != created.SHA256 || !equalCheckpoint(loaded.Checkpoint, created.Checkpoint) {
		t.Fatalf("loaded checkpoint mismatch: %#v %#v", loaded, created)
	}

	advanced, err := Advance(root, loaded.SHA256, AdvanceInput{OptimizationID: optimizationID,
		CurrentBatchID: firstBatch, SubmissionSHA256: strings.Repeat("d", 64), ReviewedDelta: 2, NoChangeDelta: 1, ReplacedDelta: 1})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := advanced.Checkpoint
	if checkpoint.CurrentBatchID != "" || checkpoint.Completed || checkpoint.ReviewedCount != 2 ||
		checkpoint.NoChangeCount != 1 || checkpoint.ReplacedCount != 1 || checkpoint.CompletedCount != 1 ||
		len(checkpoint.RemainingObjectRefs) != 1 || checkpoint.RemainingObjectRefs[0] != "code:c.go" {
		t.Fatalf("advanced checkpoint mismatch: %#v", checkpoint)
	}

	secondBatch := strings.Repeat("c", 64)
	bound, err := BindBatch(root, advanced.SHA256, optimizationID, secondBatch)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := Advance(root, bound.SHA256, AdvanceInput{OptimizationID: optimizationID,
		CurrentBatchID: secondBatch, SubmissionSHA256: strings.Repeat("e", 64), ReviewedDelta: 1, NoChangeDelta: 1})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint = completed.Checkpoint
	if !checkpoint.Completed || checkpoint.CurrentBatchID != "" || len(checkpoint.RemainingObjectRefs) != 0 ||
		checkpoint.ReviewedCount != 3 || checkpoint.NoChangeCount != 2 || checkpoint.ReplacedCount != 1 || checkpoint.CompletedCount != 2 {
		t.Fatalf("completed checkpoint mismatch: %#v", checkpoint)
	}
}

func TestCheckpointCASAndIdentityBindingsFailClosed(t *testing.T) {
	root := t.TempDir()
	optimizationID := strings.Repeat("a", 64)
	firstBatch := strings.Repeat("b", 64)
	created, err := Create(root, CreateInput{OptimizationID: optimizationID, CurrentBatchID: firstBatch,
		RemainingObjectRefs: []string{"code:a.go", "code:b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, CreateInput{OptimizationID: optimizationID, CurrentBatchID: firstBatch,
		RemainingObjectRefs: []string{"code:a.go"}}); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("duplicate Create did not conflict: %v", err)
	}
	advanced, err := Advance(root, created.SHA256, AdvanceInput{OptimizationID: optimizationID,
		CurrentBatchID: firstBatch, SubmissionSHA256: strings.Repeat("d", 64), ReviewedDelta: 1, ReplacedDelta: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Advance(root, created.SHA256, AdvanceInput{OptimizationID: optimizationID,
		CurrentBatchID: firstBatch, SubmissionSHA256: strings.Repeat("d", 64), ReviewedDelta: 1, NoChangeDelta: 1}); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("stale checkpoint SHA did not conflict: %v", err)
	}
	if _, err := BindBatch(root, advanced.SHA256, strings.Repeat("d", 64), strings.Repeat("c", 64)); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("optimization identity mismatch did not conflict: %v", err)
	}
}

func TestCheckpointBindBatchRequiresAwaitMaintain(t *testing.T) {
	root := t.TempDir()
	optimizationID := strings.Repeat("a", 64)
	batchID := strings.Repeat("b", 64)
	created, err := Create(root, CreateInput{OptimizationID: optimizationID,
		RemainingObjectRefs: []string{"code:a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindBatch(root, created.SHA256, optimizationID, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Checkpoint.CurrentBatchID != batchID {
		t.Fatalf("batch was not bound: %#v", bound.Checkpoint)
	}
	if _, err := BindBatch(root, bound.SHA256, optimizationID, strings.Repeat("c", 64)); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("second batch binding did not conflict: %v", err)
	}
}

func TestCheckpointRestartCompletedOnly(t *testing.T) {
	root := t.TempDir()
	optimizationID := strings.Repeat("a", 64)
	batchID := strings.Repeat("b", 64)
	created, err := Create(root, CreateInput{OptimizationID: optimizationID, CurrentBatchID: batchID,
		RemainingObjectRefs: []string{"code:a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestartCompleted(root, created.SHA256, CreateInput{OptimizationID: strings.Repeat("c", 64),
		RemainingObjectRefs: []string{"code:b.go"}}); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("active checkpoint restart did not conflict: %v", err)
	}
	completed, err := Advance(root, created.SHA256, AdvanceInput{OptimizationID: optimizationID,
		CurrentBatchID: batchID, SubmissionSHA256: strings.Repeat("d", 64), ReviewedDelta: 1, NoChangeDelta: 1})
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := RestartCompleted(root, completed.SHA256, CreateInput{OptimizationID: strings.Repeat("c", 64),
		RemainingObjectRefs: []string{"code:b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Checkpoint.Completed || restarted.Checkpoint.CurrentBatchID != "" || restarted.Checkpoint.ReviewedCount != 0 ||
		len(restarted.Checkpoint.RemainingObjectRefs) != 1 || restarted.Checkpoint.RemainingObjectRefs[0] != "code:b.go" {
		t.Fatalf("completed checkpoint restart mismatch: %#v", restarted.Checkpoint)
	}
}

func TestCheckpointStrictJSONRejectsUnknownDuplicateAndTrailingData(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{"unknown", `{"version":"cognition-optimization-checkpoint/v1","optimization_id":"` + strings.Repeat("a", 64) + `","current_batch_id":"` + strings.Repeat("b", 64) + `","remaining_object_refs":["code:a.go"],"reviewed_count":0,"no_change_count":0,"replaced_count":0,"completed_count":0,"completed":false,"unknown":true}`},
		{"duplicate", `{"version":"cognition-optimization-checkpoint/v1","version":"cognition-optimization-checkpoint/v1","optimization_id":"` + strings.Repeat("a", 64) + `","current_batch_id":"` + strings.Repeat("b", 64) + `","remaining_object_refs":["code:a.go"],"reviewed_count":0,"no_change_count":0,"replaced_count":0,"completed_count":0,"completed":false}`},
		{"trailing", `{"version":"cognition-optimization-checkpoint/v1","optimization_id":"` + strings.Repeat("a", 64) + `","current_batch_id":"` + strings.Repeat("b", 64) + `","remaining_object_refs":["code:a.go"],"reviewed_count":0,"no_change_count":0,"replaced_count":0,"completed_count":0,"completed":false} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path, err := Path(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.data), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); !errors.Is(err, ErrCheckpointInvalid) {
				t.Fatalf("invalid checkpoint did not fail closed: %v", err)
			}
		})
	}
}

func TestCheckpointAdvanceRejectsIncompleteClassification(t *testing.T) {
	root := t.TempDir()
	optimizationID := strings.Repeat("a", 64)
	batchID := strings.Repeat("b", 64)
	created, err := Create(root, CreateInput{OptimizationID: optimizationID, CurrentBatchID: batchID,
		RemainingObjectRefs: []string{"code:a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Advance(root, created.SHA256, AdvanceInput{OptimizationID: optimizationID,
		CurrentBatchID: batchID, ReviewedDelta: 1}); !errors.Is(err, ErrCheckpointInvalid) {
		t.Fatalf("unclassified review was accepted: %v", err)
	}
}

func equalCheckpoint(left, right Checkpoint) bool {
	if left.Version != right.Version || left.OptimizationID != right.OptimizationID || left.CurrentBatchID != right.CurrentBatchID ||
		left.ReviewedCount != right.ReviewedCount || left.NoChangeCount != right.NoChangeCount ||
		left.ReplacedCount != right.ReplacedCount || left.CompletedCount != right.CompletedCount ||
		left.LastCompletedBatchID != right.LastCompletedBatchID || left.LastSubmissionSHA256 != right.LastSubmissionSHA256 ||
		left.LastReviewedCount != right.LastReviewedCount || left.LastNoChangeCount != right.LastNoChangeCount ||
		left.LastReplacedCount != right.LastReplacedCount || left.Completed != right.Completed ||
		len(left.RemainingObjectRefs) != len(right.RemainingObjectRefs) {
		return false
	}
	for index := range left.RemainingObjectRefs {
		if left.RemainingObjectRefs[index] != right.RemainingObjectRefs[index] {
			return false
		}
	}
	return true
}
