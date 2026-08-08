// Package bootstrapapply implements the D2-B new-repository Bootstrap
// transaction. It consumes model-authored, D2-A-validated bytes and owns only
// governance. It has no semantic authoring or Legacy migration capability.
package bootstrapapply

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	OperationBootstrap      = machinecontract.CognitionOperationBootstrap
	PreimageAbsent          = machinecontract.CognitionBootstrapPreimageAbsent
	PreimageOfficialMinimal = machinecontract.CognitionBootstrapPreimageOfficialMinimal
	RecoveryPolicy          = machinecontract.CognitionBootstrapRecoveryPolicy
	ApprovalMechanism       = machinecontract.CognitionBootstrapApprovalMechanism
	AutoApprovalMechanism   = machinecontract.CognitionBootstrapAutoApprovalMechanism

	StatePreimage       = machinecontract.CognitionBootstrapDiskPreimage
	StatePostimage      = machinecontract.CognitionBootstrapDiskPostimage
	StateUnknown        = machinecontract.CognitionBootstrapDiskUnknown
	StateWrongType      = machinecontract.CognitionBootstrapDiskWrongType
	StateMissingStaging = machinecontract.CognitionBootstrapDiskMissingStaging

	StatusPrepared                 = machinecontract.CognitionBootstrapStatusPrepared
	StatusRecoveryRequiredInactive = machinecontract.CognitionBootstrapStatusRecoveryRequiredInactive
	StatusRecoveryRequiredActive   = machinecontract.CognitionBootstrapStatusRecoveryRequiredActive
	StatusRecoveryConflict         = machinecontract.CognitionBootstrapStatusRecoveryConflict
	StatusApplied                  = machinecontract.CognitionBootstrapStatusApplied
	StatusAlreadyApplied           = machinecontract.CognitionBootstrapStatusAlreadyApplied
	StatusRolledBack               = machinecontract.CognitionBootstrapStatusRolledBack
)

type ApplyRequest struct {
	Version           string                        `json:"version"`
	Plan              cognitionplan.Plan            `json:"plan"`
	Candidate         cognitionplan.LayoutCandidate `json:"candidate"`
	Preview           cognitionplan.Preview         `json:"preview"`
	BaselineTimestamp string                        `json:"baseline_timestamp"`
}

type FormalPostimage struct {
	AssetID          string `json:"asset_id"`
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	ExpectedPreimage string `json:"expected_preimage"`
	PreimageSHA256   string `json:"preimage_sha256,omitempty"`
	PreimageContent  string `json:"preimage_content,omitempty"`
	PostSHA256       string `json:"post_sha256"`
	ByteSize         int64  `json:"byte_size"`
	FileMode         string `json:"file_mode"`
	Content          string `json:"content"`
}

type BaselinePostimage struct {
	Path             string `json:"path"`
	ExpectedPreimage string `json:"expected_preimage"`
	PostSHA256       string `json:"post_sha256"`
	ByteSize         int64  `json:"byte_size"`
	FileMode         string `json:"file_mode"`
	Content          string `json:"content"`
}

type ApplyEnvelope struct {
	Version                    string                              `json:"version"`
	RequestVersion             string                              `json:"request_version"`
	Operation                  string                              `json:"operation"`
	PlanID                     string                              `json:"plan_id"`
	CandidateIdentity          string                              `json:"candidate_identity"`
	D2AApprovalDigest          string                              `json:"d2a_approval_digest"`
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
	AutomationPolicy           config.AutomationPolicy             `json:"automation_policy"`
	Plan                       cognitionplan.Plan                  `json:"plan"`
	Candidate                  cognitionplan.LayoutCandidate       `json:"candidate"`
	Preview                    cognitionplan.Preview               `json:"preview"`
	RuntimeBoundary            FormalPostimage                     `json:"runtime_boundary"`
	Targets                    []FormalPostimage                   `json:"targets"`
	Baseline                   BaselinePostimage                   `json:"baseline"`
	DatabaseBindings           []baseline.DatabaseCognitionBinding `json:"database_bindings"`
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
	ApprovedWriteSet       []string `json:"approved_write_set"`
	ApprovedRecoveryPolicy string   `json:"approved_recovery_policy"`
	Actor                  string   `json:"actor"`
	Mechanism              string   `json:"approval_mechanism"`
	ApprovedAt             string   `json:"approved_at"`
	ApprovalDigest         string   `json:"approval_digest"`
}

type StagingPostimage struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	ByteSize   int64  `json:"byte_size"`
	StagingRel string `json:"staging_rel"`
}

type RecoveryIntent struct {
	Version        string             `json:"version"`
	TransactionID  string             `json:"transaction_id"`
	Envelope       ApplyEnvelope      `json:"envelope"`
	Approval       Approval           `json:"approval"`
	Staging        []StagingPostimage `json:"staging"`
	CreatedAt      string             `json:"created_at"`
	RecoveryDigest string             `json:"recovery_digest"`
}

type ApplyResult struct {
	Version           string   `json:"version"`
	Operation         string   `json:"operation"`
	TransactionID     string   `json:"transaction_id"`
	Status            string   `json:"status"`
	LayoutActivated   bool     `json:"layout_activated"`
	FormalComplete    bool     `json:"formal_assets_complete"`
	WrittenPaths      []string `json:"written_paths"`
	RecoveredPaths    []string `json:"recovered_paths"`
	BaselineSHA256    string   `json:"baseline_sha256"`
	CompositeIdentity string   `json:"composite_identity"`
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
	TransactionID      string         `json:"transaction_id"`
	Status             string         `json:"status"`
	LayoutActivated    bool           `json:"layout_activated"`
	FormalComplete     bool           `json:"formal_assets_complete"`
	RecoveryPending    bool           `json:"recovery_pending"`
	ThirdPartyConflict bool           `json:"third_party_conflict"`
	Targets            []TargetStatus `json:"targets"`
	NextActions        []string       `json:"next_actions"`
	NetworkAccessed    bool           `json:"network_accessed"`
	StatusDigest       string         `json:"status_digest"`
}
