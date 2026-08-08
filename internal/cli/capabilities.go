package cli

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/capability"
	"github.com/spf13/cobra"
)

func init() {
	registerCommand(newCapabilitiesCmd())
}

func newCapabilitiesCmd() *cobra.Command {
	return &cobra.Command{Use: "capabilities", Short: cliMessage("cli.short.capabilities"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return &ExitError{Code: ExitConfig, MachineCode: "capability_manifest_invalid", Msg: cliMessage("capabilities.error", err.Error())}
		}
		manifest, err := capability.Build(root, version, commit)
		if err != nil {
			return &ExitError{Code: ExitInvalid, MachineCode: "capability_manifest_invalid", Msg: cliMessage("capabilities.error", err.Error())}
		}
		if flagJSON {
			return writePlannerJSON(cmd, manifest)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("capabilities.summary", manifest.Version, manifest.AOCIVersion, manifest.CurrentLayout, manifest.MCPToolCount))
		return nil
	}}
}
