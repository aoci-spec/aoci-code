// aoci mcp —— 进入 MCP stdio server 模式
// 索引条目: mcp.go[CLI.Serve.9.Stdio.T]
//
// 纪律: 本命令不得向 stdout 打印横幅/版本 —— stdout 已是 JSON-RPC 协议流;
//
//	SIGINT/SIGTERM 优雅退出;启动不自动 scan(防慢启动)。
package cli

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: cliMessage("cli.short.mcp"),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			// 信号优雅退出: SIGINT/SIGTERM 取消 ctx,server.Run 随之返回
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if err := mcptools.RunStdio(ctx, root, version); err != nil && ctx.Err() == nil {
				return err // 非信号退出的错误按内部错误处理
			}
			return nil
		},
	}
	registerCommand(cmd)
}
