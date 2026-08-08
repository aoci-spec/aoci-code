package managedscope

import (
	"fmt"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type SnapshotOptions struct{ HighRiskContentApproved bool }

// Snapshot hashes only index and observe paths after Safe Inventory and policy
// evaluation. Excluded content is never opened and therefore cannot enter
// Baseline, Evidence, Candidate, Prompt, or Ledger bodies through this path.
func Snapshot(repositoryRoot string, evaluation *Evaluation, options ...SnapshotOptions) (map[string]baseline.Fingerprint, error) {
	if evaluation == nil {
		return nil, fmt.Errorf("managed_scope_evaluation_required")
	}
	result := make(map[string]baseline.Fingerprint, len(evaluation.Index)+len(evaluation.Observe))
	approved := len(options) > 0 && options[0].HighRiskContentApproved
	for _, group := range [][]PathEvaluation{evaluation.Index, evaluation.Observe} {
		for _, item := range group {
			if item.SafetyStatus == "high_risk_exact_opt_in" && !approved {
				return nil, fmt.Errorf("managed_scope_high_risk_read_approval_required: %s", item.Path)
			}
			fingerprint, err := baseline.HashFile(filepath.Join(repositoryRoot, filepath.FromSlash(item.Path)))
			if err != nil {
				return nil, fmt.Errorf("managed_scope_source_unreadable: %s", item.Path)
			}
			fingerprint.Role = item.Role
			if fingerprint.Role == machinecontract.ScopeRoleIndex || fingerprint.Role == machinecontract.ScopeRoleObserve {
				result[item.Path] = fingerprint
			}
		}
	}
	return result, nil
}
