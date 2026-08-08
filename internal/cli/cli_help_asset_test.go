// CLI稳定Long Help文本资产迁移的真实命令树字节级测试。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func readCLIHelpGolden(
	t *testing.T,
	name string,
) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(
			"..",
			"..",
			"testdata",
			"golden",
			name,
		),
	)
	if err != nil {
		t.Fatalf(
			"读取CLI Help Golden %s失败: %v",
			name,
			err,
		)
	}

	return string(data)
}

func assertCLIHelpGolden(
	t *testing.T,
	command *cobra.Command,
	golden string,
) {
	t.Helper()

	if command == nil {
		t.Fatalf(
			"CLI Help目标命令不存在: %s",
			golden,
		)
	}

	expected := readCLIHelpGolden(
		t,
		golden,
	)

	if command.Long+"\n" != expected {
		t.Fatalf(
			"CLI Long Help与Golden不一致 %s:\nactual=%q\nexpected=%q",
			golden,
			command.Long+"\n",
			expected,
		)
	}
}

func TestCLIHelpAssetsMatchRealCommandTree(
	t *testing.T,
) {
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(textassets.LegacyLocale) })
	root := newRootCmd()

	t.Cleanup(func() {
		children := append(
			[]*cobra.Command{},
			root.Commands()...,
		)

		for _, child := range children {
			root.RemoveCommand(
				child,
			)
		}
	})

	assertCLIHelpGolden(
		t,
		root,
		"cli_help_root.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"doctor",
		),
		"cli_help_doctor.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"index",
		),
		"cli_help_index.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"index",
			"inventory",
		),
		"cli_help_index_inventory.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"index",
			"score",
		),
		"cli_help_index_score.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"index",
			"update",
		),
		"cli_help_index_update.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"verify",
		),
		"cli_help_verify.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"check",
		),
		"cli_help_check.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"scan",
		),
		"cli_help_scan.txt",
	)

	assertCLIHelpGolden(
		t,
		findCLICommand(
			root,
			"remove-entry",
		),
		"cli_help_remove_entry.txt",
	)
}
