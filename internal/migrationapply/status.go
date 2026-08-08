package migrationapply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func Pending(root string) ([]string, error) {
	return cognitiontxn.PendingForOperation(root, OperationMigration)
}

// Status derives transaction state only from current regular-file bytes and
// immutable receipts. No saved mutable phase field is trusted.
func Status(root, transactionID string) (*TransactionStatus, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if transactionID == "" {
		pending, err := Pending(absRoot)
		if err != nil {
			return nil, err
		}
		if len(pending) != 1 {
			return nil, fmt.Errorf("migration_transaction_id_required")
		}
		transactionID = pending[0]
	}
	intent, err := loadRecoveryAt(intentPath(absRoot, transactionID), transactionID)
	active := err == nil
	if errors.Is(err, os.ErrNotExist) {
		intent, err = loadRecoveryAt(archivePath(absRoot, transactionID), transactionID)
	}
	if err != nil {
		return nil, fmt.Errorf("migration_transaction_not_found")
	}
	status, err := inspectEnvelopeState(absRoot, transactionID, &intent.Envelope, active)
	if err != nil {
		return nil, err
	}
	status.RecoveryPending = active
	if !active {
		if result, rollbackErr := loadResultAt(rollbackPath(absRoot, transactionID), transactionID); rollbackErr == nil && result.Status == machinecontract.CognitionMigrationStatusRolledBack {
			status.Status = machinecontract.CognitionMigrationStatusRolledBack
			status.ActiveLayout = "legacy"
			status.FormalComplete = false
			status.NextActions = []string{"none"}
		} else if _, receiptErr := loadReceipt(absRoot, transactionID); receiptErr == nil {
			status.Status = machinecontract.CognitionMigrationStatusApplied
			status.ActiveLayout = "volumes"
			status.FormalComplete = true
			status.NextActions = []string{"none"}
		} else {
			return nil, fmt.Errorf("migration_archived_transaction_has_no_terminal_receipt")
		}
	}
	status.StatusDigest = ""
	data, _ := canonicalJSON(status)
	status.StatusDigest = sha256Hex(data)
	return status, nil
}

func inspectEnvelopeState(root, transactionID string, envelope *ApplyEnvelope, includeStaging bool) (*TransactionStatus, error) {
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	status := &TransactionStatus{
		Version: machinecontract.CognitionMigrationTransactionStatusV2, Operation: OperationMigration,
		TransactionID: transactionID, Status: machinecontract.CognitionMigrationStatusPrepared,
		ActiveLayout: "legacy", Targets: []TargetStatus{}, NextActions: []string{"apply"}, NetworkAccessed: false,
	}
	type expected struct {
		path, kind, pre, post string
		absent                bool
	}
	expectedTargets := make([]expected, 0, len(envelope.VolumeTargets)+2)
	for _, target := range envelope.VolumeTargets {
		expectedTargets = append(expectedTargets, expected{path: target.Path, kind: target.Kind, pre: target.PreimageSHA256, post: target.PostSHA256, absent: true})
	}
	expectedTargets = append(expectedTargets,
		expected{path: envelope.Root.Path, kind: "root", pre: envelope.Root.PreimageSHA256, post: envelope.Root.PostSHA256},
		expected{path: envelope.Baseline.Path, kind: "baseline", pre: envelope.Baseline.PreimageSHA256, post: envelope.Baseline.PostSHA256},
	)
	postCount := 0
	conflict := false
	diskStates := make([]string, 0, len(expectedTargets))
	for indexValue, target := range expectedTargets {
		state, actual, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(target.path)), target.pre, target.post, target.absent)
		if err != nil {
			return nil, err
		}
		stagingState := cognitiontxn.StateMissingStaging
		if includeStaging {
			stagingPath := filepath.Join(root, ".aoci", "transactions", "migration-"+transactionID, "staging", fmt.Sprintf("%02d.post", indexValue+1))
			stagingState, _, err = cognitiontxn.Classify(stagingPath, "", target.post, true)
			if err != nil {
				return nil, err
			}
			if stagingState != cognitiontxn.StatePostimage {
				conflict = true
				stagingState = cognitiontxn.StateMissingStaging
			}
		}
		status.Targets = append(status.Targets, TargetStatus{Path: target.path, Kind: target.kind, DiskState: state, StagingState: stagingState, ActualSHA256: actual})
		diskStates = append(diskStates, state)
		switch state {
		case cognitiontxn.StatePostimage:
			postCount++
		case cognitiontxn.StatePreimage:
		default:
			conflict = true
		}
	}
	if !validMigrationPrefix(diskStates, len(envelope.VolumeTargets)) {
		conflict = true
	}
	snapshotBytes, _ := prettyJSON(envelope.Snapshot)
	snapshotState := cognitiontxn.StateMissingStaging
	for _, path := range []string{snapshotPath(root, transactionID), snapshotArchivePath(root, transactionID)} {
		state, _, _ := cognitiontxn.Classify(path, "", sha256Hex(snapshotBytes), true)
		if state == cognitiontxn.StatePostimage {
			snapshotState = cognitiontxn.StatePostimage
			break
		}
		if state != cognitiontxn.StatePreimage {
			snapshotState = state
		}
	}
	status.SnapshotState = snapshotState
	if includeStaging && snapshotState != cognitiontxn.StatePostimage {
		conflict = true
	}
	rootState, _ := statusTarget(status, "aoci.txt")
	if rootState.DiskState == cognitiontxn.StatePostimage {
		status.ActiveLayout = "volumes"
	}
	status.FormalComplete = postCount == len(expectedTargets)
	status.ThirdPartyConflict = conflict
	if conflict {
		status.Status = machinecontract.CognitionMigrationStatusRecoveryConflict
		status.NextActions = []string{"resolve_third_party_conflict"}
	} else if includeStaging {
		status.RecoveryPending = true
		if status.ActiveLayout == "volumes" {
			status.Status = machinecontract.CognitionMigrationStatusRecoveryRequiredVolumes
		} else {
			status.Status = machinecontract.CognitionMigrationStatusRecoveryRequiredLegacy
		}
		status.NextActions = []string{"resume", "rollback"}
	}
	status.StatusDigest = ""
	data, _ := canonicalJSON(status)
	status.StatusDigest = sha256Hex(data)
	return status, nil
}

func validMigrationPrefix(states []string, volumeCount int) bool {
	seenPreimage := false
	prefix := true
	for _, state := range states {
		if state == cognitiontxn.StatePreimage {
			seenPreimage = true
		} else if state == cognitiontxn.StatePostimage && seenPreimage {
			prefix = false
		}
	}
	if prefix {
		return true
	}
	// Pending Rollback restores Root before Baseline. A process stop between
	// those two CAS operations is the only intentional non-prefix state.
	if len(states) != volumeCount+2 {
		return false
	}
	for index := 0; index < volumeCount; index++ {
		if states[index] != cognitiontxn.StatePostimage {
			return false
		}
	}
	return states[volumeCount] == cognitiontxn.StatePreimage && states[volumeCount+1] == cognitiontxn.StatePostimage
}

func statusTarget(status *TransactionStatus, path string) (TargetStatus, bool) {
	for _, target := range status.Targets {
		if target.Path == path {
			return target, true
		}
	}
	return TargetStatus{}, false
}
