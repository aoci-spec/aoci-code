package bootstrapapply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// RecordApproval creates the local audit record only after the caller has
// independently obtained an exact digest confirmation from a human control
// surface. It is an audit binding, not cryptographic authentication.
func RecordApproval(envelope *ApplyEnvelope, actor, approvedAt, confirmedEnvelopeDigest string) (*Approval, error) {
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	if envelope.AutomationPolicy.Mode == config.AutomationModeOff {
		return nil, fmt.Errorf("bootstrap_automation_off_apply_forbidden")
	}
	actor = strings.TrimSpace(actor)
	if !cognitiontxn.ValidAuditActor(actor) {
		return nil, fmt.Errorf("bootstrap_approval_actor_invalid")
	}
	if confirmedEnvelopeDigest != envelope.EnvelopeDigest {
		return nil, fmt.Errorf("bootstrap_approval_confirmation_mismatch")
	}
	return newApproval(envelope, actor, approvedAt, ApprovalMechanism)
}

// RecordPolicyBoundAutoApproval binds a complete new-project Bootstrap to the
// repository's explicit automation.mode=auto policy. It has no Legacy
// Migration path and does not write any formal asset.
func RecordPolicyBoundAutoApproval(root string, envelope *ApplyEnvelope, approvedAt string) (*Approval, error) {
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("bootstrap_auto_repository_invalid")
	}
	if err := validatePolicyBoundAuto(absRoot, envelope); err != nil {
		return nil, err
	}
	return newApproval(envelope, "aoci-policy", approvedAt, AutoApprovalMechanism)
}

func newApproval(envelope *ApplyEnvelope, actor, approvedAt, mechanism string) (*Approval, error) {
	if err := validateUTC(approvedAt); err != nil {
		return nil, fmt.Errorf("bootstrap_approval_timestamp_invalid")
	}
	approval := &Approval{
		Version: machinecontract.CognitionBootstrapApprovalV1, Operation: OperationBootstrap,
		D2AApprovalDigest: envelope.D2AApprovalDigest, ApplyEnvelopeDigest: envelope.EnvelopeDigest,
		ApprovedWriteSet: append([]string{}, envelope.WriteSet...), ApprovedRecoveryPolicy: RecoveryPolicy,
		Actor: actor, Mechanism: mechanism, ApprovedAt: approvedAt,
	}
	digest, err := approvalDigest(approval)
	if err != nil {
		return nil, err
	}
	approval.ApprovalDigest = digest
	return approval, nil
}

func approvalDigest(approval *Approval) (string, error) {
	copyValue := *approval
	copyValue.ApprovalDigest = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateApproval(envelope *ApplyEnvelope, approval *Approval) error {
	if envelope.AutomationPolicy.Mode == config.AutomationModeOff {
		return fmt.Errorf("bootstrap_automation_off_apply_forbidden")
	}
	if approval == nil || approval.Version != machinecontract.CognitionBootstrapApprovalV1 ||
		approval.Operation != OperationBootstrap ||
		(approval.Mechanism != ApprovalMechanism && approval.Mechanism != AutoApprovalMechanism) ||
		approval.ApprovedRecoveryPolicy != RecoveryPolicy || !cognitiontxn.ValidAuditActor(approval.Actor) {
		return fmt.Errorf("bootstrap_approval_invalid")
	}
	if approval.D2AApprovalDigest != envelope.D2AApprovalDigest || approval.ApplyEnvelopeDigest != envelope.EnvelopeDigest ||
		!reflect.DeepEqual(approval.ApprovedWriteSet, envelope.WriteSet) {
		return fmt.Errorf("bootstrap_approval_binding_mismatch")
	}
	digest, err := approvalDigest(approval)
	if err != nil || digest != approval.ApprovalDigest {
		return fmt.Errorf("bootstrap_approval_digest_mismatch")
	}
	return validateUTC(approval.ApprovedAt)
}

func validatePolicyBoundAuto(root string, envelope *ApplyEnvelope) error {
	projection, err := EvaluateAutoEligibility(root, envelope)
	if err != nil {
		return err
	}
	return autoEligibilityError(projection)
}

// matureGovernanceHistoryPresent distinguishes harmless discovery performed
// during the current new-project run from evidence that formal governance has
// already existed. Auto Bootstrap must never reinterpret the latter as an
// uninitialized repository merely because Root is absent or is the official
// zero-Entry skeleton.
func matureGovernanceHistoryPresent(root string) (bool, error) {
	for _, relative := range []string{
		".aoci/curation.json",
	} {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return false, err
		}
	}
	for _, relative := range []string{
		".aoci/reports.jsonl",
		".aoci/governance",
		".aoci/verify_history",
		".aoci/drafts",
		".aoci/transactions/history",
	} {
		present, err := nonEmptyGovernancePath(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return false, err
		}
		if present {
			return true, nil
		}
	}

	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return false, fmt.Errorf("bootstrap_auto_ledger_invalid")
	}
	for _, event := range events {
		if bootstrapMatureLedgerOperation(event.Op) {
			return true, nil
		}
	}
	return false, nil
}

func nonEmptyGovernancePath(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return info.Size() != 0, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) != 0, nil
}

func bootstrapMatureLedgerOperation(operation string) bool {
	switch operation {
	case "auto_finalize",
		"baseline_scope_refresh",
		"cognition_bootstrap_apply",
		"cognition_migration_apply",
		"cognition_migration_reversal",
		"curation_apply",
		"entries_apply",
		"entries_recover",
		"header_apply",
		"index_update",
		"managed_scope_change_apply",
		"managed_scope_change_rollback",
		"remove_entry",
		"update_entries_batch",
		"update_entries_batch_recover",
		"update_entry":
		return true
	default:
		return false
	}
}

func containsTargetKind(envelope *ApplyEnvelope, kind string) bool {
	for _, target := range envelope.Targets {
		if target.Kind == kind {
			return true
		}
	}
	return false
}

// ValidateApproval exposes the existing Bootstrap approval guard to Host
// workflow adapters that must reject transport input before persisting their
// own progress state. Formal Apply calls the same guard again under lock.
func ValidateApproval(envelope *ApplyEnvelope, approval *Approval) error {
	if err := validateEnvelope(envelope); err != nil {
		return err
	}
	return validateApproval(envelope, approval)
}
