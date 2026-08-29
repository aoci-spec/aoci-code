// Package cognitionbudget measures the formal Whole-Index and validates
// project-configured density budgets. It reports facts and rejects over-budget
// candidates; it never truncates, summarizes, retags, or rewrites FRAS.
package cognitionbudget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type WholeIndexPolicy struct {
	TargetTokens  int `json:"target_tokens"`
	WarningTokens int `json:"warning_tokens"`
	MaxTokens     int `json:"max_tokens"`
}

type FieldBand struct {
	MinC         int `json:"min_c"`
	MaxC         int `json:"max_c"`
	TargetTokens int `json:"target_tokens"`
	MaxTokens    int `json:"max_tokens"`
}

type Policy struct {
	Version    string           `json:"version"`
	Mode       string           `json:"mode"`
	WholeIndex WholeIndexPolicy `json:"whole_index"`
	R          []FieldBand      `json:"r"`
	S          []FieldBand      `json:"s"`
}

// DefaultPolicy is the NEW-PROJECT budget. config.SetNewProjectGovernance is its
// only production caller and runs only from init, after proving config.json did
// not already exist, so these numbers reach no repository that already has one.
// They MAY move. LegacyPolicy is the frozen preimage for repositories with no
// cognition_budget block and must move independently or not at all.
func DefaultPolicy(mode string) Policy {
	return Policy{
		Version: machinecontract.CognitionBudgetPolicyV1, Mode: mode,
		WholeIndex: WholeIndexPolicy{TargetTokens: 200000, WarningTokens: 300000, MaxTokens: 400000},
		R: []FieldBand{{MinC: 9, MaxC: 9, TargetTokens: 90, MaxTokens: 180}, {MinC: 8, MaxC: 8, TargetTokens: 70, MaxTokens: 140},
			{MinC: 5, MaxC: 7, TargetTokens: 45, MaxTokens: 90}, {MinC: 1, MaxC: 4, TargetTokens: 25, MaxTokens: 50}},
		S: []FieldBand{{MinC: 9, MaxC: 9, TargetTokens: 100, MaxTokens: 200}, {MinC: 8, MaxC: 8, TargetTokens: 70, MaxTokens: 140},
			{MinC: 5, MaxC: 7, TargetTokens: 40, MaxTokens: 80}, {MinC: 1, MaxC: 4, TargetTokens: 20, MaxTokens: 40}},
	}
}

// LegacyPolicy is the budget a repository resolves when its config.json carries
// no cognition_budget block, which every repository created before that block
// existed does, and which config.MutateManagedScope still produces today because
// it sets ManagedScope and never materializes a budget.
//
// Its Identity() is therefore a PERSISTED PREIMAGE. It is stamped into
// .aoci/baseline.json as managed_scope.budget_policy_identity and compared on
// every load by managedstate.Load and by cli/scope.go. Changing any literal
// below, or the Policy struct's field set, or any json tag on it, moves that
// identity for every such repository, flips budget alignment to false, and
// forces a human-approved Scope Change the repository did nothing to earn.
//
// FROZEN. Identity() == a98988839d1818e2faa245e355c256be3698f7a3552edf87338cb8ce48444eb7
//
// DefaultPolicy is the new-project default and MAY move. The two were one
// function until rc6 and must never be re-collapsed.
func LegacyPolicy() Policy {
	return Policy{
		Version:    machinecontract.CognitionBudgetPolicyV1,
		Mode:       machinecontract.BudgetModeObserve,
		WholeIndex: WholeIndexPolicy{TargetTokens: 120000, WarningTokens: 180000, MaxTokens: 240000},
		R: []FieldBand{{MinC: 9, MaxC: 9, TargetTokens: 90, MaxTokens: 180}, {MinC: 8, MaxC: 8, TargetTokens: 70, MaxTokens: 140},
			{MinC: 5, MaxC: 7, TargetTokens: 45, MaxTokens: 90}, {MinC: 1, MaxC: 4, TargetTokens: 25, MaxTokens: 50}},
		S: []FieldBand{{MinC: 9, MaxC: 9, TargetTokens: 100, MaxTokens: 200}, {MinC: 8, MaxC: 8, TargetTokens: 70, MaxTokens: 140},
			{MinC: 5, MaxC: 7, TargetTokens: 40, MaxTokens: 80}, {MinC: 1, MaxC: 4, TargetTokens: 20, MaxTokens: 40}},
	}
}

func Normalize(policy Policy) (Policy, error) {
	if policy.Version == "" {
		policy.Version = machinecontract.CognitionBudgetPolicyV1
	}
	if policy.Version != machinecontract.CognitionBudgetPolicyV1 {
		return Policy{}, fmt.Errorf("cognition_budget_policy_version_unsupported")
	}
	if policy.Mode != machinecontract.BudgetModeObserve && policy.Mode != machinecontract.BudgetModeEnforce {
		return Policy{}, fmt.Errorf("cognition_budget_mode_invalid")
	}
	whole := policy.WholeIndex
	if whole.TargetTokens <= 0 || whole.WarningTokens < whole.TargetTokens || whole.MaxTokens < whole.WarningTokens {
		return Policy{}, fmt.Errorf("cognition_budget_whole_index_invalid")
	}
	var err error
	policy.R, err = normalizeBands(policy.R, "r")
	if err != nil {
		return Policy{}, err
	}
	policy.S, err = normalizeBands(policy.S, "s")
	if err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func normalizeBands(bands []FieldBand, field string) ([]FieldBand, error) {
	if len(bands) == 0 {
		return nil, fmt.Errorf("cognition_budget_%s_bands_missing", field)
	}
	result := append([]FieldBand{}, bands...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].MinC != result[j].MinC {
			return result[i].MinC < result[j].MinC
		}
		return result[i].MaxC < result[j].MaxC
	})
	covered := map[int]bool{}
	for _, band := range result {
		if band.MinC < 1 || band.MaxC > 9 || band.MinC > band.MaxC || band.TargetTokens < 0 || band.MaxTokens <= 0 || band.TargetTokens > band.MaxTokens {
			return nil, fmt.Errorf("cognition_budget_%s_band_invalid", field)
		}
		for importance := band.MinC; importance <= band.MaxC; importance++ {
			if covered[importance] {
				return nil, fmt.Errorf("cognition_budget_%s_band_overlap", field)
			}
			covered[importance] = true
		}
	}
	for importance := 1; importance <= 9; importance++ {
		if !covered[importance] {
			return nil, fmt.Errorf("cognition_budget_%s_band_gap", field)
		}
	}
	return result, nil
}

func LimitFor(bands []FieldBand, importance int) (FieldBand, bool) {
	for _, band := range bands {
		if importance >= band.MinC && importance <= band.MaxC {
			return band, true
		}
	}
	return FieldBand{}, false
}

func Identity(policy Policy) (string, error) {
	normalized, err := Normalize(policy)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
