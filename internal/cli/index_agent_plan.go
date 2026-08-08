// `aoci index agent plan` 是宿主Agent的确定性任务发放器。
//
// 纯读边界:
//   - 不调用模型;
//   - 不访问网络;
//   - 不创建草稿;
//   - 不刷新Baseline;
//   - 不写Ledger。
//
// 任务优先级:
//   - Baseline/Header/索引自身漂移硬前置;
//   - Stale与普通或已include的新文件进入Entries;
//   - Entries清空后，PendingCuration进入Curation;
//   - 最后才处理Orphan或Aligned。
//
// 换行宽容:
//   - 漂移判定经DetectWith消费团队line_ending_tolerance;
//   - 纯CRLF/LF差异不得产生Entries更新目标;
//   - repository_sha256和目标source_sha256始终取原始字节指纹，
//     因此换行变化仍会使旧Plan和旧Stage绑定失效。
//
// JSON机器合同:
//   - 内部Plan保持历史摘要结构;
//   - 输出经writeAgentPlanJSON增加raw_missing等规范别名;
//   - 新别名不参与plan_id摘要。
//
// expected_e填充:
// populateAgentPlanTargets接收头部E阈值表，对目标经
// index.ExpectedEScaleSymbols导出所属档位；update目标缺少Lines时使用
// fs.CountFileLines现算。统计失败时留空，不阻断计划。
package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
	"github.com/spf13/cobra"
)

func newIndexAgentCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "agent",
		Short: cliMessage("cli.short.agent"),
		Long:  indexAgentLongHelp(),
	}

	command.AddCommand(
		newIndexAgentPlanCmd(),
	)
	command.AddCommand(
		newIndexAgentGuideCmd(),
	)
	command.AddCommand(
		newIndexAgentStageCmd(),
	)
	command.AddCommand(
		newIndexAgentHeaderCmd(),
	)
	command.AddCommand(
		newIndexAgentCurationCmd(),
	)

	return command
}

func newIndexAgentPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: cliMessage("cli.short.agent_plan"),
		Args:  cobra.NoArgs,
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}
			if err := guardPendingEntriesForAgent(repoRoot); err != nil {
				return &ExitError{Code: ExitInvalid, Err: err}
			}

			document, indexPath, err := loadIndexForCLI(
				cmd,
				repoRoot,
				cfg,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			plan, err := buildAgentPlan(
				repoRoot,
				cfg,
				document,
				indexPath,
			)
			if err != nil {
				var buildErr *agentPlanBuildError

				if errors.As(
					err,
					&buildErr,
				) {
					return &ExitError{
						Code: buildErr.Code,
						Err:  buildErr.Err,
					}
				}

				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}

			if flagJSON {
				return writeAgentPlanJSON(
					cmd.OutOrStdout(),
					plan,
				)
			}

			fmt.Fprint(
				cmd.OutOrStdout(),
				renderAgentPlan(plan),
			)

			return nil
		},
	}
}

func guardPendingEntriesForAgent(repoRoot string) error {
	runID, err := draft.LatestPendingRun(repoRoot, draft.KindEntries)
	if err != nil {
		return fmt.Errorf("%s: %w", cliMessage("entries.pending_recovery", runID, runID), err)
	}
	if runID != "" {
		return fmt.Errorf("%s", cliMessage("entries.pending_recovery", runID, runID))
	}
	return nil
}

func buildAgentPlan(
	repoRoot string,
	cfg *config.Config,
	document *index.Document,
	indexPath string,
) (*agentPlan, error) {
	indexBytes, err := os.ReadFile(
		indexPath,
	)
	if err != nil {
		return nil, &agentPlanBuildError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"plan.read_index_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	headerText, _ := index.ExtractHeader(
		string(indexBytes),
	)

	headerState, headerMessage :=
		inspectAgentPlanHeader(
			headerText,
		)
	indexLocale, _, localeErr := index.DetectLocale(headerText)
	if localeErr != nil {
		return nil, &agentPlanBuildError{Code: ExitConfig, Err: localeErr}
	}
	localeHeaderRequired := indexLocale != cfg.Locale
	_, localeGovernanceEntries := splitLocaleMigrationEntryPaths(cfg.LocaleMigration)
	if cfg.LocaleMigration != nil && (cfg.LocaleMigration.HeaderPending ||
		cfg.LocaleMigration.ManagedIndexTextPending ||
		cfg.LocaleMigration.AgentsManagedBlockPending ||
		len(localeGovernanceEntries) > 0) {
		localeHeaderRequired = true
	}
	if localeHeaderRequired {
		headerState = agentPlanHeaderMissing
		headerMessage = cliMessage(
			"plan.locale_migration",
			indexLocale,
			cfg.Locale,
		)
	}

	state, err := managedstate.Load(repoRoot, cfg)
	if err != nil {
		return nil, &agentPlanBuildError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"plan.snapshot_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}
	currentBaseline, baselineExists := state.Baseline, state.Baseline != nil
	snapshot := state.Snapshot
	snapshotWarnings := state.Warnings
	var safeInventorySummary *afs.SafeInventorySummary
	if state.Legacy {
		legacySnapshot, warnings, inventory, snapshotErr := baseline.SnapshotWithInventory(repoRoot, cfg.WalkOptions())
		if snapshotErr != nil {
			return nil, &agentPlanBuildError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
				"plan.snapshot_failed", localeSafeCLIDetail(snapshotErr.Error())))}
		}
		snapshot, snapshotWarnings = legacySnapshot, warnings
		safeInventorySummary = &inventory.Summary
	} else if state.Evaluation != nil {
		summary := state.Evaluation.SafeInventory
		safeInventorySummary = &summary
	}

	// 安全绑定始终使用原始SHA与实际字节数。
	repositorySHA256 := calculateRepositorySnapshotHash(snapshot)

	// 判定宽容、绑定严格：
	// 这里只决定是否派发任务；snapshot中的原始SHA仍用于Plan与Stage绑定。
	detected := &baseline.DetectResult{Missing: []string{}, Orphan: []string{}, Stale: []string{}, Unbaselined: []string{},
		LineEndingOnly: []string{}, ObservedNew: []string{}, ObservedChanged: []string{}, ObservedRemoved: []string{}, Warnings: []string{}}
	if !state.ScopeChangeRequired {
		detected, err = managedstate.Detect(repoRoot, cfg, document, state)
		if err != nil {
			return nil, &agentPlanBuildError{Code: ExitInternal, Err: err}
		}
	}

	missingClassification, missingInventory, err :=
		indexgen.BuildMissingClassification(
			repoRoot,
			cfg,
			document,
			detected.Missing,
		)
	if err != nil {
		return nil, &agentPlanBuildError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"plan.curation_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	classified := classifyUpdateTargets(
		cfg,
		detected,
		missingClassification,
	)

	plan := &agentPlan{
		Version:          agentPlanVersion,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		RepositoryRoot:   repoRoot,
		IndexPath:        cfg.IndexPath,
		IndexSHA256:      sha256Hex(indexBytes),
		HeaderSHA256:     sha256Hex([]byte(headerText)),
		CurationSHA256:   missingClassification.CurationSHA256,
		RepositorySHA256: repositorySHA256,
		HeaderState:      headerState,
		HeaderMessage:    headerMessage,
		BaselineExists:   baselineExists,
		AutomationMode:   cfg.EffectiveAutomationMode(),
		IndexSelfStale:   classified.IndexSelfStale,
		LocaleMigration:  buildLocaleMigrationCoverage(cfg),
		SafeInventory:    safeInventorySummary,
		Targets:          []agentPlanTarget{},
		CurationTargets:  []agentPlanCurationTarget{},
		CurationExcluded: append(
			[]string{},
			missingClassification.CurationExcluded...,
		),
		SkippedMissing: convertAgentPlanSkipped(
			missingClassification.Skipped,
		),
		Orphans: append(
			[]string{},
			detected.Orphan...,
		),
		Unbaselined: append(
			[]string{},
			detected.Unbaselined...,
		),
		Warnings: localizePlanWarnings(snapshotWarnings),
		Summary: agentPlanSummary{
			Changed: len(
				classified.Changed,
			),
			Missing: len(
				detected.Missing,
			),
			ActionableNew: len(
				missingClassification.Actionable,
			),
			IncludedMissing: len(
				missingClassification.Included,
			),
			CurationExcluded: len(
				missingClassification.CurationExcluded,
			),
			SkippedMissing: len(
				missingClassification.Skipped,
			),
			PendingCuration: len(
				missingClassification.Pending,
			),
			StaleCurationDecisions: len(
				missingClassification.StaleDecisions,
			),
			Orphan: len(
				detected.Orphan,
			),
			Unbaselined: len(
				detected.Unbaselined,
			),
		},
	}
	if !state.Legacy {
		budgetReport, budgetErr := cognitionbudget.Build(repoRoot, indexBytes, cfg.EffectiveCognitionBudget())
		if budgetErr != nil {
			return nil, &agentPlanBuildError{Code: ExitConfig, Err: budgetErr}
		}
		policy := cfg.EffectiveManagedScope()
		plan.Governance = &agentPlanGovernance{ScopeChangeRequired: state.ScopeChangeRequired,
			DesiredPolicyIdentity: state.DesiredPolicyIdentity, ActivePolicyIdentity: state.ActivePolicyIdentity,
			ObserveReviewRequired: policy.ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired,
			ObservedNew:           append([]string{}, detected.ObservedNew...), ObservedChanged: append([]string{}, detected.ObservedChanged...),
			ObservedRemoved: append([]string{}, detected.ObservedRemoved...), CognitionBudget: budgetReport}
		if state.Evaluation != nil {
			plan.Governance.IndexCount, plan.Governance.ObserveCount, plan.Governance.ExcludeCount =
				state.Evaluation.IndexCount, state.Evaluation.ObserveCount, state.Evaluation.ExcludeCount
		}
	}

	if currentBaseline != nil {
		plan.BaselineUpdatedAt =
			currentBaseline.UpdatedAt
	}

	switch {
	case !baselineExists:
		plan.Stage =
			agentPlanStageBaselineRequired
		plan.NextAction =
			agentPlanActionScan

	case plan.Governance != nil && plan.Governance.ScopeChangeRequired:
		plan.Stage = agentPlanStageScopeChangeRequired
		plan.NextAction = agentPlanActionScopePreview

	case state.Legacy && len(detected.Unbaselined) > 0:
		plan.Stage = agentPlanStageBaselineRequired
		plan.NextAction = agentPlanActionScan

	case headerState != agentPlanHeaderReady:
		plan.Stage =
			agentPlanStageHeaderRequired
		plan.NextAction =
			agentPlanActionGenerateHead

	case classified.IndexSelfStale:
		plan.Stage =
			agentPlanStageIndexReviewRequired
		plan.NextAction =
			agentPlanActionReviewIndex

	case plan.Governance != nil && plan.Governance.ObserveReviewRequired &&
		(len(plan.Governance.ObservedNew)+len(plan.Governance.ObservedChanged)+len(plan.Governance.ObservedRemoved) > 0):
		plan.Stage = agentPlanStageObservedReview
		plan.NextAction = agentPlanActionReviewObserved

	case plan.Governance != nil && plan.Governance.CognitionBudget != nil &&
		plan.Governance.CognitionBudget.WholeIndexTokens > plan.Governance.CognitionBudget.MaxTokens:
		plan.Stage = agentPlanStageCompressionRequired
		plan.NextAction = agentPlanActionCompressCognition

	case plan.Governance != nil && plan.Governance.CognitionBudget != nil &&
		len(plan.Governance.CognitionBudget.Violations) > 0:
		plan.Stage = agentPlanStageBudgetExceeded
		plan.NextAction = agentPlanActionCompressCognition

	default:
		if err := populateAgentPlanTargets(
			plan,
			document,
			repoRoot,
			snapshot,
			classified,
			missingInventory,
			index.ExtractEScaleThresholds(
				headerText,
			),
		); err != nil {
			return nil, err
		}
		if err := populateLocaleMigrationTargets(
			plan,
			cfg,
			document,
			repoRoot,
			snapshot,
			index.ExtractEScaleThresholds(headerText),
		); err != nil {
			return nil, err
		}

		populateAgentPlanCurationTargets(
			plan,
			missingClassification,
		)
		populateLocaleMigrationCurationTargets(plan, cfg, repoRoot)

		switch {
		case len(plan.Targets) > 0:
			plan.Stage =
				agentPlanStageEntriesRequired
			plan.NextAction =
				agentPlanActionStageEntries

		case len(plan.CurationTargets) > 0:
			plan.Stage =
				agentPlanStageCurationRequired
			plan.NextAction =
				agentPlanActionStageCuration

		case len(plan.Orphans) > 0:
			plan.Stage =
				agentPlanStageOrphanReview
			plan.NextAction =
				agentPlanActionReviewOrphans

		default:
			plan.Stage =
				agentPlanStageAligned
			plan.NextAction =
				agentPlanActionNone
		}
	}

	plan.Summary.ExecutableTargets =
		len(plan.Targets)

	plan.PlanID, err =
		calculateAgentPlanID(plan)
	if err != nil {
		return nil, &agentPlanBuildError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"plan.hash_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	return plan, nil
}

func localizePlanWarnings(warnings []string) []string {
	localized := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		localized = append(localized, localeSafeCLIDetail(warning))
	}
	return localized
}
