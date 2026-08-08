package bootstrapapply

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

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

func DecodeApplyRequest(data []byte) (*ApplyRequest, error) {
	var value ApplyRequest
	if err := strictDecode(data, &value); err != nil {
		return nil, fmt.Errorf("bootstrap_apply_request_invalid: %w", err)
	}
	if value.Version != machinecontract.CognitionBootstrapApplyRequestV1 {
		return nil, fmt.Errorf("bootstrap_apply_request_version_unknown")
	}
	return &value, nil
}

func DecodeApplyEnvelope(data []byte) (*ApplyEnvelope, error) {
	var value ApplyEnvelope
	if err := strictDecode(data, &value); err != nil {
		return nil, fmt.Errorf("bootstrap_apply_envelope_invalid: %w", err)
	}
	if value.Version != machinecontract.CognitionBootstrapApplyEnvelopeV1 {
		return nil, fmt.Errorf("bootstrap_apply_envelope_version_unknown")
	}
	return &value, nil
}

func DecodeApproval(data []byte) (*Approval, error) {
	var value Approval
	if err := strictDecode(data, &value); err != nil {
		return nil, fmt.Errorf("bootstrap_approval_invalid: %w", err)
	}
	if value.Version != machinecontract.CognitionBootstrapApprovalV1 {
		return nil, fmt.Errorf("bootstrap_approval_version_unknown")
	}
	return &value, nil
}

func DecodeRecovery(data []byte) (*RecoveryIntent, error) {
	var value RecoveryIntent
	if err := strictDecode(data, &value); err != nil {
		return nil, fmt.Errorf("bootstrap_recovery_invalid: %w", err)
	}
	if value.Version != machinecontract.CognitionBootstrapRecoveryV1 {
		return nil, fmt.Errorf("bootstrap_recovery_version_unknown")
	}
	return &value, nil
}

func DecodeApplyResult(data []byte) (*ApplyResult, error) {
	var value ApplyResult
	if err := strictDecode(data, &value); err != nil {
		return nil, fmt.Errorf("bootstrap_apply_result_invalid: %w", err)
	}
	if value.Version != machinecontract.CognitionBootstrapApplyResultV1 {
		return nil, fmt.Errorf("bootstrap_apply_result_version_unknown")
	}
	return &value, nil
}

func DecodeTransactionStatus(data []byte) (*TransactionStatus, error) {
	var value TransactionStatus
	if err := strictDecode(data, &value); err != nil {
		return nil, fmt.Errorf("bootstrap_transaction_status_invalid: %w", err)
	}
	if value.Version != machinecontract.CognitionBootstrapTransactionStatusV1 {
		return nil, fmt.Errorf("bootstrap_transaction_status_version_unknown")
	}
	return &value, nil
}

func Encode(value any) ([]byte, error) { return prettyJSON(value) }
