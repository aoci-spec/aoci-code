package textassets

import (
	"strings"
	"testing"
)

func TestEntriesBaseInstructionsRemainOrdered(
	t *testing.T,
) {
	tests := []struct {
		locale       string
		utf8Token    string
		policyTokens []string
	}{
		{
			locale:    LegacyLocale,
			utf8Token: "UTF-8 JSON文件",
			policyTokens: []string{
				"C6-C9对象应优先识别有证据支持的S约束",
				"只有无法由F/R/A推导且影响系统理解或修改的重要约束才写入S",
				"不存在合格约束时保持S:-",
			},
		},
		{
			locale:    DefaultLocale,
			utf8Token: "UTF-8 JSON file",
			policyTokens: []string{
				"For C6-C9 objects, actively look for evidence-backed S constraints",
				"Use S only when the constraint cannot be inferred from F/R/A and affects system understanding or modification",
				"Keep S:- when no qualifying constraint exists",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			lines := MustRenderLines(
				test.locale,
				ContractGuideEntriesBaseInstructions,
				nil,
			)

			if len(lines) != 9 {
				t.Fatalf(
					"expected 9 Entries base instructions, got %d: %+v",
					len(lines),
					lines,
				)
			}

			joined := strings.Join(
				lines,
				"\n",
			)
			for _, token := range append([]string{
				"header_show",
				"batch.targets",
				"old_entry",
				"entries_stage_request",
				"source_sha256",
				"commands.entries_stage",
				test.utf8Token,
			}, test.policyTokens...) {
				if !strings.Contains(joined, token) {
					t.Fatalf(
						"Entries base instructions are missing %q",
						token,
					)
				}
			}
		})
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
