// 索引条目: index_header.go[CHD7AM]
//
// 本文件承载`aoci index header`命令组的公共入口与Show。
//
// 0.2.0 起 Legacy 布局只读:Endpoint Draft、Diff、Apply 随 Legacy 写作面一起退役,
// Volumes v1 的 Meta 由 init 生成、由治理交易改写,没有人工 Header 写入命令。
package cli

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/spf13/cobra"
)

// newIndexHeaderCmd构造`aoci index header`子命令组。
func newIndexHeaderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "header",
		Short: cliMessage("cli.short.index_header"),
	}
	cmd.AddCommand(newHeaderShowCmd())
	return cmd
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
