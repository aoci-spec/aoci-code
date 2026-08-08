// `aoci index entries diff`: 对照当前草稿内存快照并追加P-23 Review。
//
// 人读与JSON模式消费同一entriesDiffReport。JSON保留完整单行old_entry和
// new_entry，不拆解F/R/A/S；非可审阅目标也保留为skipped项。
package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

func newEntriesDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [run_id]",
		Short: cliMessage("cli.short.entries_diff"),
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

			cfg, err := config.Load(
				repoRoot,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			runID, err := resolveEntriesRunID(
				repoRoot,
				args,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
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

			snapshot, err := loadEntryDraftSnapshot(
				repoRoot,
				runID,
				manifest,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err: fmt.Errorf("%s", cliMessage(
						"entries.diff.snapshot_read_failed",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			doc, _, err := loadIndexForCLI(
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

			report := buildEntriesDiffReport(
				runID,
				manifest,
				snapshot,
				doc,
			)

			if err := draft.AppendReview(
				repoRoot,
				runID,
				draft.ReviewRecord{
					Action:     draft.ReviewActionDiff,
					DraftHash:  snapshot.Hash,
					PathsCount: len(manifest.Entries),
					Passed:     report.Reviewed,
					Skipped:    report.Skipped,
				},
			); err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"entries.diff.audit_failed",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			ledger.Append(
				repoRoot,
				cfg.LedgerEnabled,
				ledger.Event{
					Op:         "entries_diff",
					Source:     ledger.SourceHuman,
					PathsCount: len(manifest.Entries),
					DurationMs: time.Since(start).Milliseconds(),
					DraftRunID: runID,
				},
			)

			if flagJSON {
				if err := writeEntriesJSON(
					cmd.OutOrStdout(),
					report,
				); err != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"entries.diff.json_failed",
							localeSafeCLIDetail(err.Error()),
						)),
					}
				}
				return nil
			}

			renderEntriesDiffHuman(
				cmd.OutOrStdout(),
				report,
			)
			return nil
		},
	}
}

// buildEntriesDiffReport构造一次固定快照的完整Diff业务对象。
func buildEntriesDiffReport(
	runID string,
	manifest *draft.Manifest,
	snapshot *entryDraftSnapshot,
	doc *index.Document,
) entriesDiffReport {
	report := entriesDiffReport{
		Version:   entriesReportVersion,
		OK:        true,
		RunID:     runID,
		DraftHash: snapshot.Hash,
		Total:     len(manifest.Entries),
		Items:     []entriesDiffItem{},
		NextCommand: "aoci index entries apply " +
			runID,
	}

	for _, status := range manifest.Entries {
		item := entriesDiffItem{
			Path:     status.Path,
			Status:   status.Status,
			Note:     status.Note,
			Reviewed: false,
			Change:   "skipped",
			OldEntry: "",
			NewEntry: "",
		}

		if status.Status != "drafted" &&
			status.Status != "warned" {
			item.SkipReason = status.Note
			report.Skipped++
			report.Items = append(
				report.Items,
				item,
			)
			continue
		}

		line, lineErr := snapshot.line(
			status.Path,
		)
		if lineErr != nil {
			item.SkipReason = localeSafeCLIDetail(lineErr.Error())
			report.Skipped++
			report.Items = append(
				report.Items,
				item,
			)
			continue
		}

		if hit := index.FindEntry(
			doc,
			status.Path,
		); hit != nil {
			item.OldEntry = hit.FullLine
		}
		item.NewEntry = line
		item.Reviewed = true

		switch {
		case item.OldEntry == "":
			item.Change = "create"
		case item.OldEntry == item.NewEntry:
			item.Change = "unchanged"
		default:
			item.Change = "update"
		}

		report.Reviewed++
		report.Items = append(
			report.Items,
			item,
		)
	}

	return report
}

// renderEntriesDiffHuman保持既有人读对照格式。
func renderEntriesDiffHuman(
	out io.Writer,
	report entriesDiffReport,
) {
	fmt.Fprint(
		out,
		cliMessage("entries.diff.heading", report.RunID, report.Total),
	)
	fmt.Fprint(
		out,
		cliMessage("entries.diff.draft_hash", shortDraftHash(report.DraftHash)),
	)

	for _, item := range report.Items {
		fmt.Fprintln(
			out,
			"──────────────────────────────",
		)
		fmt.Fprintf(
			out,
			"[%s] %s\n",
			item.Status,
			item.Path,
		)

		if !item.Reviewed {
			if item.SkipReason != "" {
				fmt.Fprintln(
					out,
					"  "+item.SkipReason,
				)
			}
			continue
		}

		fmt.Fprint(
			out,
			index.RenderEntryDiff(
				item.OldEntry,
				item.NewEntry,
				cliMessage("entries.diff.new_entry"),
			),
		)
		if item.Note != "" {
			fmt.Fprintln(
				out,
				cliMessage("entries.diff.note", item.Note),
			)
		}
	}

	fmt.Fprintln(
		out,
		"──────────────────────────────",
	)
	fmt.Fprint(
		out,
		cliMessage("entries.diff.apply_hint", report.RunID),
	)
	fmt.Fprint(
		out,
		cliMessage("entries.diff.audit_record", shortDraftHash(report.DraftHash)),
	)
}
