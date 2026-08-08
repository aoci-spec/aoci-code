// 索引条目: index_header.go[CHD7AM]
//
// 本文件承载`aoci index header`命令组的公共入口、Run解析、Show和
// Endpoint-native Draft。
//
// 模块边界:
//   - index_header.go: 命令挂载、Run解析、Show、Endpoint Draft;
//   - index_header_review.go: R52、Header P-23、草稿快照和Diff;
//   - index_header_apply.go: Apply全部闸门与正式索引写入事务。
//
// Draft-first:
// Endpoint Draft和Host-Agent Header Stage都只写.aoci/drafts/<run_id>/，
// 不修改正式索引或Baseline。正式写入只能由header apply完成。
//
// 人工批准:
// Header Diff负责展示候选并记录被审阅内容的SHA-256；Header Apply同时核对
// R52 Run一致性和Header P-23内容一致性。显式run_id不能绕过内容摘要核对。
package cli

import (
	"context"
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/llm"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/internal/workflow"
	"github.com/spf13/cobra"
)

// newIndexHeaderCmd构造`aoci index header`子命令组。
func newIndexHeaderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "header",
		Short: cliMessage("cli.short.index_header"),
	}
	cmd.AddCommand(newHeaderDraftCmd())
	cmd.AddCommand(newHeaderDiffCmd())
	cmd.AddCommand(newHeaderApplyJSONCmd())
	cmd.AddCommand(newHeaderShowCmd())
	return cmd
}

// resolveHeaderRunID解析run_id参数。
//
// 显式给定时使用指定Run；省略时选择最新Header草稿。
func resolveHeaderRunID(
	root string,
	args []string,
) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	runID, err := draft.LatestRunID(
		root,
		draft.KindHeader,
	)
	if err != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"header.run_not_found",
			localeSafeCLIDetail(err.Error()),
		))
	}
	return runID, nil
}

// newHeaderShowCmd构造`aoci index header show`。
//
// 与MCP的aoci_header共用mcptools.BuildHeaderText，避免出现第二套头部读取逻辑。
func newHeaderShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: cliMessage("cli.short.index_header_show"),
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

			header, failure := mcptools.BuildHeaderText(
				repoRoot,
				ledger.SourceHuman,
			)
			if failure != nil {
				return &ExitError{
					Code: ExitConfig,
					Err: fmt.Errorf("%s", cliMessage(
						"header.show.failed",
						failure.Code,
						localeSafeCLIDetail(failure.Msg),
						localeSafeCLIDetail(failure.Hint),
					)),
				}
			}

			output := cmd.OutOrStdout()
			if header == "" {
				fmt.Fprintln(output, cliMessage("header.show.empty"))
				return nil
			}

			fmt.Fprint(output, header)
			return nil
		},
	}
}

// newHeaderDraftCmd构造`aoci index header draft`。
//
// 本命令调用用户配置端点，只把候选写入标准Header草稿区。
func newHeaderDraftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "draft",
		Short: cliMessage("cli.short.index_header_draft"),
		Long:  headerDraftLongHelp(),
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

			client, err := buildAIClient(cfg)
			if err != nil {
				return err
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

			output := cmd.OutOrStdout()
			fmt.Fprint(output, cliMessage("header.draft.start", cfg.AI.Model))

			ctx, cancel := context.WithTimeout(
				context.Background(),
				singleCallTimeout(cfg),
			)
			defer cancel()

			result, err := workflow.RunHeaderDraft(
				ctx,
				repoRoot,
				cfg,
				doc,
				client,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}

			fmt.Fprint(output, cliMessage(
				"header.draft.created",
				result.RunID,
				result.RunID,
				draft.HeaderFileName,
			))

			if result.Usage.Source == llm.TokenSourceExact {
				fmt.Fprint(output, cliMessage(
					"header.draft.tokens_exact",
					result.Usage.InputTokens,
					result.Usage.OutputTokens,
				))
			} else {
				fmt.Fprintln(output, cliMessage("header.draft.tokens_estimated"))
			}

			for _, warning := range result.Warnings {
				fmt.Fprintln(output, cliMessage(
					"header.warning",
					localeSafeCLIDetail(warning),
				))
			}

			fmt.Fprintln(output, cliMessage("header.draft.next"))
			return nil
		},
	}
}
