package capability

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type TTYRequirement struct {
	RequiredFor       []string `json:"required_for"`
	Mechanism         string   `json:"mechanism"`
	DigestRequired    bool     `json:"digest_required"`
	YesFlagAllowed    bool     `json:"yes_flag_allowed"`
	ModelSelfApproval bool     `json:"model_self_approval"`
}

type Manifest struct {
	Version                  string         `json:"version"`
	Product                  string         `json:"product"`
	Binary                   string         `json:"binary"`
	CanonicalRepository      string         `json:"canonical_repository"`
	GoModule                 string         `json:"go_module"`
	AOCIVersion              string         `json:"aoci_version"`
	Commit                   string         `json:"commit"`
	MCPProtocol              string         `json:"mcp_protocol"`
	MCPTools                 []string       `json:"mcp_tools"`
	MCPToolCount             int            `json:"mcp_tool_count"`
	MCPToolNameIdentity      string         `json:"mcp_tool_name_identity"`
	CLILifecycleCapabilities []string       `json:"cli_lifecycle_capabilities"`
	InputSchemaVersions      []string       `json:"input_schema_versions"`
	CurrentLayout            string         `json:"current_layout"`
	SupportedPlanner         []string       `json:"supported_planner"`
	SupportedApply           []string       `json:"supported_apply"`
	SupportedRecovery        []string       `json:"supported_recovery"`
	RequiredAgentFields      []string       `json:"required_agent_fields"`
	TTY                      TTYRequirement `json:"tty"`
	DatabaseCapabilities     []string       `json:"database_capabilities"`
	OverviewDeliveryModes    []string       `json:"overview_delivery_modes"`
	CompatibilityStrategy    []string       `json:"compatibility_strategy"`
	NetworkAccessed          bool           `json:"network_accessed"`
}

func Build(repositoryRoot, version, commit string) (*Manifest, error) {
	layout, err := detectLayout(repositoryRoot)
	if err != nil {
		return nil, err
	}
	tools := machinecontract.MCPToolNames()
	return &Manifest{
		Version: machinecontract.CapabilityManifestV1,
		Product: machinecontract.ProductName, Binary: machinecontract.BinaryName,
		CanonicalRepository: machinecontract.CanonicalRepository, GoModule: machinecontract.GoModulePath,
		AOCIVersion: version, Commit: commit,
		MCPProtocol: machinecontract.MCPProtocolCurrent, MCPTools: tools, MCPToolCount: len(tools), MCPToolNameIdentity: machinecontract.MCPToolNameIdentity(),
		CLILifecycleCapabilities: []string{"bootstrap", "database_cognition_bootstrap", "migration", "migration_reversal", "baseline_scope_refresh", "managed_scope_change", "policy_bound_auto_authorization", "fresh_bootstrap_auto_eligibility_v1", "observe_evidence_review", "cognition_budget", "cognition_onboarding", "fresh_bootstrap_semantic_authoring_v2", "cognition_lineage_v1", "database_to_code_impact_v1", "cognition_evolution_v1", "narrow_system_relation_projection_v1"},
		InputSchemaVersions: []string{machinecontract.CapabilityManifestV1, machinecontract.SafeInventoryV2, machinecontract.BaselineScopePlanV1,
			machinecontract.BaselineScopePreviewV1, machinecontract.BaselineScopeApprovalV1, machinecontract.BaselineScopeApplyResultV1,
			machinecontract.BusinessSourceManifestV1, machinecontract.CognitionMigrationPlanV2, machinecontract.CognitionMigrationMappingV2,
			machinecontract.CognitionMigrationApplyEnvelopeV2, machinecontract.CognitionMigrationApprovalV2,
			machinecontract.CognitionEntryPreservationV1, "migration-semantic-diff/v2", machinecontract.HostInteractionV1,
			machinecontract.CognitionBootstrapAutoEligibilityV1,
			machinecontract.OverviewDeliveryReceiptV1, machinecontract.OverviewChunkReceiptV1,
			machinecontract.ModelCognitionAttestationV1, machinecontract.CognitionStateV2,
			machinecontract.CognitionOnboardingSessionV1, machinecontract.CognitionOnboardingSessionV2,
			machinecontract.CognitionOnboardingBatchV1, machinecontract.CognitionOnboardingBatchV2,
			machinecontract.CognitionOnboardingCompleteV1, machinecontract.CognitionOnboardingCompleteV2,
			machinecontract.SemanticAuthoringRequirementV1, machinecontract.SemanticAuthoringProvenanceV1,
			machinecontract.DatabaseCognitionBootstrapPreviewV1,
			machinecontract.DatabaseCognitionBootstrapRecoveryV1,
			machinecontract.DatabaseCognitionBootstrapResultV1,
			machinecontract.ManagedScopePolicyV2, machinecontract.ManagedScopeEvaluationV2,
			machinecontract.ManagedScopeProposalV1,
			machinecontract.ManagedScopeChangePlanV2, machinecontract.ManagedScopeChangePreviewV2,
			machinecontract.ManagedScopeChangeEnvelopeV2, machinecontract.ManagedScopeChangeApprovalV2,
			machinecontract.ManagedScopeChangeResultV2, machinecontract.ManagedScopeChangeStatusV2,
			machinecontract.ManagedScopeStatusV2, machinecontract.ManagedScopeRecoveryV2,
			machinecontract.ManagedScopeCandidateSetV1, machinecontract.ManagedScopeSafetyApprovalV1,
			machinecontract.ManagedScopeBaselineV1, machinecontract.ScopeEntryDispositionV1,
			machinecontract.ApplyAuthorizationPolicyV1, machinecontract.PolicyBoundApprovalV1,
			machinecontract.CognitionBudgetPolicyV1, machinecontract.CognitionBudgetReportV1,
			machinecontract.CognitionBudgetValidationV1,
			cognition.SystemRelationProjectionV1, cognition.CognitionLineageV1,
			cognition.DatabaseImpactV1, cognition.CognitionSnapshotV1, cognition.CognitionEvolutionV1},
		CurrentLayout: layout, SupportedPlanner: []string{"D2-A", "bootstrap", "migration", "baseline_scope_refresh", "managed_scope_change"},
		SupportedApply:      []string{"D2-B-bootstrap", "database-cognition-bootstrap", "D2-C-migration", "baseline_scope_refresh", "managed_scope_change"},
		SupportedRecovery:   []string{"resume", "rollback", "database_bootstrap_resume", "database_bootstrap_rollback", "reversal", "scope_resume", "scope_rollback", "onboarding_resume"},
		RequiredAgentFields: []string{"agent", "schema_version", "request_file", "expected_preimage", "plan_or_run_identity"},
		TTY: TTYRequirement{RequiredFor: []string{"bootstrap_review_apply", "migration_apply", "migration_reversal", "ordinary_legacy_scope_reduction", "managed_scope_review", "high_risk_exact_opt_in", "budget_relaxation_review"},
			Mechanism: machinecontract.ApprovalMechanismInteractiveDigestConfirmation, DigestRequired: true, YesFlagAllowed: false, ModelSelfApproval: false},
		DatabaseCapabilities:  []string{"postgresql_schema_evidence", "mysql_schema_evidence", "database_cognition", "independent_database_cognition_bootstrap", "environment_credential_provider", "database_access_preflight_v1", "database_to_code_impact_v1", "business_rows_read_zero", "ddl_dml_zero"},
		OverviewDeliveryModes: []string{"full", "checkpoint", "blocked", "scope_absent", "chunked_full"},
		CompatibilityStrategy: []string{"legacy_monolithic_read", "volumes_v1_read_write", "baseline_v1_read", "additive_json_fields", "fail_closed_unknown_schema"},
		NetworkAccessed:       false,
	}, nil
}

func detectLayout(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return "uninitialized", nil
	}
	if err != nil {
		return "", fmt.Errorf("capability_layout_unavailable")
	}
	layout, err := cognition.DetectLayout(data)
	if err != nil {
		return "invalid_or_mixed", nil
	}
	return string(layout), nil
}
