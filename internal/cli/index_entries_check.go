// `aoci index entries check`: 人工CLI参数解析、输出和退出策略薄壳。
//
// 格式、R关系Warning、字典、S配额、E档位、草稿快照、ReviewRecord与Ledger
// 全部委托runEntriesCheckCore。JSON模式仅改变渲染，不得恢复第二套校验循环。
//
// 工作流纪律：
//   - Check只负责机器预检和摘要审计，不授权直接Apply；
//   - Check通过后的唯一下一步是Entries Diff；
//   - Diff形成完整旧新对照与P-23内容审阅记录后，再按Guide模式决定Apply；
//   - Auto可以连续执行Diff和Apply，不因该导航增加人工停点。
package cli

import (
	"fmt"
	"io"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

func newEntriesCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [run_id]",
		Short: cliMessage("cli.short.index_entries_check"),
		Long:  entriesCheckLongHelp(),
		Args:  cobra.MaximumNArgs(1),
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

			coreOutput := cmd.OutOrStdout()
			if flagJSON {
				coreOutput = io.Discard
			}

			result, err := runEntriesCheckCore(
				repoRoot,
				runID,
				cfg,
				doc,
				coreOutput,
				ledger.SourceHuman,
			)
			if err != nil {
				return err
			}
			if result == nil ||
				result.Snapshot == nil {
				return &ExitError{
					Code: ExitInternal,
					Err:  fmt.Errorf("%s", cliMessage("entries.check.result_incomplete")),
				}
			}

			if flagJSON {
				report := entriesCheckReport{
					Version:   entriesReportVersion,
					OK:        result.Review.Rejected == 0,
					RunID:     result.RunID,
					DraftHash: result.Snapshot.Hash,
					Total:     result.Review.PathsCount,
					Passed:    result.Review.Passed,
					Warned:    result.Review.Warned,
					Rejected:  result.Review.Rejected,
					Skipped:   result.Review.Skipped,
					Items:     result.Items,
				}

				if result.Review.Rejected > 0 {
					report.Recovery = cliMessage("entries.check.recovery", runID)
				} else {
					report.NextCommand =
						"aoci index entries diff " +
							runID
				}

				if err := writeEntriesJSON(
					cmd.OutOrStdout(),
					report,
				); err != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"entries.check.json_failed",
							localeSafeCLIDetail(err.Error()),
						)),
					}
				}

				if result.Review.Rejected > 0 {
					return &ExitError{
						Code: ExitInvalid,
					}
				}
				return nil
			}

			if result.Review.Rejected > 0 {
				fmt.Fprintln(
					cmd.OutOrStdout(),
					cliMessage("entries.check.repair_hint"),
				)
				return &ExitError{
					Code: ExitInvalid,
					Msg:  "",
				}
			}

			fmt.Fprintln(
				cmd.OutOrStdout(),
				cliMessage("entries.check.next_diff", runID),
			)
			return nil
		},
	}
}
