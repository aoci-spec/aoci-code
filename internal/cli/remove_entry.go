// aoci remove-entry —— 人用单条删除命令(v2.8 P1,D75下半句的人工执行工具)
// 索引条目: remove_entry.go(待补录,随本批入册)
//
// 与 MCP 的 aoci_remove_entry 共用 mcptools.ApplyRemoveEntry 同一管线
// (定位→删除→原子落盘→索引自身基线前移→落账),差异仅护栏:
// CLI 传 orphanOnly=false —— 删除是人工策展全权,活文件条目亦可删
// (删后该文件按 Missing 态浮出属正确语义);MCP 侧恒 orphanOnly=true
// 仅允许删孤儿。--preview 干跑只回显将删行不落盘。
// 退出码经 exitCodeForFail(定义在同包 update_entry.go)按 Fail.Code 分流。
package cli

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/spf13/cobra"
)

func init() {
	var path string
	var preview bool

	cmd := &cobra.Command{
		Use:   "remove-entry",
		Short: cliMessage("cli.short.remove_entry"),
		Long:  removeEntryLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			if path == "" {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("remove.path_required")}
			}

			out, fail := mcptools.ApplyRemoveEntry(root, path, "human", false, preview)
			if fail != nil {
				msg := "[" + fail.Code + "] " + fail.Msg
				if fail.Hint != "" {
					msg += "\n" + cliMessage("error.hint_prefix") + fail.Hint
				}
				return &ExitError{Code: exitCodeForFail(fail.Code), Msg: msg}
			}
			if !flagQuiet {
				fmt.Print(mcptools.RenderRemoveOutcome(out))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", cliMessage("cli.flag.entry_path"))
	cmd.Flags().BoolVar(&preview, "preview", false, cliMessage("cli.flag.remove_preview"))
	registerCommand(cmd)
}
