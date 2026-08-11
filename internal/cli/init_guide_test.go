// init完整索引Guide发现与automation.mode提示测试。
package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestInitAgentGuideCommand(
	t *testing.T,
) {
	tests := []struct {
		agent string
		want  string
	}{
		{
			agent: "codex",
			want: "aoci index agent guide --agent " +
				"codex --json",
		},
		{
			agent: "claude",
			want: "aoci index agent guide --agent " +
				"claude --json",
		},
		{
			agent: "cursor",
			want: "aoci index agent guide --agent " +
				"cursor --json",
		},
		{
			agent: "opencode",
			want: "aoci index agent guide --agent " +
				"opencode --json",
		},
		{
			agent: "",
			want: "aoci index agent guide --agent " +
				"<codex|claude|cursor|opencode> --json",
		},
		{
			agent: "all",
			want: "aoci index agent guide --agent " +
				"<codex|claude|cursor|opencode> --json",
		},
	}

	for _, current := range tests {
		t.Run(
			current.agent,
			func(t *testing.T) {
				if got := initAgentGuideCommand(
					current.agent,
				); got != current.want {
					t.Fatalf(
						"Guide命令不符: got=%q want=%q",
						got,
						current.want,
					)
				}
			},
		)
	}
}

func TestInitAutomationGuideHint(
	t *testing.T,
) {
	tests := []struct {
		mode  string
		wants []string
	}{
		{
			mode: config.AutomationModeAuto,
			wants: []string{
				"严格按当前 Guide 阶段连续执行",
				"Entries Stage 内部完成 Check、Diff、P-23 与原子 Apply",
				"applied 后执行 Verify 并重新 Guide",
				"repair_required 时只修失败条目并自动重新 Stage",
				"不要求用户继续",
				"只有审批/外部动作边界、不可证明Recovery、第三方冲突或其他真实安全故障才停止用户任务",
			},
		},
		{
			mode: config.AutomationModeReview,
			wants: []string{
				"Apply 前等待批准",
			},
		},
		{
			mode: config.AutomationModeLegacy,
			wants: []string{
				"旧仓兼容态",
			},
		},
		{
			mode: config.AutomationModeOff,
			wants: []string{
				"不生成候选、不 Stage、不 Apply",
			},
		},
	}

	for _, current := range tests {
		hint, err := agentAutomationInitHint(
			current.mode,
		)
		if err != nil {
			t.Fatal(err)
		}

		for _, want := range current.wants {
			if !strings.Contains(
				hint,
				want,
			) {
				t.Fatalf(
					"模式提示不符: mode=%s hint=%q want=%q",
					current.mode,
					hint,
					want,
				)
			}
		}
	}
}

func TestInitOutputPointsToConcreteCodexGuide(
	t *testing.T,
) {
	root := t.TempDir()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = reader.Close()
		_ = writer.Close()
	})

	exitCode := executeCLI([]string{
		"--repo=" + root,
		"init",
		"--agent=codex",
		"--hooks=false",
		"--here=false",
	}, io.Discard, io.Discard)

	_ = writer.Close()
	os.Stdout = oldStdout

	output, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if exitCode != ExitOK {
		t.Fatalf(
			"init --agent=codex失败: exit=%d\n%s",
			exitCode,
			output,
		)
	}

	text := string(output)

	for _, anchor := range []string{
		"aoci index agent guide --agent codex --json",
		"automation.mode=auto",
		"auto follows the current Guide stage continuously and exactly",
		"Entries Stage completes Check, Diff, P-23, and atomic Apply internally",
		"After applied, run Verify and request Guide again",
		"On repair_required, fix only the failed Entries",
		"without asking the user to continue",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"新仓init输出缺少Auto合同%q:\n%s",
				anchor,
				text,
			)
		}
	}

	if strings.Contains(
		text,
		"连续执行 Stage、Check、Diff、Apply 与 Verify",
	) {
		t.Fatalf(
			"新仓init输出仍含旧机械链合同:\n%s",
			text,
		)
	}
}
