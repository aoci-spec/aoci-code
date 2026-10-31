// aoci mcp: enter the MCP stdio server mode
// Index entry: mcp.go[CLI.Serve.9.Stdio.T]
//
// Discipline: this command must not print a banner or version to stdout, which is already the JSON-RPC protocol stream;
//
//	SIGINT/SIGTERM exit gracefully; startup does not scan automatically (against a slow start).
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
			// Graceful exit on signal: SIGINT/SIGTERM cancel ctx, and server.Run returns with it
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if err := mcptools.RunStdio(ctx, root, version); err != nil && ctx.Err() == nil {
				return err // an error from a non-signal exit is handled as an internal error
			}
			return nil
		},
	}
	registerCommand(cmd)
}
