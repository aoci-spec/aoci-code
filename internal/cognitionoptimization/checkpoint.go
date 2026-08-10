package cognitionoptimization

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

const CheckpointVersion = "cognition-optimization-checkpoint/v1"

var (
	ErrCheckpointNotFound = errors.New("cognition optimization checkpoint not found")
	ErrCheckpointInvalid  = errors.New("cognition optimization checkpoint invalid")
	ErrCheckpointConflict = errors.New("cognition optimization checkpoint conflict")
)

// Checkpoint is intentionally only resumable draft progress. It is not an
// Index, Baseline, transaction, recovery receipt, or formal completion proof.
// RemainingObjectRefs retains selector priority order. CurrentBatchID is empty
// while the next Maintain call is expected to bind that prefix, and contains a
// batch identity only while the corresponding Update is pending.
type Checkpoint struct {
	Version              string   `json:"version"`
	OptimizationID       string   `json:"optimization_id"`
	CurrentBatchID       string   `json:"current_batch_id"`
	RemainingObjectRefs  []string `json:"remaining_object_refs"`
	ReviewedCount        int      `json:"reviewed_count"`
	NoChangeCount        int      `json:"no_change_count"`
	ReplacedCount        int      `json:"replaced_count"`
	CompletedCount       int      `json:"completed_count"`
	LastCompletedBatchID string   `json:"last_completed_batch_id"`
	LastSubmissionSHA256 string   `json:"last_submission_sha256"`
	LastReviewedCount    int      `json:"last_reviewed_count"`
	LastNoChangeCount    int      `json:"last_no_change_count"`
	LastReplacedCount    int      `json:"last_replaced_count"`
	Completed            bool     `json:"completed"`
}

// StoredCheckpoint carries the exact file digest required by Advance CAS.
type StoredCheckpoint struct {
	Checkpoint Checkpoint
	SHA256     string
}

type CreateInput struct {
	OptimizationID      string
	CurrentBatchID      string
	RemainingObjectRefs []string
}

// AdvanceInput completes exactly the checkpoint's current batch. The reviewed
// delta is removed from the front of RemainingObjectRefs; callers cannot
// reorder or skip the deterministic selection tail. A later Maintain call must
// bind the next batch against the resulting cognition postimage.
type AdvanceInput struct {
	OptimizationID   string
	CurrentBatchID   string
	SubmissionSHA256 string
	ReviewedDelta    int
	NoChangeDelta    int
	ReplacedDelta    int
}

// Path returns the repository-local active checkpoint path.
func Path(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%w: repository root is empty", ErrCheckpointInvalid)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: repository root: %v", ErrCheckpointInvalid, err)
	}
	return filepath.Join(absolute, ".aoci", "drafts", "code-cognition", "optimization-active.json"), nil
}

// Create atomically publishes a new active checkpoint only when none exists.
func Create(root string, input CreateInput) (StoredCheckpoint, error) {
	checkpoint := Checkpoint{
		Version: CheckpointVersion, OptimizationID: input.OptimizationID,
		CurrentBatchID:      input.CurrentBatchID,
		RemainingObjectRefs: append([]string{}, input.RemainingObjectRefs...),
	}
	if err := validateCheckpoint(checkpoint); err != nil {
		return StoredCheckpoint{}, err
	}
	path, err := Path(root)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return StoredCheckpoint{}, fmt.Errorf("create checkpoint directory: %w", err)
	}
	data, err := encodeCheckpoint(checkpoint)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	if err := afs.AtomicCreateCAS(path, data); err != nil {
		if errors.Is(err, afs.ErrAtomicCreateConflict) {
			return StoredCheckpoint{}, fmt.Errorf("%w: active checkpoint already exists", ErrCheckpointConflict)
		}
		return StoredCheckpoint{}, fmt.Errorf("create checkpoint: %w", err)
	}
	return StoredCheckpoint{Checkpoint: checkpoint, SHA256: digest(data)}, nil
}

// Load strictly decodes the active checkpoint. Unknown, duplicate, trailing,
// malformed, or internally inconsistent JSON fails closed.
func Load(root string) (StoredCheckpoint, error) {
	path, err := Path(root)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return StoredCheckpoint{}, ErrCheckpointNotFound
	}
	if err != nil {
		return StoredCheckpoint{}, fmt.Errorf("load checkpoint: %w", err)
	}
	checkpoint, err := decodeCheckpoint(data)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	return StoredCheckpoint{Checkpoint: checkpoint, SHA256: digest(data)}, nil
}

// BindBatch CAS-binds the next machine-issued batch after Maintain replans
// against the current Code Volume. It is valid only in the active
// await-maintain state and never changes semantic progress counts.
func BindBatch(root, expectedSHA256, optimizationID, batchID string) (StoredCheckpoint, error) {
	stored, err := loadForCAS(root, expectedSHA256)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	current := stored.Checkpoint
	if current.Completed || current.CurrentBatchID != "" || len(current.RemainingObjectRefs) == 0 {
		return StoredCheckpoint{}, fmt.Errorf("%w: checkpoint is not awaiting Maintain", ErrCheckpointConflict)
	}
	if current.OptimizationID != optimizationID || !validSHA256(batchID) {
		return StoredCheckpoint{}, fmt.Errorf("%w: optimization or batch identity mismatch", ErrCheckpointConflict)
	}
	current.CurrentBatchID = batchID
	return replaceCheckpointCAS(root, expectedSHA256, current)
}

// RebindBatch replaces a stale current machine batch after ordinary
// governance has independently returned to alignment. It preserves the exact
// remaining review universe and progress counters; no object is completed or
// removed by this operation.
func RebindBatch(root, expectedSHA256, optimizationID, currentBatchID, nextBatchID string) (StoredCheckpoint, error) {
	stored, err := loadForCAS(root, expectedSHA256)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	current := stored.Checkpoint
	if current.Completed || current.OptimizationID != optimizationID || current.CurrentBatchID != currentBatchID ||
		!validSHA256(currentBatchID) || !validSHA256(nextBatchID) || len(current.RemainingObjectRefs) == 0 {
		return StoredCheckpoint{}, fmt.Errorf("%w: current batch cannot be rebound", ErrCheckpointConflict)
	}
	current.CurrentBatchID = nextBatchID
	return replaceCheckpointCAS(root, expectedSHA256, current)
}

// Advance CAS-updates draft progress only after the caller's existing Update
// transaction has completed. It does not write any formal cognition asset.
func Advance(root, expectedSHA256 string, input AdvanceInput) (StoredCheckpoint, error) {
	stored, err := loadForCAS(root, expectedSHA256)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	current := stored.Checkpoint
	if current.Completed {
		return StoredCheckpoint{}, fmt.Errorf("%w: checkpoint already completed", ErrCheckpointConflict)
	}
	if !validSHA256(current.CurrentBatchID) || input.OptimizationID != current.OptimizationID || input.CurrentBatchID != current.CurrentBatchID {
		return StoredCheckpoint{}, fmt.Errorf("%w: optimization or batch identity mismatch", ErrCheckpointConflict)
	}
	if !validSHA256(input.SubmissionSHA256) {
		return StoredCheckpoint{}, fmt.Errorf("%w: completed-batch submission identity", ErrCheckpointInvalid)
	}
	if input.ReviewedDelta < 1 || input.ReviewedDelta > MaxBatchEntries ||
		input.NoChangeDelta < 0 || input.ReplacedDelta < 0 ||
		input.NoChangeDelta+input.ReplacedDelta != input.ReviewedDelta ||
		input.ReviewedDelta > len(current.RemainingObjectRefs) {
		return StoredCheckpoint{}, fmt.Errorf("%w: invalid completed-batch counts", ErrCheckpointInvalid)
	}

	remaining := append([]string{}, current.RemainingObjectRefs[input.ReviewedDelta:]...)
	current.CurrentBatchID = ""
	if len(remaining) == 0 {
		current.Completed = true
	}
	current.RemainingObjectRefs = remaining
	current.ReviewedCount += input.ReviewedDelta
	current.NoChangeCount += input.NoChangeDelta
	current.ReplacedCount += input.ReplacedDelta
	current.CompletedCount++
	current.LastCompletedBatchID = input.CurrentBatchID
	current.LastSubmissionSHA256 = input.SubmissionSHA256
	current.LastReviewedCount = input.ReviewedDelta
	current.LastNoChangeCount = input.NoChangeDelta
	current.LastReplacedCount = input.ReplacedDelta
	if err := validateCheckpoint(current); err != nil {
		return StoredCheckpoint{}, err
	}
	return replaceCheckpointCAS(root, expectedSHA256, current)
}

// RestartCompleted replaces only a strictly valid completed checkpoint. It
// permits a later explicit optimization request without retaining a separate
// run Manifest or completion Receipt.
func RestartCompleted(root, expectedSHA256 string, input CreateInput) (StoredCheckpoint, error) {
	stored, err := loadForCAS(root, expectedSHA256)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	if !stored.Checkpoint.Completed {
		return StoredCheckpoint{}, fmt.Errorf("%w: active checkpoint cannot be restarted", ErrCheckpointConflict)
	}
	replacement := Checkpoint{Version: CheckpointVersion, OptimizationID: input.OptimizationID,
		CurrentBatchID: input.CurrentBatchID, RemainingObjectRefs: append([]string{}, input.RemainingObjectRefs...)}
	if err := validateCheckpoint(replacement); err != nil {
		return StoredCheckpoint{}, err
	}
	return replaceCheckpointCAS(root, expectedSHA256, replacement)
}

func decodeCheckpoint(data []byte) (Checkpoint, error) {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return Checkpoint{}, fmt.Errorf("%w: %v", ErrCheckpointInvalid, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var checkpoint Checkpoint
	if err := decoder.Decode(&checkpoint); err != nil {
		return Checkpoint{}, fmt.Errorf("%w: decode: %v", ErrCheckpointInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Checkpoint{}, fmt.Errorf("%w: trailing JSON", ErrCheckpointInvalid)
	}
	if err := validateCheckpoint(checkpoint); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

func encodeCheckpoint(checkpoint Checkpoint) ([]byte, error) {
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode checkpoint: %w", err)
	}
	return append(data, '\n'), nil
}

func validateCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.Version != CheckpointVersion || !validSHA256(checkpoint.OptimizationID) {
		return fmt.Errorf("%w: version or optimization identity", ErrCheckpointInvalid)
	}
	if checkpoint.ReviewedCount < 0 || checkpoint.NoChangeCount < 0 || checkpoint.ReplacedCount < 0 ||
		checkpoint.CompletedCount < 0 || checkpoint.ReviewedCount != checkpoint.NoChangeCount+checkpoint.ReplacedCount ||
		checkpoint.CompletedCount > checkpoint.ReviewedCount || checkpoint.ReviewedCount > checkpoint.CompletedCount*MaxBatchEntries ||
		(checkpoint.ReviewedCount > 0 && checkpoint.CompletedCount == 0) {
		return fmt.Errorf("%w: progress counts", ErrCheckpointInvalid)
	}
	if checkpoint.LastCompletedBatchID == "" {
		if checkpoint.LastSubmissionSHA256 != "" || checkpoint.LastReviewedCount != 0 || checkpoint.LastNoChangeCount != 0 ||
			checkpoint.LastReplacedCount != 0 || checkpoint.CompletedCount != 0 {
			return fmt.Errorf("%w: last completed batch", ErrCheckpointInvalid)
		}
	} else if !validSHA256(checkpoint.LastCompletedBatchID) || !validSHA256(checkpoint.LastSubmissionSHA256) || checkpoint.LastReviewedCount < 1 ||
		checkpoint.LastReviewedCount > MaxBatchEntries || checkpoint.LastNoChangeCount < 0 || checkpoint.LastReplacedCount < 0 ||
		checkpoint.LastNoChangeCount+checkpoint.LastReplacedCount != checkpoint.LastReviewedCount || checkpoint.CompletedCount == 0 ||
		checkpoint.LastReviewedCount > checkpoint.ReviewedCount || checkpoint.LastNoChangeCount > checkpoint.NoChangeCount ||
		checkpoint.LastReplacedCount > checkpoint.ReplacedCount ||
		(checkpoint.CompletedCount == 1 && (checkpoint.LastReviewedCount != checkpoint.ReviewedCount ||
			checkpoint.LastNoChangeCount != checkpoint.NoChangeCount || checkpoint.LastReplacedCount != checkpoint.ReplacedCount)) {
		return fmt.Errorf("%w: last completed batch", ErrCheckpointInvalid)
	}
	if checkpoint.Completed {
		if checkpoint.CurrentBatchID != "" || len(checkpoint.RemainingObjectRefs) != 0 || checkpoint.CompletedCount == 0 {
			return fmt.Errorf("%w: completed state", ErrCheckpointInvalid)
		}
	} else {
		if len(checkpoint.RemainingObjectRefs) == 0 || (checkpoint.CurrentBatchID != "" && !validSHA256(checkpoint.CurrentBatchID)) {
			return fmt.Errorf("%w: active state", ErrCheckpointInvalid)
		}
	}
	seen := make(map[string]bool, len(checkpoint.RemainingObjectRefs))
	for _, objectRef := range checkpoint.RemainingObjectRefs {
		if !canonicalCodeObjectRef(objectRef) || seen[objectRef] {
			return fmt.Errorf("%w: remaining object_ref %q", ErrCheckpointInvalid, objectRef)
		}
		seen[objectRef] = true
	}
	return nil
}

func digest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

func loadForCAS(root, expectedSHA256 string) (StoredCheckpoint, error) {
	if !validSHA256(expectedSHA256) {
		return StoredCheckpoint{}, fmt.Errorf("%w: expected checkpoint sha256 invalid", ErrCheckpointInvalid)
	}
	stored, err := Load(root)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	if stored.SHA256 != expectedSHA256 {
		return StoredCheckpoint{}, fmt.Errorf("%w: checkpoint preimage changed", ErrCheckpointConflict)
	}
	return stored, nil
}

func replaceCheckpointCAS(root, expectedSHA256 string, checkpoint Checkpoint) (StoredCheckpoint, error) {
	if err := validateCheckpoint(checkpoint); err != nil {
		return StoredCheckpoint{}, err
	}
	data, err := encodeCheckpoint(checkpoint)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	path, err := Path(root)
	if err != nil {
		return StoredCheckpoint{}, err
	}
	if err := afs.AtomicWriteCAS(path, data, expectedSHA256); err != nil {
		if errors.Is(err, afs.ErrAtomicCASConflict) {
			return StoredCheckpoint{}, fmt.Errorf("%w: checkpoint CAS failed", ErrCheckpointConflict)
		}
		return StoredCheckpoint{}, fmt.Errorf("write checkpoint: %w", err)
	}
	return StoredCheckpoint{Checkpoint: checkpoint, SHA256: digest(data)}, nil
}
