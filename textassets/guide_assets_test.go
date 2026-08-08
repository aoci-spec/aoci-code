package textassets

import (
	"strings"
	"testing"
)

func TestMustRenderRemovesOneTerminalNewline(
	t *testing.T,
) {
	raw, err := Render(
		LegacyLocale,
		ContractGuideAlignedCleanMessage,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(
		raw,
		"\n",
	) {
		t.Fatalf(
			"embedded scalar asset must retain its file-level newline: %q",
			raw,
		)
	}

	scalar := MustRender(
		LegacyLocale,
		ContractGuideAlignedCleanMessage,
		nil,
	)

	if strings.HasSuffix(
		scalar,
		"\n",
	) {
		t.Fatalf(
			"Guide scalar message must not retain the file-level newline: %q",
			scalar,
		)
	}

	if scalar+"\n" != raw {
		t.Fatalf(
			"MustRender must remove exactly one terminal newline: raw=%q scalar=%q",
			raw,
			scalar,
		)
	}
}

func TestRenderLinesPreservesInstructionOrder(
	t *testing.T,
) {
	lines, err := RenderLines(
		LegacyLocale,
		ContractGuideBaselineFirstInstructions,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(lines) != 2 {
		t.Fatalf(
			"expected two Baseline instructions, got %d: %+v",
			len(lines),
			lines,
		)
	}

	if lines[0] !=
		"执行aoci scan，不要添加--force。" {
		t.Fatalf(
			"unexpected first instruction: %q",
			lines[0],
		)
	}

	if lines[1] !=
		"scan成功后立即重新运行guide，不要在无Baseline状态下生成语义候选。" {
		t.Fatalf(
			"unexpected second instruction: %q",
			lines[1],
		)
	}
}

func TestGuideBaseInstructionPreservesCurrentStageBindings(
	t *testing.T,
) {
	lines, err := RenderLines(
		LegacyLocale,
		ContractGuideBaseInstructions,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf(
			"expected one Guide base instruction, got %d: %+v",
			len(lines),
			lines,
		)
	}

	instruction := lines[0]
	for _, anchor := range []string{
		"只有instructions明确要求时才重新运行guide",
		"plan_id",
		"source_sha256",
		"run_id",
	} {
		if !strings.Contains(instruction, anchor) {
			t.Fatalf(
				"Guide base instruction缺少当前阶段绑定%q: %s",
				anchor,
				instruction,
			)
		}
	}
	if strings.Contains(
		instruction,
		"每完成一个允许动作后重新运行guide",
	) {
		t.Fatalf(
			"Guide base instruction仍要求多步阶段中途重跑: %s",
			instruction,
		)
	}
}

func TestObserveMessageRendersScope(
	t *testing.T,
) {
	message := MustRender(
		LegacyLocale,
		ContractGuideObserveMessage,
		struct {
			Scope string
		}{
			Scope: "EntriesRequired",
		},
	)

	if !strings.Contains(
		message,
		"EntriesRequired事实",
	) {
		t.Fatalf(
			"observe message did not render scope: %q",
			message,
		)
	}

	if strings.HasSuffix(
		message,
		"\n",
	) {
		t.Fatalf(
			"observe scalar message contains a terminal newline: %q",
			message,
		)
	}
}
