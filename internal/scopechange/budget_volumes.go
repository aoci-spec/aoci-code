package scopechange

import (
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// BudgetMeasure names the bytes whose Entries a budget measures and the size
// of the complete Whole-Index they belong to. A Legacy index is both. Under
// Volumes v1 the Root carries no Entries: the per-Entry R and S bands are
// measured on the Code Volume, where the Entries are, and the Whole-Index is
// the Root plus every declared asset. scope status, a Scope Change preview,
// and its postimage check share this one projection (audit G1-08: the Scope
// Change measured the Root alone, so a tightened band was never checked
// against a written Entry, and its approval recorded zero Entries).
func BudgetMeasure(root string, cfg *config.Config, raw []byte) (measured []byte, wholeIndexBytes int, volumes bool) {
	layout, err := cognition.DetectLayout(raw)
	if err != nil || layout != cognition.LayoutVolumesV1 {
		return raw, len(raw), false
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		return raw, len(raw), false
	}
	code := set.Volumes[cognition.ScopeCode]
	if code == nil || code.State != cognition.AssetPresent {
		return raw, len(raw), false
	}
	total := len(raw)
	for _, id := range set.DeclaredOrder {
		if asset := set.Volumes[id]; asset != nil {
			total += len(asset.Raw)
		}
	}
	return code.Raw, total, true
}

// BuildBudgetReport is the budget report of raw (the bytes at the configured
// index path) under policy, measured through BudgetMeasure.
func BuildBudgetReport(root string, cfg *config.Config, raw []byte, policy cognitionbudget.Policy) (*cognitionbudget.Report, error) {
	measured, total, volumes := BudgetMeasure(root, cfg, raw)
	report, err := cognitionbudget.Build(root, measured, policy)
	if err != nil || !volumes {
		return report, err
	}
	if err := report.RebindWholeIndexTokens(cognitionbudget.EstimateTokensOfSize(total), policy); err != nil {
		return nil, err
	}
	return report, nil
}
