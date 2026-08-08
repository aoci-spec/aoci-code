package migrationapply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestMigrationMachineContractGolden(t *testing.T) {
	actual := struct {
		LegacySnapshot       string   `json:"legacy_snapshot"`
		ApplyGradeMapping    string   `json:"apply_grade_mapping"`
		ApplyRequest         string   `json:"apply_request"`
		ApplyEnvelope        string   `json:"apply_envelope"`
		Approval             string   `json:"approval"`
		Recovery             string   `json:"recovery"`
		Receipt              string   `json:"receipt"`
		ApplyResult          string   `json:"apply_result"`
		TransactionStatus    string   `json:"transaction_status"`
		ReversalPlan         string   `json:"reversal_plan"`
		ReversalApproval     string   `json:"reversal_approval"`
		ReversalRecovery     string   `json:"reversal_recovery"`
		Operations           []string `json:"operations"`
		MappingModes         []string `json:"mapping_modes"`
		FormalOrder          []string `json:"formal_order"`
		PendingRollbackOrder []string `json:"pending_rollback_order"`
		ReversalOrder        []string `json:"reversal_order"`
		TerminalStates       []string `json:"terminal_states"`
	}{
		machinecontract.CognitionLegacySnapshotV1, machinecontract.CognitionMigrationMappingV2,
		machinecontract.CognitionMigrationApplyRequestV2, machinecontract.CognitionMigrationApplyEnvelopeV2,
		machinecontract.CognitionMigrationApprovalV2, machinecontract.CognitionMigrationRecoveryV2,
		machinecontract.CognitionMigrationReceiptV2, machinecontract.CognitionMigrationApplyResultV2,
		machinecontract.CognitionMigrationTransactionStatusV2, machinecontract.CognitionMigrationReversalPlanV2,
		machinecontract.CognitionMigrationReversalApprovalV2, machinecontract.CognitionMigrationReversalRecoveryV2,
		[]string{OperationMigration, OperationReversal},
		[]string{machinecontract.CognitionMappingPreserved, machinecontract.CognitionMappingFieldPreserved, machinecontract.CognitionMigrationModelRegenerated, machinecontract.CognitionMappingStructuralOnly},
		[]string{"meta", "code", "database", "root", "baseline", "verify", "receipt", "ledger", "archive"},
		[]string{"root", "baseline", "database", "code", "meta"},
		[]string{"root", "baseline", "database", "code", "meta", "verify", "receipt", "ledger", "archive"},
		[]string{machinecontract.CognitionMigrationStatusApplied, machinecontract.CognitionMigrationStatusAlreadyApplied,
			machinecontract.CognitionMigrationStatusRolledBack, machinecontract.CognitionMigrationStatusReversed,
			machinecontract.CognitionMigrationStatusAlreadyReversed},
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
		t.Fatalf("D2-C machine contract Golden changed without review:\n%s", data)
	}
}
