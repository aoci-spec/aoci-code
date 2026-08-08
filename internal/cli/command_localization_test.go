package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func TestLocalizedCommandMetadataPathsResolve(t *testing.T) {
	root := newRootCmd()
	available := map[string]struct{}{}
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		available[command.CommandPath()] = struct{}{}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
	for _, registry := range []map[string]string{commandShortMessages} {
		for path := range registry {
			if _, exists := available[path]; !exists {
				t.Errorf("localized command path does not exist: %s", path)
			}
		}
	}
	for path := range commandLongMessages {
		if _, exists := available[path]; !exists {
			t.Errorf("localized long-help path does not exist: %s", path)
		}
	}
	for path := range commandShortRenderers {
		if _, exists := available[path]; !exists {
			t.Errorf("localized short-renderer path does not exist: %s", path)
		}
	}
}

func TestCommandMetadataRebindsToEachProjectLocale(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	var roots []*cobra.Command
	t.Cleanup(func() {
		for index := len(roots) - 1; index >= 0; index-- {
			roots[index].RemoveCommand(subCommands...)
		}
	})

	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	englishRoot := newRootCmd()
	roots = append(roots, englishRoot)
	var inspectEnglish func(*cobra.Command)
	inspectEnglish = func(command *cobra.Command) {
		for _, value := range []string{command.Short, command.Long, command.Example} {
			if hanTextPattern.MatchString(value) {
				t.Fatalf("English command %s contains Han text: %q", command.CommandPath(), value)
			}
		}
		for _, usages := range []string{
			command.LocalNonPersistentFlags().FlagUsages(),
			command.PersistentFlags().FlagUsages(),
		} {
			if hanTextPattern.MatchString(usages) {
				t.Fatalf("English flags for %s contain Han text: %q", command.CommandPath(), usages)
			}
		}
		for _, child := range command.Commands() {
			inspectEnglish(child)
		}
	}
	inspectEnglish(englishRoot)

	if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
		t.Fatal(err)
	}
	chineseRoot := newRootCmd()
	roots = append(roots, chineseRoot)
	if !strings.Contains(chineseRoot.Short, "认知资产") {
		t.Fatalf("Chinese root Short was not rebound: %q", chineseRoot.Short)
	}
	configCommand := findCLICommand(chineseRoot, "config")
	if configCommand == nil || !strings.Contains(configCommand.Short, "读写") {
		t.Fatalf("Chinese config Short was not rebound: %#v", configCommand)
	}
	pretoolCommand := findCLICommand(chineseRoot, "hook", "pretool")
	if pretoolCommand == nil || !strings.Contains(pretoolCommand.Short, "写前") {
		t.Fatalf("Chinese hidden pretool Short was not rebound: %#v", pretoolCommand)
	}

	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	reboundEnglish := newRootCmd()
	roots = append(roots, reboundEnglish)
	configCommand = findCLICommand(reboundEnglish, "config")
	if configCommand == nil || hanTextPattern.MatchString(configCommand.Short) {
		t.Fatalf("English rebound retained Chinese text: %#v", configCommand)
	}
}
