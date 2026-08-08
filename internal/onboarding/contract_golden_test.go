package onboarding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestOnboardingMachineContractGolden(t *testing.T) {
	actual := struct {
		LegacySession        string `json:"legacy_session"`
		FreshSession         string `json:"fresh_session"`
		LegacyBatch          string `json:"legacy_batch"`
		FreshBatch           string `json:"fresh_batch"`
		LegacyCompletion     string `json:"legacy_completion"`
		FreshCompletion      string `json:"fresh_completion"`
		AuthoringRequirement string `json:"semantic_authoring_requirement"`
		AuthoringProvenance  string `json:"semantic_authoring_provenance"`
		AutoEligibility      string `json:"auto_eligibility"`
		MCPToolCount         int    `json:"mcp_tool_count"`
	}{
		machinecontract.CognitionOnboardingSessionV1,
		machinecontract.CognitionOnboardingSessionV2,
		machinecontract.CognitionOnboardingBatchV1,
		machinecontract.CognitionOnboardingBatchV2,
		machinecontract.CognitionOnboardingCompleteV1,
		machinecontract.CognitionOnboardingCompleteV2,
		machinecontract.SemanticAuthoringRequirementV1,
		machinecontract.SemanticAuthoringProvenanceV1,
		machinecontract.CognitionBootstrapAutoEligibilityV1,
		len(machinecontract.MCPToolNames()),
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
		t.Fatalf("onboarding machine contract Golden changed without review:\n%s", data)
	}
}
