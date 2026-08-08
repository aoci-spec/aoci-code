// aoci check —— 提交前聚合入口。
//
// check回答“当前治理状态是否允许提交”，不是重新定义物理事实。
//
// 阻断:
//   - ActionableMissing，包括有效IncludedMissing;
//   - PendingCurationMissing;
//   - Stale;
//   - Unbaselined;
//   - dict;
//   - format;
//   - 未完成的Entries或Curation草稿批次。
//
// 展示但不阻断:
//   - RawMissing本身;
//   - CurationExcludedMissing;
//   - 非Pending的SkippedMissing;
//   - Orphan;
//   - LineEndingOnly;
//   - S配额;
//   - E档位;
//   - tagparse;
//   - StaleCurationDecisions自身不重复计Issue，对应路径已进入Actionable或Pending。
//
// 人读口径:
//   - RawMissing只表示磁盘有、正式索引无Entry的原始物理事实;
//   - ActionableMissing和PendingCurationMissing才表示尚需治理的Missing路径;
//   - 兼容机器字段missing和pending_curation继续由Score保留，
//     新消费者优先使用raw_missing和pending_curation_missing;
//   - Ledger的exit_code与report.OK使用同一判定结果。
package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/spf13/cobra"
)

type checkReport struct {
	OK                      bool                    `json:"ok"`
	Issues                  []string                `json:"issues"`
	Score                   *indexgen.Score         `json:"score"`
	PendingDraftRun         string                  `json:"pending_draft_run,omitempty"`
	PendingCurationDraftRun string                  `json:"pending_curation_draft_run,omitempty"`
	LocaleMigration         localeMigrationCoverage `json:"locale_migration"`
}

type volumeCheckReport struct {
	OK                bool                       `json:"ok"`
	ExitCode          int                        `json:"exit_code"`
	StructureValid    bool                       `json:"structure_valid"`
	GovernanceAligned bool                       `json:"governance_aligned"`
	Findings          []volumegovernance.Finding `json:"findings"`
	NextAction        string                     `json:"next_action"`
	Governance        *volumegovernance.Facts    `json:"governance"`
}

func dimByName(score *indexgen.Score, name string) indexgen.Dimension {
	for _, dimension := range score.Dimensions {
		if dimension.Name == name {
			return dimension
		}
	}

	return indexgen.Dimension{Name: name}
}

func pendingDraftRun(root, kind string) string {
	runID, _ := draft.LatestPendingRun(root, kind)
	return runID
}

func pendingEntriesRun(root string) string {
	return pendingDraftRun(root, draft.KindEntries)
}

func pendingCurationRun(root string) string {
	return pendingDraftRun(root, draft.KindCuration)
}

func init() {
	registerCommand(newCheckCmd())
}

func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: checkShortHelp(),
		Long:  checkLongHelp(),
		RunE:  runCheckCommand,
	}
}

func runCheckCommand(
	cmd *cobra.Command,
	args []string,
) error {
	start := time.Now()

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}

	cfg, err := config.Load(repoRoot)
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}
	set, cognitionErr := cognition.Load(repoRoot, cfg.IndexPath)
	if cognitionErr != nil {
		return &ExitError{Code: ExitConfig, Err: cognitionErr}
	}
	if set.LayoutMode == cognition.LayoutVolumesV1 {
		return runVolumeCheck(cmd, repoRoot, cfg, set, start)
	}

	doc, _, err := loadIndexForCLI(cmd, repoRoot, cfg)
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}

	score, err := indexgen.BuildScore(repoRoot, cfg, doc)
	if err != nil {
		return &ExitError{Code: ExitInternal, Err: err}
	}

	pendingEntries := pendingEntriesRun(repoRoot)
	pendingCuration := pendingCurationRun(repoRoot)

	formatDimension := dimByName(score, "format")
	dictDimension := dimByName(score, "dict")
	sQuotaDimension := dimByName(score, "squota")
	eScaleDimension := dimByName(score, "escale")
	tagParseDimension := dimByName(score, "tagparse")
	agentDimension := dimByName(score, "agent_ready")

	actionableDrift := score.Drift.ActionableMissing +
		score.Drift.PendingCurationMissing +
		score.Drift.Stale +
		score.Drift.Unbaselined +
		score.ManagedScope.ObservedPendingReview

	issues := []string{}
	if score.ManagedScope.ScopeChangeRequired {
		issues = append(issues, cliMessage("check.issue_scope_change", score.ManagedScope.PolicyIdentity, score.ManagedScope.ActivePolicyIdentity))
	}
	if score.ManagedScope.ObservedPendingReview > 0 {
		issues = append(issues, cliMessage("check.issue_observed_review", score.ManagedScope.ObservedPendingReview))
	}
	if score.CognitionBudget != nil && score.CognitionBudget.Mode == "enforce" && len(score.CognitionBudget.Violations) > 0 {
		issues = append(issues, cliMessage("check.issue_cognition_budget", score.CognitionBudget.WholeIndexTokens,
			score.CognitionBudget.MaxTokens, len(score.CognitionBudget.Violations)))
	}
	localeCoverage := buildLocaleMigrationCoverage(cfg)
	if localeCoverage.Active {
		issues = append(issues, cliMessage(
			"check.issue_locale_migration",
			localeCoverage.FromLocale,
			localeCoverage.ToLocale,
			len(localeCoverage.Unresolved),
		))
	}

	if score.Drift.ActionableMissing+score.Drift.Stale > 0 {
		issues = append(
			issues,
			cliMessage(
				"check.issue_entries",
				score.Drift.ActionableMissing,
				score.Drift.Stale,
			),
		)
	}

	if score.Drift.PendingCurationMissing > 0 {
		issues = append(
			issues,
			cliMessage(
				"check.issue_pending",
				score.Drift.PendingCurationMissing,
			),
		)
	}

	if score.Drift.Unbaselined > 0 {
		issues = append(
			issues,
			cliMessage(
				"check.issue_unbaselined",
				score.Drift.Unbaselined,
			),
		)
	}

	if dictDimension.Bad > 0 {
		issues = append(
			issues,
			cliMessage(
				"check.issue_dict",
				dictDimension.Bad,
			),
		)
	}

	if formatDimension.Bad > 0 {
		issues = append(
			issues,
			cliMessage(
				"check.issue_format",
				formatDimension.Bad,
			),
		)
	}

	if pendingEntries != "" {
		issues = append(
			issues,
			cliMessage(
				"check.issue_entries_draft",
				pendingEntries,
			),
		)
	}

	if pendingCuration != "" {
		issues = append(
			issues,
			cliMessage(
				"check.issue_curation_draft",
				pendingCuration,
			),
		)
	}

	report := &checkReport{
		OK:                      len(issues) == 0,
		Issues:                  issues,
		Score:                   score,
		PendingDraftRun:         pendingEntries,
		PendingCurationDraftRun: pendingCuration,
		LocaleMigration:         localeCoverage,
	}

	exitCode := ExitOK
	if !report.OK {
		exitCode = ExitDrift
	}

	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:            "check",
			Source:        ledger.SourceHuman,
			PathsCount:    score.DiskCount,
			DurationMs:    time.Since(start).Milliseconds(),
			DriftWarned:   actionableDrift > 0,
			WarningsCount: len(issues),
			ExitCode:      &exitCode,
		},
	)

	out := cmd.OutOrStdout()

	if flagJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(report); err != nil {
			return &ExitError{Code: ExitInternal, Err: err}
		}

		if exitCode != ExitOK {
			return &ExitError{Code: exitCode}
		}

		return nil
	}

	fmt.Fprintln(out, cliMessage("check.start", time.Now().UTC().Format(time.RFC3339)))

	if report.OK {
		fmt.Fprintln(out, cliMessage("check.ok"))
	} else {
		fmt.Fprintln(out, cliMessage("check.failed", len(issues)))

		for position, issue := range issues {
			fmt.Fprintf(out, "  %d. %s\n", position+1, issue)
		}
	}

	fmt.Fprintln(out, "──────────────────────────────")

	fmt.Fprintln(out, cliMessage(
		"check.detail",
		score.DiskCount,
		score.EntryCount,
		score.Drift.RawMissing,
		score.Drift.ActionableMissing,
		score.Drift.IncludedMissing,
		score.Drift.CurationExcludedMissing,
		score.Drift.SkippedMissing,
		score.Drift.PendingCurationMissing,
		score.Drift.StaleCurationDecisions,
		score.Drift.Orphan,
		score.Drift.Stale,
		score.Drift.Unbaselined,
		score.Drift.LineEndingOnly,
		sQuotaDimension.Bad,
		eScaleDimension.Bad,
		tagParseDimension.Bad,
		agentDimension.Bad == 0,
		score.IndexTokens,
	))

	if score.Drift.IncludedMissing > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_included", score.Drift.IncludedMissing))
	}

	if score.Drift.CurationExcludedMissing > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_excluded", score.Drift.CurationExcludedMissing))
	}

	nonPendingSkipped := score.Drift.SkippedMissing -
		score.Drift.PendingCurationMissing

	if nonPendingSkipped > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_skipped", nonPendingSkipped))
	}

	if score.Drift.PendingCurationMissing > 0 {
		fmt.Fprintln(out, cliMessage(
			"check.hint_pending",
			score.Drift.PendingCurationMissing,
			score.Drift.PendingCurationMissing,
		))
	}

	if score.Drift.StaleCurationDecisions > 0 {
		fmt.Fprintln(
			out,
			cliMessage("check.hint_stale_curation"),
		)
	}

	if score.Drift.Orphan > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_orphan"))
	}

	if score.Drift.LineEndingOnly > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_line_endings", score.Drift.LineEndingOnly))
	}

	if sQuotaDimension.Bad > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_squota"))
	}

	if eScaleDimension.Bad > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_escale"))
	}

	if tagParseDimension.Bad > 0 {
		fmt.Fprintln(out, cliMessage("check.hint_tagparse"))
	}

	if exitCode != ExitOK {
		return &ExitError{Code: exitCode}
	}

	return nil
}

func runVolumeCheck(cmd *cobra.Command, root string, cfg *config.Config, set *cognition.Set, start time.Time) error {
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		return &ExitError{Code: ExitInvalid, Err: err}
	}
	exitCode := ExitOK
	if !facts.GovernanceAligned {
		exitCode = ExitDrift
	}
	report := volumeCheckReport{OK: exitCode == ExitOK, ExitCode: exitCode,
		StructureValid: facts.StructureValid, GovernanceAligned: facts.GovernanceAligned,
		Findings: append([]volumegovernance.Finding{}, facts.Findings...), NextAction: facts.NextRequiredAction,
		Governance: facts}
	ledger.Append(root, cfg.LedgerEnabled, ledger.Event{Op: "check", Source: ledger.SourceHuman,
		PathsCount: facts.CodeSourceCount, DurationMs: time.Since(start).Milliseconds(),
		DriftWarned: !report.OK, WarningsCount: len(report.Findings), ExitCode: &exitCode})
	if flagJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return &ExitError{Code: ExitInternal, Err: err}
		}
	} else if report.OK {
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("check.ok"))
	} else {
		fmt.Fprint(cmd.OutOrStdout(), cliMessage("check.volumes_drift", report.NextAction, len(report.Findings)))
	}
	if exitCode != ExitOK {
		return &ExitError{Code: exitCode}
	}
	return nil
}
