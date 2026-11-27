// Package managedstate is the shared read-only adapter that activates Managed
// Scope only when the desired policy and the Baseline receipt agree. It keeps
// Legacy projects on their historical snapshot path and fails closed on direct
// policy edits rather than reinterpreting formal state.
package managedstate

import (
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

type State struct {
	Inventory             *afs.SafeInventory              `json:"-"` // Actual Legacy selection, not a second scan.
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
	value, _, err := baseline.Load(root)
	if err != nil {
		return nil, err
	}
	return LoadFromBaseline(root, cfg, value)
}

// LoadFromBaseline uses the caller's Baseline, so the governed snapshot and
// other facts can share the same read. A nil value means the file was absent.
func LoadFromBaseline(root string, cfg *config.Config, value *baseline.Baseline) (*State, error) {
	if cfg == nil {
		return nil, fmt.Errorf("managed_state_configuration_required")
	}
	exists := value != nil
	state := &State{Snapshot: map[string]baseline.Fingerprint{}, Baseline: value, Warnings: []string{}}
	if cfg.ManagedScope == nil && cfg.CognitionBudget == nil && (!exists || value.ManagedScope == nil) {
		state.Legacy, state.PolicyAligned, state.BudgetAligned = true, true, true
		var err error
		state.Snapshot, state.Warnings, state.Inventory, err = baseline.SnapshotWithInventory(root, cfg.WalkOptions())
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
	// The Baseline was already read above, so its fingerprints are free. They
	// only ever spare an unchanged Go file a gofmt reparse; every digest this
	// produces is byte-identical to a cold hash.
	var prior map[string]baseline.Fingerprint
	if exists {
		prior = value.Files
	}
	state.Snapshot, err = managedscope.SnapshotReusing(root, evaluation, prior,
		managedscope.SnapshotOptions{HighRiskContentApproved: approved})
	return state, err
}

func Detect(root string, cfg *config.Config, document *index.Document, state *State) (*baseline.DetectResult, error) {
	if state == nil {
		return nil, fmt.Errorf("managed_state_required")
	}
	if state.ScopeChangeRequired {
		return nil, fmt.Errorf("scope_refresh_required")
	}
	var result *baseline.DetectResult
	if state.Legacy {
		result = baseline.DetectWith(root, document, state.Baseline, state.Snapshot, cfg.WalkOptions(), cfg.LineEndingTolerance)
	} else {
		result = baseline.DetectManagedScope(root, document, state.Baseline, state.Snapshot,
			cfg.WalkOptions(), cfg.LineEndingTolerance)
	}
	if cfg.RecurseSubmodules {
		holdUnavailableSubmodules(result, unavailableSubmodules(state))
	}
	return result, nil
}

// unavailableSubmodules names the gitlinks the inventory kept as a
// git_submodule boundary: the superproject still records the submodule, but
// its checkout is missing locally (never initialized, or `git submodule
// deinit`). Under recurse_submodules that is a local fact, not a team
// decision.
func unavailableSubmodules(state *State) []string {
	roots := []string{}
	if state.Evaluation != nil {
		for _, item := range state.Evaluation.Exclude {
			if strings.HasPrefix(item.Reason, "git_submodule:") {
				roots = append(roots, item.Path)
			}
		}
	} else if state.Inventory != nil {
		for _, exclusion := range state.Inventory.Exclusions {
			if exclusion.RuleSource == "git_submodule" {
				roots = append(roots, exclusion.PathSummary)
			}
		}
	}
	return roots
}

// holdUnavailableSubmodules keeps the governed objects below an unavailable
// submodule out of Orphan and ObservedRemoved: their files are absent only
// from this checkout, so Maintain must not offer to remove shared Entries,
// and a re-init finds them current again. Their Baseline records are left
// alone for the same reason.
func holdUnavailableSubmodules(result *baseline.DetectResult, roots []string) {
	if result == nil || len(roots) == 0 {
		return
	}
	below := func(rel string) bool {
		for _, root := range roots {
			if strings.HasPrefix(rel, root+"/") {
				return true
			}
		}
		return false
	}
	keep := func(paths []string) []string {
		kept := paths[:0:0]
		for _, path := range paths {
			if !below(path) {
				kept = append(kept, path)
			}
		}
		return kept
	}
	result.Orphan = keep(result.Orphan)
	result.ObservedRemoved = keep(result.ObservedRemoved)
}

func CurationExclusions(root string, cfg *config.Config, value *baseline.Baseline) ([]string, error) {
	document, _, _, err := curation.Load(root)
	if err != nil {
		return nil, err
	}
	return curation.ActiveExclusions(root, cfg.CurationExclude, document, value), nil
}
