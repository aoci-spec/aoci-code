package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

var (
	helpSubcommandLine = regexp.MustCompile(`^([a-z][a-z-]*(?: [a-z][a-z-]*)?):\s`)
	helpFlagToken      = regexp.MustCompile(`--[a-z][a-z-]*`)
)

// TestHelpTextsNameOnlyWhatIsRegistered keeps the help texts honest about the
// command tree they describe, in every locale. The index and index agent help
// list their subcommands as "name:" lines, and each must resolve to a
// registered command; every --flag a help text names must be one its command
// has. The goldens alone kept the retired Legacy commands and doctor --net
// advertised on a binary that no longer had them (#152).
func TestHelpTextsNameOnlyWhatIsRegistered(t *testing.T) {
	root := newRootCmd()
	t.Cleanup(func() {
		children := append([]*cobra.Command{}, root.Commands()...)
		for _, child := range children {
			root.RemoveCommand(child)
		}
	})
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		for _, test := range []struct {
			id     textassets.ID
			parent []string
		}{
			{textassets.ContractHelpIndexLong, []string{"index"}},
			{textassets.ContractHelpIndexAgentLong, []string{"index", "agent"}},
		} {
			text, err := textassets.RenderScalar(locale, test.id, nil)
			if err != nil {
				t.Fatal(err)
			}
			named := 0
			for _, line := range strings.Split(text, "\n") {
				match := helpSubcommandLine.FindStringSubmatch(line)
				if match == nil {
					continue
				}
				named++
				path := append(append([]string{}, test.parent...), strings.Fields(match[1])...)
				if findCLICommand(root, path...) == nil {
					t.Fatalf("%s %s names %q, which is not a registered command", locale, test.id, strings.Join(path, " "))
				}
			}
			if named == 0 {
				t.Fatalf("%s %s lists no subcommands; the check would pass vacuously", locale, test.id)
			}
		}
		for _, test := range []struct {
			id   textassets.ID
			path []string
		}{
			{textassets.ContractHelpRootLong, nil},
			{textassets.ContractHelpDoctorLong, []string{"doctor"}},
			{textassets.ContractHelpIndexLong, []string{"index"}},
			{textassets.ContractHelpIndexAgentLong, []string{"index", "agent"}},
		} {
			text, err := textassets.RenderScalar(locale, test.id, nil)
			if err != nil {
				t.Fatal(err)
			}
			command := root
			if len(test.path) > 0 {
				command = findCLICommand(root, test.path...)
			}
			if command == nil {
				t.Fatalf("%v is not registered", test.path)
			}
			for _, flag := range helpFlagToken.FindAllString(text, -1) {
				name := strings.TrimPrefix(flag, "--")
				if command.Flags().Lookup(name) == nil && command.InheritedFlags().Lookup(name) == nil &&
					root.PersistentFlags().Lookup(name) == nil {
					t.Fatalf("%s %s names %s, which %q does not have", locale, test.id, flag, command.CommandPath())
				}
			}
		}
	}
}
