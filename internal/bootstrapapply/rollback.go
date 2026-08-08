package bootstrapapply

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Rollback only reverses an incomplete, active Bootstrap. Completed layouts
// require a future lifecycle Plan and are intentionally outside D2-B.
func Rollback(root, transactionID string) (*ApplyResult, error) {
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
	if err != nil {
		return nil, fmt.Errorf("bootstrap_incomplete_transaction_required")
	}
	if _, err := loadCompletion(absRoot, transactionID); err == nil {
		return nil, fmt.Errorf("bootstrap_completed_transaction_rollback_forbidden")
	}
	if err := rejectOtherPending(absRoot, transactionID); err != nil {
		return nil, err
	}
	if err := cognitionplan.ValidateExternalGuards(absRoot, &intent.Envelope.Plan); err != nil {
		return nil, fmt.Errorf("bootstrap_guard_drift: %w", err)
	}
	status, err := inspectEnvelopeState(absRoot, transactionID, &intent.Envelope, true)
	if err != nil {
		return nil, err
	}
	if status.Status == StatusRecoveryConflict {
		return nil, fmt.Errorf("bootstrap_recovery_conflict")
	}
	if err := ensureSafeDirectory(absRoot, filepath.ToSlash(filepath.Join(".aoci", "transactions", "bootstrap-"+transactionID, "recovered"))); err != nil {
		return nil, err
	}

	order := []string{}
	if status.LayoutActivated {
		order = append(order, "aoci.txt", ".aoci/baseline.json")
	}
	order = append(order, "aoci.database.txt", "aoci.code.txt", "aoci.meta.txt")
	recovered := []string{}
	for index, path := range order {
		target, declared := declaredTargetStatus(status, path)
		if !declared || target.DiskState == StatePreimage {
			continue
		}
		if target.DiskState != StatePostimage {
			return nil, fmt.Errorf("bootstrap_recovery_conflict: %s", path)
		}
		recoveryTarget := filepath.Join(absRoot, ".aoci", "transactions", "bootstrap-"+transactionID, "recovered", fmt.Sprintf("%02d-%s", index, filepath.Base(path)))
		if err := bootstrapFault("before_rollback_" + target.Kind); err != nil {
			return nil, err
		}
		formalTarget := formalPostimageByPath(&intent.Envelope, path)
		if formalTarget != nil && formalTarget.PreimageSHA256 != "" {
			if err := afs.AtomicWriteCAS(filepath.Join(absRoot, filepath.FromSlash(path)), []byte(formalTarget.PreimageContent), target.ActualSHA256); err != nil {
				return nil, fmt.Errorf("bootstrap_rollback_restore_failed[%s]: %w", path, err)
			}
		} else if err := afs.AtomicMoveCAS(filepath.Join(absRoot, filepath.FromSlash(path)), recoveryTarget, target.ActualSHA256); err != nil {
			return nil, fmt.Errorf("bootstrap_rollback_move_failed[%s]: %w", path, err)
		}
		if err := bootstrapFault("after_rollback_" + target.Kind); err != nil {
			return nil, err
		}
		recovered = append(recovered, path)
	}
	postStatus, err := inspectEnvelopeState(absRoot, transactionID, &intent.Envelope, true)
	if err != nil {
		return nil, err
	}
	for _, target := range postStatus.Targets {
		if target.DiskState != StatePreimage {
			return nil, fmt.Errorf("bootstrap_rollback_incomplete: %s", target.Path)
		}
	}
	result := &ApplyResult{
		Version: machinecontract.CognitionBootstrapApplyResultV1, Operation: OperationBootstrap,
		TransactionID: transactionID, Status: StatusRolledBack, LayoutActivated: false, FormalComplete: false,
		WrittenPaths: []string{}, RecoveredPaths: recovered, BaselineSHA256: intent.Envelope.Baseline.PostSHA256,
		CompositeIdentity: intent.Envelope.ProjectedCompositeIdentity, NetworkAccessed: false, NextAction: "bootstrap_may_be_replanned",
	}
	result.ResultDigest, _ = applyResultDigest(result)
	data, err := prettyJSON(result)
	if err != nil {
		return nil, err
	}
	path := rollbackPath(absRoot, transactionID)
	if err := afs.AtomicCreateCAS(path, data); err != nil {
		if existing, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(existing, data) {
			return nil, err
		}
	}
	if err := archiveRecovery(absRoot, intent); err != nil {
		return nil, err
	}
	return result, nil
}

func formalPostimageByPath(envelope *ApplyEnvelope, path string) *FormalPostimage {
	for index := range envelope.Targets {
		if envelope.Targets[index].Path == path {
			return &envelope.Targets[index]
		}
	}
	return nil
}

func declaredTargetStatus(status *TransactionStatus, path string) (TargetStatus, bool) {
	for _, target := range status.Targets {
		if target.Path == path {
			return target, true
		}
	}
	return TargetStatus{}, false
}
