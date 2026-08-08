// `aoci index`命令组挂载、Inventory和共用索引读取原语。
//
// Build实现位于index_build.go。
// AI客户端与超时辅助位于index_ai_helpers.go。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

func init() {
	indexCommand := &cobra.Command{
		Use:   "index",
		Short: cliMessage("cli.short.index"),
		Long:  indexLongHelp(),
	}

	indexCommand.AddCommand(newIndexInventoryCmd())
	indexCommand.AddCommand(newIndexHeaderCmd())
	indexCommand.AddCommand(newIndexBuildCmd())
	indexCommand.AddCommand(newIndexUpdateCmd())
	indexCommand.AddCommand(newIndexEntriesCmd())
	indexCommand.AddCommand(newIndexScoreCmd())
	indexCommand.AddCommand(newIndexAgentCmd())

	registerCommand(indexCommand)
}

func loadIndexForCLI(
	cmd *cobra.Command,
	repoRoot string,
	cfg *config.Config,
) (*index.Document, string, error) {
	indexPath := filepath.Join(
		repoRoot,
		cfg.IndexPath,
	)

	set, err := cognition.Load(repoRoot, cfg.IndexPath)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return nil, "", errors.New(cliMessage("inventory.index_read_error", err))
		}
		return nil, "", fmt.Errorf("%s", cliMessage("mcp.error.cognition_invalid", localeSafeCLIDetail(err.Error())))
	}
	if set.LayoutMode == cognition.LayoutVolumesV1 {
		return nil, "", fmt.Errorf("%s", cliMessage("mcp.error.volume_read_only"))
	}
	if len(set.Warnings) > 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), cliMessage("inventory.parse_warnings", len(set.Warnings)))
	}
	return set.Root.Document, indexPath, nil
}

func newIndexInventoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inventory",
		Short: indexInventoryShortHelp(),
		Long:  indexInventoryLongHelp(),
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

			start := time.Now()
			inventory, err :=
				indexgen.BuildInventory(
					repoRoot,
					cfg,
					doc,
				)
			if err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}

			ledger.Append(
				repoRoot,
				cfg.LedgerEnabled,
				ledger.Event{
					Op:         "index_inventory",
					Source:     ledger.SourceHuman,
					PathsCount: len(inventory.Items),
					DurationMs: time.Since(start).Milliseconds(),
				},
			)

			out := cmd.OutOrStdout()
			if flagJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(inventory)
			}

			fmt.Fprintf(
				out,
				"AOCI inventory @ %s\n",
				time.Now().
					UTC().
					Format(time.RFC3339),
			)
			fmt.Fprintln(out, cliMessage(
				"inventory.summary",
				inventory.DiskTotal,
				inventory.IndexedTotal,
				len(inventory.Items),
			))

			if len(inventory.Items) == 0 {
				fmt.Fprintln(
					out,
					cliMessage("inventory.clean"),
				)
				return nil
			}

			fmt.Fprintln(
				out,
				"──────────────────────────────",
			)
			for _, item := range inventory.Items {
				mark := " "
				if item.SkipReason != "" {
					mark = "!"
				}

				fmt.Fprint(out, cliMessage(
					"inventory.row",
					mark,
					item.RelPath,
					item.SizeBytes,
					item.Lines,
					item.SuggestedSection,
				))

				if item.SkipReason != "" {
					fmt.Fprint(out, cliMessage("inventory.skip", item.SkipReason))
				}
				fmt.Fprintln(out)
			}

			fmt.Fprintln(
				out,
				"──────────────────────────────",
			)
			fmt.Fprintln(
				out,
				cliMessage("inventory.note"),
			)

			return nil
		},
	}
}
