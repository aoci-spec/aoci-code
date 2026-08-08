// `aoci index agent curation apply`应用已审阅文件级语义策展决策。
//
// 正式写入前核对:
//   - R52 Run一致性;
//   - Generation Plan与curation_sha256;
//   - P-23草稿摘要;
//   - 写锁内curation.json CAS;
//   - Decision字段与source_sha256。
package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

func newAgentCurationApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [run_id]",
		Short: cliMessage("cli.short.agent_curation_apply"),
		Args:  cobra.MaximumNArgs(1),
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

			runID, err := resolveCurationRunID(
				repoRoot,
				args,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			output := cmd.OutOrStdout()

			runWarning, runErr := guardImplicitApply(
				repoRoot,
				runID,
				len(args) > 0,
				"aoci index agent curation apply",
				"curation_diff",
			)
			if runErr != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  runErr,
				}
			}
			if runWarning != "" {
				fmt.Fprintln(
					output,
					runWarning,
				)
			}

			manifest, err := draft.LoadManifest(
				repoRoot,
				runID,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			generationNote, err := guardHostAgentCurationGeneration(
				cmd,
				repoRoot,
				cfg,
				manifest,
			)
			if err != nil {
				return err
			}
			fmt.Fprintln(
				output,
				generationNote,
			)

			snapshot, err := loadCurationDraftSnapshot(
				repoRoot,
				runID,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  err,
				}
			}

			contentWarning, err := guardReviewedCurationHash(
				manifest,
				snapshot.Hash,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  err,
				}
			}
			if contentWarning != "" {
				fmt.Fprintln(
					output,
					contentWarning,
				)
			} else {
				fmt.Fprint(output, cliMessage(
					"curation.apply.review_ok",
					shortDraftHash(snapshot.Hash),
				))
			}

			lock, lockErr := afs.AcquireIndexLock(
				repoRoot,
			)
			if lockErr != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"curation.apply.lock_failed",
						localeSafeCLIDetail(lockErr.Error()),
					)),
				}
			}
			defer func() {
				if releaseErr := lock.Release(); releaseErr != nil {
					fmt.Fprintln(os.Stderr, cliMessage(
						"curation.apply.lock_release_warning",
						localeSafeCLIDetail(releaseErr.Error()),
					))
				}
			}()

			current, _, currentHash, err := curation.Load(
				repoRoot,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			if currentHash != manifest.CurationSHA256 {
				return &ExitError{
					Code: ExitInvalid,
					Err: fmt.Errorf("%s", cliMessage(
						"curation.apply.cas_conflict",
						shortAgentStageHash(
							manifest.CurationSHA256,
						),
						shortAgentStageHash(
							currentHash,
						),
					)),
				}
			}

			merged, err := curation.Merge(
				current,
				snapshot.Document.Decisions,
				manifest.AgentName,
				manifest.Model,
				time.Now(),
			)
			if err != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  err,
				}
			}

			if err := curation.Save(
				repoRoot,
				merged,
			); err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}
			completedLocalePaths := make(
				[]string,
				0,
				len(snapshot.Document.Decisions),
			)
			for _, decision := range snapshot.Document.Decisions {
				completedLocalePaths = append(completedLocalePaths, decision.Path)
			}
			if err := config.AdvanceLocaleMigration(
				repoRoot,
				false,
				nil,
				completedLocalePaths,
			); err != nil {
				if rollbackErr := curation.Save(repoRoot, current); rollbackErr != nil {
					return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
						"curation.apply.migration_rollback_failed",
						localeSafeCLIDetail(err.Error()),
						localeSafeCLIDetail(rollbackErr.Error()),
					))}
				}
				return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
					"curation.apply.migration_rolled_back",
					localeSafeCLIDetail(err.Error()),
				))}
			}

			if err := draft.AppendApplication(
				repoRoot,
				runID,
				draft.ApplicationRecord{
					DraftHash:  snapshot.Hash,
					PathsCount: len(snapshot.Document.Decisions),
					Applied:    len(snapshot.Document.Decisions),
				},
				true,
			); err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"curation.apply.application_failed",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			ledger.Append(
				repoRoot,
				cfg.LedgerEnabled,
				ledger.Event{
					Op:           "curation_apply",
					Source:       ledger.SourceHuman,
					PathsCount:   len(snapshot.Document.Decisions),
					AppliedCount: len(snapshot.Document.Decisions),
					DurationMs:   time.Since(start).Milliseconds(),
					DraftRunID:   runID,
				},
			)

			includeCount := 0
			excludeCount := 0

			for _, decision := range snapshot.Document.Decisions {
				if decision.Decision == curation.DecisionInclude {
					includeCount++
				} else {
					excludeCount++
				}
			}

			fmt.Fprint(output, cliMessage("curation.apply.applied", runID))
			fmt.Fprint(output, cliMessage(
				"curation.apply.total",
				includeCount,
				excludeCount,
			))
			fmt.Fprintln(output, cliMessage("curation.apply.next"))

			return nil
		},
	}
}
