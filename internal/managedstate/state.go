// Package managedstate is the shared read-only adapter that activates Managed
// Scope only when the desired policy and the Baseline receipt agree. It keeps
// Legacy projects on their historical snapshot path and fails closed on direct
// policy edits rather than reinterpreting formal state.
package managedstate

import (
	"fmt"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/index"
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
	// 跨大小写语义的收据接受: Baseline 在另一种文件系统语义下建立, 且机器已经
	// 逐路径证明两种语义的角色分配完全一致时, 收据身份就是本范围的合法身份。
	// 采纳收据身份为当前身份, 让计划与提交在两个平台间共享同一个标识。
	if !state.PolicyAligned && exists && evaluation.AlternatePolicyIdentity != "" &&
		state.ActivePolicyIdentity == evaluation.AlternatePolicyIdentity {
		state.PolicyAligned = true
		state.DesiredPolicyIdentity = state.ActivePolicyIdentity
	}
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
