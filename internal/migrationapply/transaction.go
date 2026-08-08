package migrationapply

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbaseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var migrationFault = func(string) error { return nil }

// Apply starts or idempotently resumes one exact Snapshot/Mapping/Approval-
// bound migration under the existing global AOCI write lock.
func Apply(root string, envelope *ApplyEnvelope, approval *Approval) (*ApplyResult, error) {
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	if err := validateApproval(envelope, approval); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	transactionID := transactionIdentity(envelope, approval)
	lock, err := afs.AcquireIndexLock(absRoot)
	if err != nil {
		return nil, fmt.Errorf("migration_write_lock_failed: %w", err)
	}
	defer lock.Release()

	if archived, archiveErr := loadRecoveryAt(archivePath(absRoot, transactionID), transactionID); archiveErr == nil {
		if archived.Envelope.EnvelopeDigest != envelope.EnvelopeDigest || archived.Approval.ApprovalDigest != approval.ApprovalDigest {
			return nil, fmt.Errorf("migration_completed_transaction_binding_conflict")
		}
		result, err := loadResultAt(resultPath(absRoot, transactionID), transactionID)
		if err != nil {
			return nil, fmt.Errorf("migration_completion_result_invalid: %w", err)
		}
		copyResult := *result
		copyResult.Status = machinecontract.CognitionMigrationStatusAlreadyApplied
		copyResult.NextAction = "none"
		return &copyResult, nil
	} else if !errors.Is(archiveErr, os.ErrNotExist) {
		return nil, archiveErr
	}
	if err := cognitiontxn.RejectOtherPending(absRoot, "migration-"+transactionID+".json"); err != nil {
		return nil, err
	}
	if err := cognitionplan.ValidateExternalGuards(absRoot, &envelope.Plan); err != nil {
		return nil, fmt.Errorf("migration_guard_drift: %w", err)
	}

	intent, loadErr := loadRecoveryAt(intentPath(absRoot, transactionID), transactionID)
	if errors.Is(loadErr, os.ErrNotExist) {
		replayed, prepareErr := Prepare(absRoot, &ApplyRequest{
			Version: envelope.RequestVersion, Snapshot: envelope.Snapshot, Plan: envelope.Plan,
			Mapping: envelope.Mapping, Candidate: envelope.Candidate, Preview: envelope.Preview,
			BaselineTimestamp: envelope.PreparedAt,
		})
		if prepareErr != nil {
			var mismatch *ReplayMismatchError
			if errors.As(prepareErr, &mismatch) {
				return nil, newReplayMismatch("migration_prepare_replay_mismatch", envelope.Version, mismatch.Report.Items)
			}
			return nil, fmt.Errorf("migration_prepare_replay_mismatch: %v", prepareErr)
		}
		if mismatches := envelopeReplayMismatches(envelope, replayed, approval); len(mismatches) != 0 {
			return nil, newReplayMismatch("migration_prepare_replay_mismatch", envelope.Version, mismatches)
		}
		status, statusErr := inspectEnvelopeState(absRoot, transactionID, envelope, false)
		if statusErr != nil || status.Status != machinecontract.CognitionMigrationStatusPrepared {
			return nil, fmt.Errorf("migration_preimage_conflict: %v", statusErr)
		}
		if err := cognitiontxn.EnsureRuntimeBoundary(absRoot, envelope.RuntimeBoundary.Path, []byte(envelope.RuntimeBoundary.Content)); err != nil {
			return nil, fmt.Errorf("migration_runtime_boundary_failed: %w", err)
		}
		if err := ensureTransactionDirectories(absRoot, transactionID); err != nil {
			return nil, err
		}
		if err := migrationFault("before_snapshot_persist"); err != nil {
			return nil, err
		}
		snapshotBytes, _ := prettyJSON(envelope.Snapshot)
		if err := cognitiontxn.SaveImmutable(snapshotPath(absRoot, transactionID), snapshotBytes); err != nil {
			return nil, fmt.Errorf("migration_snapshot_persist_failed: %w", err)
		}
		if err := migrationFault("after_snapshot_persist"); err != nil {
			return nil, err
		}
		staging, err := stageEnvelope(absRoot, transactionID, envelope, snapshotBytes)
		if err != nil {
			return nil, err
		}
		if err := migrationFault("before_recovery_intent"); err != nil {
			return nil, err
		}
		intent = newRecoveryIntent(transactionID, envelope, approval, staging)
		if err := saveRecoveryIntent(absRoot, intent); err != nil {
			return nil, err
		}
		if err := migrationFault("after_recovery_intent"); err != nil {
			return nil, err
		}
		if err := validateLiveSnapshotPreimages(absRoot, &envelope.Snapshot); err != nil {
			return nil, err
		}
	} else if loadErr != nil {
		return nil, loadErr
	} else if intent.Envelope.EnvelopeDigest != envelope.EnvelopeDigest || intent.Approval.ApprovalDigest != approval.ApprovalDigest {
		return nil, fmt.Errorf("migration_pending_transaction_binding_conflict")
	}
	return advanceTransaction(absRoot, intent)
}

func Resume(root, transactionID string) (*ApplyResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lock, err := afs.AcquireIndexLock(absRoot)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	intent, err := loadRecoveryAt(intentPath(absRoot, transactionID), transactionID)
	if errors.Is(err, os.ErrNotExist) {
		if _, archiveErr := loadRecoveryAt(archivePath(absRoot, transactionID), transactionID); archiveErr != nil {
			return nil, fmt.Errorf("migration_transaction_not_found")
		}
		result, resultErr := loadResultAt(resultPath(absRoot, transactionID), transactionID)
		if resultErr != nil {
			return nil, fmt.Errorf("migration_completed_transaction_invalid")
		}
		copyResult := *result
		copyResult.Status = machinecontract.CognitionMigrationStatusAlreadyApplied
		copyResult.NextAction = "none"
		return &copyResult, nil
	}
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.RejectOtherPending(absRoot, "migration-"+transactionID+".json"); err != nil {
		return nil, err
	}
	if err := cognitionplan.ValidateExternalGuards(absRoot, &intent.Envelope.Plan); err != nil {
		return nil, fmt.Errorf("migration_guard_drift: %w", err)
	}
	return advanceTransaction(absRoot, intent)
}

func advanceTransaction(root string, intent *RecoveryIntent) (*ApplyResult, error) {
	status, err := inspectEnvelopeState(root, intent.TransactionID, &intent.Envelope, true)
	if err != nil {
		return nil, err
	}
	if status.Status == machinecontract.CognitionMigrationStatusRecoveryConflict {
		return nil, fmt.Errorf("migration_recovery_conflict")
	}
	for _, target := range intent.Envelope.VolumeTargets {
		state, _ := statusTarget(status, target.Path)
		if state.DiskState == cognitiontxn.StatePostimage {
			continue
		}
		if state.DiskState != cognitiontxn.StatePreimage || state.StagingState != cognitiontxn.StatePostimage {
			return nil, fmt.Errorf("migration_recovery_conflict: %s", target.Path)
		}
		if err := migrationFault("before_publish_" + target.AssetID); err != nil {
			return nil, err
		}
		data, err := cognitiontxn.ReadStaged(root, intent.Staging, target.Path)
		if err != nil {
			return nil, err
		}
		if err := afs.AtomicCreateCASMode(filepath.Join(root, filepath.FromSlash(target.Path)), data, 0o644); err != nil {
			return nil, fmt.Errorf("migration_atomic_create_failed[%s]: %w", target.Path, err)
		}
		if err := migrationFault("after_publish_" + target.AssetID); err != nil {
			return nil, err
		}
		status, err = inspectEnvelopeState(root, intent.TransactionID, &intent.Envelope, true)
		if err != nil || status.Status == machinecontract.CognitionMigrationStatusRecoveryConflict {
			return nil, fmt.Errorf("migration_post_publish_recovery_conflict: %s", target.Path)
		}
	}
	if err := verifyDormantVolumes(root, &intent.Envelope); err != nil {
		return nil, err
	}
	rootState, _ := statusTarget(status, intent.Envelope.Root.Path)
	if rootState.DiskState == cognitiontxn.StatePreimage {
		if rootState.StagingState != cognitiontxn.StatePostimage {
			return nil, fmt.Errorf("migration_root_staging_missing")
		}
		if err := migrationFault("before_publish_root"); err != nil {
			return nil, err
		}
		data, err := cognitiontxn.ReadStaged(root, intent.Staging, intent.Envelope.Root.Path)
		if err != nil {
			return nil, err
		}
		if err := afs.AtomicWriteCAS(filepath.Join(root, "aoci.txt"), data, intent.Envelope.Root.PreimageSHA256); err != nil {
			return nil, fmt.Errorf("migration_root_cas_failed: %w", err)
		}
		if err := migrationFault("after_publish_root"); err != nil {
			return nil, err
		}
		status, err = inspectEnvelopeState(root, intent.TransactionID, &intent.Envelope, true)
		if err != nil || status.Status == machinecontract.CognitionMigrationStatusRecoveryConflict {
			return nil, fmt.Errorf("migration_root_post_publish_conflict")
		}
	} else if rootState.DiskState != cognitiontxn.StatePostimage {
		return nil, fmt.Errorf("migration_root_recovery_conflict")
	}

	baselineState, _ := statusTarget(status, intent.Envelope.Baseline.Path)
	if baselineState.DiskState == cognitiontxn.StatePreimage {
		if baselineState.StagingState != cognitiontxn.StatePostimage {
			return nil, fmt.Errorf("migration_baseline_staging_missing")
		}
		if err := migrationFault("before_publish_baseline"); err != nil {
			return nil, err
		}
		data, err := cognitiontxn.ReadStaged(root, intent.Staging, intent.Envelope.Baseline.Path)
		if err != nil {
			return nil, err
		}
		if err := afs.AtomicWriteCAS(filepath.Join(root, ".aoci", "baseline.json"), data, intent.Envelope.Baseline.PreimageSHA256); err != nil {
			return nil, fmt.Errorf("migration_baseline_cas_failed: %w", err)
		}
		if err := migrationFault("after_publish_baseline"); err != nil {
			return nil, err
		}
	} else if baselineState.DiskState != cognitiontxn.StatePostimage {
		return nil, fmt.Errorf("migration_baseline_recovery_conflict")
	}

	if err := migrationFault("before_internal_verify"); err != nil {
		return nil, err
	}
	set, err := internalVerify(root, &intent.Envelope)
	if err != nil {
		return nil, fmt.Errorf("migration_internal_verify_failed: %w", err)
	}
	if err := migrationFault("after_internal_verify"); err != nil {
		return nil, err
	}
	receipt := newReceipt(intent, set)
	if err := migrationFault("before_completion_receipt"); err != nil {
		return nil, err
	}
	if err := saveReceipt(root, receipt); err != nil {
		return nil, err
	}
	result := newApplyResult(intent, receipt)
	if err := saveResult(root, result); err != nil {
		return nil, err
	}
	if err := migrationFault("after_completion_receipt"); err != nil {
		return nil, err
	}
	if err := migrationFault("before_ledger"); err != nil {
		return nil, err
	}
	cfg, _ := config.LoadReadOnly(root)
	ledgerEnabled := cfg != nil && cfg.LedgerEnabled
	if err := cognitiontxn.EnsureLedger(root, ledgerEnabled, ledger.Event{
		Op: "cognition_migration_apply", Source: ledger.SourceHuman, Result: ledger.ResultOK,
		AppliedCount: len(intent.Envelope.VolumeTargets) + 2, RecoveryTransactionID: intent.TransactionID,
		PreIndexSHA256: intent.Envelope.Snapshot.LegacySHA256, PostIndexSHA256: intent.Envelope.Root.PostSHA256,
		BaselineSHA256: intent.Envelope.Baseline.PostSHA256, IndexSHA256: intent.Envelope.Root.PostSHA256,
	}); err != nil {
		return nil, err
	}
	if err := migrationFault("after_ledger"); err != nil {
		return nil, err
	}
	if err := migrationFault("before_transaction_archive"); err != nil {
		return nil, err
	}
	if err := archiveRecovery(root, intent); err != nil {
		return nil, err
	}
	if err := migrationFault("after_transaction_archive"); err != nil {
		return nil, err
	}
	return result, nil
}

func verifyDormantVolumes(root string, envelope *ApplyEnvelope) error {
	assets := map[string]cognitionplan.CandidateAsset{}
	volumeRaw := map[string][]byte{}
	for _, asset := range envelope.Candidate.Assets {
		assets[asset.AssetID] = asset
		if asset.AssetID != "root" {
			volumeRaw[asset.AssetID] = []byte(asset.Content)
		}
	}
	projected, findings := cognition.BuildProjectedSet(root, []byte(assets["root"].Content), volumeRaw)
	if len(findings) != 0 || projected == nil || projected.CompositeIdentity != envelope.ProjectedCompositeIdentity {
		return fmt.Errorf("migration_dormant_volume_projection_invalid")
	}
	for _, target := range envelope.VolumeTargets {
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(target.Path)), "", target.PostSHA256, true)
		if err != nil || state != cognitiontxn.StatePostimage {
			return fmt.Errorf("migration_dormant_volume_invalid: %s", target.Path)
		}
	}
	return nil
}

func internalVerify(root string, envelope *ApplyEnvelope) (*cognition.Set, error) {
	targets := make([]cognitionbaseline.FormalTarget, 0, len(envelope.VolumeTargets)+1)
	for _, target := range envelope.VolumeTargets {
		targets = append(targets, cognitionbaseline.FormalTarget{Path: target.Path, SHA256: target.PostSHA256})
	}
	targets = append(targets, cognitionbaseline.FormalTarget{Path: envelope.Root.Path, SHA256: envelope.Root.PostSHA256})
	enabled := []string{}
	for _, descriptor := range envelope.Preview.ProjectedDescriptors {
		if descriptor.State == machinecontract.CognitionVolumeEnabled {
			enabled = append(enabled, descriptor.ID)
		}
	}
	return cognitionbaseline.VerifyVolumeState(root, envelope.ProjectedCompositeIdentity, envelope.Baseline.PostSHA256, targets, enabled)
}

func ensureTransactionDirectories(root, transactionID string) error {
	return cognitiontxn.EnsureSafeDirectory(root, filepath.ToSlash(filepath.Join(".aoci", "transactions", "migration-"+transactionID)))
}

func stageEnvelope(root, transactionID string, envelope *ApplyEnvelope, snapshotBytes []byte) ([]cognitiontxn.StagedPostimage, error) {
	posts := []cognitiontxn.Postimage{{Path: "legacy_snapshot", SHA: sha256Hex(snapshotBytes), Data: snapshotBytes}}
	for _, target := range envelope.VolumeTargets {
		posts = append(posts, cognitiontxn.Postimage{Path: target.Path, SHA: target.PostSHA256, Data: []byte(target.Content)})
	}
	posts = append(posts,
		cognitiontxn.Postimage{Path: envelope.Root.Path, SHA: envelope.Root.PostSHA256, Data: []byte(envelope.Root.Content)},
		cognitiontxn.Postimage{Path: envelope.Baseline.Path, SHA: envelope.Baseline.PostSHA256, Data: []byte(envelope.Baseline.Content)},
	)
	return cognitiontxn.Stage(root, OperationMigration, transactionID, posts, migrationFault)
}

func newRecoveryIntent(transactionID string, envelope *ApplyEnvelope, approval *Approval, staging []cognitiontxn.StagedPostimage) *RecoveryIntent {
	intent := &RecoveryIntent{
		Version: machinecontract.CognitionMigrationRecoveryV2, Operation: OperationMigration, TransactionID: transactionID,
		Envelope: *envelope, Approval: *approval, Staging: staging, CreatedAt: approval.ApprovedAt,
	}
	intent.RecoveryDigest, _ = recoveryDigest(intent)
	return intent
}

func recoveryDigest(intent *RecoveryIntent) (string, error) {
	copyValue := *intent
	copyValue.RecoveryDigest = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func saveRecoveryIntent(root string, intent *RecoveryIntent) error {
	data, err := prettyJSON(intent)
	if err != nil {
		return err
	}
	if err := cognitiontxn.SaveImmutable(intentPath(root, intent.TransactionID), data); err != nil {
		return fmt.Errorf("migration_recovery_intent_persist_failed: %w", err)
	}
	return nil
}

func loadRecoveryAt(path, transactionID string) (*RecoveryIntent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	intent, err := DecodeRecovery(data)
	if err != nil {
		return nil, err
	}
	digest, digestErr := recoveryDigest(intent)
	if digestErr != nil || intent.Operation != OperationMigration || intent.TransactionID != transactionID || digest != intent.RecoveryDigest ||
		validateEnvelope(&intent.Envelope) != nil || validateApproval(&intent.Envelope, &intent.Approval) != nil ||
		transactionIdentity(&intent.Envelope, &intent.Approval) != transactionID {
		return nil, fmt.Errorf("migration_recovery_binding_invalid")
	}
	return intent, nil
}

func archiveRecovery(root string, intent *RecoveryIntent) error {
	snapshotBytes, _ := prettyJSON(intent.Envelope.Snapshot)
	activeSnapshot := snapshotPath(root, intent.TransactionID)
	archiveSnapshot := snapshotArchivePath(root, intent.TransactionID)
	if existing, err := os.ReadFile(activeSnapshot); err == nil {
		if !bytes.Equal(existing, snapshotBytes) {
			return fmt.Errorf("migration_snapshot_archive_conflict")
		}
		if err := cognitiontxn.SaveImmutable(archiveSnapshot, existing); err != nil {
			return err
		}
		if err := os.Remove(activeSnapshot); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if archived, readErr := os.ReadFile(archiveSnapshot); readErr != nil || !bytes.Equal(archived, snapshotBytes) {
			return fmt.Errorf("migration_snapshot_archive_missing")
		}
	} else {
		return err
	}
	intentBytes, _ := prettyJSON(intent)
	return cognitiontxn.ArchiveImmutable(intentPath(root, intent.TransactionID), archivePath(root, intent.TransactionID), intentBytes)
}

func transactionIdentity(envelope *ApplyEnvelope, approval *Approval) string {
	approvalCopy := *approval
	approvalCopy.ApprovedAt = ""
	approvalCopy.ApprovalDigest = ""
	data, _ := canonicalJSON(approvalCopy)
	return sha256Hex([]byte("cognition-migration-transaction/v1\n" + envelope.EnvelopeDigest + "\n" + sha256Hex(data) + "\n"))
}

func newReceipt(intent *RecoveryIntent, set *cognition.Set) *MigrationReceipt {
	paths := []string{}
	for _, target := range intent.Envelope.VolumeTargets {
		paths = append(paths, target.Path)
	}
	paths = append(paths, intent.Envelope.Root.Path, intent.Envelope.Baseline.Path)
	receipt := &MigrationReceipt{
		Version: machinecontract.CognitionMigrationReceiptV2, Operation: OperationMigration, TransactionID: intent.TransactionID,
		SnapshotIdentity: intent.Envelope.Snapshot.SnapshotIdentity, MappingSHA256: intent.Envelope.MappingSHA256,
		ApprovalDigest: intent.Approval.ApprovalDigest, EnvelopeDigest: intent.Envelope.EnvelopeDigest,
		LegacySHA256: intent.Envelope.Snapshot.LegacySHA256, RootSHA256: intent.Envelope.Root.PostSHA256,
		BaselinePreimageSHA256: intent.Envelope.Snapshot.BaselineSHA256, BaselinePostimageSHA256: intent.Envelope.Baseline.PostSHA256,
		ProjectedCompositeIdentity: set.CompositeIdentity, FormalPostimagePaths: paths,
		ByteReversible: true, SemanticCoverageComplete: true, SemanticEquivalence: machinecontract.CognitionMigrationSemanticReviewed,
		CompletedAt: intent.Approval.ApprovedAt, NetworkAccessed: false,
	}
	receipt.ReceiptDigest, _ = receiptDigest(receipt)
	return receipt
}

func receiptDigest(receipt *MigrationReceipt) (string, error) {
	copyValue := *receipt
	copyValue.CompletedAt = ""
	copyValue.ReceiptDigest = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func saveReceipt(root string, receipt *MigrationReceipt) error {
	data, err := prettyJSON(receipt)
	if err != nil {
		return err
	}
	return cognitiontxn.SaveImmutable(receiptPath(root, receipt.TransactionID), data)
}

func loadReceipt(root, transactionID string) (*MigrationReceipt, error) {
	data, err := os.ReadFile(receiptPath(root, transactionID))
	if err != nil {
		return nil, err
	}
	receipt, err := DecodeReceipt(data)
	if err != nil {
		return nil, err
	}
	digest, err := receiptDigest(receipt)
	if err != nil || digest != receipt.ReceiptDigest || receipt.TransactionID != transactionID || !receipt.ByteReversible || !receipt.SemanticCoverageComplete {
		return nil, fmt.Errorf("migration_receipt_invalid")
	}
	return receipt, nil
}

func newApplyResult(intent *RecoveryIntent, receipt *MigrationReceipt) *ApplyResult {
	written := append([]string{}, intent.Envelope.WriteSet...)
	result := &ApplyResult{
		Version: machinecontract.CognitionMigrationApplyResultV2, Operation: OperationMigration, TransactionID: intent.TransactionID,
		Status: machinecontract.CognitionMigrationStatusApplied, ActiveLayout: "volumes", FormalComplete: true,
		WrittenPaths: written, RecoveredPaths: []string{}, BaselineSHA256: intent.Envelope.Baseline.PostSHA256,
		CompositeIdentity: intent.Envelope.ProjectedCompositeIdentity, ReceiptDigest: receipt.ReceiptDigest,
		NetworkAccessed: false, NextAction: "migration_complete",
	}
	result.ResultDigest, _ = applyResultDigest(result)
	return result
}

func applyResultDigest(result *ApplyResult) (string, error) {
	copyValue := *result
	copyValue.ResultDigest = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func saveResult(root string, result *ApplyResult) error {
	data, err := prettyJSON(result)
	if err != nil {
		return err
	}
	return cognitiontxn.SaveImmutable(resultPath(root, result.TransactionID), data)
}

func loadResultAt(path, transactionID string) (*ApplyResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result, err := DecodeApplyResult(data)
	if err != nil {
		return nil, err
	}
	digest, err := applyResultDigest(result)
	if err != nil || digest != result.ResultDigest || result.TransactionID != transactionID {
		return nil, fmt.Errorf("migration_result_invalid")
	}
	return result, nil
}

func intentPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "migration-"+transactionID+".json")
}

func archivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", "migration-"+transactionID+".json")
}

func snapshotPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "migration-"+transactionID, "snapshot.json")
}

func snapshotArchivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", "migration-"+transactionID+".snapshot.json")
}

func receiptPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "migration-"+transactionID, "receipt.json")
}

func resultPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "migration-"+transactionID, "result.json")
}

func rollbackPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "migration-"+transactionID, "rollback.json")
}
