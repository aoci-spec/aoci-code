// Package migrationapply implements the D2-C Legacy-to-Volumes governance
// transaction. Model-authored semantics arrive through D2-A candidates and an
// independently reviewed Apply-grade mapping; this package owns only evidence,
// identity, approval, CAS, Baseline, verification, and recovery.
package migrationapply

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
)

const (
	OperationMigration = "migration"
	OperationReversal  = "migration_reversal"
)

type ByteRange struct {
	Identity  string `json:"identity"`
	Kind      string `json:"kind"`
	ParentID  string `json:"parent_identity,omitempty"`
	ByteStart int64  `json:"byte_start"`
	ByteEnd   int64  `json:"byte_end"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	SHA256    string `json:"sha256"`
}

type SnapshotFinding struct {
	Code     string `json:"code"`
	Line     int    `json:"line,omitempty"`
	Identity string `json:"identity,omitempty"`
}

type SnapshotPreimage struct {
	Path     string `json:"path"`
	State    string `json:"state"`
	SHA256   string `json:"sha256"`
	ByteSize int64  `json:"byte_size"`
	FileMode string `json:"file_mode"`
}

type LegacySnapshot struct {
	Version                string             `json:"version"`
	Eligibility            string             `json:"eligibility"`
	LegacyPath             string             `json:"legacy_path"`
	LegacySHA256           string             `json:"legacy_sha256"`
	LegacyByteSize         int64              `json:"legacy_byte_size"`
	LegacyFileMode         string             `json:"legacy_file_mode"`
	LegacyEncoding         string             `json:"legacy_encoding"`
	LegacyContentBase64    string             `json:"legacy_content_base64"`
	BOM                    string             `json:"bom"`
	LineEndings            string             `json:"line_endings"`
	BaselinePath           string             `json:"baseline_path"`
	BaselineSHA256         string             `json:"baseline_sha256"`
	BaselineByteSize       int64              `json:"baseline_byte_size"`
	BaselineFileMode       string             `json:"baseline_file_mode"`
	BaselineEncoding       string             `json:"baseline_encoding"`
	BaselineContentBase64  string             `json:"baseline_content_base64"`
	EntryCount             int                `json:"entry_count"`
	HeaderState            string             `json:"header_state"`
	ParseIdentity          string             `json:"parse_identity"`
	Ranges                 []ByteRange        `json:"ranges"`
	Findings               []SnapshotFinding  `json:"findings"`
	RepositoryIdentity     string             `json:"repository_identity"`
	LayoutIdentity         string             `json:"layout_identity"`
	BaselineIdentity       string             `json:"baseline_identity"`
	InventoryIdentity      string             `json:"inventory_identity"`
	SourceEvidenceIdentity string             `json:"source_evidence_identity"`
	CurationIdentity       string             `json:"curation_identity"`
	RegistryIdentity       string             `json:"registry_identity"`
	ValidatorIdentity      string             `json:"validator_identity"`
	FormalPreimages        []SnapshotPreimage `json:"formal_preimages"`
	CapturedAt             string             `json:"captured_at"`
	NetworkAccessed        bool               `json:"network_accessed"`
	SnapshotIdentity       string             `json:"snapshot_identity"`
}

type MappingRecord struct {
	SourceIdentity              string             `json:"source_identity"`
	ParentSourceIdentity        string             `json:"parent_source_identity,omitempty"`
	SourceByteStart             int64              `json:"source_byte_start"`
	SourceByteEnd               int64              `json:"source_byte_end"`
	SourceLineStart             int                `json:"source_line_start"`
	SourceLineEnd               int                `json:"source_line_end"`
	SourceSHA256                string             `json:"source_sha256"`
	SourceKind                  string             `json:"source_kind"`
	SemanticRole                string             `json:"semantic_role"`
	TargetAsset                 string             `json:"target_asset"`
	TargetObject                string             `json:"target_object,omitempty"`
	TargetSemanticRangeIdentity string             `json:"target_semantic_range_identity,omitempty"`
	MappingMode                 string             `json:"mapping_mode"`
	AuthoringTaskID             string             `json:"authoring_task_id,omitempty"`
	MappingGroupID              string             `json:"mapping_group_id,omitempty"`
	ReviewStatus                string             `json:"review_status"`
	Reviewer                    string             `json:"reviewer,omitempty"`
	EntryPreservation           *EntryPreservation `json:"entry_preservation,omitempty"`
}

type IdentityCanonicalizationProposal struct {
	SourceObjectIdentity string `json:"source_object_identity"`
	TargetObjectIdentity string `json:"target_object_identity"`
	OneToOne             bool   `json:"one_to_one"`
	TargetExists         bool   `json:"target_exists"`
	RepresentationOnly   bool   `json:"representation_only"`
	ReviewStatus         string `json:"review_status"`
	Reviewer             string `json:"reviewer,omitempty"`
}

type EntryPreservation struct {
	Version                          string                            `json:"version"`
	PreservedFields                  []string                          `json:"preserved_fields"`
	RegeneratedFields                []string                          `json:"regenerated_fields"`
	IdentityCanonicalizationProposal *IdentityCanonicalizationProposal `json:"identity_canonicalization_proposal,omitempty"`
	ReviewStatus                     string                            `json:"review_status"`
	Reviewer                         string                            `json:"reviewer"`
}

type MappingGroup struct {
	MappingGroupID        string   `json:"mapping_group_id"`
	SourceIdentities      []string `json:"source_identities"`
	TargetRangeIdentities []string `json:"target_range_identities"`
	AuthoringTaskID       string   `json:"authoring_task_id"`
	ReviewStatus          string   `json:"review_status"`
	Reviewer              string   `json:"reviewer"`
}

type MappingAuthoringTask struct {
	TaskID                    string             `json:"task_id"`
	SourceIdentities          []string           `json:"source_identities"`
	SourceEvidenceRefs        []string           `json:"source_evidence_refs"`
	SourceEvidenceIdentity    string             `json:"source_evidence_identity"`
	TargetAsset               string             `json:"target_asset"`
	TargetObject              string             `json:"target_object,omitempty"`
	CandidateRangeIdentities  []string           `json:"candidate_range_identities"`
	Status                    string             `json:"status"`
	Reviewer                  string             `json:"reviewer,omitempty"`
	EntryPreservationProposal *EntryPreservation `json:"entry_preservation_proposal,omitempty"`
}

type TargetRange struct {
	Identity  string `json:"identity"`
	Asset     string `json:"asset"`
	Object    string `json:"object,omitempty"`
	Kind      string `json:"kind"`
	ByteStart int64  `json:"byte_start"`
	ByteEnd   int64  `json:"byte_end"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	SHA256    string `json:"sha256"`
}

type MappingCoverage struct {
	ByteReversible                 bool   `json:"byte_reversible"`
	LegacyEntryTotal               int    `json:"legacy_entry_total"`
	LegacyEntryMapped              int    `json:"legacy_entry_mapped"`
	LegacyEntryCoverage            string `json:"legacy_entry_coverage"`
	LegacySemanticAtomTotal        int    `json:"legacy_semantic_atom_total"`
	LegacySemanticAtomMapped       int    `json:"legacy_semantic_atom_mapped"`
	LegacySemanticAtomCoverage     string `json:"legacy_semantic_atom_coverage"`
	DuplicateTargetCount           int    `json:"duplicate_target_count"`
	UnexplainedDropCount           int    `json:"unexplained_drop_count"`
	AmbiguousMappingCount          int    `json:"ambiguous_mapping_count"`
	ProjectedCognitionValid        bool   `json:"projected_cognition_valid"`
	AllModelAuthoringTasksComplete bool   `json:"all_model_authoring_tasks_complete"`
	SemanticReviewStatus           string `json:"semantic_review_status"`
	SemanticEquivalence            string `json:"semantic_equivalence"`
	PreservedFieldCount            int    `json:"preserved_field_count"`
	RegeneratedFieldCount          int    `json:"regenerated_field_count"`
	FieldPreservedEntryCount       int    `json:"field_preserved_entry_count"`
	FullRegeneratedEntryCount      int    `json:"full_regenerated_entry_count"`
	IdentityCanonicalizationCount  int    `json:"identity_canonicalization_count"`
}

type FieldPreservationDiff struct {
	SourceIdentity           string   `json:"source_identity"`
	TargetObject             string   `json:"target_object"`
	PreservedFields          []string `json:"preserved_fields"`
	RegeneratedFields        []string `json:"regenerated_fields"`
	IdentityCanonicalization bool     `json:"identity_canonicalization"`
	Mode                     string   `json:"mode"`
}

type MigrationSemanticDiff struct {
	Version           string                  `json:"version"`
	Entries           []FieldPreservationDiff `json:"entries"`
	PreservedFields   int                     `json:"preserved_fields"`
	RegeneratedFields int                     `json:"regenerated_fields"`
	FullRegenerated   int                     `json:"full_regenerated_entries"`
	DiffSHA256        string                  `json:"diff_sha256"`
}

type MigrationMapping struct {
	Version              string                 `json:"version"`
	SnapshotIdentity     string                 `json:"snapshot_identity"`
	PlannerMappingSHA256 string                 `json:"planner_mapping_sha256"`
	TargetRanges         []TargetRange          `json:"target_ranges"`
	Records              []MappingRecord        `json:"records"`
	MappingGroups        []MappingGroup         `json:"mapping_groups"`
	AuthoringTasks       []MappingAuthoringTask `json:"authoring_tasks"`
	Coverage             MappingCoverage        `json:"coverage"`
	MappingSHA256        string                 `json:"mapping_sha256"`
}

type ApplyRequest struct {
	Version           string                        `json:"version"`
	Snapshot          LegacySnapshot                `json:"legacy_snapshot"`
	Plan              cognitionplan.Plan            `json:"plan"`
	Mapping           MigrationMapping              `json:"apply_grade_mapping"`
	Candidate         cognitionplan.LayoutCandidate `json:"candidate"`
	Preview           cognitionplan.Preview         `json:"preview"`
	BaselineTimestamp string                        `json:"baseline_timestamp"`
}

type FormalPostimage struct {
	AssetID        string `json:"asset_id"`
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	PreimageState  string `json:"preimage_state"`
	PreimageSHA256 string `json:"preimage_sha256"`
	PostSHA256     string `json:"post_sha256"`
	ByteSize       int64  `json:"byte_size"`
	FileMode       string `json:"file_mode"`
	Content        string `json:"content"`
}

type BaselinePostimage struct {
	Path           string `json:"path"`
	PreimageSHA256 string `json:"preimage_sha256"`
	PostSHA256     string `json:"post_sha256"`
	ByteSize       int64  `json:"byte_size"`
	FileMode       string `json:"file_mode"`
	Content        string `json:"content"`
}

type ApplyEnvelope struct {
	Version                    string                              `json:"version"`
	RequestVersion             string                              `json:"request_version"`
	Operation                  string                              `json:"operation"`
	PlanID                     string                              `json:"plan_id"`
	CandidateIdentity          string                              `json:"candidate_identity"`
	D2AApprovalDigest          string                              `json:"d2a_approval_digest"`
	Snapshot                   LegacySnapshot                      `json:"legacy_snapshot"`
	Mapping                    MigrationMapping                    `json:"apply_grade_mapping"`
	MappingSHA256              string                              `json:"mapping_sha256"`
	RepositoryIdentity         string                              `json:"repository_identity"`
	LayoutIdentity             string                              `json:"layout_identity"`
	BaselineIdentity           string                              `json:"baseline_identity"`
	InventoryIdentity          string                              `json:"inventory_identity"`
	SourceEvidenceIdentity     string                              `json:"source_evidence_identity"`
	CurationIdentity           string                              `json:"curation_identity"`
	RegistryIdentity           string                              `json:"registry_identity"`
	ValidatorIdentity          string                              `json:"validator_identity"`
	ProjectedCompositeIdentity string                              `json:"projected_composite_identity"`
	Locale                     string                              `json:"locale"`
	Plan                       cognitionplan.Plan                  `json:"plan"`
	Candidate                  cognitionplan.LayoutCandidate       `json:"candidate"`
	Preview                    cognitionplan.Preview               `json:"preview"`
	RuntimeBoundary            FormalPostimage                     `json:"runtime_boundary"`
	VolumeTargets              []FormalPostimage                   `json:"volume_targets"`
	Root                       FormalPostimage                     `json:"root"`
	Baseline                   BaselinePostimage                   `json:"baseline"`
	DatabaseBindings           []baseline.DatabaseCognitionBinding `json:"database_bindings"`
	PhysicalDiffSHA256         string                              `json:"physical_diff_sha256"`
	SemanticDiffSHA256         string                              `json:"semantic_diff_sha256"`
	SemanticDiff               MigrationSemanticDiff               `json:"semantic_diff"`
	RiskDiffSHA256             string                              `json:"risk_diff_sha256"`
	ReviewSet                  []string                            `json:"review_set"`
	WriteSet                   []string                            `json:"write_set"`
	GuardSet                   []string                            `json:"guard_set"`
	WriteOrder                 []string                            `json:"write_order"`
	RootLast                   bool                                `json:"root_last"`
	RecoveryDirection          string                              `json:"recovery_direction"`
	NetworkAccessed            bool                                `json:"network_accessed"`
	PreparedAt                 string                              `json:"prepared_at"`
	EnvelopeDigest             string                              `json:"envelope_digest"`
}

type Approval struct {
	Version                string   `json:"version"`
	Operation              string   `json:"operation"`
	D2AApprovalDigest      string   `json:"d2a_approval_digest"`
	ApplyEnvelopeDigest    string   `json:"apply_envelope_digest"`
	MappingSHA256          string   `json:"mapping_sha256"`
	ApprovedWriteSet       []string `json:"approved_write_set"`
	ApprovedRecoveryPolicy string   `json:"approved_recovery_policy"`
	Actor                  string   `json:"actor"`
	Mechanism              string   `json:"approval_mechanism"`
	ApprovedAt             string   `json:"approved_at"`
	ApprovalDigest         string   `json:"approval_digest"`
}

type RecoveryIntent struct {
	Version        string                         `json:"version"`
	Operation      string                         `json:"operation"`
	TransactionID  string                         `json:"transaction_id"`
	Envelope       ApplyEnvelope                  `json:"envelope"`
	Approval       Approval                       `json:"approval"`
	Staging        []cognitiontxn.StagedPostimage `json:"staging"`
	CreatedAt      string                         `json:"created_at"`
	RecoveryDigest string                         `json:"recovery_digest"`
}

type MigrationReceipt struct {
	Version                    string   `json:"version"`
	Operation                  string   `json:"operation"`
	TransactionID              string   `json:"transaction_id"`
	SnapshotIdentity           string   `json:"snapshot_identity"`
	MappingSHA256              string   `json:"mapping_sha256"`
	ApprovalDigest             string   `json:"approval_digest"`
	EnvelopeDigest             string   `json:"envelope_digest"`
	LegacySHA256               string   `json:"legacy_sha256"`
	RootSHA256                 string   `json:"root_sha256"`
	BaselinePreimageSHA256     string   `json:"baseline_preimage_sha256"`
	BaselinePostimageSHA256    string   `json:"baseline_postimage_sha256"`
	ProjectedCompositeIdentity string   `json:"projected_composite_identity"`
	FormalPostimagePaths       []string `json:"formal_postimage_paths"`
	ByteReversible             bool     `json:"byte_reversible"`
	SemanticCoverageComplete   bool     `json:"semantic_coverage_complete"`
	SemanticEquivalence        string   `json:"semantic_equivalence"`
	CompletedAt                string   `json:"completed_at"`
	NetworkAccessed            bool     `json:"network_accessed"`
	ReceiptDigest              string   `json:"receipt_digest"`
}

type ApplyResult struct {
	Version           string   `json:"version"`
	Operation         string   `json:"operation"`
	TransactionID     string   `json:"transaction_id"`
	Status            string   `json:"status"`
	ActiveLayout      string   `json:"active_layout"`
	FormalComplete    bool     `json:"formal_assets_complete"`
	WrittenPaths      []string `json:"written_paths"`
	RecoveredPaths    []string `json:"recovered_paths"`
	BaselineSHA256    string   `json:"baseline_sha256"`
	CompositeIdentity string   `json:"composite_identity"`
	ReceiptDigest     string   `json:"receipt_digest,omitempty"`
	NetworkAccessed   bool     `json:"network_accessed"`
	NextAction        string   `json:"next_action"`
	ResultDigest      string   `json:"result_digest"`
}

type TargetStatus struct {
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	DiskState    string `json:"disk_state"`
	StagingState string `json:"staging_state"`
	ActualSHA256 string `json:"actual_sha256,omitempty"`
}

type TransactionStatus struct {
	Version            string         `json:"version"`
	Operation          string         `json:"operation"`
	TransactionID      string         `json:"transaction_id"`
	Status             string         `json:"status"`
	ActiveLayout       string         `json:"active_layout"`
	FormalComplete     bool           `json:"formal_assets_complete"`
	RecoveryPending    bool           `json:"recovery_pending"`
	ThirdPartyConflict bool           `json:"third_party_conflict"`
	SnapshotState      string         `json:"snapshot_state"`
	Targets            []TargetStatus `json:"targets"`
	NextActions        []string       `json:"next_actions"`
	NetworkAccessed    bool           `json:"network_accessed"`
	StatusDigest       string         `json:"status_digest"`
}

type ReversalPlan struct {
	Version                string             `json:"version"`
	Operation              string             `json:"operation"`
	OriginalTransactionID  string             `json:"original_transaction_id"`
	OriginalReceiptDigest  string             `json:"original_receipt_digest"`
	SnapshotIdentity       string             `json:"snapshot_identity"`
	RepositoryIdentity     string             `json:"repository_identity"`
	InventoryIdentity      string             `json:"inventory_identity"`
	SourceEvidenceIdentity string             `json:"source_evidence_identity"`
	CurationIdentity       string             `json:"curation_identity"`
	RegistryIdentity       string             `json:"registry_identity"`
	CurrentPostimages      []SnapshotPreimage `json:"current_postimages"`
	WriteSet               []string           `json:"write_set"`
	WriteOrder             []string           `json:"write_order"`
	RecoveryDirection      string             `json:"recovery_direction"`
	Eligible               bool               `json:"eligible"`
	Risks                  []string           `json:"risks"`
	PreparedAt             string             `json:"prepared_at"`
	NetworkAccessed        bool               `json:"network_accessed"`
	PlanDigest             string             `json:"plan_digest"`
}

type ReversalApproval struct {
	Version            string   `json:"version"`
	Operation          string   `json:"operation"`
	ReversalPlanDigest string   `json:"reversal_plan_digest"`
	ApprovedWriteSet   []string `json:"approved_write_set"`
	ApprovedPolicy     string   `json:"approved_policy"`
	Actor              string   `json:"actor"`
	Mechanism          string   `json:"approval_mechanism"`
	ApprovedAt         string   `json:"approved_at"`
	ApprovalDigest     string   `json:"approval_digest"`
}
