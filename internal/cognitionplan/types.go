// Package cognitionplan implements the D2-A read-only Bootstrap and Legacy
// Migration planner. It may create in-memory candidates and reports, but it has
// no Apply, Baseline, Ledger, recovery, or formal-layout write capability.
package cognitionplan

import (
	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	OperationBootstrap = machinecontract.CognitionOperationBootstrap
	OperationMigration = machinecontract.CognitionOperationMigration
)

type Options struct {
	RepositoryRoot string
	Locale         string
	TargetKinds    []string
}

type Identity struct {
	SHA256 string `json:"sha256"`
}

type InventoryObject struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256"`
	SizeBytes    int64  `json:"size_bytes"`
	Lines        int    `json:"lines"`
	Extension    string `json:"extension"`
	Eligible     bool   `json:"eligible"`
	ScopeRole    string `json:"scope_role,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type EvidenceObject struct {
	SourceID            string `json:"source_id"`
	ObjectRef           string `json:"object_ref"`
	EvidenceVersion     string `json:"evidence_version"`
	TableEvidenceSHA256 string `json:"table_evidence_sha256"`
	EvidenceRef         string `json:"evidence_ref"`
}

type AuthoringTask struct {
	TaskID           string   `json:"task_id"`
	AssetID          string   `json:"asset_id"`
	ObjectKind       string   `json:"object_kind"`
	ObjectRef        string   `json:"object_ref,omitempty"`
	EvidenceRefs     []string `json:"evidence_refs"`
	RequiredSemantic []string `json:"required_semantic_fields"`
	Reason           string   `json:"reason"`
}

type CandidateFramework struct {
	AssetID   string `json:"asset_id"`
	Path      string `json:"path"`
	Framework string `json:"framework"`
}

type FormalAssetState struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type FormalAssetProof struct {
	Before                []FormalAssetState `json:"before"`
	After                 []FormalAssetState `json:"after"`
	FormalAssetsUnchanged bool               `json:"formal_assets_unchanged"`
	BaselineUnchanged     bool               `json:"baseline_unchanged"`
	CurationUnchanged     bool               `json:"curation_unchanged"`
	LedgerWritten         bool               `json:"ledger_written"`
}

type MappingRecord struct {
	UnitID             string   `json:"unit_id"`
	UnitKind           string   `json:"unit_kind"`
	SourceLine         int      `json:"source_line"`
	SourceSHA256       string   `json:"source_sha256"`
	SourceText         string   `json:"source_text"`
	Mode               string   `json:"mode"`
	TargetAsset        string   `json:"target_asset"`
	TargetRef          string   `json:"target_ref,omitempty"`
	ReasonCode         string   `json:"reason_code"`
	DispositionVersion string   `json:"disposition_version"`
	Disposition        string   `json:"disposition"`
	LegacySelfEntry    bool     `json:"legacy_self_entry,omitempty"`
	AllowedTargets     []string `json:"allowed_target_assets"`
}

type MappingCoverage struct {
	ByteReversible                 bool   `json:"byte_reversible"`
	LegacyEntryTotal               int    `json:"legacy_entry_total"`
	LegacyEntryMapped              int    `json:"legacy_entry_mapped"`
	LegacyEntryCoverage            string `json:"legacy_entry_coverage"`
	LegacyEntryDispositionTotal    int    `json:"legacy_entry_disposition_total"`
	LegacyEntryDispositionComplete int    `json:"legacy_entry_disposition_complete"`
	LegacySelfEntryTotal           int    `json:"legacy_self_entry_total"`
	LegacySemanticAtomTotal        int    `json:"legacy_semantic_atom_total"`
	LegacySemanticAtomMapped       int    `json:"legacy_semantic_atom_mapped"`
	LegacySemanticAtomCoverage     string `json:"legacy_semantic_atom_coverage"`
	DuplicateTargetCount           int    `json:"duplicate_target_count"`
	UnexplainedDropCount           int    `json:"unexplained_drop_count"`
	AmbiguousMappingCount          int    `json:"ambiguous_mapping_count"`
	ProjectedCognitionValid        bool   `json:"projected_cognition_valid"`
	SemanticReviewStatus           string `json:"semantic_review_status"`
}

type SemanticMapping struct {
	Version        string          `json:"version"`
	LegacySHA256   string          `json:"legacy_sha256"`
	LegacyPreimage string          `json:"legacy_preimage"`
	Records        []MappingRecord `json:"records"`
	Coverage       MappingCoverage `json:"coverage"`
	MappingSHA256  string          `json:"mapping_sha256"`
}

type Plan struct {
	Version                      string                           `json:"version"`
	Operation                    string                           `json:"operation"`
	Status                       string                           `json:"status"`
	Layout                       string                           `json:"layout"`
	PlanID                       string                           `json:"plan_id"`
	RepositoryIdentity           string                           `json:"repository_identity"`
	LayoutIdentity               string                           `json:"layout_identity"`
	BaselineIdentity             string                           `json:"baseline_identity"`
	InventoryIdentity            string                           `json:"inventory_identity"`
	SourceEvidenceIdentity       string                           `json:"source_evidence_identity"`
	CurationIdentity             string                           `json:"curation_identity"`
	Locale                       string                           `json:"locale"`
	Registry                     cognition.VolumeRegistryDocument `json:"volume_registry"`
	RegistryIdentity             string                           `json:"volume_registry_identity"`
	TargetKinds                  []string                         `json:"target_kinds"`
	RecommendedKinds             []string                         `json:"recommended_kinds"`
	Inventory                    []InventoryObject                `json:"inventory"`
	SafeInventory                afs.SafeInventorySummary         `json:"safe_inventory"`
	BusinessSourceManifest       businesssource.Manifest          `json:"business_source_manifest"`
	Evidence                     []EvidenceObject                 `json:"evidence"`
	AuthoringTasks               []AuthoringTask                  `json:"authoring_tasks"`
	SemanticAuthoringRequirement *SemanticAuthoringRequirement    `json:"semantic_authoring_requirement,omitempty"`
	CandidateFrameworks          []CandidateFramework             `json:"candidate_frameworks"`
	Mapping                      *SemanticMapping                 `json:"semantic_mapping,omitempty"`
	Warnings                     []cognition.Finding              `json:"warnings"`
	FormalAssetProof             FormalAssetProof                 `json:"formal_asset_proof"`
	NetworkAccessed              bool                             `json:"network_accessed"`
	NextAction                   string                           `json:"next_action"`
}

type CandidateAsset struct {
	AssetID string `json:"asset_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type MappingResolution struct {
	UnitID           string `json:"unit_id"`
	TargetAsset      string `json:"target_asset"`
	TargetRef        string `json:"target_ref,omitempty"`
	Reviewer         string `json:"reviewer"`
	SemanticReviewed bool   `json:"semantic_reviewed"`
}

// SemanticAuthoringProvenance is a Host assertion that the model authored the
// semantic payload from the exact Plan-bound evidence. SHA-256 fields provide
// integrity binding only; they are not cryptographic actor authentication.
type SemanticAuthoringProvenance struct {
	Version                string `json:"version"`
	Origin                 string `json:"origin"`
	AuthoringRunID         string `json:"authoring_run_id"`
	PlanID                 string `json:"plan_id"`
	EvidenceBindingSHA256  string `json:"evidence_binding_sha256"`
	CandidatePayloadSHA256 string `json:"candidate_payload_sha256"`
}

// SemanticAuthoringRequirement is the deterministic contract that a Host must
// satisfy. It never asserts that model authoring has occurred.
type SemanticAuthoringRequirement struct {
	Version                  string `json:"version"`
	RequiredOrigin           string `json:"required_origin"`
	AuthoringRunIDRequired   bool   `json:"authoring_run_id_required"`
	DiscoveryPlanID          string `json:"discovery_plan_id"`
	EvidenceBindingSHA256    string `json:"evidence_binding_sha256"`
	CandidatePayloadSHA256   string `json:"candidate_payload_sha256,omitempty"`
	CandidatePayloadRequired bool   `json:"candidate_payload_required"`
}

// SemanticAuthoringDeclaration records the Host's authoring-run assertion at
// Completion time, before the final Candidate payload digest exists.
type SemanticAuthoringDeclaration struct {
	Version               string `json:"version"`
	Origin                string `json:"origin"`
	AuthoringRunID        string `json:"authoring_run_id"`
	DiscoveryPlanID       string `json:"discovery_plan_id"`
	EvidenceBindingSHA256 string `json:"evidence_binding_sha256"`
}

type LayoutCandidate struct {
	Version                     string                       `json:"version"`
	PlanID                      string                       `json:"plan_id"`
	Assets                      []CandidateAsset             `json:"assets"`
	MappingResolutions          []MappingResolution          `json:"mapping_resolutions"`
	SemanticAuthoringProvenance *SemanticAuthoringProvenance `json:"semantic_authoring_provenance,omitempty"`
}

type FileDiff struct {
	Path         string `json:"path"`
	Change       string `json:"change"`
	BeforeSHA256 string `json:"before_sha256"`
	AfterSHA256  string `json:"after_sha256"`
	BeforeBytes  int64  `json:"before_bytes"`
	AfterBytes   int64  `json:"after_bytes"`
}

type PhysicalDiff struct {
	Files              []FileDiff `json:"files"`
	BaselineDelta      string     `json:"baseline_delta"`
	PhysicalDiffSHA256 string     `json:"physical_diff_sha256"`
}

type LogicalChange struct {
	Kind      string `json:"kind"`
	SourceRef string `json:"source_ref,omitempty"`
	TargetRef string `json:"target_ref,omitempty"`
	Mode      string `json:"mode"`
}

type LogicalDiff struct {
	Changes           []LogicalChange `json:"changes"`
	LogicalDiffSHA256 string          `json:"logical_diff_sha256"`
}

type ReviewSets struct {
	Review []string `json:"review_set"`
	Write  []string `json:"write_set"`
	Guard  []string `json:"guard_set"`
}

type Risk struct {
	Code   string `json:"code"`
	Target string `json:"target,omitempty"`
}

type RecoverySummary struct {
	CommitPoint       string   `json:"commit_point"`
	PreimageSet       []string `json:"preimage_set"`
	PostimageSet      []string `json:"postimage_set"`
	RollbackCondition string   `json:"rollback_condition"`
	Direction         string   `json:"direction"`
}

type ApprovalDigest struct {
	Version                string                   `json:"version"`
	Operation              string                   `json:"operation"`
	PlanID                 string                   `json:"plan_id"`
	ProtocolVersion        string                   `json:"protocol_version"`
	RepositoryIdentity     string                   `json:"repository_identity"`
	LayoutIdentity         string                   `json:"layout_identity"`
	BaselineIdentity       string                   `json:"baseline_identity"`
	InventoryIdentity      string                   `json:"inventory_identity"`
	SourceEvidenceIdentity string                   `json:"source_evidence_identity"`
	CandidateAssets        []CandidateAssetIdentity `json:"candidate_asset_identities"`
	MappingSHA256          string                   `json:"mapping_sha256"`
	PhysicalDiffSHA256     string                   `json:"physical_diff_sha256"`
	LogicalDiffSHA256      string                   `json:"logical_diff_sha256"`
	Sets                   ReviewSets               `json:"sets"`
	RecoveryDirection      string                   `json:"recovery_direction"`
	Digest                 string                   `json:"digest"`
}

type CandidateAssetIdentity struct {
	AssetID string `json:"asset_id"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Bytes   int    `json:"bytes"`
}

type SemanticAuthoringVerification struct {
	Version                string `json:"version"`
	Status                 string `json:"status"`
	Origin                 string `json:"origin"`
	AuthoringRunID         string `json:"authoring_run_id"`
	DiscoveryPlanID        string `json:"discovery_plan_id"`
	EvidenceBindingSHA256  string `json:"evidence_binding_sha256"`
	CandidatePayloadSHA256 string `json:"candidate_payload_sha256"`
	ReceiptSHA256          string `json:"receipt_sha256"`
}

type Preview struct {
	Version                      string                         `json:"version"`
	Operation                    string                         `json:"operation"`
	Status                       string                         `json:"status"`
	PlanID                       string                         `json:"plan_id"`
	CandidateIdentity            string                         `json:"candidate_identity"`
	ProjectedCompositeIdentity   string                         `json:"projected_composite_identity"`
	ProjectedDescriptors         []cognition.Descriptor         `json:"projected_descriptors"`
	PhysicalDiff                 PhysicalDiff                   `json:"physical_diff"`
	LogicalDiff                  LogicalDiff                    `json:"logical_diff"`
	SemanticMapping              *SemanticMapping               `json:"semantic_mapping,omitempty"`
	SemanticAuthoringProvenance  *SemanticAuthoringVerification `json:"semantic_authoring_provenance,omitempty"`
	SemanticAuthoringRequirement *SemanticAuthoringRequirement  `json:"semantic_authoring_requirement,omitempty"`
	Sets                         ReviewSets                     `json:"sets"`
	Risks                        []Risk                         `json:"risks"`
	Recovery                     RecoverySummary                `json:"recovery_summary"`
	ApprovalDigest               *ApprovalDigest                `json:"approval_digest,omitempty"`
	FormalAssetProof             FormalAssetProof               `json:"formal_asset_proof"`
	NetworkAccessed              bool                           `json:"network_accessed"`
	NextAction                   string                         `json:"next_action"`
}
