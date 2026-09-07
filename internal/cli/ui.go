package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ui"
	"github.com/aoci-spec/aoci-code/textassets"
)

// newUICmd serves the local read-only status page. It is a separate process
// from `aoci mcp` on purpose: the MCP server keeps stdout for JSON-RPC and
// opens no socket, so the page can never be mistaken for a transport. The
// page reads the same governance facts Verify, Check, Guide, and Maintain
// consume, takes no lock, and appends nothing to the Ledger.
func newUICmd() *cobra.Command {
	var (
		port     int
		open     bool
		also     []string
		discover bool
		host     string
	)
	command := &cobra.Command{
		Use:   "ui",
		Short: cliMessage("cli.short.ui"),
		Long:  cliMessage("cli.long.ui"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			roots := []string{}
			if root, err := config.FindRepoRoot(".", flagRepo); err == nil {
				roots = append(roots, root)
			} else if flagRepo != "" {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			roots = append(roots, also...)
			options := ui.Options{
				Roots: roots, Discover: discover, Host: host, Port: port,
				Locale: textassets.ActiveLocale(), BinaryVersion: version,
				Guide: func(root string, cfg *config.Config, set *cognition.Set) (any, error) {
					return buildVolumeAgentGuide(root, cfg, set, "ui")
				},
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ready := func(url string, repositories int) {
				if flagJSON {
					fmt.Fprintf(cmd.OutOrStdout(), "{\"url\":%q,\"repositories\":%d}\n", url, repositories)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), cliMessage("ui.listening", url, repositories))
				}
				if open {
					openBrowser(url)
				}
			}
			if err := ui.Serve(ctx, options, ready); err != nil {
				return &ExitError{Code: ExitConfig, Msg: localizeUIError(err)}
			}
			return nil
		},
	}
	command.Flags().IntVar(&port, "port", 0, cliMessage("cli.flag.ui_port"))
	command.Flags().StringVar(&host, "host", "127.0.0.1", cliMessage("cli.flag.ui_host"))
	command.Flags().BoolVar(&open, "open", false, cliMessage("cli.flag.ui_open"))
	command.Flags().StringArrayVar(&also, "also", nil, cliMessage("cli.flag.ui_also"))
	command.Flags().BoolVar(&discover, "discover", true, cliMessage("cli.flag.ui_discover"))
	return command
}

func localizeUIError(err error) string {
	switch {
	case err == nil:
		return ""
	case ui.IsNotLoopback(err):
		return cliMessage("ui.error.loopback_only", ui.ErrorDetail(err))
	case ui.IsNoRepository(err):
		return cliMessage("ui.error.no_repository")
	}
	return err.Error()
}

// openBrowser is best effort; a failure leaves the printed URL as the answer.
func openBrowser(url string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	_ = command.Start()
}
