package bootstrapapply

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbaseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var bootstrapFault = func(string) error { return nil }

// Apply starts or idempotently resumes one digest-bound Bootstrap.
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
		return nil, fmt.Errorf("bootstrap_write_lock_failed: %w", err)
	}
	defer lock.Release()
	if archived, archiveErr := loadRecoveryAt(archivePath(absRoot, transactionID), transactionID); archiveErr == nil {
		if archived.Envelope.EnvelopeDigest != envelope.EnvelopeDigest || archived.Approval.ApprovalDigest != approval.ApprovalDigest {
			return nil, fmt.Errorf("bootstrap_completed_transaction_binding_conflict")
		}
		result, err := loadCompletion(absRoot, transactionID)
		if err != nil {
			return nil, fmt.Errorf("bootstrap_completion_receipt_invalid: %w", err)
		}
		copyResult := *result
		copyResult.Status = StatusAlreadyApplied
		copyResult.NextAction = "none"
		return &copyResult, nil
	} else if !os.IsNotExist(archiveErr) {
		return nil, archiveErr
	}
	if err := rejectOtherPending(absRoot, transactionID); err != nil {
		return nil, err
	}
	if err := cognitionplan.ValidateExternalGuards(absRoot, &envelope.Plan); err != nil {
		return nil, fmt.Errorf("bootstrap_guard_drift: %w", err)
	}

	intent, loadErr := loadRecoveryAt(intentPath(absRoot, transactionID), transactionID)
	if os.IsNotExist(loadErr) {
		if approval.Mechanism == AutoApprovalMechanism {
			if err := validatePolicyBoundAuto(absRoot, envelope); err != nil {
				return nil, err
			}
		}
		replayed, prepareErr := Prepare(absRoot, &ApplyRequest{
			Version: envelope.RequestVersion, Plan: envelope.Plan, Candidate: envelope.Candidate,
			Preview: envelope.Preview, BaselineTimestamp: envelope.PreparedAt,
		})
		if prepareErr != nil || !reflect.DeepEqual(*replayed, *envelope) {
			return nil, fmt.Errorf("bootstrap_prepare_replay_mismatch: %v", prepareErr)
		}
		status, statusErr := inspectEnvelopeState(absRoot, transactionID, envelope, false)
		if statusErr != nil {
			return nil, statusErr
		}
		if status.Status != StatusPrepared {
			return nil, fmt.Errorf("bootstrap_preimage_conflict")
		}
		if err := ensureRuntimeBoundary(absRoot, envelope); err != nil {
			return nil, err
		}
		staging, err := stageEnvelope(absRoot, transactionID, envelope)
		if err != nil {
			return nil, err
		}
		if err := bootstrapFault("before_recovery_intent"); err != nil {
			return nil, err
		}
		intent = newRecoveryIntent(transactionID, envelope, approval, staging)
		if err := saveRecoveryIntent(absRoot, intent); err != nil {
			return nil, err
		}
		if err := bootstrapFault("after_recovery_intent"); err != nil {
			return nil, err
		}
	} else if loadErr != nil {
		return nil, loadErr
	} else if intent.Envelope.EnvelopeDigest != envelope.EnvelopeDigest || intent.Approval.ApprovalDigest != approval.ApprovalDigest {
		return nil, fmt.Errorf("bootstrap_pending_transaction_binding_conflict")
	}
	return advanceTransaction(absRoot, intent)
}

// Resume continues only the immutable active transaction identified by id.
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
	if os.IsNotExist(err) {
		archived, archiveErr := loadRecoveryAt(archivePath(absRoot, transactionID), transactionID)
		if archiveErr != nil {
			return nil, fmt.Errorf("bootstrap_transaction_not_found")
		}
		result, completionErr := loadCompletion(absRoot, transactionID)
		if completionErr != nil || archived.TransactionID != transactionID {
			return nil, fmt.Errorf("bootstrap_completed_transaction_invalid")
		}
		copyResult := *result
		copyResult.Status = StatusAlreadyApplied
		copyResult.NextAction = "none"
		return &copyResult, nil
	}
	if err != nil {
		return nil, err
	}
	if err := rejectOtherPending(absRoot, transactionID); err != nil {
		return nil, err
	}
	if err := cognitionplan.ValidateExternalGuards(absRoot, &intent.Envelope.Plan); err != nil {
		return nil, fmt.Errorf("bootstrap_guard_drift: %w", err)
	}
	return advanceTransaction(absRoot, intent)
}

func advanceTransaction(root string, intent *RecoveryIntent) (*ApplyResult, error) {
	status, err := inspectEnvelopeState(root, intent.TransactionID, &intent.Envelope, true)
	if err != nil {
		return nil, err
	}
	if status.Status == StatusRecoveryConflict {
		return nil, fmt.Errorf("bootstrap_recovery_conflict")
	}
	for _, target := range intent.Envelope.Targets {
		state := targetStatusByPath(status, target.Path)
		if state.DiskState == StatePostimage {
			continue
		}
		if state.DiskState != StatePreimage || state.StagingState != StatePostimage {
			return nil, fmt.Errorf("bootstrap_recovery_conflict: %s", target.Path)
		}
		if err := bootstrapFault("before_publish_" + target.AssetID); err != nil {
			return nil, err
		}
		data, err := readStagedPostimage(root, intent, target.Path)
		if err != nil {
			return nil, err
		}
		targetPath := filepath.Join(root, filepath.FromSlash(target.Path))
		if target.PreimageSHA256 != "" {
			if err := afs.AtomicWriteCAS(targetPath, data, target.PreimageSHA256); err != nil {
				return nil, fmt.Errorf("bootstrap_atomic_replace_failed[%s]: %w", target.Path, err)
			}
		} else if err := afs.AtomicCreateCASMode(targetPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("bootstrap_atomic_create_failed[%s]: %w", target.Path, err)
		}
		if err := bootstrapFault("after_publish_" + target.AssetID); err != nil {
			return nil, err
		}
		status, err = inspectEnvelopeState(root, intent.TransactionID, &intent.Envelope, true)
		if err != nil || status.Status == StatusRecoveryConflict {
			return nil, fmt.Errorf("bootstrap_post_publish_recovery_conflict: %s", target.Path)
		}
	}
	baselineState := targetStatusByPath(status, intent.Envelope.Baseline.Path)
	if baselineState.DiskState == StatePreimage {
		if baselineState.StagingState != StatePostimage {
			return nil, fmt.Errorf("bootstrap_baseline_staging_missing")
		}
		if err := bootstrapFault("before_publish_baseline"); err != nil {
			return nil, err
		}
		data, err := readStagedPostimage(root, intent, intent.Envelope.Baseline.Path)
		if err != nil {
			return nil, err
		}
		if err := afs.AtomicCreateCASMode(filepath.Join(root, ".aoci", "baseline.json"), data, 0o644); err != nil {
			return nil, fmt.Errorf("bootstrap_baseline_create_failed: %w", err)
		}
		if err := bootstrapFault("after_publish_baseline"); err != nil {
			return nil, err
		}
	} else if baselineState.DiskState != StatePostimage {
		return nil, fmt.Errorf("bootstrap_baseline_recovery_conflict")
	}

	if err := bootstrapFault("before_internal_verify"); err != nil {
		return nil, err
	}
	set, err := internalVerify(root, &intent.Envelope)
	if err != nil {
		return nil, fmt.Errorf("bootstrap_internal_verify_failed: %w", err)
	}
	if err := bootstrapFault("after_internal_verify"); err != nil {
		return nil, err
	}
	result := newApplyResult(intent, set)
	if err := bootstrapFault("before_completion_receipt"); err != nil {
		return nil, err
	}
	if err := saveCompletion(root, result); err != nil {
		return nil, err
	}
	if err := bootstrapFault("after_completion_receipt"); err != nil {
		return nil, err
	}
	if err := bootstrapFault("before_ledger"); err != nil {
		return nil, err
	}
	cfg, _ := config.LoadReadOnly(root)
	ledgerEnabled := true
	if cfg != nil {
		ledgerEnabled = cfg.LedgerEnabled
	}
	ledgerSource := ledger.SourceHuman
	if intent.Approval.Mechanism == AutoApprovalMechanism {
		ledgerSource = ledger.SourceAgent
	}
	if err := ensureBootstrapLedger(root, ledgerEnabled, ledger.Event{
		Op: "cognition_bootstrap_apply", Source: ledgerSource, Result: ledger.ResultOK,
		AppliedCount: len(intent.Envelope.Targets) + 1, RecoveryTransactionID: intent.TransactionID,
		BaselineSHA256: intent.Envelope.Baseline.PostSHA256, IndexSHA256: intent.Envelope.Targets[len(intent.Envelope.Targets)-1].PostSHA256,
	}); err != nil {
		return nil, err
	}
	if err := bootstrapFault("after_ledger"); err != nil {
		return nil, err
	}
	if err := bootstrapFault("before_transaction_archive"); err != nil {
		return nil, err
	}
	if err := archiveRecovery(root, intent); err != nil {
		return nil, err
	}
	if err := bootstrapFault("after_transaction_archive"); err != nil {
		return nil, err
	}
	return result, nil
}

func ensureBootstrapLedger(root string, enabled bool, expected ledger.Event) error {
	if !enabled {
		// D2-B remains backward compatible with configurations that disabled
		// Ledger before the shared D2 transaction kernel made it mandatory for
		// reversible migrations.
		return nil
	}
	return cognitiontxn.EnsureLedger(root, true, expected)
}

func internalVerify(root string, envelope *ApplyEnvelope) (*cognition.Set, error) {
	targets := make([]cognitionbaseline.FormalTarget, 0, len(envelope.Targets))
	for _, target := range envelope.Targets {
		targets = append(targets, cognitionbaseline.FormalTarget{Path: target.Path, SHA256: target.PostSHA256})
	}
	enabled := []string{}
	for _, descriptor := range envelope.Preview.ProjectedDescriptors {
		if descriptor.State == machinecontract.CognitionVolumeEnabled {
			enabled = append(enabled, descriptor.ID)
		}
	}
	return cognitionbaseline.VerifyVolumeState(root, envelope.ProjectedCompositeIdentity, envelope.Baseline.PostSHA256, targets, enabled)
}

func ensureRuntimeBoundary(root string, envelope *ApplyEnvelope) error {
	for _, rel := range []string{".aoci", ".aoci/transactions", ".aoci/transactions/history"} {
		if err := ensureSafeDirectory(root, rel); err != nil {
			return err
		}
	}
	path := filepath.Join(root, ".aoci", ".gitignore")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("bootstrap_runtime_gitignore_wrong_type")
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || sha256Hex(data) != envelope.RuntimeBoundary.PostSHA256 {
			return fmt.Errorf("bootstrap_runtime_gitignore_conflict")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := afs.AtomicCreateCAS(path, []byte(envelope.RuntimeBoundary.Content)); err != nil && !errors.Is(err, afs.ErrAtomicCreateConflict) {
		return err
	}
	return nil
}

func ensureSafeDirectory(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.ToSlash(relative), "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("bootstrap_runtime_path_invalid")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("bootstrap_runtime_directory_unsafe: %s", relative)
		}
	}
	return nil
}

func stageEnvelope(root, transactionID string, envelope *ApplyEnvelope) ([]StagingPostimage, error) {
	if err := ensureSafeDirectory(root, filepath.ToSlash(filepath.Join(".aoci", "transactions", "bootstrap-"+transactionID))); err != nil {
		return nil, err
	}
	if err := ensureSafeDirectory(root, filepath.ToSlash(filepath.Join(".aoci", "transactions", "bootstrap-"+transactionID, "staging"))); err != nil {
		return nil, err
	}
	posts := []struct {
		path, sha string
		data      []byte
	}{}
	for _, target := range envelope.Targets {
		posts = append(posts, struct {
			path, sha string
			data      []byte
		}{target.Path, target.PostSHA256, []byte(target.Content)})
	}
	posts = append(posts, struct {
		path, sha string
		data      []byte
	}{envelope.Baseline.Path, envelope.Baseline.PostSHA256, []byte(envelope.Baseline.Content)})
	staging := make([]StagingPostimage, 0, len(posts))
	for index, post := range posts {
		rel := filepath.ToSlash(filepath.Join(".aoci", "transactions", "bootstrap-"+transactionID, "staging", fmt.Sprintf("%02d.post", index)))
		path := filepath.Join(root, filepath.FromSlash(rel))
		if state, _, _ := classifyPath(path, post.sha); state == StatePostimage {
			staging = append(staging, StagingPostimage{Path: post.path, SHA256: post.sha, ByteSize: int64(len(post.data)), StagingRel: rel})
			continue
		} else if state != StatePreimage {
			return nil, fmt.Errorf("bootstrap_staging_conflict: %s", rel)
		}
		if err := bootstrapFault("before_stage_" + fmt.Sprint(index)); err != nil {
			return nil, err
		}
		if err := afs.AtomicCreateCAS(path, post.data); err != nil {
			return nil, err
		}
		if err := bootstrapFault("after_stage_" + fmt.Sprint(index)); err != nil {
			return nil, err
		}
		staging = append(staging, StagingPostimage{Path: post.path, SHA256: post.sha, ByteSize: int64(len(post.data)), StagingRel: rel})
	}
	return staging, nil
}

func readStagedPostimage(root string, intent *RecoveryIntent, path string) ([]byte, error) {
	for _, staged := range intent.Staging {
		if staged.Path != path {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(staged.StagingRel)))
		if err != nil || sha256Hex(data) != staged.SHA256 || int64(len(data)) != staged.ByteSize {
			return nil, fmt.Errorf("bootstrap_staging_invalid: %s", path)
		}
		return data, nil
	}
	return nil, fmt.Errorf("bootstrap_staging_missing: %s", path)
}

func newRecoveryIntent(transactionID string, envelope *ApplyEnvelope, approval *Approval, staging []StagingPostimage) *RecoveryIntent {
	intent := &RecoveryIntent{
		Version: machinecontract.CognitionBootstrapRecoveryV1, TransactionID: transactionID,
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
	path := intentPath(root, intent.TransactionID)
	if err := afs.AtomicCreateCAS(path, data); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return err
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
	if digestErr != nil || intent.TransactionID != transactionID || digest != intent.RecoveryDigest ||
		validateEnvelope(&intent.Envelope) != nil || validateApproval(&intent.Envelope, &intent.Approval) != nil ||
		transactionIdentity(&intent.Envelope, &intent.Approval) != transactionID {
		return nil, fmt.Errorf("bootstrap_recovery_binding_invalid")
	}
	return intent, nil
}

func archiveRecovery(root string, intent *RecoveryIntent) error {
	active := intentPath(root, intent.TransactionID)
	archive := archivePath(root, intent.TransactionID)
	data, err := os.ReadFile(active)
	if err != nil {
		if os.IsNotExist(err) {
			if existing, archiveErr := os.ReadFile(archive); archiveErr == nil {
				want, _ := prettyJSON(intent)
				if bytes.Equal(existing, want) {
					return nil
				}
			}
		}
		return err
	}
	if existing, err := os.ReadFile(archive); err == nil {
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("bootstrap_recovery_archive_conflict")
		}
	} else if os.IsNotExist(err) {
		if err := afs.AtomicCreateCAS(archive, data); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := os.Remove(active); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func transactionIdentity(envelope *ApplyEnvelope, approval *Approval) string {
	return sha256Hex([]byte("cognition-bootstrap-transaction/v1\n" + envelope.EnvelopeDigest + "\n" + approval.ApprovalDigest + "\n"))
}

func intentPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "bootstrap-"+transactionID+".json")
}

func archivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", "bootstrap-"+transactionID+".json")
}

func completionPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "bootstrap-"+transactionID, "completion.json")
}

func rollbackPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "bootstrap-"+transactionID, "rollback.json")
}

func rejectOtherPending(root, allowedTransactionID string) error {
	return cognitiontxn.RejectOtherPending(root, "bootstrap-"+allowedTransactionID+".json")
}

func newApplyResult(intent *RecoveryIntent, set *cognition.Set) *ApplyResult {
	written := append([]string{}, intent.Envelope.WriteSet...)
	result := &ApplyResult{
		Version: machinecontract.CognitionBootstrapApplyResultV1, Operation: OperationBootstrap,
		TransactionID: intent.TransactionID, Status: StatusApplied, LayoutActivated: true, FormalComplete: true,
		WrittenPaths: written, RecoveredPaths: []string{}, BaselineSHA256: intent.Envelope.Baseline.PostSHA256,
		CompositeIdentity: set.CompositeIdentity, NetworkAccessed: false, NextAction: "bootstrap_complete",
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

func saveCompletion(root string, result *ApplyResult) error {
	data, err := prettyJSON(result)
	if err != nil {
		return err
	}
	path := completionPath(root, result.TransactionID)
	if err := afs.AtomicCreateCAS(path, data); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return err
	}
	return nil
}

func loadCompletion(root, transactionID string) (*ApplyResult, error) {
	data, err := os.ReadFile(completionPath(root, transactionID))
	if err != nil {
		return nil, err
	}
	var result ApplyResult
	if err := strictDecode(data, &result); err != nil || result.Version != machinecontract.CognitionBootstrapApplyResultV1 || result.TransactionID != transactionID {
		return nil, fmt.Errorf("bootstrap_completion_invalid")
	}
	digest, err := applyResultDigest(&result)
	if err != nil || digest != result.ResultDigest {
		return nil, fmt.Errorf("bootstrap_completion_digest_invalid")
	}
	return &result, nil
}

func targetStatusByPath(status *TransactionStatus, path string) TargetStatus {
	for _, target := range status.Targets {
		if target.Path == path {
			return target
		}
	}
	return TargetStatus{Path: path, DiskState: StateUnknown, StagingState: StateMissingStaging}
}
