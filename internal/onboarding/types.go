// Package onboarding persists deterministic Host progress for Bootstrap and
// Legacy Migration without generating or evaluating model-owned semantics.
package onboarding

import (
	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	SessionVersion          = machinecontract.CognitionOnboardingSessionV2
	LegacySessionVersion    = machinecontract.CognitionOnboardingSessionV1
	BatchVersion            = machinecontract.CognitionOnboardingBatchV2
	LegacyBatchVersion      = machinecontract.CognitionOnboardingBatchV1
	CompletionVersion       = machinecontract.CognitionOnboardingCompleteV2
	LegacyCompletionVersion = machinecontract.CognitionOnboardingCompleteV1
)

type ActiveAuthoringBatch struct {
	BatchID          string   `json:"batch_id"`
	TaskIDs          []string `json:"task_ids"`
	EvidenceBytes    int64    `json:"evidence_bytes"`
	MaxObjects       int      `json:"max_objects,omitempty"`
	MaxEvidenceBytes int64    `json:"max_evidence_bytes,omitempty"`
}

type DatabaseSourceProposal struct {
	Version               string   `json:"version"`
	EvidencePaths         []string `json:"evidence_paths"`
	EngineCandidates      []string `json:"engine_candidates"`
	SourceIDRequired      bool     `json:"source_id_required"`
	DatabaseRequired      bool     `json:"database_or_namespace_required"`
	CredentialEnvRequired bool     `json:"credential_env_name_required"`
	CredentialValueStored bool     `json:"credential_value_stored"`
}

type HostDeliveryReceipt struct {
	Version            string `json:"version"`
	Scope              string `json:"scope"`
	BodySHA256         string `json:"body_sha256"`
	BodyBytes          int    `json:"body_bytes"`
	EndMarkerObserved  bool   `json:"end_marker_observed"`
	BodySHA256Verified bool   `json:"body_sha256_verified"`
	BodyBytesVerified  bool   `json:"body_bytes_verified"`
	Confirmed          bool   `json:"confirmed"`
}

type Session struct {
	Version                      string                                      `json:"version"`
	Revision                     int                                         `json:"revision"`
	Status                       string                                      `json:"status"`
	OnboardingSessionID          string                                      `json:"onboarding_session_id"`
	Operation                    string                                      `json:"operation"`
	AutomationPolicy             *config.AutomationPolicy                    `json:"automation_policy,omitempty"`
	AuthorizationProjection      *bootstrapapply.AutoEligibility             `json:"authorization_projection,omitempty"`
	RepositoryIdentity           string                                      `json:"repository_identity"`
	CurrentLayout                string                                      `json:"current_layout"`
	SafeInventoryIdentity        string                                      `json:"safe_inventory_identity"`
	BusinessSourceManifest       businesssource.Manifest                     `json:"business_source_manifest"`
	DatabaseSourceProposal       *DatabaseSourceProposal                     `json:"database_source_proposal,omitempty"`
	EvidenceIdentity             string                                      `json:"evidence_identity"`
	CompletedAuthoringTargets    []string                                    `json:"completed_authoring_targets"`
	PendingAuthoringTargets      []string                                    `json:"pending_authoring_targets"`
	SemanticAuthoringDeclaration *cognitionplan.SemanticAuthoringDeclaration `json:"semantic_authoring_declaration,omitempty"`
	ActiveAuthoringBatch         *ActiveAuthoringBatch                       `json:"active_authoring_batch,omitempty"`
	CandidateAssetIdentities     map[string]string                           `json:"candidate_asset_identities"`
	CandidateIdentity            string                                      `json:"candidate_identity,omitempty"`
	MappingIdentity              string                                      `json:"mapping_identity,omitempty"`
	PreviewIdentity              string                                      `json:"preview_identity,omitempty"`
	ApprovalState                string                                      `json:"approval_state"`
	TransactionState             string                                      `json:"transaction_state"`
	TransactionID                string                                      `json:"transaction_id,omitempty"`
	LastSuccessPoint             string                                      `json:"last_success_point"`
	GovernanceResult             string                                      `json:"governance_result,omitempty"`
	StructureValid               bool                                        `json:"structure_valid"`
	GovernanceAligned            bool                                        `json:"governance_aligned"`
	CheckOK                      bool                                        `json:"check_ok"`
	GuideStage                   string                                      `json:"guide_stage,omitempty"`
	PendingWarnings              []string                                    `json:"pending_warnings"`
	NextAction                   string                                      `json:"next_action"`
	HostDeliveryReceipt          *HostDeliveryReceipt                        `json:"host_delivery_receipt,omitempty"`
	RecoveryDirection            string                                      `json:"recovery_direction"`
	PlanID                       string                                      `json:"plan_id"`
	PlanArtifact                 string                                      `json:"plan_artifact"`
	SnapshotArtifact             string                                      `json:"snapshot_artifact,omitempty"`
	CandidateArtifact            string                                      `json:"candidate_artifact,omitempty"`
	MappingArtifact              string                                      `json:"mapping_artifact,omitempty"`
	PreviewArtifact              string                                      `json:"preview_artifact,omitempty"`
	EnvelopeArtifact             string                                      `json:"envelope_artifact,omitempty"`
	ApprovalArtifact             string                                      `json:"approval_artifact,omitempty"`
	ResultArtifact               string                                      `json:"result_artifact,omitempty"`
	FrozenBaselineTimestamp      string                                      `json:"frozen_baseline_timestamp"`
	CreatedAt                    string                                      `json:"created_at"`
	UpdatedAt                    string                                      `json:"updated_at"`
	BusinessRowsRead             int                                         `json:"business_rows_read"`
	DDLDMLStatements             int                                         `json:"ddl_dml_statements"`
	NetworkAccessed              bool                                        `json:"network_accessed"`
	Plan                         *cognitionplan.Plan                         `json:"-"`
	PreimageSHA256               string                                      `json:"-"`
}

type AuthoringBatch struct {
	Version                      string                                      `json:"version"`
	OnboardingSessionID          string                                      `json:"onboarding_session_id"`
	BatchID                      string                                      `json:"batch_id"`
	Tasks                        []cognitionplan.AuthoringTask               `json:"tasks"`
	ObjectCount                  int                                         `json:"object_count"`
	EvidenceBytes                int64                                       `json:"evidence_bytes"`
	CompletedCount               int                                         `json:"completed_count"`
	PendingCount                 int                                         `json:"pending_count"`
	NextAction                   string                                      `json:"next_action"`
	SemanticGenerated            bool                                        `json:"semantic_generated"`
	SemanticAuthoringRequirement *cognitionplan.SemanticAuthoringRequirement `json:"semantic_authoring_requirement,omitempty"`
	PlanArtifact                 string                                      `json:"plan_artifact,omitempty"`
	CompletionRequestTemplate    *Completion                                 `json:"completion_request_template,omitempty"`
	CandidateDraftRequest        *CandidateDraftRequest                      `json:"candidate_draft_request,omitempty"`
	NextActionContract           *NextActionContract                         `json:"next_action_contract,omitempty"`
}

type Completion struct {
	Version                      string                                      `json:"version"`
	SessionID                    string                                      `json:"onboarding_session_id"`
	BatchID                      string                                      `json:"batch_id,omitempty"`
	CompletedTasks               []string                                    `json:"completed_task_ids"`
	SemanticAuthoringDeclaration *cognitionplan.SemanticAuthoringDeclaration `json:"semantic_authoring_declaration,omitempty"`
}

// EffectiveAutomationPolicy returns the immutable policy captured when this
// Session started. Sessions created before this projection existed remain on
// the legacy human-approval boundary regardless of later config changes.
func EffectiveAutomationPolicy(session *Session) config.AutomationPolicy {
	if session == nil || session.AutomationPolicy == nil {
		return config.AutomationPolicy{Mode: config.AutomationModeLegacy, Source: machinecontract.CognitionAutomationPolicyPersistedLegacy}
	}
	return *session.AutomationPolicy
}
