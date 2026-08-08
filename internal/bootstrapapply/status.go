package bootstrapapply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Pending reports active Bootstrap recovery intents. History and completed
// transaction directories are not pending.
func Pending(root string) ([]string, error) {
	return cognitiontxn.PendingForOperation(root, OperationBootstrap)
}

// Status derives transaction phase from actual formal and staging bytes. It
// never trusts a saved mutable phase field.
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
			return nil, fmt.Errorf("bootstrap_transaction_id_required")
		}
		transactionID = pending[0]
	}
	intent, err := loadRecoveryAt(intentPath(absRoot, transactionID), transactionID)
	active := err == nil
	if os.IsNotExist(err) {
		intent, err = loadRecoveryAt(archivePath(absRoot, transactionID), transactionID)
	}
	if err != nil {
		return nil, fmt.Errorf("bootstrap_transaction_not_found")
	}
	status, err := inspectEnvelopeState(absRoot, transactionID, &intent.Envelope, true)
	if err != nil {
		return nil, err
	}
	status.RecoveryPending = active
	if !active {
		if _, rollbackErr := loadResultAt(rollbackPath(absRoot, transactionID), transactionID); rollbackErr == nil {
			status.Status = StatusRolledBack
			status.LayoutActivated = false
			status.FormalComplete = false
			status.NextActions = []string{"none"}
		} else if _, completionErr := loadCompletion(absRoot, transactionID); completionErr == nil {
			status.Status = StatusApplied
			status.LayoutActivated = true
			status.FormalComplete = true
			status.NextActions = []string{"none"}
		} else {
			return nil, fmt.Errorf("bootstrap_archived_transaction_has_no_terminal_receipt")
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
		Version:       machinecontract.CognitionBootstrapTransactionStatusV1,
		TransactionID: transactionID, Status: StatusPrepared, Targets: []TargetStatus{},
		NextActions: []string{"apply"}, NetworkAccessed: false,
	}
	type expected struct{ path, kind, sha, preSHA string }
	expectedTargets := make([]expected, 0, len(envelope.Targets)+1)
	for _, target := range envelope.Targets {
		expectedTargets = append(expectedTargets, expected{target.Path, target.Kind, target.PostSHA256, target.PreimageSHA256})
	}
	expectedTargets = append(expectedTargets, expected{envelope.Baseline.Path, "baseline", envelope.Baseline.PostSHA256, ""})
	seenPreimage := false
	postCount := 0
	conflict := false
	for index, target := range expectedTargets {
		diskState, actualSHA, err := classifyFormalPath(filepath.Join(root, filepath.FromSlash(target.path)), target.sha, target.preSHA)
		if err != nil {
			return nil, err
		}
		stagingState := StateMissingStaging
		if includeStaging {
			stagingPath := filepath.Join(root, ".aoci", "transactions", "bootstrap-"+transactionID, "staging", fmt.Sprintf("%02d.post", index))
			stagingState, _, err = classifyPath(stagingPath, target.sha)
			if err != nil {
				return nil, err
			}
			if stagingState != StatePostimage {
				conflict = true
				stagingState = StateMissingStaging
			}
		}
		status.Targets = append(status.Targets, TargetStatus{
			Path: target.path, Kind: target.kind, DiskState: diskState,
			StagingState: stagingState, ActualSHA256: actualSHA,
		})
		switch diskState {
		case StatePostimage:
			postCount++
			if seenPreimage {
				conflict = true
			}
		case StatePreimage:
			seenPreimage = true
		default:
			conflict = true
		}
	}
	rootState := targetStatusByPath(status, "aoci.txt")
	status.LayoutActivated = rootState.DiskState == StatePostimage
	status.FormalComplete = postCount == len(expectedTargets)
	status.ThirdPartyConflict = conflict
	if conflict {
		status.Status = StatusRecoveryConflict
		status.NextActions = []string{"resolve_third_party_conflict"}
	} else if includeStaging {
		status.RecoveryPending = true
		if status.LayoutActivated {
			status.Status = StatusRecoveryRequiredActive
		} else {
			status.Status = StatusRecoveryRequiredInactive
		}
		status.NextActions = []string{"resume", "rollback"}
	}
	status.StatusDigest = ""
	data, _ := canonicalJSON(status)
	status.StatusDigest = sha256Hex(data)
	return status, nil
}

func classifyPath(path, expectedSHA string) (string, string, error) {
	return classifyFormalPath(path, expectedSHA, "")
}

func classifyFormalPath(path, expectedSHA, preimageSHA string) (string, string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return StatePreimage, "", nil
	}
	if err != nil {
		return StateUnknown, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return StateWrongType, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return StateUnknown, "", err
	}
	digest := sha256Hex(data)
	if digest == expectedSHA {
		return StatePostimage, digest, nil
	}
	if preimageSHA != "" && digest == preimageSHA {
		return StatePreimage, digest, nil
	}
	return StateUnknown, digest, nil
}

func loadResultAt(path, transactionID string) (*ApplyResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result ApplyResult
	if err := strictDecode(data, &result); err != nil || result.Version != machinecontract.CognitionBootstrapApplyResultV1 || result.TransactionID != transactionID {
		return nil, fmt.Errorf("bootstrap_result_invalid")
	}
	digest, err := applyResultDigest(&result)
	if err != nil || digest != result.ResultDigest {
		return nil, fmt.Errorf("bootstrap_result_digest_invalid")
	}
	return &result, nil
}
