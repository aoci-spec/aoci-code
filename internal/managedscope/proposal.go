package managedscope

import (
	"path"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type DirectoryImpact struct {
	Directory string `json:"directory"`
	Index     int    `json:"index"`
	Observe   int    `json:"observe"`
	Exclude   int    `json:"exclude"`
	Total     int    `json:"total"`
}

type Proposal struct {
	Version                   string            `json:"version"`
	PolicyIdentity            string            `json:"policy_identity"`
	GitTracked                int               `json:"git_tracked"`
	NewSourceFiles            int               `json:"new_source_files"`
	IndexObjects              int               `json:"index_objects"`
	ObserveObjects            int               `json:"observe_objects"`
	ExcludeObjects            int               `json:"exclude_objects"`
	SafetyExcluded            int               `json:"safety_excluded"`
	EstimatedWholeIndexTokens int               `json:"estimated_whole_index_tokens"`
	LargestDirectories        []DirectoryImpact `json:"largest_directories"`
	HighRiskRules             int               `json:"high_risk_rules"`
	RequiredHumanDecisions    int               `json:"required_human_decisions"`
	RequiresHumanApproval     bool              `json:"requires_human_approval"`
}

// BuildProposal summarizes names and deterministic policy facts. It does not
// open candidate contents and therefore remains safe before any high-risk read
// approval. Token impact is a planning estimate, not generated cognition.
func BuildProposal(evaluation *Evaluation, profile string, highRiskExactCount int) Proposal {
	result := Proposal{Version: machinecontract.ManagedScopeProposalV1, LargestDirectories: []DirectoryImpact{}, HighRiskRules: highRiskExactCount}
	if evaluation == nil {
		return result
	}
	result.PolicyIdentity = evaluation.PolicyIdentity
	result.GitTracked = evaluation.SafeInventory.GitTracked
	result.NewSourceFiles = evaluation.SafeInventory.NonignoredUntracked
	result.IndexObjects, result.ObserveObjects, result.ExcludeObjects = evaluation.IndexCount, evaluation.ObserveCount, evaluation.ExcludeCount
	result.SafetyExcluded = evaluation.SafetyExcluded
	result.RequiredHumanDecisions = evaluation.RequiredHumanReview
	// The fixed planning coefficient estimates an authored Entry plus index
	// structure. It never supplies, scores, or rewrites F/R/A/S semantics.
	result.EstimatedWholeIndexTokens = 1800 + evaluation.IndexCount*110
	result.RequiresHumanApproval = highRiskExactCount > 0 || evaluation.RequiredHumanReview > 0 ||
		(evaluation.IndexCount == 0 && evaluation.SafeInventory.FinalManagedCandidates > 0)
	type counts struct{ index, observe, exclude int }
	byDirectory := map[string]*counts{}
	add := func(values []PathEvaluation, role string) {
		for _, value := range values {
			directory := path.Dir(value.Path)
			if directory == "." {
				directory = "./"
			}
			item := byDirectory[directory]
			if item == nil {
				item = &counts{}
				byDirectory[directory] = item
			}
			switch role {
			case machinecontract.ScopeRoleIndex:
				item.index++
			case machinecontract.ScopeRoleObserve:
				item.observe++
			default:
				item.exclude++
			}
		}
	}
	add(evaluation.Index, machinecontract.ScopeRoleIndex)
	add(evaluation.Observe, machinecontract.ScopeRoleObserve)
	add(evaluation.Exclude, machinecontract.ScopeRoleExclude)
	for directory, value := range byDirectory {
		result.LargestDirectories = append(result.LargestDirectories, DirectoryImpact{Directory: directory,
			Index: value.index, Observe: value.observe, Exclude: value.exclude, Total: value.index + value.observe + value.exclude})
	}
	sort.Slice(result.LargestDirectories, func(i, j int) bool {
		if result.LargestDirectories[i].Total != result.LargestDirectories[j].Total {
			return result.LargestDirectories[i].Total > result.LargestDirectories[j].Total
		}
		return result.LargestDirectories[i].Directory < result.LargestDirectories[j].Directory
	})
	if len(result.LargestDirectories) > 5 {
		result.LargestDirectories = result.LargestDirectories[:5]
	}
	return result
}
