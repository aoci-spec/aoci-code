// aoci remove-entry: the human single-entry removal command (v2.8 P1, the human execution tool of the second half of D75)
// Index entry: remove_entry.go (to be recorded, entering the index with this batch)
//
// It shares the mcptools.ApplyRemoveEntry pipeline with the MCP aoci_remove_entry
// (locate → remove → atomic write → the index's own baseline advance → ledger); the only difference is the guardrail:
// the CLI passes orphanOnly=false, since removal is the human curator's full authority and a live file's entry may be removed too
// (the file then surfaces as Missing, which is the correct semantics); the MCP side is always orphanOnly=true
// and may only remove orphans. --preview dry-runs, echoing the line to be removed without writing.
// The exit code routes by Fail.Code through exitCodeForFail (defined in update_entry.go of the same package).
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
