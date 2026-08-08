package textassets

import (
	"strings"
	"testing"
)

func TestCurationBaseInstructionsRemainOrdered(
	t *testing.T,
) {
	lines := MustRenderLines(
		LegacyLocale,
		ContractGuideCurationBaseInstructions,
		nil,
	)

	if len(lines) != 10 {
		t.Fatalf(
			"expected 10 Curation base instructions, got %d: %+v",
			len(lines),
			lines,
		)
	}

	joined := strings.Join(
		lines,
		"\n",
	)

	for _, token := range []string{
		"curation_batch.targets",
		"profile_reason",
		"decision=include",
		"decision=exclude",
		"source_sha256",
		"confidence=-1",
		"UTF-8 JSON文件",
		"curation diff",
		"P-23",
	} {
		if !strings.Contains(
			joined,
			token,
		) {
			t.Fatalf(
				"Curation base instructions are missing %q",
				token,
			)
		}
	}
}

func TestCurationModeMessagesAreScalars(
	t *testing.T,
) {
	for _, id := range []ID{
		ContractGuideCurationAutoMessage,
		ContractGuideCurationReviewMessage,
		ContractGuideCurationLegacyMessage,
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
				"Curation mode message is not a scalar: id=%s message=%q",
				id,
				message,
			)
		}
	}
}
