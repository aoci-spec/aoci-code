package migrationapply

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// RecordApproval creates an audit binding only after the caller has obtained
// exact interactive confirmation from an independent human control surface.
// It is intentionally not represented as cryptographic authentication.
func RecordApproval(envelope *ApplyEnvelope, actor, approvedAt, confirmedEnvelopeDigest string) (*Approval, error) {
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	actor = strings.TrimSpace(actor)
	if !cognitiontxn.ValidAuditActor(actor) {
		return nil, fmt.Errorf("migration_approval_actor_invalid")
	}
	if confirmedEnvelopeDigest != envelope.EnvelopeDigest {
		return nil, fmt.Errorf("migration_approval_confirmation_mismatch")
	}
	if err := validateUTC(approvedAt); err != nil {
		return nil, fmt.Errorf("migration_approval_timestamp_invalid")
	}
	approval := &Approval{
		Version: machinecontract.CognitionMigrationApprovalV2, Operation: OperationMigration,
		D2AApprovalDigest: envelope.D2AApprovalDigest, ApplyEnvelopeDigest: envelope.EnvelopeDigest,
		MappingSHA256: envelope.MappingSHA256, ApprovedWriteSet: append([]string{}, envelope.WriteSet...),
		ApprovedRecoveryPolicy: machinecontract.CognitionMigrationRecoveryPolicy,
		Actor:                  actor, Mechanism: machinecontract.CognitionMigrationApprovalMechanism, ApprovedAt: approvedAt,
	}
	approval.ApprovalDigest, _ = approvalDigest(approval)
	return approval, nil
}

// RecordHostDialogApproval records the answer a person gave in the MCP host's
// dialog for this exact envelope. It binds what RecordApproval binds; Apply
// admits it only while the team's human_approval_channel does.
func RecordHostDialogApproval(envelope *ApplyEnvelope, actor, approvedAt string) (*Approval, error) {
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	actor = strings.TrimSpace(actor)
	if !cognitiontxn.ValidAuditActor(actor) {
		return nil, fmt.Errorf("migration_approval_actor_invalid")
	}
	if err := validateUTC(approvedAt); err != nil {
		return nil, fmt.Errorf("migration_approval_timestamp_invalid")
	}
	approval := &Approval{
		Version: machinecontract.CognitionMigrationApprovalV2, Operation: OperationMigration,
		D2AApprovalDigest: envelope.D2AApprovalDigest, ApplyEnvelopeDigest: envelope.EnvelopeDigest,
		MappingSHA256: envelope.MappingSHA256, ApprovedWriteSet: append([]string{}, envelope.WriteSet...),
		ApprovedRecoveryPolicy: machinecontract.CognitionMigrationRecoveryPolicy,
		Actor:                  actor, Mechanism: machinecontract.ApprovalMechanismHostElicitationConfirmation, ApprovedAt: approvedAt,
	}
	approval.ApprovalDigest, _ = approvalDigest(approval)
	return approval, nil
}

// AdmitApprovalChannel applies the team's human_approval_channel to an approval
// about to start a transaction. Resume never asks again: an Intent that
// started under the old channel finishes under it.
func AdmitApprovalChannel(root string, approval *Approval) error {
	if approval == nil || approval.Mechanism != machinecontract.ApprovalMechanismHostElicitationConfirmation {
		return nil
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return fmt.Errorf("migration_approval_channel_unreadable")
	}
	if !cfg.AdmitsApprovalMechanism(approval.Mechanism) {
		return fmt.Errorf("migration_host_dialog_approval_disabled: human_approval_channel=%s", cfg.EffectiveHumanApprovalChannel())
	}
	return nil
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
	if approval == nil || approval.Version != machinecontract.CognitionMigrationApprovalV2 || approval.Operation != OperationMigration ||
		(approval.Mechanism != machinecontract.CognitionMigrationApprovalMechanism &&
			approval.Mechanism != machinecontract.ApprovalMechanismHostElicitationConfirmation) ||
		approval.ApprovedRecoveryPolicy != machinecontract.CognitionMigrationRecoveryPolicy ||
		!cognitiontxn.ValidAuditActor(approval.Actor) {
		return fmt.Errorf("migration_approval_invalid")
	}
	if approval.D2AApprovalDigest != envelope.D2AApprovalDigest || approval.ApplyEnvelopeDigest != envelope.EnvelopeDigest ||
		approval.MappingSHA256 != envelope.MappingSHA256 || !reflect.DeepEqual(approval.ApprovedWriteSet, envelope.WriteSet) {
		return fmt.Errorf("migration_approval_binding_mismatch")
	}
	digest, err := approvalDigest(approval)
	if err != nil || digest != approval.ApprovalDigest {
		return fmt.Errorf("migration_approval_digest_mismatch")
	}
	return validateUTC(approval.ApprovedAt)
}

// ValidateApproval exposes the existing Migration approval guard to Host
// workflow adapters that must reject transport input before persisting their
// own progress state. Formal Apply calls the same guard again under lock.
func ValidateApproval(envelope *ApplyEnvelope, approval *Approval) error {
	if err := validateEnvelope(envelope); err != nil {
		return err
	}
	return validateApproval(envelope, approval)
}
