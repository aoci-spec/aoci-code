package cli

import (
	"fmt"
	"time"

	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/spf13/cobra"
)

func init() {
	registerCommand(newSourceCmd())
}

func newSourceCmd() *cobra.Command {
	command := &cobra.Command{Use: "source", Short: cliMessage("cli.short.source")}
	command.AddCommand(newBusinessSourceManifestCmd())
	return command
}

func newBusinessSourceManifestCmd() *cobra.Command {
	var generatedAt string
	command := &cobra.Command{Use: "manifest", Short: cliMessage("cli.short.source_manifest"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return &ExitError{Code: ExitConfig, MachineCode: "business_source_manifest_invalid", Msg: cliMessage("business.source.error", err.Error())}
		}
		if generatedAt == "" {
			generatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		}
		manifest, err := businesssource.Build(root, generatedAt)
		if err != nil {
			return &ExitError{Code: ExitInvalid, MachineCode: "business_source_manifest_invalid", Msg: cliMessage("business.source.error", err.Error())}
		}
		if flagJSON {
			return writePlannerJSON(cmd, manifest)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("business.source.summary", len(manifest.Files), manifest.AggregateSHA256, manifest.NetworkAccessed))
		return nil
	}}
	command.Flags().StringVar(&generatedAt, "generated-at", "", cliMessage("business.source.flag.generated_at"))
	return command
}
