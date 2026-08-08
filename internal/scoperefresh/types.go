// Package scoperefresh implements the versioned Baseline managed-scope
// transition. It changes only the existing Baseline through the repository's
// shared lock, CAS, atomic-write, Ledger, and cognition transaction primitives.
package scoperefresh

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

type ScopeObject struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	Reason     string `json:"reason,omitempty"`
	RuleSource string `json:"rule_source,omitempty"`
	GitTracked bool   `json:"git_tracked,omitempty"`
}

type SourceDrift struct {
	Path           string `json:"path"`
	Code           string `json:"code"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	ActualSHA256   string `json:"actual_sha256,omitempty"`
}

type Plan struct {
	Version                 string                   `json:"version"`
	PlanID                  string                   `json:"plan_id"`
	RepositoryRootIdentity  string                   `json:"repository_root_identity"`
	ExpectedBaselineSHA256  string                   `json:"expected_baseline_sha256"`
	OldManagedSetIdentity   string                   `json:"old_managed_set_identity"`
	NewManagedSetIdentity   string                   `json:"new_managed_set_identity"`
	RulesIdentity           string                   `json:"include_exclude_rules_identity"`
	CurationIdentity        string                   `json:"curation_identity"`
	SourceIdentity          string                   `json:"source_identity"`
	SafeInventory           afs.SafeInventorySummary `json:"safe_inventory"`
	Added                   []ScopeObject            `json:"added"`
	Removed                 []ScopeObject            `json:"removed"`
	Preserved               []ScopeObject            `json:"preserved"`
	SourceDrift             []SourceDrift            `json:"source_drift"`
	ScopeOnlyDelta          int                      `json:"scope_only_delta"`
	SafeRemovalCount        int                      `json:"safe_removal_count"`
	OrdinaryRemovalCount    int                      `json:"ordinary_removal_count"`
	HighRiskReduction       bool                     `json:"high_risk_reduction"`
	InteractionRequired     bool                     `json:"interaction_required"`
	InteractionKind         string                   `json:"interaction_kind,omitempty"`
	ConfirmationPhrase      string                   `json:"confirmation_phrase,omitempty"`
	BaselineTimestamp       string                   `json:"baseline_timestamp"`
	BaselinePostimageSHA256 string                   `json:"baseline_postimage_sha256"`
	NetworkAccessed         bool                     `json:"network_accessed"`
}

type Preview struct {
	Version                 string            `json:"version"`
	PreviewID               string            `json:"preview_id"`
	Plan                    Plan              `json:"plan"`
	BaselinePostimage       baseline.Baseline `json:"baseline_postimage"`
	BaselinePostimageSHA256 string            `json:"baseline_postimage_sha256"`
	WriteSet                []string          `json:"write_set"`
	GuardSet                []string          `json:"guard_set"`
	RecoveryDirection       string            `json:"recovery_direction"`
	NetworkAccessed         bool              `json:"network_accessed"`
}

type Approval struct {
	Version        string `json:"version"`
	PreviewID      string `json:"preview_id"`
	Actor          string `json:"actor"`
	Mechanism      string `json:"mechanism"`
	ApprovedAt     string `json:"approved_at"`
	ApprovalDigest string `json:"approval_digest"`
}

type ApplyResult struct {
	Version           string `json:"version"`
	TransactionID     string `json:"transaction_id"`
	Status            string `json:"status"`
	PlanID            string `json:"plan_id"`
	PreviewID         string `json:"preview_id"`
	BaselineSHA256    string `json:"baseline_sha256"`
	AddedCount        int    `json:"added_count"`
	RemovedCount      int    `json:"removed_count"`
	PreservedCount    int    `json:"preserved_count"`
	SourceDriftCount  int    `json:"source_drift_count"`
	NetworkAccessed   bool   `json:"network_accessed"`
	RecoveryAvailable bool   `json:"recovery_available"`
}

type Status struct {
	Version             string `json:"version"`
	TransactionID       string `json:"transaction_id"`
	State               string `json:"state"`
	ExpectedBaselineSHA string `json:"expected_baseline_sha256"`
	PostBaselineSHA     string `json:"post_baseline_sha256"`
	ActualBaselineSHA   string `json:"actual_baseline_sha256,omitempty"`
	RecoveryAvailable   bool   `json:"recovery_available"`
}

type recoveryIntent struct {
	Version       string    `json:"version"`
	TransactionID string    `json:"transaction_id"`
	Preview       Preview   `json:"preview"`
	Approval      *Approval `json:"approval,omitempty"`
}
