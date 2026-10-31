// Tests of the R65-04B final CLI Long Help wiring and the removal of duplicate sources of truth.
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func TestCLIHelpAssetGuardFailsBeforeOutputAndStaysLocal(
	t *testing.T,
) {
	root := &cobra.Command{
		Use:  "aoci",
		Long: "正常根帮助",
	}
	broken := &cobra.Command{
		Use:   "broken",
		Short: "损坏帮助测试",
		Long: fmt.Sprintf(
			"%s%q加载失败: %v]",
			cliHelpAssetFailurePrefix,
			textassets.ContractHelpRootLong,
			errors.New("缺少协议词"),
		),
	}
	root.AddCommand(broken)
	installCLIHelpAssetGuard(root)

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("无关根帮助不应被兄弟命令资源阻断: %v", err)
	}
	if !strings.Contains(stdout.String(), "正常根帮助") {
		t.Fatalf("根帮助未正常渲染: %q", stdout.String())
	}

	stdout.Reset()
	root.SetArgs([]string{"broken", "--help"})
	_ = root.Execute()
	err := cliHelpExecutionError(root)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitInternal {
		t.Fatalf("损坏帮助未返回内部错误: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), string(textassets.ContractHelpRootLong)) {
		t.Fatalf("帮助错误未指出资源来源: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("资源错误后仍产生部分帮助输出: %q", stdout.String())
	}
}

func TestFinalCLIHelpFunctionsUseTextAssets(
	t *testing.T,
) {
	tests := []struct {
		name        string
		id          textassets.ID
		got         func() string
		observation bool
	}{
		{
			name:        "index score",
			id:          textassets.ContractHelpIndexScoreLong,
			got:         indexScoreLongHelp,
			observation: true,
		},
		{
			name: "index agent",
			id:   textassets.ContractHelpIndexAgentLong,
			got:  indexAgentLongHelp,
		},
		{
			name: "update-entry",
			id:   textassets.ContractHelpUpdateEntryLong,
			got:  updateEntryLongHelp,
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				expected := textassets.MustRender(
					textassets.LegacyLocale,
					current.id,
					nil,
				)
				if current.observation {
					expected += "\n" + textassets.MustRender(
						textassets.LegacyLocale,
						textassets.ContractHelpReadObservationAudit,
						nil,
					)
				}

				if actual := current.got(); actual != expected {
					t.Fatalf(
						"CLI Help消费者未返回资产原文: want=%q got=%q",
						expected,
						actual,
					)
				}
			},
		)
	}
}

func TestFinalCLICommandsWireStableLongHelp(
	t *testing.T,
) {
	tests := []struct {
		name     string
		command  *cobra.Command
		expected string
	}{
		{
			name:     "index score",
			command:  newIndexScoreCmd(),
			expected: indexScoreLongHelp(),
		},
		{
			name:     "index inventory",
			command:  newIndexInventoryCmd(),
			expected: indexInventoryLongHelp(),
		},
		{
			name:     "index agent",
			command:  newIndexAgentCmd(),
			expected: indexAgentLongHelp(),
		},
		{
			name:     "update-entry",
			command:  newUpdateEntryCmd(),
			expected: updateEntryLongHelp(),
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				if current.command == nil {
					t.Fatal(
						"命令构造结果为空",
					)
				}

				if current.command.Long != current.expected {
					t.Fatalf(
						"命令Long未接入稳定资产: want=%q got=%q",
						current.expected,
						current.command.Long,
					)
				}
			},
		)
	}
}

func TestDecoratedHostAgentCommandsKeepNoLocalLongCopy(
	t *testing.T,
) {
	tests := []struct {
		name    string
		command *cobra.Command
	}{
		{
			name:    "guide",
			command: newIndexAgentGuideCmd(),
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				if current.command == nil {
					t.Fatal(
						"命令构造结果为空",
					)
				}

				if current.command.Long != "" {
					t.Fatalf(
						"运行时装饰命令仍保留本地Long副本: %q",
						current.command.Long,
					)
				}
			},
		)
	}
}
