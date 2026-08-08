package migrationapply

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func prettyJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func strictDecode(data []byte, target any) error {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func decodeVersioned[T any](data []byte, version string, field func(*T) string, label string) (*T, error) {
	var value T
	if err := strictDecode(data, &value); err != nil {
		return nil, fmt.Errorf("%s_invalid: %w", label, err)
	}
	if field(&value) != version {
		return nil, fmt.Errorf("%s_version_unknown", label)
	}
	return &value, nil
}

func DecodeLegacySnapshot(data []byte) (*LegacySnapshot, error) {
	return decodeVersioned(data, machinecontract.CognitionLegacySnapshotV1, func(value *LegacySnapshot) string { return value.Version }, "migration_snapshot")
}

func DecodeMapping(data []byte) (*MigrationMapping, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationMappingV2, func(value *MigrationMapping) string { return value.Version }, "migration_mapping")
}

func DecodeApplyRequest(data []byte) (*ApplyRequest, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationApplyRequestV2, func(value *ApplyRequest) string { return value.Version }, "migration_apply_request")
}

func DecodeApplyEnvelope(data []byte) (*ApplyEnvelope, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationApplyEnvelopeV2, func(value *ApplyEnvelope) string { return value.Version }, "migration_apply_envelope")
}

func DecodeApproval(data []byte) (*Approval, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationApprovalV2, func(value *Approval) string { return value.Version }, "migration_approval")
}

func DecodeRecovery(data []byte) (*RecoveryIntent, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationRecoveryV2, func(value *RecoveryIntent) string { return value.Version }, "migration_recovery")
}

func DecodeReceipt(data []byte) (*MigrationReceipt, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationReceiptV2, func(value *MigrationReceipt) string { return value.Version }, "migration_receipt")
}

func DecodeApplyResult(data []byte) (*ApplyResult, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationApplyResultV2, func(value *ApplyResult) string { return value.Version }, "migration_apply_result")
}

func DecodeTransactionStatus(data []byte) (*TransactionStatus, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationTransactionStatusV2, func(value *TransactionStatus) string { return value.Version }, "migration_transaction_status")
}

func DecodeReversalPlan(data []byte) (*ReversalPlan, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationReversalPlanV2, func(value *ReversalPlan) string { return value.Version }, "migration_reversal_plan")
}

func DecodeReversalApproval(data []byte) (*ReversalApproval, error) {
	return decodeVersioned(data, machinecontract.CognitionMigrationReversalApprovalV2, func(value *ReversalApproval) string { return value.Version }, "migration_reversal_approval")
}

func Encode(value any) ([]byte, error) { return prettyJSON(value) }

func validateUTC(value string) error {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("migration_timestamp_invalid")
	}
	_, offset := parsed.Zone()
	if offset != 0 || !strings.HasSuffix(value, "Z") {
		return fmt.Errorf("migration_timestamp_not_utc")
	}
	return nil
}
