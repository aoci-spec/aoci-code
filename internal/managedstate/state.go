// Package managedstate is the shared read-only adapter that activates Managed
// Scope only when the desired policy and the Baseline receipt agree. It keeps
// Legacy projects on their historical snapshot path and fails closed on direct
// policy edits rather than reinterpreting formal state.
package managedstate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

type State struct {
	Legacy                bool                            `json:"legacy"`
	ScopeChangeRequired   bool                            `json:"scope_change_required"`
	PolicyAligned         bool                            `json:"policy_aligned"`
	BudgetAligned         bool                            `json:"budget_aligned"`
	DesiredPolicyIdentity string                          `json:"desired_policy_identity,omitempty"`
	ActivePolicyIdentity  string                          `json:"active_policy_identity,omitempty"`
	DesiredBudgetIdentity string                          `json:"desired_budget_identity,omitempty"`
	ActiveBudgetIdentity  string                          `json:"active_budget_identity,omitempty"`
	Evaluation            *managedscope.Evaluation        `json:"evaluation,omitempty"`
	Snapshot              map[string]baseline.Fingerprint `json:"-"`
	Baseline              *baseline.Baseline              `json:"-"`
	Warnings              []string                        `json:"-"`
}

func Load(root string, cfg *config.Config) (*State, error) {
	if cfg == nil {
		return nil, fmt.Errorf("managed_state_configuration_required")
	}
	value, exists, err := baseline.Load(root)
	if err != nil {
		return nil, err
	}
	state := &State{Snapshot: map[string]baseline.Fingerprint{}, Baseline: value, Warnings: []string{}}
	if cfg.ManagedScope == nil && cfg.CognitionBudget == nil && (!exists || value.ManagedScope == nil) {
		state.Legacy, state.PolicyAligned, state.BudgetAligned = true, true, true
		state.Snapshot, state.Warnings, err = baseline.Snapshot(root, cfg.WalkOptions())
		return state, err
	}
	curationExclude, err := CurationExclusions(root, cfg, value)
	if err != nil {
		return nil, err
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
		WalkOptions: cfg.WalkOptions(), CurationExclude: curationExclude})
	if err != nil {
		return nil, err
	}
	state.Evaluation = evaluation
	state.DesiredPolicyIdentity = evaluation.PolicyIdentity
	state.DesiredBudgetIdentity, err = cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		return nil, err
	}
	if exists && value.ManagedScope != nil {
		state.ActivePolicyIdentity = value.ManagedScope.PolicyIdentity
		state.ActiveBudgetIdentity = value.ManagedScope.BudgetPolicyIdentity
	}
	state.PolicyAligned = exists && state.ActivePolicyIdentity == state.DesiredPolicyIdentity
	// Empty is accepted only as the compatibility identity of an early
	// managed-scope receipt that predated the budget field. Once an identity is
	// recorded, direct policy edits are detected normally.
	state.BudgetAligned = exists && (state.ActiveBudgetIdentity == "" || state.ActiveBudgetIdentity == state.DesiredBudgetIdentity)
	state.ScopeChangeRequired = !state.PolicyAligned || !state.BudgetAligned
	if state.ScopeChangeRequired {
		return state, nil
	}
	approved := len(cfg.SafeInventoryHighRiskOptIn) == 0 || value.ManagedScope.HighRiskApprovalDigest != ""
	state.Snapshot, err = managedscope.Snapshot(root, evaluation, managedscope.SnapshotOptions{HighRiskContentApproved: approved})
	return state, err
}

// LoadInitial evaluates the desired Managed Scope and Cognition Budget for a
// Fresh Bootstrap before any Baseline exists. It is deliberately separate
// from Load: every mature repository must continue to require an active
// Baseline receipt before a desired policy can govern formal cognition.
func LoadInitial(root string, cfg *config.Config) (*State, error) {
	if cfg == nil {
		return nil, fmt.Errorf("managed_state_configuration_required")
	}
	if _, exists, err := baseline.Load(root); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("managed_state_initial_baseline_present")
	}
	return EvaluateInitial(root, cfg)
}

// EvaluateInitial deterministically replays the pre-Baseline desired policy.
// Bootstrap recovery uses it after partial Root-last publication as well, so
// it intentionally ignores any Baseline that the same transaction may already
// have published. High-risk exact opt-ins still fail before content hashing
// unless their selected path is absent or excluded by policy.
func EvaluateInitial(root string, cfg *config.Config) (*State, error) {
	if cfg == nil {
		return nil, fmt.Errorf("managed_state_configuration_required")
	}
	if cfg.ManagedScope == nil && cfg.CognitionBudget == nil {
		return nil, fmt.Errorf("managed_state_initial_policy_required")
	}
	curationExclude, err := CurationExclusions(root, cfg, nil)
	if err != nil {
		return nil, err
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
		WalkOptions: cfg.WalkOptions(), CurationExclude: curationExclude})
	if err != nil {
		return nil, err
	}
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		return nil, err
	}
	snapshot, err := managedscope.Snapshot(root, evaluation, managedscope.SnapshotOptions{HighRiskContentApproved: false})
	if err != nil {
		return nil, err
	}
	return &State{
		ScopeChangeRequired: false, PolicyAligned: true, BudgetAligned: true,
		DesiredPolicyIdentity: evaluation.PolicyIdentity, DesiredBudgetIdentity: budgetIdentity,
		Evaluation: evaluation, Snapshot: snapshot, Baseline: nil, Warnings: []string{},
	}, nil
}

// InitialBaselineReceipt materializes the existing managed-scope Baseline
// contract from a previously evaluated Fresh state. It is shared by scan and
// Cognition Bootstrap so both initial governance paths record identical policy
// and budget authority without introducing another receipt format.
func InitialBaselineReceipt(cfg *config.Config, state *State) (*baseline.ManagedScopeState, error) {
	if cfg == nil || state == nil || state.Evaluation == nil {
		return nil, fmt.Errorf("managed_state_initial_evaluation_required")
	}
	budgetPolicy := cfg.EffectiveCognitionBudget()
	budgetIdentity, err := cognitionbudget.Identity(budgetPolicy)
	if err != nil {
		return nil, err
	}
	if state.ScopeChangeRequired || state.DesiredPolicyIdentity == "" ||
		state.DesiredPolicyIdentity != state.Evaluation.PolicyIdentity ||
		state.DesiredBudgetIdentity != budgetIdentity {
		return nil, fmt.Errorf("managed_state_initial_identity_mismatch")
	}
	return &baseline.ManagedScopeState{
		Version:              machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity:       state.DesiredPolicyIdentity,
		ObserveChangePolicy:  cfg.EffectiveManagedScope().ObserveChangePolicy,
		BudgetPolicyIdentity: budgetIdentity,
		BudgetPolicy:         &budgetPolicy,
	}, nil
}

// HighRiskOptInIdentity binds the exact normalized exception list even when it
// is empty. Safe Inventory's rules identity also includes these paths; this
// focused identity makes their Bootstrap guard explicit without reading them.
func HighRiskOptInIdentity(paths []string) (string, error) {
	normalized := make([]string, 0, len(paths))
	for _, value := range paths {
		path, err := afs.NormalizeRelPath(value)
		if err != nil {
			return "", fmt.Errorf("managed_state_high_risk_path_invalid")
		}
		normalized = append(normalized, path)
	}
	sort.Strings(normalized)
	deduplicated := normalized[:0]
	for _, path := range normalized {
		if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != path {
			deduplicated = append(deduplicated, path)
		}
	}
	hash := sha256.New()
	for _, value := range append([]string{"managed-scope-high-risk-opt-in/v1"}, deduplicated...) {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func Detect(root string, cfg *config.Config, document *index.Document, state *State) (*baseline.DetectResult, error) {
	if state == nil {
		return nil, fmt.Errorf("managed_state_required")
	}
	if state.ScopeChangeRequired {
		return nil, fmt.Errorf("scope_refresh_required")
	}
	if state.Legacy {
		return baseline.DetectWith(root, document, state.Baseline, state.Snapshot, cfg.WalkOptions(), cfg.LineEndingTolerance), nil
	}
	return baseline.DetectManagedScope(root, document, state.Baseline, state.Snapshot,
		cfg.WalkOptions(), cfg.LineEndingTolerance), nil
}

func CurationExclusions(root string, cfg *config.Config, value *baseline.Baseline) ([]string, error) {
	result := append([]string{}, cfg.CurationExclude...)
	document, _, _, err := curation.Load(root)
	if err != nil {
		return nil, err
	}
	if value != nil {
		for _, decision := range document.Decisions {
			fingerprint, exists := value.Files[decision.Path]
			if exists && decision.Decision == curation.DecisionExclude && fingerprint.SHA256 == decision.SourceSHA256 {
				result = append(result, decision.Path)
			}
		}
	}
	sort.Strings(result)
	deduplicated := result[:0]
	for _, path := range result {
		if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != path {
			deduplicated = append(deduplicated, path)
		}
	}
	return deduplicated, nil
}
