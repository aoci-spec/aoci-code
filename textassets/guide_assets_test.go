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
		ContractGuideBaselineFirstMessage,
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
		ContractGuideBaselineFirstMessage,
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
