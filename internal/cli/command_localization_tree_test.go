package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/testkit"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Locks: every visible command in the tree has a localized short description.
// The mapping from command path to message key is hand-kept, so a command
// registered without an entry rendered its package-init English Short under
// every locale; `aoci scope budget` listed its subcommands in English on a
// zh-CN repository that way.
func TestEveryVisibleCommandShortIsLocalized(t *testing.T) {
	// Registered commands are package-level singletons shared by every root,
	// so the English baseline is collected first and the same objects are then
	// relocalized under zh-CN.
	testkit.PinLocale(t, "en-US")
	english := map[string]string{}
	var collect func(*cobra.Command)
	collect = func(command *cobra.Command) {
		english[command.CommandPath()] = command.Short
		for _, child := range command.Commands() {
			collect(child)
		}
	}
	collect(newRootCmdLocalized())
	testkit.PinLocale(t, "zh-CN")
	root := newRootCmdLocalized()
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		path := command.CommandPath()
		if !command.Hidden && path != "aoci" {
			_, mapped := commandShortMessages[path]
			_, rendered := commandShortRenderers[path]
			if !mapped && !rendered {
				t.Errorf("%q has no localized short description entry", path)
			} else if command.Short == english[path] || command.Short == "" {
				t.Errorf("%q short description is not localized under zh-CN: %q", path, command.Short)
			}
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func newRootCmdLocalized() *cobra.Command {
	root := newRootCmd()
	refreshCommandLocalization(root)
	return root
}

// Locks: no flag usage stays in English under zh-CN when the catalog has its
// zh-CN rendering; a hand-kept flag-name list left thirteen flags English
// (audit G5-17).
func TestEveryFlagUsageWithACatalogEntryIsLocalized(t *testing.T) {
	testkit.PinLocale(t, "zh-CN")
	root := newRootCmdLocalized()
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		check := func(flag *pflag.Flag) {
			localized, found, err := textassets.RelocalizeMessageExact("zh-CN", flag.Usage)
			if err == nil && found && localized != flag.Usage {
				t.Errorf("%s --%s usage is still English under zh-CN: %q", command.CommandPath(), flag.Name, flag.Usage)
			}
		}
		command.LocalNonPersistentFlags().VisitAll(check)
		command.PersistentFlags().VisitAll(check)
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
}
