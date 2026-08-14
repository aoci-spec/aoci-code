// Package managedscope evaluates project-owned index, observe, and exclude
// roles over Safe Inventory facts. It never authors or rewrites cognition.
package managedscope

import (
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type Rule struct {
	RuleID        string   `json:"rule_id"`
	Action        string   `json:"action"`
	Pattern       string   `json:"pattern"`
	PatternKind   string   `json:"pattern_kind"`
	Reason        string   `json:"reason"`
	DecisionBasis string   `json:"decision_basis,omitempty"`
	Source        string   `json:"source"`
	CreatedBy     string   `json:"created_by"`
	Order         int      `json:"order"`
	Enabled       bool     `json:"enabled"`
	Exceptions    []string `json:"exceptions,omitempty"`
}

type Policy struct {
	Version             string             `json:"version"`
	Profile             string             `json:"profile"`
	ObserveChangePolicy string             `json:"observe_change_policy"`
	ApprovalMode        string             `json:"approval_mode"`
	ApprovalThresholds  ApprovalThresholds `json:"approval_thresholds"`
	Rules               []Rule             `json:"rules"`
}

type ApprovalThresholds struct {
	EntryRemovalCount   int `json:"entry_removal_count"`
	EntryRemovalPercent int `json:"entry_removal_percent"`
}

type PathEvaluation struct {
	Version                  string `json:"version"`
	Path                     string `json:"path"`
	Role                     string `json:"role"`
	MatchedRule              *Rule  `json:"matched_rule,omitempty"`
	RuleSource               string `json:"rule_source"`
	RulePriority             int    `json:"rule_priority"`
	SafetyStatus             string `json:"safety_status"`
	GitStatus                string `json:"git_status"`
	ReadsContent             bool   `json:"reads_content"`
	EntersWholeIndex         bool   `json:"enters_whole_index"`
	EntersObserveFingerprint bool   `json:"enters_observe_fingerprint"`
	Reason                   string `json:"reason"`
	CaseSensitive            bool   `json:"case_sensitive"`
}

type Evaluation struct {
	Version             string                  `json:"version"`
	PolicyIdentity      string                  `json:"policy_identity"`
	SafeInventory       fs.SafeInventorySummary `json:"safe_inventory"`
	Index               []PathEvaluation        `json:"index"`
	Observe             []PathEvaluation        `json:"observe"`
	Exclude             []PathEvaluation        `json:"exclude"`
	IndexCount          int                     `json:"index_count"`
	ObserveCount        int                     `json:"observe_count"`
	ExcludeCount        int                     `json:"exclude_count"`
	SafetyExcluded      int                     `json:"safety_excluded_count"`
	RequiredHumanReview int                     `json:"required_human_review"`
	CaseSensitive       bool                    `json:"case_sensitive"`
	// AlternatePolicyIdentity 是同一份应用范围在相反大小写语义下的身份, 只在机器
	// 逐路径证明两种语义给出完全相同的角色分配时才存在。它让在大小写敏感文件系统
	// 上建立的 Baseline 能被大小写不敏感平台(如 Windows)原样接受, 反之亦然; 任何
	// 真实的匹配差异都会让它保持为空, 跨平台仍走人工 Scope Change。
	AlternatePolicyIdentity string `json:"alternate_policy_identity,omitempty"`
}

func LegacyPolicy() Policy {
	return Policy{
		Version:             machinecontract.ManagedScopePolicyV2,
		Profile:             machinecontract.ScopeProfileCustom,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		ApprovalMode:        machinecontract.ScopeApprovalModeInherit,
		ApprovalThresholds:  DefaultApprovalThresholds(),
		Rules: []Rule{{
			RuleID: "legacy-index-all", Action: machinecontract.ScopeRoleIndex,
			Pattern: "**", PatternKind: machinecontract.ScopePatternGlob,
			Reason: "legacy managed candidates remain indexed", Source: machinecontract.ScopeRuleProfile,
			CreatedBy: "aoci-legacy-compatibility", Order: 0, Enabled: true,
		}},
	}
}

func DefaultPolicy(profile string) Policy {
	return Policy{Version: machinecontract.ManagedScopePolicyV2, Profile: profile,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		ApprovalMode:        machinecontract.ScopeApprovalModeInherit,
		ApprovalThresholds:  DefaultApprovalThresholds(), Rules: []Rule{}}
}

func DefaultApprovalThresholds() ApprovalThresholds {
	return ApprovalThresholds{EntryRemovalCount: 25, EntryRemovalPercent: 25}
}
