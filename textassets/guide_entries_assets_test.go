package textassets

import (
	"strings"
	"testing"
)

func TestEntriesBaseInstructionsRemainOrdered(
	t *testing.T,
) {
	lines := MustRenderLines(
		LegacyLocale,
		ContractGuideEntriesBaseInstructions,
		nil,
	)

	if len(lines) != 8 {
		t.Fatalf(
			"expected 8 Entries base instructions, got %d: %+v",
			len(lines),
			lines,
		)
	}

	joined := strings.Join(
		lines,
		"\n",
	)

	for _, token := range []string{
		"header_show",
		"batch.targets",
		"old_entry",
		"entries_stage_request",
		"source_sha256",
		"commands.entries_stage",
		"UTF-8 JSON文件",
	} {
		if !strings.Contains(
			joined,
			token,
		) {
			t.Fatalf(
				"Entries base instructions are missing %q",
				token,
			)
		}
	}
}

func TestEntriesAutoInstructionsKeepThreeStates(
	t *testing.T,
) {
	instructions := strings.Join(
		MustRenderLines(
			LegacyLocale,
			ContractGuideEntriesAutoInstructions,
			nil,
		),
		"\n",
	)

	for _, token := range []string{
		"auto_finalize.status=applied",
		"auto_finalize.status=repair_required",
		"auto_finalize.status=stopped",
		"只修正其中失败条目",
		"普通回复边界、用户提问、repair_required和可自动恢复的stopped都不是结束条件",
		"证明零写入则记录closure并重新Plan",
	} {
		if !strings.Contains(
			instructions,
			token,
		) {
			t.Fatalf(
				"Entries Auto instructions are missing %q",
				token,
			)
		}
	}
}
