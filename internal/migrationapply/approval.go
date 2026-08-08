package migrationapply

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
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
		approval.Mechanism != machinecontract.CognitionMigrationApprovalMechanism || approval.ApprovedRecoveryPolicy != machinecontract.CognitionMigrationRecoveryPolicy ||
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
