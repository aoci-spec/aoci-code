// Host-Agent Guide 的 UTF-8 request-file 命令测试。
package cli

import (
	"strings"
	"testing"
)

func TestAgentGuidePrefersRequestFileCommands(
	t *testing.T,
) {
	headerPlan := guideTestPlan(
		agentPlanStageHeaderRequired,
	)
	headerPlan.HeaderState =
		agentPlanHeaderMissing

	headerGuide, err := buildAgentGuide(
		"codex",
		headerPlan,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		headerGuide.Commands.HeaderStage,
		"--request-file {request_file}",
	) ||
		strings.Contains(
			headerGuide.Commands.HeaderStage,
			"--stdin-json",
		) {
		t.Fatalf(
			"Header Guide应优先request-file: %q",
			headerGuide.Commands.HeaderStage,
		)
	}

	entriesPlan := guideTestPlan(
		agentPlanStageEntriesRequired,
	)
	entriesPlan.Targets =
		[]agentPlanTarget{
			{
				Path: "x.go",
				Kind: "create",
				SourceSHA256: strings.Repeat(
					"a",
					64,
				),
			},
		}

	entriesGuide, err := buildAgentGuide(
		"codex",
		entriesPlan,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		entriesGuide.Commands.EntriesStage,
		"--request-file {request_file}",
	) ||
		strings.Contains(
			entriesGuide.Commands.EntriesStage,
			"--stdin-json",
		) {
		t.Fatalf(
			"Entries Guide应优先request-file: %q",
			entriesGuide.Commands.EntriesStage,
		)
	}

	instructions := strings.Join(
		append(
			headerGuide.Instructions,
			entriesGuide.Instructions...,
		),
		"\n",
	)

	for _, anchor := range []string{
		"UTF-8 JSON文件",
		"Windows PowerShell 5不得用文本管道传递中文JSON",
		"--stdin-json仅用于可靠字节流环境",
	} {
		if !strings.Contains(
			instructions,
			anchor,
		) {
			t.Fatalf(
				"Guide缺少request-file纪律%q:\n%s",
				anchor,
				instructions,
			)
		}
	}
}
