// `aoci index update`持续维护入口。
//
// changed始终来自已有条目的Stale。
// new来自正式文件级策展分类后的ActionableMissing。
// Included是new子集，Pending是Skipped子集。
//
// 换行宽容:
//   - 漂移判定经DetectWith消费团队line_ending_tolerance;
//   - 纯CRLF/LF表示差异不进入changed，不调用模型、不创建草稿;
//   - 真实内容变化与团队显式严格模式仍按Stale进入changed;
//   - 本命令只决定任务分类，不参与CAS或Stage源码绑定。
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/workflow"
	"github.com/spf13/cobra"
)

const updateListCap = 20

type updateClassification struct {
	Changed                []string
	NewFiles               []string
	IncludedNew            []string
	CurationExcludedNew    []string
	SkippedMissing         []indexgen.SkippedMissing
	PendingCuration        []indexgen.SkippedMissing
	StaleCurationDecisions []string
	CurationSHA256         string
	IndexSelfStale         bool
}

func classifyUpdateTargets(
	cfg *config.Config,
	result *baseline.DetectResult,
	provided ...indexgen.MissingClassification,
) updateClassification {
	classified := updateClassification{
		Changed:                []string{},
		NewFiles:               []string{},
		IncludedNew:            []string{},
		CurationExcludedNew:    []string{},
		SkippedMissing:         []indexgen.SkippedMissing{},
		PendingCuration:        []indexgen.SkippedMissing{},
		StaleCurationDecisions: []string{},
	}

	if result == nil {
		return classified
	}

	for _, relPath := range result.Stale {
		if cfg != nil &&
			relPath == cfg.IndexPath {
			classified.IndexSelfStale = true
			continue
		}

		classified.Changed = append(
			classified.Changed,
			relPath,
		)
	}

	missing := indexgen.ClassifyMissing(
		cfg,
		result.Missing,
		nil,
	)

	if len(provided) > 0 {
		missing = provided[0]
	}

	classified.NewFiles = append(
		classified.NewFiles,
		missing.Actionable...,
	)

	classified.IncludedNew = append(
		classified.IncludedNew,
		missing.Included...,
	)

	classified.CurationExcludedNew = append(
		classified.CurationExcludedNew,
		missing.CurationExcluded...,
	)

	classified.SkippedMissing = append(
		classified.SkippedMissing,
		missing.Skipped...,
	)

	for _, pending := range missing.Pending {
		classified.PendingCuration = append(
			classified.PendingCuration,
			indexgen.SkippedMissing{
				Path:   pending.Path,
				Reason: pending.ProfileReason,
			},
		)
	}

	classified.StaleCurationDecisions = append(
		classified.StaleCurationDecisions,
		missing.StaleDecisions...,
	)

	classified.CurationSHA256 =
		missing.CurationSHA256

	return classified
}

func newIndexUpdateCmd() *cobra.Command {
	var dryRun bool

	command := &cobra.Command{
		Use:   "update",
		Short: cliMessage("cli.short.index_update"),
		Long:  indexUpdateLongHelp(),
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			start := time.Now()

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

			mode := cfg.EffectiveAutomationMode()

			document, _, err := loadIndexForCLI(
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

			options := cfg.WalkOptions()

			snapshot, warnings, err :=
				baseline.Snapshot(
					repoRoot,
					options,
				)
			if err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"update.snapshot_failed",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			for _, warning := range warnings {
				fmt.Fprintln(
					cmd.ErrOrStderr(),
					cliMessage("update.snapshot_warning", localeSafeCLIDetail(warning)),
				)
			}

			currentBaseline, _, err :=
				baseline.Load(repoRoot)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err: fmt.Errorf("%s", cliMessage(
						"update.baseline_damaged",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			detected := baseline.DetectWith(
				repoRoot,
				document,
				currentBaseline,
				snapshot,
				options,
				cfg.LineEndingTolerance,
			)

			missingClassification, _, err :=
				indexgen.BuildMissingClassification(
					repoRoot,
					cfg,
					document,
					detected.Missing,
				)
			if err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"update.classification_failed",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			classified := classifyUpdateTargets(
				cfg,
				detected,
				missingClassification,
			)

			out := cmd.OutOrStdout()

			renderUpdateClassification(
				out,
				mode,
				cfg.IndexPath,
				classified,
				detected,
			)

			targets := append(
				append(
					[]string{},
					classified.Changed...,
				),
				classified.NewFiles...,
			)

			appendUpdateLedger := func(
				runID string,
			) {
				ledger.Append(
					repoRoot,
					cfg.LedgerEnabled,
					ledger.Event{
						Op: "index_update",
						Source: ledger.
							SourceHuman,
						PathsCount: len(targets),
						DurationMs: time.
							Since(start).
							Milliseconds(),
						DraftRunID: runID,
					},
				)
			}

			if len(targets) == 0 {
				appendUpdateLedger("")

				fmt.Fprintln(out, cliMessage("update.no_actionable"))

				if len(
					classified.PendingCuration,
				) > 0 {
					fmt.Fprintln(out, cliMessage("update.pending_remains"))
				}

				return nil
			}

			if dryRun {
				appendUpdateLedger("")

				renderUpdateDryRunSummary(
					out,
					targets,
					classified,
				)

				return nil
			}

			if mode ==
				config.AutomationModeOff {
				appendUpdateLedger("")

				// 保留既有稳定文案锚点：
				// 测试据此证明早退发生在AI客户端构造之前。
				fmt.Fprint(out, cliMessage("update.off", len(targets)))

				return nil
			}

			client, err := buildAIClient(cfg)
			if err != nil {
				return err
			}

			oldEntries := make(
				map[string]string,
				len(classified.Changed),
			)

			for _, relPath := range classified.Changed {
				hit := index.FindEntry(
					document,
					relPath,
				)

				if hit != nil {
					oldEntries[relPath] =
						hit.FullLine
				}
			}

			fmt.Fprint(out, cliMessage(
				"update.start",
				len(targets),
				len(classified.Changed),
				len(classified.NewFiles),
				len(classified.IncludedNew),
				cfg.AI.Model,
				displayConcurrency(cfg),
			))

			ctx, cancel := context.WithTimeout(
				context.Background(),
				singleCallTimeout(cfg)*
					time.Duration(
						len(targets),
					),
			)
			defer cancel()

			draftResult, err :=
				workflow.RunEntriesDraft(
					ctx,
					repoRoot,
					cfg,
					document,
					client,
					targets,
					oldEntries,
					workflow.WithProgress(
						entriesProgressPrinter(
							cmd.ErrOrStderr(),
						),
					),
				)
			if err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err:  wrapAIErr(cfg, err),
				}
			}

			appendUpdateLedger(
				draftResult.RunID,
			)

			fmt.Fprint(out, cliMessage("build.run", draftResult.RunID))

			fmt.Fprintln(
				out,
				"──────────────────────────────",
			)

			for _, status := range draftResult.Statuses {
				line := fmt.Sprintf(
					"[%s] %s",
					status.Status,
					status.Path,
				)

				if _, isUpdate :=
					oldEntries[status.Path]; isUpdate {
					line += cliMessage("update.mode_suffix")
				}

				if status.Note != "" {
					line += " —— " + localeSafeCLIDetail(status.Note)
				}

				fmt.Fprintln(
					out,
					line,
				)
			}

			fmt.Fprintln(
				out,
				"──────────────────────────────",
			)

			fmt.Fprint(out, cliMessage(
				"build.total",
				draftResult.Drafted,
				draftResult.Warned,
				draftResult.Failed,
				draftResult.Skipped,
			))

			fmt.Fprint(out, cliMessage(
				"build.tokens",
				draftResult.InputTokens,
				draftResult.OutputTokens,
				draftResult.TokenSrc,
			))

			return finishIndexUpdateAutomation(
				repoRoot,
				cfg,
				document,
				draftResult,
				len(targets),
				out,
			)
		},
	}

	command.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		cliMessage("cli.flag.update_dry_run"),
	)

	return command
}
