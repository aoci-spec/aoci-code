// `aoci index agent curation diff`展示文件级策展变化并追加P-23 Review。
//
// 人读和JSON消费同一curationDiffReport。必须先完成Manifest核验与Review追加，
// 再输出成功报告，避免审计失败时stdout残留不完整或看似成功的JSON。
package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

// curationDecisionToView把领域决策转换为稳定Diff协议。
func curationDecisionToView(
	decision curation.Decision,
) curationDecisionView {
	return curationDecisionView{
		Decision:     decision.Decision,
		Role:         decision.Role,
		Reason:       decision.Reason,
		Confidence:   decision.Confidence,
		SourceSHA256: decision.SourceSHA256,
		Agent:        decision.Agent,
		Model:        decision.Model,
		UpdatedAt:    decision.UpdatedAt,
	}
}

// sameCurationDecisionContent只比较影响策展事实的字段。
//
// Agent、Model和UpdatedAt是Apply时补齐的审计字段，不参与草稿语义是否变化的判断。
func sameCurationDecisionContent(
	oldDecision,
	newDecision curation.Decision,
) bool {
	return oldDecision.Decision == newDecision.Decision &&
		oldDecision.Role == newDecision.Role &&
		oldDecision.Reason == newDecision.Reason &&
		oldDecision.Confidence == newDecision.Confidence &&
		oldDecision.SourceSHA256 == newDecision.SourceSHA256
}

// buildCurationDiffReport从同一份正式资产和草稿快照构造共享报告。
func buildCurationDiffReport(
	runID string,
	snapshot *curationDraftSnapshot,
	current *curation.Document,
	currentExists bool,
	currentHash string,
) curationDiffReport {
	report := curationDiffReport{
		Version:       governanceDiffReportVersion,
		OK:            true,
		RunID:         runID,
		DraftHash:     snapshot.Hash,
		Total:         len(snapshot.Document.Decisions),
		CurrentExists: currentExists,
		CurrentSHA256: currentHash,
		Items:         []curationDiffItem{},
		NextCommand: "aoci index agent curation apply " +
			runID,
	}

	for _, decision := range snapshot.Document.Decisions {
		item := curationDiffItem{
			Path:        decision.Path,
			Change:      "create",
			NewDecision: curationDecisionToView(decision),
		}

		if decision.Decision == curation.DecisionInclude {
			report.Include++
		} else {
			report.Exclude++
		}

		if old, found := curation.DecisionByPath(
			current,
			decision.Path,
		); found {
			oldView := curationDecisionToView(
				old,
			)
			item.OldExists = true
			item.OldDecision = &oldView

			if sameCurationDecisionContent(
				old,
				decision,
			) {
				item.Change = "unchanged"
			} else {
				item.Change = "update"
			}
		}

		report.Items = append(
			report.Items,
			item,
		)
	}

	return report
}

// renderCurationDiffHuman保持既有终端对照形式。
func renderCurationDiffHuman(
	output io.Writer,
	report curationDiffReport,
) {
	fmt.Fprint(output, cliMessage("curation.diff.title", report.RunID))
	fmt.Fprint(output, cliMessage(
		"curation.diff.hash",
		shortDraftHash(report.DraftHash),
	))

	for _, item := range report.Items {
		fmt.Fprintln(
			output,
			"──────────────────────────────",
		)
		fmt.Fprintln(output, cliMessage("curation.diff.path", item.Path))

		if item.OldExists &&
			item.OldDecision != nil {
			fmt.Fprint(output, cliMessage(
				"curation.diff.old",
				item.OldDecision.Decision,
				item.OldDecision.Role,
				item.OldDecision.Confidence,
				item.OldDecision.Reason,
			))
		} else {
			fmt.Fprintln(output, cliMessage("curation.diff.old_missing"))
		}

		fmt.Fprint(output, cliMessage(
			"curation.diff.new",
			item.NewDecision.Decision,
			item.NewDecision.Role,
			item.NewDecision.Confidence,
			item.NewDecision.Reason,
		))
	}

	fmt.Fprintln(
		output,
		"──────────────────────────────",
	)
	fmt.Fprint(output, cliMessage(
		"curation.diff.audit",
		shortDraftHash(report.DraftHash),
	))
	fmt.Fprint(output, cliMessage("curation.diff.apply_next", report.NextCommand))
}

func newAgentCurationDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [run_id]",
		Short: cliMessage("cli.short.agent_curation_diff"),
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
			if manifest.Kind != draft.KindCuration {
				return &ExitError{
					Code: ExitConfig,
					Err:  fmt.Errorf("%s", cliMessage("curation.diff.wrong_kind", runID)),
				}
			}

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

			current, currentExists, currentHash, err :=
				curation.Load(
					repoRoot,
				)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			report := buildCurationDiffReport(
				runID,
				snapshot,
				current,
				currentExists,
				currentHash,
			)

			if err := draft.AppendReview(
				repoRoot,
				runID,
				draft.ReviewRecord{
					Action:     draft.ReviewActionDiff,
					DraftHash:  snapshot.Hash,
					PathsCount: len(snapshot.Document.Decisions),
					Passed:     len(snapshot.Document.Decisions),
				},
			); err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"curation.diff.audit_failed",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}
			report.ReviewRecorded = true

			ledger.Append(
				repoRoot,
				cfg.LedgerEnabled,
				ledger.Event{
					Op:         "curation_diff",
					Source:     ledger.SourceHuman,
					PathsCount: len(snapshot.Document.Decisions),
					DurationMs: time.Since(start).Milliseconds(),
					DraftRunID: runID,
				},
			)

			if flagJSON {
				if err := writeGovernanceDiffJSON(
					cmd.OutOrStdout(),
					report,
				); err != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"curation.diff.json_failed",
							localeSafeCLIDetail(err.Error()),
						)),
					}
				}
				return nil
			}

			renderCurationDiffHuman(
				cmd.OutOrStdout(),
				report,
			)
			return nil
		},
	}
}
