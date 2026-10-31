package volumegovernance

import (
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// ScoreInput projects the facts into what indexgen.BuildVolumeScore reads,
// so index score reports the drift, budget, and E scale Verify and Check
// report and never measures them again. The E scale audit is passed in
// because Assess never reads a source; Verify and Check run it themselves.
func (f *Facts) ScoreInput(cfg *config.Config, escale EScaleFacts) indexgen.VolumeScoreInput {
	in := indexgen.VolumeScoreInput{CodeSourceCount: f.CodeSourceCount}
	for _, finding := range f.Findings {
		if finding.Code == "baseline_missing" {
			in.BaselineMissing = true
		}
	}
	drift := f.CodeDrift
	in.Drift = indexgen.VolumeScoreDrift{
		Missing: copyList(drift.Missing), Orphan: copyList(drift.Orphan), Stale: copyList(drift.Stale),
		Unbaselined: copyList(drift.Unbaselined), LineEndingOnly: copyList(drift.LineEndingOnly),
		Skipped: copyList(drift.Skipped), CurationExcluded: copyList(drift.CurationExcluded),
		ObservedNew: copyList(drift.ObservedNew), ObservedChanged: copyList(drift.ObservedChanged), ObservedRemoved: copyList(drift.ObservedRemoved),
	}
	in.ManagedScope = indexgen.ManagedScopeSummary{
		ScopeChangeRequired: f.ManagedScope.ScopeChangeRequired,
		PolicyIdentity:      f.ManagedScope.PolicyIdentity, ActivePolicyIdentity: f.ManagedScope.ActivePolicyIdentity,
		IndexCount: f.ManagedScope.IndexCount, ObserveCount: f.ManagedScope.ObserveCount, ExcludeCount: f.ManagedScope.ExcludeCount,
		ObservedPendingReview: f.ManagedScope.ObservedPendingReview,
		ObserveReviewRequired: cfg != nil && cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired,
	}
	in.Budget = indexgen.VolumeScoreBudget{
		Mode: f.Budget.Mode, Status: f.Budget.Status, WholeIndexTokens: f.Budget.WholeIndexTokens,
		TargetTokens: f.Budget.TargetTokens, WarningTokens: f.Budget.WarningTokens, MaxTokens: f.Budget.MaxTokens,
		Violations: len(f.Budget.Violations), SCoverage: append([]cognitionbudget.SCoverageBand{}, f.Budget.SCoverage...),
		HighImportanceEntries: f.Budget.HighImportanceEntries, HighImportanceSAbsent: f.Budget.HighImportanceSAbsent,
		HighImportanceSAbsentPercent: f.Budget.HighImportanceSAbsentPercent,
	}
	in.EScale = indexgen.VolumeScoreEScale{Checked: escale.Checked, Mismatched: escale.Mismatched, Paths: []string{}}
	for _, item := range escale.Items {
		in.EScale.Paths = append(in.EScale.Paths, item.Path)
	}
	return in
}

func copyList(values []string) []string {
	return append([]string{}, values...)
}
