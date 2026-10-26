// init完整索引Guide发现与automation.mode提示测试。
package cli

import (
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
				"每个 aoci_update_entry 批次在内部完成校验、Diff、P-23 与原子 Apply",
				"任何写入批次applied后都按当前Guide返回的后续命令继续",
				"remaining非零时继续Maintain",
				"最终批次remaining=0后依次执行Verify、Aggregate Check和Guide",
				"repair_required 时只修失败条目并重提同一完整批次",
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
				"旧仓兼容授权",
			},
		},
		{
			mode: config.AutomationModeOff,
			wants: []string{
				"不发候选、不进入 Apply",
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

	output, _, _, exitCode := runCLIPrinted(t, root, "init", "--agent=codex", "--hooks=false", "--here=false")
	if exitCode != ExitOK {
		t.Fatalf(
			"init --agent=codex失败: exit=%d\n%s",
			exitCode,
			output,
		)
	}

	text := output

	for _, anchor := range []string{
		"aoci index agent guide --agent codex --json",
		"automation.mode=auto",
		"auto follows the current Guide stage continuously and exactly",
		"Each aoci_update_entry batch completes validation, Diff, P-23 review, and atomic Apply internally",
		"After any applied write batch, follow the next commands returned by the current Guide",
		"continue Maintain while remaining is nonzero",
		"after the final remaining=0 batch run Verify, aggregate Check, and Guide",
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
