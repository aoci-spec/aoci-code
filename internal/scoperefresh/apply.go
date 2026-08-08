package scoperefresh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var applyFault = func(string) error { return nil }

func NewApproval(preview *Preview, actor, approvedAt string) (*Approval, error) {
	if preview == nil || !cognitiontxn.ValidAuditActor(actor) {
		return nil, fmt.Errorf("baseline_scope_approval_actor_invalid")
	}
	parsed, err := time.Parse(time.RFC3339, approvedAt)
	if err != nil || parsed.Location() != time.UTC {
		return nil, fmt.Errorf("baseline_scope_approval_timestamp_invalid")
	}
	approval := &Approval{Version: machinecontract.BaselineScopeApprovalV1, PreviewID: preview.PreviewID, Actor: actor,
		Mechanism: "interactive_digest_confirmation", ApprovedAt: approvedAt}
	approval.ApprovalDigest, err = approvalIdentity(*approval)
	return approval, err
}

func DecodeApproval(data []byte) (*Approval, error) {
	var approval Approval
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&approval); err != nil || approval.Version != machinecontract.BaselineScopeApprovalV1 {
		return nil, fmt.Errorf("baseline_scope_approval_invalid")
	}
	want, err := approvalIdentity(approval)
	if err != nil || want != approval.ApprovalDigest || !cognitiontxn.ValidAuditActor(approval.Actor) {
		return nil, fmt.Errorf("baseline_scope_approval_identity_invalid")
	}
	return &approval, nil
}

func approvalIdentity(approval Approval) (string, error) {
	approval.ApprovalDigest = ""
	return digestJSON(approval)
}

func Apply(repositoryRoot string, preview *Preview, approval *Approval) (*ApplyResult, error) {
	if preview == nil {
		return nil, fmt.Errorf("baseline_scope_preview_required")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_repository_invalid")
	}
	postimageBytes, err := baseline.MarshalExact(&preview.BaselinePostimage)
	if err != nil || digestBytes(postimageBytes) != preview.BaselinePostimageSHA256 {
		return nil, fmt.Errorf("baseline_scope_postimage_identity_invalid")
	}
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	currentBytes, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_preimage_unavailable")
	}
	transactionID := preview.PreviewID[:24]
	result := &ApplyResult{Version: machinecontract.BaselineScopeApplyResultV1, TransactionID: transactionID, PlanID: preview.Plan.PlanID,
		PreviewID: preview.PreviewID, BaselineSHA256: preview.BaselinePostimageSHA256, AddedCount: len(preview.Plan.Added),
		RemovedCount: len(preview.Plan.Removed), PreservedCount: len(preview.Plan.Preserved), SourceDriftCount: len(preview.Plan.SourceDrift),
		NetworkAccessed: false, RecoveryAvailable: true}
	if digestBytes(currentBytes) == preview.BaselinePostimageSHA256 {
		active, _, pathErr := intentPaths(root, transactionID)
		recovered := pathErr == nil && fileExists(active)
		if recovered {
			result.Status = "recovered"
		} else {
			result.Status = "already_applied"
		}
		return result, complete(root, preview, approval, result, recovered)
	}
	if digestBytes(currentBytes) != preview.Plan.ExpectedBaselineSHA256 {
		return nil, fmt.Errorf("baseline_scope_third_party_baseline_conflict")
	}
	if len(preview.Plan.SourceDrift) != 0 {
		return nil, fmt.Errorf("baseline_scope_source_drift")
	}
	if preview.Plan.InteractionRequired {
		if err := validateApproval(preview, approval); err != nil {
			return nil, err
		}
	}
	rebuilt, err := Build(root, preview.Plan.BaselineTimestamp)
	if err != nil || rebuilt.PreviewID != preview.PreviewID {
		return nil, fmt.Errorf("baseline_scope_replay_mismatch")
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil || !cfg.LedgerEnabled {
		return nil, fmt.Errorf("baseline_scope_ledger_required")
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_lock_failed")
	}
	defer lock.Release()
	currentBytes, err = os.ReadFile(baselinePath)
	if err != nil || digestBytes(currentBytes) != preview.Plan.ExpectedBaselineSHA256 {
		return nil, fmt.Errorf("baseline_scope_third_party_baseline_conflict")
	}
	intent := recoveryIntent{Version: "baseline-scope-recovery/v1", TransactionID: transactionID, Preview: *preview, Approval: approval}
	intentBytes, err := Encode(intent)
	if err != nil {
		return nil, err
	}
	active, archive, err := intentPaths(root, transactionID)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.SaveImmutable(active, intentBytes); err != nil {
		return nil, fmt.Errorf("baseline_scope_intent_failed: %w", err)
	}
	if err := applyFault("after_intent"); err != nil {
		return nil, err
	}
	if err := afs.AtomicWrite(baselinePath+".bak", currentBytes); err != nil {
		return nil, fmt.Errorf("baseline_scope_backup_failed")
	}
	if err := applyFault("before_baseline"); err != nil {
		return nil, err
	}
	if err := afs.AtomicWriteCAS(baselinePath, postimageBytes, preview.Plan.ExpectedBaselineSHA256); err != nil {
		return nil, fmt.Errorf("baseline_scope_baseline_cas_failed: %w", err)
	}
	if err := applyFault("after_baseline"); err != nil {
		return nil, err
	}
	result.Status = "applied"
	if err := completePaths(root, preview, result, intentBytes, active, archive, false); err != nil {
		return nil, err
	}
	return result, nil
}

func complete(root string, preview *Preview, approval *Approval, result *ApplyResult, recovered bool) error {
	active, archive, err := intentPaths(root, result.TransactionID)
	if err != nil {
		return err
	}
	intent := recoveryIntent{Version: "baseline-scope-recovery/v1", TransactionID: result.TransactionID, Preview: *preview, Approval: approval}
	intentBytes, err := Encode(intent)
	if err != nil {
		return err
	}
	if _, err := os.ReadFile(active); errors.Is(err, os.ErrNotExist) {
		if existing, archiveErr := os.ReadFile(archive); archiveErr == nil && bytes.Equal(existing, intentBytes) {
			return nil
		}
		// A completed postimage without this intent is an idempotent replay of
		// an already archived transaction; do not invent a second transaction.
		return nil
	}
	return completePaths(root, preview, result, intentBytes, active, archive, recovered)
}

func completePaths(root string, preview *Preview, result *ApplyResult, intentBytes []byte, active, archive string, recovered bool) error {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return err
	}
	event := ledger.Event{Op: "baseline_scope_refresh", Result: ledger.ResultOK, Source: ledger.SourceAgent,
		RecoveryTransactionID: result.TransactionID, BaselineSHA256: preview.BaselinePostimageSHA256, AppliedCount: 1}
	if recovered {
		event.AppliedCount = 0
		event.RecoveredCount = 1
	}
	if err := cognitiontxn.EnsureLedger(root, cfg.LedgerEnabled, event); err != nil {
		return fmt.Errorf("baseline_scope_ledger_failed: %w", err)
	}
	if err := applyFault("before_archive"); err != nil {
		return err
	}
	if err := cognitiontxn.ArchiveImmutable(active, archive, intentBytes); err != nil {
		return fmt.Errorf("baseline_scope_archive_failed: %w", err)
	}
	return nil
}

func validateApproval(preview *Preview, approval *Approval) error {
	if approval == nil || approval.Version != machinecontract.BaselineScopeApprovalV1 || approval.PreviewID != preview.PreviewID ||
		approval.Mechanism != "interactive_digest_confirmation" || !cognitiontxn.ValidAuditActor(approval.Actor) {
		return fmt.Errorf("baseline_scope_approval_required")
	}
	want, err := approvalIdentity(*approval)
	if err != nil || want != approval.ApprovalDigest {
		return fmt.Errorf("baseline_scope_approval_digest_mismatch")
	}
	return nil
}

func intentPaths(root, transactionID string) (string, string, error) {
	for _, rel := range []string{".aoci", ".aoci/transactions", ".aoci/transactions/history"} {
		if err := cognitiontxn.EnsureSafeDirectory(root, rel); err != nil {
			return "", "", err
		}
	}
	name := "scope-" + transactionID + ".json"
	return filepath.Join(root, ".aoci", "transactions", name), filepath.Join(root, ".aoci", "transactions", "history", name), nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func DecodeRecovery(data []byte) (*Preview, *Approval, error) {
	var intent recoveryIntent
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil || intent.Version != "baseline-scope-recovery/v1" {
		return nil, nil, fmt.Errorf("baseline_scope_recovery_invalid")
	}
	return &intent.Preview, intent.Approval, nil
}

func Resume(repositoryRoot, transactionID string) (*ApplyResult, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || !validTransactionID(transactionID) {
		return nil, fmt.Errorf("baseline_scope_transaction_invalid")
	}
	active, _, err := intentPaths(root, transactionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(active)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_recovery_not_found")
	}
	preview, approval, err := DecodeRecovery(data)
	if err != nil {
		return nil, err
	}
	return Apply(root, preview, approval)
}

func Inspect(repositoryRoot, transactionID string) (*Status, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || !validTransactionID(transactionID) {
		return nil, fmt.Errorf("baseline_scope_transaction_invalid")
	}
	active, archive, err := intentPaths(root, transactionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(active)
	activeIntent := true
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(archive)
		activeIntent = false
	}
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_transaction_not_found")
	}
	preview, _, err := DecodeRecovery(data)
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_preimage_unavailable")
	}
	actual := digestBytes(current)
	state := "conflict"
	if actual == preview.Plan.ExpectedBaselineSHA256 {
		state = "preimage"
	} else if actual == preview.BaselinePostimageSHA256 {
		state = "postimage"
	}
	if !activeIntent && state == "postimage" {
		state = "complete"
	}
	return &Status{Version: "baseline-scope-status/v1", TransactionID: transactionID, State: state,
		ExpectedBaselineSHA: preview.Plan.ExpectedBaselineSHA256, PostBaselineSHA: preview.BaselinePostimageSHA256,
		ActualBaselineSHA: actual, RecoveryAvailable: activeIntent && state != "conflict"}, nil
}

func validTransactionID(value string) bool {
	return len(value) == 24 && strings.Trim(value, "0123456789abcdef") == ""
}
