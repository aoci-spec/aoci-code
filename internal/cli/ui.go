package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

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
//
// --detach starts the page as its own session and returns once it reports
// its URL, which is what an agent needs: its tool shell ends when the
// command does, and a page that died with that shell would leave the user a
// dead link. --stop ends the page registered for the repository.
func newUICmd() *cobra.Command {
	var (
		port     int
		open     bool
		detach   bool
		stop     bool
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
			if stop || detach {
				if len(roots) == 0 {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("ui.error.no_repository")}
				}
				registry, err := ui.RegistryDir("")
				if err != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("ui.error.registry", err.Error())}
				}
				if stop {
					return stopUI(cmd, registry, roots[0])
				}
				return detachUI(cmd, registry, roots[0], port, host, discover, also, open)
			}
			options := ui.Options{
				Roots: roots, Discover: discover, Host: host, Port: port,
				Locale: textassets.ActiveLocale(), BinaryVersion: version,
				Guide: func(root string, cfg *config.Config, set *cognition.Set) (any, error) {
					return buildVolumeAgentGuide(root, cfg, set, "ui")
				},
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			// Readiness goes straight to the process stdout: the command's own
			// writer stays buffered until the command exits, and this command
			// exits only when the page stops. A detached parent and a person
			// watching the terminal both need the URL now.
			ready := func(url string, repositories int) {
				if flagJSON {
					fmt.Fprintf(os.Stdout, "{\"url\":%q,\"repositories\":%d}\n", url, repositories)
				} else {
					fmt.Fprintln(os.Stdout, cliMessage("ui.listening", url, repositories))
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
	command.Flags().BoolVar(&detach, "detach", false, cliMessage("cli.flag.ui_detach"))
	command.Flags().BoolVar(&stop, "stop", false, cliMessage("cli.flag.ui_stop"))
	command.Flags().StringArrayVar(&also, "also", nil, cliMessage("cli.flag.ui_also"))
	command.Flags().BoolVar(&discover, "discover", true, cliMessage("cli.flag.ui_discover"))
	return command
}

func stopUI(cmd *cobra.Command, registry, root string) error {
	reg, live, err := ui.Stop(registry, root, 5*time.Second)
	if err != nil {
		return &ExitError{Code: ExitConfig, Msg: err.Error()}
	}
	switch {
	case flagJSON:
		fmt.Fprintf(cmd.OutOrStdout(), "{\"stopped\":%t,\"pid\":%d}\n", live, reg.PID)
	case live:
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("ui.stopped", reg.PID))
	default:
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("ui.none_running"))
	}
	return nil
}

// detachUI reuses a page that is already answering for this repository —
// two agents asking for a panel should share one, not spawn one each — and
// otherwise starts the same command as a detached child.
func detachUI(cmd *cobra.Command, registry, root string, port int, host string, discover bool, also []string, open bool) error {
	if reg, ok := ui.Registered(registry, root); ok && ui.Live(reg, time.Second) {
		reportDetached(cmd, reg.URL, reg.PID, -1, true)
		if open {
			openBrowser(reg.URL)
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return &ExitError{Code: ExitConfig, Msg: cliMessage("ui.error.detach_failed", err.Error())}
	}
	args := []string{"ui", "--repo", root, "--port", strconv.Itoa(port), "--host", host, "--discover=" + strconv.FormatBool(discover), "--json"}
	for _, extra := range also {
		args = append(args, "--also", extra)
	}
	logPath := ui.LogPath(registry, root)
	result, err := ui.Detach(executable, args, logPath, 20*time.Second)
	if err != nil {
		detail := err.Error()
		if tail := logTail(logPath, 400); tail != "" {
			detail += ": " + tail
		}
		return &ExitError{Code: ExitConfig, Msg: cliMessage("ui.error.detach_failed", detail)}
	}
	reportDetached(cmd, result.URL, result.PID, result.Repositories, false)
	if open {
		openBrowser(result.URL)
	}
	return nil
}

func reportDetached(cmd *cobra.Command, url string, pid, repositories int, reused bool) {
	out := cmd.OutOrStdout()
	switch {
	case flagJSON && repositories >= 0:
		fmt.Fprintf(out, "{\"url\":%q,\"repositories\":%d,\"pid\":%d,\"detached\":true,\"reused\":%t}\n", url, repositories, pid, reused)
	case flagJSON:
		fmt.Fprintf(out, "{\"url\":%q,\"pid\":%d,\"detached\":true,\"reused\":%t}\n", url, pid, reused)
	case reused:
		fmt.Fprintln(out, cliMessage("ui.reused", url, pid))
	default:
		fmt.Fprintln(out, cliMessage("ui.detached", url, pid))
	}
}

// logTail returns the end of a detached page's log so a start failure names
// its cause instead of only the fact.
func logTail(path string, limit int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	return strings.TrimSpace(string(data))
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
