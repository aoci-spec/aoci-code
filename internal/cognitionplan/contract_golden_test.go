package cognitionplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestPlannerMachineContractGolden(t *testing.T) {
	actual := struct {
		BootstrapPlan                string                           `json:"bootstrap_plan"`
		MigrationPlan                string                           `json:"migration_plan"`
		SemanticMapping              string                           `json:"semantic_mapping"`
		LayoutCandidate              string                           `json:"layout_candidate"`
		LayoutPreview                string                           `json:"layout_preview"`
		ApprovalDigest               string                           `json:"approval_digest"`
		SemanticAuthoringRequirement string                           `json:"semantic_authoring_requirement"`
		SemanticAuthoringProvenance  string                           `json:"semantic_authoring_provenance"`
		VolumeRegistry               string                           `json:"volume_registry"`
		Registry                     cognition.VolumeRegistryDocument `json:"registry"`
	}{
		machinecontract.CognitionBootstrapPlanV1,
		machinecontract.CognitionMigrationPlanV2,
		machinecontract.CognitionSemanticMappingV1,
		machinecontract.CognitionLayoutCandidateV1,
		machinecontract.CognitionLayoutPreviewV1,
		machinecontract.CognitionApprovalDigestV1,
		machinecontract.SemanticAuthoringRequirementV1,
		machinecontract.SemanticAuthoringProvenanceV1,
		machinecontract.CognitionVolumeRegistryV1,
		cognition.VolumeRegistry(),
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
		t.Fatalf("D2-A machine contract Golden changed without review:\n%s", data)
	}
}
