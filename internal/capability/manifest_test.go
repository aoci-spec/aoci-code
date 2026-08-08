package capability

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestManifestIsSingleReadOnlyNineToolAuthority(t *testing.T) {
	manifest, err := Build(t.TempDir(), "rc17-test", "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != machinecontract.CapabilityManifestV1 || manifest.CurrentLayout != "uninitialized" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}
	if manifest.Product != machinecontract.ProductName ||
		manifest.Binary != machinecontract.BinaryName ||
		manifest.CanonicalRepository != machinecontract.CanonicalRepository ||
		manifest.GoModule != machinecontract.GoModulePath {
		t.Fatalf("public repository identity is incomplete: %#v", manifest)
	}
	if manifest.MCPToolCount != 9 || len(manifest.MCPTools) != 9 {
		t.Fatalf("tool contract changed: %#v", manifest.MCPTools)
	}
	if manifest.NetworkAccessed || manifest.TTY.YesFlagAllowed || manifest.TTY.ModelSelfApproval || !manifest.TTY.DigestRequired {
		t.Fatalf("unsafe capability contract: %#v", manifest)
	}
	if !containsCapability(manifest.InputSchemaVersions, machinecontract.ApplyAuthorizationPolicyV1) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.PolicyBoundApprovalV1) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.ModelCognitionAttestationV1) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.CognitionStateV2) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.CognitionOnboardingSessionV1) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.CognitionOnboardingSessionV2) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.SemanticAuthoringRequirementV1) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.SemanticAuthoringProvenanceV1) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.CognitionBootstrapAutoEligibilityV1) ||
		!containsCapability(manifest.InputSchemaVersions, machinecontract.DatabaseCognitionBootstrapPreviewV1) ||
		!containsCapability(manifest.CLILifecycleCapabilities, "database_cognition_bootstrap") ||
		!containsCapability(manifest.CLILifecycleCapabilities, "fresh_bootstrap_auto_eligibility_v1") ||
		!containsCapability(manifest.CLILifecycleCapabilities, "fresh_bootstrap_semantic_authoring_v2") ||
		!containsCapability(manifest.CLILifecycleCapabilities, "cognition_lineage_v1") ||
		!containsCapability(manifest.CLILifecycleCapabilities, "database_to_code_impact_v1") ||
		!containsCapability(manifest.InputSchemaVersions, cognition.CognitionEvolutionV1) ||
		manifest.TTY.Mechanism != machinecontract.ApprovalMechanismInteractiveDigestConfirmation {
		t.Fatalf("authorization contracts missing: %#v", manifest)
	}
	if containsCapability(manifest.TTY.RequiredFor, "bootstrap_apply") ||
		!containsCapability(manifest.TTY.RequiredFor, "bootstrap_review_apply") {
		t.Fatalf("bootstrap TTY scope is not policy-qualified: %#v", manifest.TTY.RequiredFor)
	}
}

func containsCapability(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
