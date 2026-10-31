package cli

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/testkit"
	"github.com/spf13/cobra"
)

// newGroupedRootForTest builds a root and detaches the shared registered
// commands again on cleanup, as the other help tests do, since registered
// commands are package-level singletons that every root reparents.
func newGroupedRootForTest(t *testing.T) *cobra.Command {
	t.Helper()
	root := newRootCmdLocalized()
	t.Cleanup(func() {
		children := append([]*cobra.Command{}, root.Commands()...)
		for _, child := range children {
			root.RemoveCommand(child)
		}
	})
	return root
}

func helpOutput(t *testing.T, root *cobra.Command, path ...string) string {
	t.Helper()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append(append([]string{}, path...), "--help"))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// helpSection returns the command names listed under title, in order.
func helpSection(help, title string) []string {
	names := []string{}
	lines := strings.Split(help, "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) != title {
			continue
		}
		for _, item := range lines[index+1:] {
			if strings.TrimSpace(item) == "" {
				break
			}
			names = append(names, strings.Fields(item)[0])
		}
		return names
	}
	return nil
}

// Locks: `aoci --help` lists the five everyday commands first, under their
// own title, and everything else under Additional Commands; a command
// cannot be in both, and the everyday set is exactly the one the
// getting-started page names.
func TestRootHelpListsTheEverydayCommandsFirst(t *testing.T) {
	testkit.PinLocale(t, "en-US")
	root := newGroupedRootForTest(t)
	help := helpOutput(t, root)
	everyday := helpSection(help, "Everyday Commands:")
	sort.Strings(everyday)
	if strings.Join(everyday, " ") != "check init mcp scan verify" {
		t.Fatalf("everyday commands = %v\n%s", everyday, help)
	}
	additional := helpSection(help, "Additional Commands:")
	if len(additional) == 0 {
		t.Fatalf("the other commands stay listed under Additional Commands\n%s", help)
	}
	for _, name := range everyday {
		for _, other := range additional {
			if name == other {
				t.Fatalf("%s is listed twice", name)
			}
		}
	}
	if strings.Index(help, "Everyday Commands:") > strings.Index(help, "Additional Commands:") {
		t.Fatalf("the everyday group comes first\n%s", help)
	}
	if strings.Contains(help, "Available Commands:") {
		t.Fatalf("a grouped help has no ungrouped list\n%s", help)
	}
}

// Locks: `aoci scope --help` sorts every subcommand into Policy, Inspect, or
// Transactions, with no Additional Commands left over, and the three groups
// together are exactly the registered scope subcommands.
func TestScopeHelpGroupsEverySubcommand(t *testing.T) {
	testkit.PinLocale(t, "en-US")
	root := newGroupedRootForTest(t)
	scope := findCLICommand(root, "scope")
	if scope == nil || !scope.AllChildCommandsHaveGroup() {
		t.Fatal("every scope subcommand carries a group")
	}
	help := helpOutput(t, root, "scope")
	grouped := []string{}
	for _, title := range []string{"Policy:", "Inspect:", "Transactions:"} {
		names := helpSection(help, title)
		if len(names) == 0 {
			t.Fatalf("%s lists nothing\n%s", title, help)
		}
		grouped = append(grouped, names...)
	}
	registered := []string{}
	for _, child := range scope.Commands() {
		if child.IsAvailableCommand() {
			registered = append(registered, child.Name())
		}
	}
	sort.Strings(grouped)
	sort.Strings(registered)
	if strings.Join(grouped, " ") != strings.Join(registered, " ") {
		t.Fatalf("grouped = %v, registered = %v", grouped, registered)
	}
	if strings.Contains(help, "Additional Commands:") || strings.Contains(help, "Available Commands:") {
		t.Fatalf("no scope subcommand is left ungrouped\n%s", help)
	}
}

// Locks: the group titles are locale text assets, rendered under the active
// locale at construction and again by refreshCommandLocalization.
func TestCommandGroupTitlesFollowTheLocale(t *testing.T) {
	testkit.PinLocale(t, "zh-CN")
	root := newGroupedRootForTest(t)
	if help := helpOutput(t, root); !strings.Contains(help, "日常命令:") || !strings.Contains(help, "其他命令:") {
		t.Fatalf("zh-CN root help is not localized\n%s", help)
	}
	if help := helpOutput(t, root, "scope"); !strings.Contains(help, "策略:") || !strings.Contains(help, "查看:") || !strings.Contains(help, "事务:") {
		t.Fatalf("zh-CN scope help is not localized\n%s", help)
	}
	testkit.PinLocale(t, "en-US")
	refreshCommandLocalization(root)
	if help := helpOutput(t, root); !strings.Contains(help, "Everyday Commands:") {
		t.Fatalf("a locale refresh re-renders the group titles\n%s", help)
	}
	for _, groupID := range []string{commandGroupEveryday, commandGroupScopePolicy, commandGroupScopeInspect, commandGroupScopeTransaction} {
		if _, exists := commandGroupTitles[groupID]; !exists {
			t.Fatalf("group %s has no title asset", groupID)
		}
	}
}
