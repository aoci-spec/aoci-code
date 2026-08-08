package migrationapply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type reversalRecovery struct {
	Version        string                         `json:"version"`
	Operation      string                         `json:"operation"`
	TransactionID  string                         `json:"transaction_id"`
	Plan           ReversalPlan                   `json:"plan"`
	Approval       ReversalApproval               `json:"approval"`
	Envelope       ApplyEnvelope                  `json:"original_envelope"`
	Staging        []cognitiontxn.StagedPostimage `json:"staging"`
	RecoveryDigest string                         `json:"recovery_digest"`
}

// PrepareReversal creates a new independently reviewable reversal plan. It is
// eligible only while every migration postimage and governance guard is still
// exact and the Ledger proves that no later cognition mutation occurred.
func PrepareReversal(root, originalTransactionID, preparedAt string) (*ReversalPlan, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := validateUTC(preparedAt); err != nil {
		return nil, err
	}
	if pending, err := cognitiontxn.Pending(absRoot); err != nil || len(pending) != 0 {
		return nil, fmt.Errorf("migration_reversal_pending_transaction")
	}
	intent, err := loadRecoveryAt(archivePath(absRoot, originalTransactionID), originalTransactionID)
	if err != nil {
		return nil, fmt.Errorf("migration_reversal_receipt_not_found")
	}
	receipt, err := loadReceipt(absRoot, originalTransactionID)
	if err != nil {
		return nil, fmt.Errorf("migration_reversal_receipt_invalid")
	}
	if err := cognitionplan.ValidateExternalGuards(absRoot, &intent.Envelope.Plan); err != nil {
		return nil, fmt.Errorf("migration_reversal_guard_drift: %w", err)
	}
	if err := ensureNoLaterCognitionMutation(absRoot, originalTransactionID); err != nil {
		return nil, err
	}
	postimages, risks := exactMigrationPostimages(absRoot, &intent.Envelope)
	plan := &ReversalPlan{
		Version: machinecontract.CognitionMigrationReversalPlanV2, Operation: OperationReversal,
		OriginalTransactionID: originalTransactionID, OriginalReceiptDigest: receipt.ReceiptDigest,
		SnapshotIdentity: intent.Envelope.Snapshot.SnapshotIdentity, RepositoryIdentity: intent.Envelope.RepositoryIdentity,
		InventoryIdentity: intent.Envelope.InventoryIdentity, SourceEvidenceIdentity: intent.Envelope.SourceEvidenceIdentity,
		CurationIdentity: intent.Envelope.CurationIdentity, RegistryIdentity: intent.Envelope.RegistryIdentity,
		CurrentPostimages: postimages,
		WriteSet:          append([]string{}, intent.Envelope.WriteSet[1:]...),
		WriteOrder:        reversalWriteOrder(&intent.Envelope),
		RecoveryDirection: machinecontract.CognitionMigrationReversalDirection, Eligible: len(risks) == 0,
		Risks: risks, PreparedAt: preparedAt, NetworkAccessed: false,
	}
	plan.PlanDigest, _ = reversalPlanDigest(plan)
	return plan, nil
}

func reversalWriteOrder(envelope *ApplyEnvelope) []string {
	result := []string{"aoci.txt", ".aoci/baseline.json"}
	for _, path := range []string{"aoci.database.txt", "aoci.code.txt", "aoci.meta.txt"} {
		for _, target := range envelope.VolumeTargets {
			if target.Path == path {
				result = append(result, path)
			}
		}
	}
	return result
}

func exactMigrationPostimages(root string, envelope *ApplyEnvelope) ([]SnapshotPreimage, []string) {
	targets := append([]FormalPostimage{}, envelope.VolumeTargets...)
	targets = append(targets, envelope.Root)
	result := make([]SnapshotPreimage, 0, len(targets)+1)
	risks := []string{}
	for _, target := range targets {
		state, actual, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(target.Path)), "", target.PostSHA256, true)
		if err != nil || state != cognitiontxn.StatePostimage {
			risks = append(risks, "formal_postimage_drift:"+target.Path)
		}
		result = append(result, SnapshotPreimage{Path: target.Path, State: state, SHA256: actual, ByteSize: target.ByteSize, FileMode: target.FileMode})
	}
	state, actual, err := cognitiontxn.Classify(filepath.Join(root, ".aoci", "baseline.json"), "", envelope.Baseline.PostSHA256, true)
	if err != nil || state != cognitiontxn.StatePostimage {
		risks = append(risks, "formal_postimage_drift:.aoci/baseline.json")
	}
	result = append(result, SnapshotPreimage{Path: ".aoci/baseline.json", State: state, SHA256: actual, ByteSize: envelope.Baseline.ByteSize, FileMode: envelope.Baseline.FileMode})
	return result, risks
}

func ensureNoLaterCognitionMutation(root, transactionID string) error {
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("migration_reversal_ledger_corrupt")
	}
	found := false
	for _, event := range events {
		if event.RecoveryTransactionID == transactionID && event.Op == "cognition_migration_apply" && event.Result == ledger.ResultOK {
			found = true
			continue
		}
		if found && (event.AppliedCount > 0 || event.RecoveredCount > 0 || event.PostIndexSHA256 != "") {
			return fmt.Errorf("migration_reversal_later_cognition_write")
		}
	}
	if !found {
		return fmt.Errorf("migration_reversal_ledger_proof_missing")
	}
	return nil
}

func reversalPlanDigest(plan *ReversalPlan) (string, error) {
	copyValue := *plan
	copyValue.PreparedAt = ""
	copyValue.PlanDigest = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateReversalPlan(plan *ReversalPlan) error {
	if plan == nil || plan.Version != machinecontract.CognitionMigrationReversalPlanV2 || plan.Operation != OperationReversal ||
		!plan.Eligible || len(plan.Risks) != 0 || plan.NetworkAccessed || plan.RecoveryDirection != machinecontract.CognitionMigrationReversalDirection {
		return fmt.Errorf("migration_reversal_plan_not_eligible")
	}
	digest, err := reversalPlanDigest(plan)
	if err != nil || digest != plan.PlanDigest {
		return fmt.Errorf("migration_reversal_plan_digest_mismatch")
	}
	return validateUTC(plan.PreparedAt)
}

func RecordReversalApproval(plan *ReversalPlan, actor, approvedAt, confirmedPlanDigest string) (*ReversalApproval, error) {
	if err := validateReversalPlan(plan); err != nil {
		return nil, err
	}
	if confirmedPlanDigest != plan.PlanDigest || !cognitiontxn.ValidAuditActor(strings.TrimSpace(actor)) {
		return nil, fmt.Errorf("migration_reversal_approval_confirmation_invalid")
	}
	if err := validateUTC(approvedAt); err != nil {
		return nil, err
	}
	approval := &ReversalApproval{
		Version: machinecontract.CognitionMigrationReversalApprovalV2, Operation: OperationReversal,
		ReversalPlanDigest: plan.PlanDigest, ApprovedWriteSet: append([]string{}, plan.WriteSet...),
		ApprovedPolicy: machinecontract.CognitionMigrationReversalPolicy, Actor: strings.TrimSpace(actor),
		Mechanism: machinecontract.CognitionMigrationApprovalMechanism, ApprovedAt: approvedAt,
	}
	approval.ApprovalDigest, _ = reversalApprovalDigest(approval)
	return approval, nil
}

func reversalApprovalDigest(approval *ReversalApproval) (string, error) {
	copyValue := *approval
	copyValue.ApprovalDigest = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateReversalApproval(plan *ReversalPlan, approval *ReversalApproval) error {
	if approval == nil || approval.Version != machinecontract.CognitionMigrationReversalApprovalV2 || approval.Operation != OperationReversal ||
		approval.ReversalPlanDigest != plan.PlanDigest || approval.ApprovedPolicy != machinecontract.CognitionMigrationReversalPolicy ||
		approval.Mechanism != machinecontract.CognitionMigrationApprovalMechanism || !cognitiontxn.ValidAuditActor(approval.Actor) ||
		!reflect.DeepEqual(approval.ApprovedWriteSet, plan.WriteSet) {
		return fmt.Errorf("migration_reversal_approval_invalid")
	}
	digest, err := reversalApprovalDigest(approval)
	if err != nil || digest != approval.ApprovalDigest {
		return fmt.Errorf("migration_reversal_approval_digest_mismatch")
	}
	return validateUTC(approval.ApprovedAt)
}

func ApplyReversal(root string, plan *ReversalPlan, approval *ReversalApproval) (*ApplyResult, error) {
	if err := validateReversalPlan(plan); err != nil {
		return nil, err
	}
	if err := validateReversalApproval(plan, approval); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	transactionID := reversalTransactionIdentity(plan, approval)
	lock, err := afs.AcquireIndexLock(absRoot)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	_, pendingErr := os.Lstat(reversalIntentPath(absRoot, transactionID))
	if errors.Is(pendingErr, os.ErrNotExist) {
		if result, err := loadResultAt(reversalResultPath(absRoot, transactionID), transactionID); err == nil {
			copyResult := *result
			copyResult.Status = machinecontract.CognitionMigrationStatusAlreadyReversed
			return &copyResult, nil
		}
	}
	if err := cognitiontxn.RejectOtherPending(absRoot, "reversal-"+transactionID+".json"); err != nil {
		return nil, err
	}
	intent, err := loadReversalRecovery(reversalIntentPath(absRoot, transactionID), transactionID)
	if errors.Is(err, os.ErrNotExist) {
		current, prepareErr := PrepareReversal(absRoot, plan.OriginalTransactionID, plan.PreparedAt)
		if prepareErr != nil || !reflect.DeepEqual(*current, *plan) {
			return nil, fmt.Errorf("migration_reversal_plan_superseded")
		}
		original, loadErr := loadRecoveryAt(archivePath(absRoot, plan.OriginalTransactionID), plan.OriginalTransactionID)
		if loadErr != nil {
			return nil, loadErr
		}
		legacyBytes, _ := decodeSnapshotContent(original.Envelope.Snapshot.LegacyContentBase64, original.Envelope.Snapshot.LegacySHA256)
		baselineBytes, _ := decodeSnapshotContent(original.Envelope.Snapshot.BaselineContentBase64, original.Envelope.Snapshot.BaselineSHA256)
		staging, stageErr := cognitiontxn.Stage(absRoot, "reversal", transactionID, []cognitiontxn.Postimage{
			{Path: "aoci.txt", SHA: original.Envelope.Snapshot.LegacySHA256, Data: legacyBytes},
			{Path: ".aoci/baseline.json", SHA: original.Envelope.Snapshot.BaselineSHA256, Data: baselineBytes},
		}, migrationFault)
		if stageErr != nil {
			return nil, stageErr
		}
		intent = &reversalRecovery{Version: machinecontract.CognitionMigrationReversalRecoveryV2, Operation: OperationReversal,
			TransactionID: transactionID, Plan: *plan, Approval: *approval, Envelope: original.Envelope, Staging: staging}
		intent.RecoveryDigest, _ = reversalRecoveryDigest(intent)
		if saveErr := saveReversalRecovery(absRoot, intent); saveErr != nil {
			return nil, saveErr
		}
	} else if err != nil {
		return nil, err
	}
	return advanceReversal(absRoot, intent)
}

func ResumeReversal(root, transactionID string) (*ApplyResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lock, err := afs.AcquireIndexLock(absRoot)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	intent, err := loadReversalRecovery(reversalIntentPath(absRoot, transactionID), transactionID)
	if errors.Is(err, os.ErrNotExist) {
		result, loadErr := loadResultAt(reversalResultPath(absRoot, transactionID), transactionID)
		if loadErr != nil {
			return nil, fmt.Errorf("migration_reversal_transaction_not_found")
		}
		copyResult := *result
		copyResult.Status = machinecontract.CognitionMigrationStatusAlreadyReversed
		return &copyResult, nil
	}
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.RejectOtherPending(absRoot, "reversal-"+transactionID+".json"); err != nil {
		return nil, err
	}
	return advanceReversal(absRoot, intent)
}

// ReversalStatus derives progress from the two exact CAS replacements and the
// no-replace recovery moves. The immutable intent is only an identity source.
func ReversalStatus(root, transactionID string) (*TransactionStatus, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if transactionID == "" {
		pending, err := cognitiontxn.PendingForOperation(absRoot, "reversal")
		if err != nil || len(pending) != 1 {
			return nil, fmt.Errorf("migration_reversal_transaction_id_required")
		}
		transactionID = pending[0]
	}
	intent, err := loadReversalRecovery(reversalIntentPath(absRoot, transactionID), transactionID)
	active := err == nil
	if errors.Is(err, os.ErrNotExist) {
		intent, err = loadReversalRecovery(reversalArchivePath(absRoot, transactionID), transactionID)
	}
	if err != nil {
		return nil, fmt.Errorf("migration_reversal_transaction_not_found")
	}
	status := &TransactionStatus{Version: machinecontract.CognitionMigrationTransactionStatusV2, Operation: OperationReversal,
		TransactionID: transactionID, Status: machinecontract.CognitionMigrationStatusRecoveryRequiredVolumes,
		ActiveLayout: "volumes", RecoveryPending: active, Targets: []TargetStatus{}, NextActions: []string{"resume"}, NetworkAccessed: false}
	conflict := false
	rootState, rootActual, _ := cognitiontxn.Classify(filepath.Join(absRoot, "aoci.txt"), intent.Envelope.Root.PostSHA256, intent.Envelope.Snapshot.LegacySHA256, false)
	baselineState, baselineActual, _ := cognitiontxn.Classify(filepath.Join(absRoot, ".aoci", "baseline.json"), intent.Envelope.Baseline.PostSHA256, intent.Envelope.Snapshot.BaselineSHA256, false)
	status.Targets = append(status.Targets,
		TargetStatus{Path: "aoci.txt", Kind: "root", DiskState: rootState, ActualSHA256: rootActual},
		TargetStatus{Path: ".aoci/baseline.json", Kind: "baseline", DiskState: baselineState, ActualSHA256: baselineActual})
	if rootState == cognitiontxn.StatePostimage {
		status.ActiveLayout = "legacy"
	}
	if (rootState != cognitiontxn.StatePreimage && rootState != cognitiontxn.StatePostimage) ||
		(baselineState != cognitiontxn.StatePreimage && baselineState != cognitiontxn.StatePostimage) ||
		(rootState == cognitiontxn.StatePreimage && baselineState == cognitiontxn.StatePostimage) {
		conflict = true
	}
	allMoved := true
	for index, relative := range []string{"aoci.database.txt", "aoci.code.txt", "aoci.meta.txt"} {
		var target *FormalPostimage
		for targetIndex := range intent.Envelope.VolumeTargets {
			if intent.Envelope.VolumeTargets[targetIndex].Path == relative {
				target = &intent.Envelope.VolumeTargets[targetIndex]
			}
		}
		if target == nil {
			continue
		}
		sourceState, actual, _ := cognitiontxn.Classify(filepath.Join(absRoot, relative), "", target.PostSHA256, true)
		archiveState, _, _ := cognitiontxn.Classify(filepath.Join(absRoot, ".aoci", "transactions", "reversal-"+transactionID, "recovered", fmt.Sprintf("%02d-%s", index, filepath.Base(relative))), "", target.PostSHA256, true)
		diskState := cognitiontxn.StateUnknown
		if sourceState == cognitiontxn.StatePostimage && archiveState == cognitiontxn.StatePreimage {
			diskState = cognitiontxn.StatePreimage
			allMoved = false
		} else if sourceState == cognitiontxn.StatePreimage && archiveState == cognitiontxn.StatePostimage {
			diskState = cognitiontxn.StatePostimage
		} else {
			conflict = true
		}
		status.Targets = append(status.Targets, TargetStatus{Path: relative, Kind: target.Kind, DiskState: diskState, ActualSHA256: actual})
	}
	status.ThirdPartyConflict = conflict
	status.FormalComplete = rootState == cognitiontxn.StatePostimage && baselineState == cognitiontxn.StatePostimage && allMoved
	if conflict {
		status.Status = machinecontract.CognitionMigrationStatusRecoveryConflict
		status.NextActions = []string{"resolve_third_party_conflict"}
	} else if !active {
		status.Status = machinecontract.CognitionMigrationStatusReversed
		status.ActiveLayout = "legacy"
		status.NextActions = []string{"none"}
	} else if status.ActiveLayout == "legacy" {
		status.Status = machinecontract.CognitionMigrationStatusRecoveryRequiredLegacy
	}
	status.StatusDigest = ""
	data, _ := canonicalJSON(status)
	status.StatusDigest = sha256Hex(data)
	return status, nil
}

func advanceReversal(root string, intent *reversalRecovery) (*ApplyResult, error) {
	if err := validateActiveReversalGuards(root, intent); err != nil {
		return nil, err
	}
	rootState, _, err := cognitiontxn.Classify(filepath.Join(root, "aoci.txt"), intent.Envelope.Root.PostSHA256, intent.Envelope.Snapshot.LegacySHA256, false)
	if err != nil || (rootState != cognitiontxn.StatePreimage && rootState != cognitiontxn.StatePostimage) {
		return nil, fmt.Errorf("migration_reversal_recovery_conflict: aoci.txt")
	}
	baselineState, _, err := cognitiontxn.Classify(filepath.Join(root, ".aoci", "baseline.json"), intent.Envelope.Baseline.PostSHA256, intent.Envelope.Snapshot.BaselineSHA256, false)
	if err != nil || (baselineState != cognitiontxn.StatePreimage && baselineState != cognitiontxn.StatePostimage) {
		return nil, fmt.Errorf("migration_reversal_recovery_conflict: .aoci/baseline.json")
	}
	if rootState == cognitiontxn.StatePreimage {
		data, err := cognitiontxn.ReadStaged(root, intent.Staging, "aoci.txt")
		if err != nil {
			return nil, err
		}
		if err := migrationFault("before_reversal_root"); err != nil {
			return nil, err
		}
		if err := afs.AtomicWriteCAS(filepath.Join(root, "aoci.txt"), data, intent.Envelope.Root.PostSHA256); err != nil {
			return nil, err
		}
		if err := migrationFault("after_reversal_root"); err != nil {
			return nil, err
		}
	}
	if baselineState == cognitiontxn.StatePreimage {
		data, err := cognitiontxn.ReadStaged(root, intent.Staging, ".aoci/baseline.json")
		if err != nil {
			return nil, err
		}
		if err := migrationFault("before_reversal_baseline"); err != nil {
			return nil, err
		}
		if err := afs.AtomicWriteCAS(filepath.Join(root, ".aoci", "baseline.json"), data, intent.Envelope.Baseline.PostSHA256); err != nil {
			return nil, err
		}
		if err := migrationFault("after_reversal_baseline"); err != nil {
			return nil, err
		}
	}
	archiveDir := filepath.Join(root, ".aoci", "transactions", "reversal-"+intent.TransactionID, "recovered")
	if err := cognitiontxn.EnsureSafeDirectory(root, filepath.ToSlash(filepath.Join(".aoci", "transactions", "reversal-"+intent.TransactionID, "recovered"))); err != nil {
		return nil, err
	}
	recovered := []string{}
	for index, relative := range []string{"aoci.database.txt", "aoci.code.txt", "aoci.meta.txt"} {
		var target *FormalPostimage
		for targetIndex := range intent.Envelope.VolumeTargets {
			if intent.Envelope.VolumeTargets[targetIndex].Path == relative {
				target = &intent.Envelope.VolumeTargets[targetIndex]
			}
		}
		if target == nil {
			continue
		}
		recovered = append(recovered, relative)
		destination := filepath.Join(archiveDir, fmt.Sprintf("%02d-%s", index, filepath.Base(relative)))
		if existing, err := os.ReadFile(destination); err == nil {
			if sha256Hex(existing) != target.PostSHA256 {
				return nil, fmt.Errorf("migration_reversal_archive_conflict: %s", relative)
			}
			if _, statErr := os.Lstat(filepath.Join(root, relative)); !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("migration_reversal_recovery_conflict: %s", relative)
			}
			continue
		}
		state, actual, _ := cognitiontxn.Classify(filepath.Join(root, relative), "", target.PostSHA256, true)
		if state != cognitiontxn.StatePostimage {
			return nil, fmt.Errorf("migration_reversal_recovery_conflict: %s", relative)
		}
		if err := migrationFault("before_reversal_" + target.Kind); err != nil {
			return nil, err
		}
		if err := afs.AtomicMoveCAS(filepath.Join(root, relative), destination, actual); err != nil {
			return nil, err
		}
		if err := migrationFault("after_reversal_" + target.Kind); err != nil {
			return nil, err
		}
	}
	if err := verifyLegacyPreimage(root, &intent.Envelope.Snapshot); err != nil {
		return nil, err
	}
	result := &ApplyResult{Version: machinecontract.CognitionMigrationApplyResultV2, Operation: OperationReversal,
		TransactionID: intent.TransactionID, Status: machinecontract.CognitionMigrationStatusReversed, ActiveLayout: "legacy",
		FormalComplete: true, WrittenPaths: []string{"aoci.txt", ".aoci/baseline.json"}, RecoveredPaths: recovered,
		BaselineSHA256: intent.Envelope.Snapshot.BaselineSHA256, NetworkAccessed: false, NextAction: "reversal_complete"}
	result.ResultDigest, _ = applyResultDigest(result)
	data, _ := prettyJSON(result)
	if err := migrationFault("before_reversal_receipt"); err != nil {
		return nil, err
	}
	if err := cognitiontxn.SaveImmutable(reversalResultPath(root, intent.TransactionID), data); err != nil {
		return nil, err
	}
	if err := migrationFault("after_reversal_receipt"); err != nil {
		return nil, err
	}
	cfg, _ := config.LoadReadOnly(root)
	if err := migrationFault("before_reversal_ledger"); err != nil {
		return nil, err
	}
	if err := cognitiontxn.EnsureLedger(root, cfg != nil && cfg.LedgerEnabled, ledger.Event{Op: "cognition_migration_reversal", Source: ledger.SourceHuman,
		Result: ledger.ResultOK, AppliedCount: 2, RecoveredCount: len(recovered), RecoveryTransactionID: intent.TransactionID,
		PreIndexSHA256: intent.Envelope.Root.PostSHA256, PostIndexSHA256: intent.Envelope.Snapshot.LegacySHA256,
		BaselineSHA256: intent.Envelope.Snapshot.BaselineSHA256, IndexSHA256: intent.Envelope.Snapshot.LegacySHA256}); err != nil {
		return nil, err
	}
	if err := migrationFault("after_reversal_ledger"); err != nil {
		return nil, err
	}
	intentBytes, _ := prettyJSON(intent)
	if err := migrationFault("before_reversal_archive"); err != nil {
		return nil, err
	}
	if err := cognitiontxn.ArchiveImmutable(reversalIntentPath(root, intent.TransactionID), reversalArchivePath(root, intent.TransactionID), intentBytes); err != nil {
		return nil, err
	}
	if err := migrationFault("after_reversal_archive"); err != nil {
		return nil, err
	}
	return result, nil
}

func validateActiveReversalGuards(root string, intent *reversalRecovery) error {
	if err := cognitionplan.ValidateExternalGuards(root, &intent.Envelope.Plan); err != nil {
		return fmt.Errorf("migration_reversal_guard_drift: %w", err)
	}
	receipt, err := loadReceipt(root, intent.Plan.OriginalTransactionID)
	if err != nil || receipt.ReceiptDigest != intent.Plan.OriginalReceiptDigest {
		return fmt.Errorf("migration_reversal_receipt_drift")
	}
	if err := ensureNoLaterCognitionMutationForReversal(root, intent.Plan.OriginalTransactionID, intent.TransactionID); err != nil {
		return err
	}
	return nil
}

func ensureNoLaterCognitionMutationForReversal(root, originalTransactionID, reversalTransactionID string) error {
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("migration_reversal_ledger_corrupt")
	}
	found := false
	for _, event := range events {
		if event.RecoveryTransactionID == originalTransactionID && event.Op == "cognition_migration_apply" && event.Result == ledger.ResultOK {
			found = true
			continue
		}
		if !found {
			continue
		}
		if event.RecoveryTransactionID == reversalTransactionID && event.Op == "cognition_migration_reversal" && event.Result == ledger.ResultOK {
			continue
		}
		if event.AppliedCount > 0 || event.RecoveredCount > 0 || event.PostIndexSHA256 != "" {
			return fmt.Errorf("migration_reversal_later_cognition_write")
		}
	}
	if !found {
		return fmt.Errorf("migration_reversal_ledger_proof_missing")
	}
	return nil
}

func reversalTransactionIdentity(plan *ReversalPlan, approval *ReversalApproval) string {
	copyApproval := *approval
	copyApproval.ApprovedAt = ""
	copyApproval.ApprovalDigest = ""
	data, _ := canonicalJSON(copyApproval)
	return sha256Hex([]byte("cognition-migration-reversal/v1\n" + plan.PlanDigest + "\n" + sha256Hex(data) + "\n"))
}

func reversalRecoveryDigest(intent *reversalRecovery) (string, error) {
	copyValue := *intent
	copyValue.RecoveryDigest = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func saveReversalRecovery(root string, intent *reversalRecovery) error {
	data, err := prettyJSON(intent)
	if err != nil {
		return err
	}
	return cognitiontxn.SaveImmutable(reversalIntentPath(root, intent.TransactionID), data)
}

func loadReversalRecovery(path, transactionID string) (*reversalRecovery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var intent reversalRecovery
	if err := strictDecode(data, &intent); err != nil {
		return nil, err
	}
	digest, digestErr := reversalRecoveryDigest(&intent)
	if digestErr != nil || intent.Version != machinecontract.CognitionMigrationReversalRecoveryV2 || intent.Operation != OperationReversal ||
		intent.TransactionID != transactionID || digest != intent.RecoveryDigest || validateReversalPlan(&intent.Plan) != nil ||
		validateReversalApproval(&intent.Plan, &intent.Approval) != nil || reversalTransactionIdentity(&intent.Plan, &intent.Approval) != transactionID {
		return nil, fmt.Errorf("migration_reversal_recovery_invalid")
	}
	return &intent, nil
}

func reversalIntentPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "reversal-"+transactionID+".json")
}

func reversalArchivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", "reversal-"+transactionID+".json")
}

func reversalResultPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "reversal-"+transactionID, "result.json")
}
