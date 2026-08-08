package bootstrapapply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestBootstrapMachineContractGolden(t *testing.T) {
	actual := struct {
		ApplyRequest      string   `json:"apply_request"`
		ApplyEnvelope     string   `json:"apply_envelope"`
		Approval          string   `json:"approval"`
		Recovery          string   `json:"recovery"`
		ApplyResult       string   `json:"apply_result"`
		TransactionStatus string   `json:"transaction_status"`
		Operation         string   `json:"operation"`
		Preimage          string   `json:"preimage"`
		RecoveryPolicy    string   `json:"recovery_policy"`
		ApprovalMechanism string   `json:"approval_mechanism"`
		FormalOrder       []string `json:"formal_order"`
		DiskStates        []string `json:"disk_states"`
		TerminalStates    []string `json:"terminal_states"`
	}{
		machinecontract.CognitionBootstrapApplyRequestV1,
		machinecontract.CognitionBootstrapApplyEnvelopeV1,
		machinecontract.CognitionBootstrapApprovalV1,
		machinecontract.CognitionBootstrapRecoveryV1,
		machinecontract.CognitionBootstrapApplyResultV1,
		machinecontract.CognitionBootstrapTransactionStatusV1,
		OperationBootstrap, PreimageAbsent, RecoveryPolicy, ApprovalMechanism,
		[]string{"meta", "code", "database", "root", "baseline"},
		[]string{StatePreimage, StatePostimage, StateUnknown, StateWrongType, StateMissingStaging},
		[]string{StatusApplied, StatusAlreadyApplied, StatusRolledBack},
	}
	data, err := json.MarshalIndent(actual, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	expected, err := os.ReadFile(filepath.Join("testdata", "contracts.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(expected) {
		t.Fatalf("D2-B machine contract Golden changed without review:\n%s", data)
	}
}
