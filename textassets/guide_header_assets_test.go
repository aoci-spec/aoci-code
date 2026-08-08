package textassets

import (
	"strings"
	"testing"
)

func TestHeaderBaseInstructionsRemainOrdered(
	t *testing.T,
) {
	lines := MustRenderLines(
		LegacyLocale,
		ContractGuideHeaderBaseInstructions,
		nil,
	)

	if len(lines) != 13 {
		t.Fatalf(
			"expected 13 Header base instructions, got %d: %+v",
			len(lines),
			lines,
		)
	}

	for _, token := range []string{
		"Header必须声明",
		"header_stage_request.header",
		"header_stage_request.managed_index_text",
		"commands.header_stage",
		"UTF-8 JSON文件",
		"真实run_id",
	} {
		if !strings.Contains(
			strings.Join(lines, "\n"),
			token,
		) {
			t.Fatalf(
				"Header base instructions are missing %q",
				token,
			)
		}
	}
}

func TestHeaderModeMessagesAreScalars(
	t *testing.T,
) {
	for _, id := range []ID{
		ContractGuideHeaderAutoMessage,
		ContractGuideHeaderReviewMessage,
		ContractGuideHeaderLegacyMessage,
	} {
		message := MustRender(
			LegacyLocale,
			id,
			nil,
		)

		if message == "" ||
			strings.HasSuffix(
				message,
				"\n",
			) {
			t.Fatalf(
				"Header mode message is not a scalar: id=%s message=%q",
				id,
				message,
			)
		}
	}
}
