package cognitionplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// DecodePlan applies duplicate-key, unknown-field, and trailing-value checks.
func DecodePlan(data []byte) (*Plan, error) {
	var plan Plan
	if err := decodeStrict(data, &plan); err != nil {
		return nil, fmt.Errorf("planner_plan_invalid: %w", err)
	}
	if plan.Version != machinecontract.CognitionBootstrapPlanV1 && plan.Version != machinecontract.CognitionMigrationPlanV2 {
		return nil, fmt.Errorf("planner_plan_version_unknown")
	}
	if plan.Operation != OperationBootstrap && plan.Operation != OperationMigration {
		return nil, fmt.Errorf("planner_operation_unknown")
	}
	return &plan, nil
}

// DecodeCandidate applies the same strict JSON boundary and checks the D2-A
// candidate protocol identifier before any asset bytes are interpreted.
func DecodeCandidate(data []byte) (*LayoutCandidate, error) {
	var candidate LayoutCandidate
	if err := decodeStrict(data, &candidate); err != nil {
		return nil, fmt.Errorf("layout_candidate_invalid: %w", err)
	}
	if candidate.Version != machinecontract.CognitionLayoutCandidateV1 {
		return nil, fmt.Errorf("layout_candidate_version_unknown")
	}
	return &candidate, nil
}

// DecodePreview applies the same strict boundary used by Plan and Candidate.
// D2-B consumes the exact D2-A preview instead of re-declaring its contract.
func DecodePreview(data []byte) (*Preview, error) {
	var preview Preview
	if err := decodeStrict(data, &preview); err != nil {
		return nil, fmt.Errorf("layout_preview_invalid: %w", err)
	}
	if preview.Version != machinecontract.CognitionLayoutPreviewV1 {
		return nil, fmt.Errorf("layout_preview_version_unknown")
	}
	return &preview, nil
}

func decodeStrict(data []byte, target any) error {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("trailing JSON value")
	}
	return err
}
