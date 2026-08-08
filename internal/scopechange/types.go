// Package scopechange implements the single governed transaction for Managed
// Scope role changes, model-authored Entry/Header candidates, Baseline roles,
// observe fingerprints, policy activation, Ledger, and recovery.
package scopechange

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

const Operation = "scope"

const (
	DispositionNoUniqueSemantics = "no_unique_semantics"
	DispositionTransferEntry     = "transfer_to_existing_entry"
	DispositionTransferHeader    = "transfer_to_header"
	DispositionTransferSpec      = "transfer_to_spec"
	DispositionRetainIndex       = "retain_as_index"
	DispositionExplicitDrop      = "explicit_drop_approved"
	ReviewStatusReviewed         = "reviewed"
)

type EntryCandidate struct {
	CandidateID        string `json:"candidate_id"`
	Path               string `json:"path"`
	SourceSHA256       string `json:"source_sha256"`
	CurrentEntrySHA256 string `json:"current_entry_sha256,omitempty"`
	NewEntry           string `json:"new_entry"`
	ReviewStatus       string `json:"review_status"`
}

type HeaderCandidate struct {
	CandidateID         string `json:"candidate_id"`
	CurrentHeaderSHA256 string `json:"current_header_sha256"`
	NewHeader           string `json:"new_header"`
	ReviewStatus        string `json:"review_status"`
}

type EntryDisposition struct {
	Version            string   `json:"version"`
	SourcePath         string   `json:"source_path"`
	CurrentEntrySHA256 string   `json:"current_entry_sha256"`
	TargetRole         string   `json:"target_role"`
	UniqueSemantics    []string `json:"unique_semantics"`
	Disposition        string   `json:"disposition"`
	TargetEntry        string   `json:"target_entry,omitempty"`
	ReviewStatus       string   `json:"review_status"`
	Reviewer           string   `json:"reviewer"`
}

type CandidateSet struct {
	Version        string             `json:"version"`
	Entries        []EntryCandidate   `json:"entries"`
	Header         *HeaderCandidate   `json:"header,omitempty"`
	Dispositions   []EntryDisposition `json:"dispositions"`
	Curation       *curation.Document `json:"curation,omitempty"`
	ObserveReview  *ObserveReview     `json:"observe_review,omitempty"`
	SafetyApproval *SafetyApproval    `json:"safety_approval,omitempty"`
}

type SafetyApproval struct {
	Version        string   `json:"version"`
	PolicyIdentity string   `json:"policy_identity"`
	ExactPaths     []string `json:"exact_paths"`
	Actor          string   `json:"actor"`
	Mechanism      string   `json:"approval_mechanism"`
	ApprovedAt     string   `json:"approved_at"`
	ApprovalDigest string   `json:"approval_digest"`
}

type ObserveReview struct {
	Paths        []string `json:"paths"`
	ReviewStatus string   `json:"review_status"`
	Reviewer     string   `json:"reviewer"`
}

type RoleChange struct {
	Path         string `json:"path"`
	OldRole      string `json:"old_role"`
	NewRole      string `json:"new_role"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
}

type CoverageReduction struct {
	Path           string `json:"path"`
	OldRole        string `json:"old_role"`
	NewRole        string `json:"new_role"`
	AuthoringState string `json:"authoring_state"`
	DecisionBasis  string `json:"decision_basis"`
	RuleID         string `json:"rule_id,omitempty"`
}

type ScopeObject struct {
	Path         string `json:"path"`
	Role         string `json:"role"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
}

type EntryChange struct {
	Path         string `json:"path"`
	Action       string `json:"action"`
	BeforeSHA256 string `json:"before_sha256,omitempty"`
	AfterSHA256  string `json:"after_sha256,omitempty"`
}

type Risk struct {
	Level                         string `json:"level"`
	LargeReduction                bool   `json:"large_reduction"`
	EntryRemovalThreshold         int    `json:"entry_removal_threshold"`
	EntryRemovalPercentThreshold  int    `json:"entry_removal_percent_threshold"`
	RootOrPrimaryReduction        bool   `json:"root_or_primary_reduction"`
	HighRiskOptIn                 bool   `json:"high_risk_opt_in"`
	BudgetPolicyChange            bool   `json:"budget_policy_change"`
	BudgetRelaxation              bool   `json:"budget_relaxation"`
	EntryRemovalCount             int    `json:"entry_removal_count"`
	P0                            int    `json:"p0"`
	P1                            int    `json:"p1"`
	CognitionCoverageReduction    bool   `json:"cognition_coverage_reduction"`
	TransportConstraintNotAllowed bool   `json:"transport_constraint_not_allowed"`
	CoverageReductionCount        int    `json:"coverage_reduction_count"`
}

type ApplyAuthorizationPolicy struct {
	Version           string `json:"version"`
	Operation         string `json:"operation"`
	AutomationMode    string `json:"automation_mode"`
	ScopeApprovalMode string `json:"scope_approval_mode"`
	EffectiveMode     string `json:"effective_mode"`
}

type Plan struct {
	Version                     string                   `json:"version"`
	PlanID                      string                   `json:"plan_id"`
	RepositoryRootIdentity      string                   `json:"repository_root_identity"`
	OldPolicyIdentity           string                   `json:"old_policy_identity"`
	NewPolicyIdentity           string                   `json:"new_policy_identity"`
	OldBudgetPolicyIdentity     string                   `json:"old_budget_policy_identity,omitempty"`
	NewBudgetPolicyIdentity     string                   `json:"new_budget_policy_identity"`
	IndexAdded                  []ScopeObject            `json:"index_added"`
	IndexRemoved                []ScopeObject            `json:"index_removed"`
	ObserveAdded                []ScopeObject            `json:"observe_added"`
	ObserveRemoved              []ScopeObject            `json:"observe_removed"`
	ExcludeAdded                []ScopeObject            `json:"exclude_added"`
	ExcludeRemoved              []ScopeObject            `json:"exclude_removed"`
	Preserved                   []ScopeObject            `json:"preserved"`
	RoleChanges                 []RoleChange             `json:"role_changes"`
	CoverageReductions          []CoverageReduction      `json:"coverage_reductions"`
	EntryCreates                []EntryChange            `json:"entry_creates"`
	EntryRemoves                []EntryChange            `json:"entry_removes"`
	EntryUpdates                []EntryChange            `json:"entry_updates"`
	RetentionReview             []EntryDisposition       `json:"retention_review"`
	BaselineAdded               []ScopeObject            `json:"baseline_added"`
	BaselineRemoved             []ScopeObject            `json:"baseline_removed"`
	ObserveFingerprintAdded     []ScopeObject            `json:"observe_fingerprint_added"`
	ObserveFingerprintRemoved   []ScopeObject            `json:"observe_fingerprint_removed"`
	WholeIndexBefore            cognitionbudget.Report   `json:"whole_index_before"`
	WholeIndexAfter             cognitionbudget.Report   `json:"whole_index_after"`
	Risk                        Risk                     `json:"risk"`
	AuthorizationPolicy         ApplyAuthorizationPolicy `json:"authorization_policy"`
	AuthorizationPolicyIdentity string                   `json:"authorization_policy_identity"`
	WriteSet                    []string                 `json:"write_set"`
	GuardSet                    []string                 `json:"guard_set"`
	RecoveryDirection           string                   `json:"recovery_direction"`
	InteractionRequired         bool                     `json:"interaction_required"`
	ConfirmationPhrase          string                   `json:"confirmation_phrase,omitempty"`
	PreparedAt                  string                   `json:"prepared_at"`
	NetworkAccessed             bool                     `json:"network_accessed"`
}

type FormalImage struct {
	Path            string `json:"path"`
	PreimageState   string `json:"preimage_state"`
	PreimageSHA256  string `json:"preimage_sha256,omitempty"`
	PostimageSHA256 string `json:"postimage_sha256"`
	PostimageBytes  []byte `json:"postimage_bytes"`
}

type Preview struct {
	Version            string                          `json:"version"`
	EnvelopeVersion    string                          `json:"envelope_version"`
	PreviewID          string                          `json:"preview_id"`
	Plan               Plan                            `json:"plan"`
	CandidateSet       CandidateSet                    `json:"candidate_set"`
	Evaluation         managedscope.Evaluation         `json:"managed_scope_evaluation"`
	SourceGuard        map[string]baseline.Fingerprint `json:"source_guard,omitempty"`
	IndexPostimage     FormalImage                     `json:"index_postimage"`
	ConfigPostimage    FormalImage                     `json:"config_postimage"`
	CurationPostimage  *FormalImage                    `json:"curation_postimage,omitempty"`
	BaselinePostimage  FormalImage                     `json:"baseline_postimage"`
	Baseline           baseline.Baseline               `json:"baseline"`
	PhysicalDiffSHA256 string                          `json:"physical_diff_sha256"`
	SemanticDiffSHA256 string                          `json:"semantic_diff_sha256"`
	RiskDiffSHA256     string                          `json:"risk_diff_sha256"`
	EnvelopeDigest     string                          `json:"envelope_digest"`
	NetworkAccessed    bool                            `json:"network_accessed"`
}

type Approval struct {
	Version                string   `json:"version"`
	EnvelopeDigest         string   `json:"envelope_digest"`
	ApprovedWriteSet       []string `json:"approved_write_set"`
	ApprovedRecoveryPolicy string   `json:"approved_recovery_policy"`
	Actor                  string   `json:"actor"`
	Mechanism              string   `json:"approval_mechanism"`
	ApprovedAt             string   `json:"approved_at"`
	ApprovalDigest         string   `json:"approval_digest"`
}

type PolicyBoundApproval struct {
	Version                     string   `json:"version"`
	Mechanism                   string   `json:"mechanism"`
	Operation                   string   `json:"operation"`
	RepositoryIdentity          string   `json:"repository_identity"`
	AutomationMode              string   `json:"automation_mode"`
	ScopeApprovalMode           string   `json:"scope_approval_mode"`
	AuthorizationPolicyIdentity string   `json:"authorization_policy_identity"`
	EnvelopeDigest              string   `json:"envelope_digest"`
	PreviewDigest               string   `json:"preview_digest"`
	CurrentIndexSHA256          string   `json:"current_index_sha256"`
	CurrentBaselineSHA256       string   `json:"current_baseline_sha256"`
	CurrentConfigSHA256         string   `json:"current_config_sha256"`
	CurrentScopePolicyIdentity  string   `json:"current_scope_policy_identity"`
	CurrentCurationIdentity     string   `json:"current_curation_identity"`
	ProjectedIndexSHA256        string   `json:"projected_index_sha256"`
	ProjectedBaselineSHA256     string   `json:"projected_baseline_sha256"`
	ProjectedWholeIndexTokens   int      `json:"projected_whole_index_tokens"`
	EntryBefore                 int      `json:"entry_before"`
	EntryAfter                  int      `json:"entry_after"`
	IndexCount                  int      `json:"index_count"`
	ObserveCount                int      `json:"observe_count"`
	ExcludeCount                int      `json:"exclude_count"`
	RetentionReviewTotal        int      `json:"retention_review_total"`
	RetentionReviewComplete     bool     `json:"retention_review_complete"`
	P0                          int      `json:"p0"`
	P1                          int      `json:"p1"`
	WriteSet                    []string `json:"write_set"`
	GuardSet                    []string `json:"guard_set"`
	RecoveryDirection           string   `json:"recovery_direction"`
	CreatedAt                   string   `json:"created_at"`
	ApprovalDigest              string   `json:"approval_digest"`
}

type RecoveryIntent struct {
	Version             string                         `json:"version"`
	Operation           string                         `json:"operation"`
	TransactionID       string                         `json:"transaction_id"`
	Preview             Preview                        `json:"preview"`
	Approval            *Approval                      `json:"approval,omitempty"`
	PolicyBoundApproval *PolicyBoundApproval           `json:"policy_bound_approval,omitempty"`
	Staging             []cognitiontxn.StagedPostimage `json:"staging"`
	Preimages           []FormalImage                  `json:"preimages"`
	CreatedAt           string                         `json:"created_at"`
	RecoveryDigest      string                         `json:"recovery_digest"`
}

type Result struct {
	Version                string   `json:"version"`
	TransactionID          string   `json:"transaction_id"`
	Status                 string   `json:"status"`
	EnvelopeDigest         string   `json:"envelope_digest"`
	AuthorizationMechanism string   `json:"authorization_mechanism"`
	ApprovalDigest         string   `json:"approval_digest"`
	WrittenPaths           []string `json:"written_paths"`
	RecoveredPaths         []string `json:"recovered_paths"`
	IndexSHA256            string   `json:"index_sha256"`
	BaselineSHA256         string   `json:"baseline_sha256"`
	PolicyIdentity         string   `json:"policy_identity"`
	BudgetPolicyIdentity   string   `json:"budget_policy_identity"`
	RecoveryAvailable      bool     `json:"recovery_available"`
	NetworkAccessed        bool     `json:"network_accessed"`
}

type TargetStatus struct {
	Path         string `json:"path"`
	State        string `json:"state"`
	ActualSHA256 string `json:"actual_sha256,omitempty"`
}

type Status struct {
	Version            string         `json:"version"`
	TransactionID      string         `json:"transaction_id"`
	State              string         `json:"state"`
	Targets            []TargetStatus `json:"targets"`
	RecoveryAvailable  bool           `json:"recovery_available"`
	RollbackAvailable  bool           `json:"rollback_available"`
	ThirdPartyConflict bool           `json:"third_party_conflict"`
}
