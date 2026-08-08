package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func buildEntriesGuideForAssetTest(
	t *testing.T,
	mode string,
) *agentGuide {
	t.Helper()

	plan := guideTestPlan(
		agentPlanStageEntriesRequired,
	)
	plan.AutomationMode = mode
	plan.Targets = []agentPlanTarget{
		{
			Path: "x.go",
			Kind: "create",
			SourceSHA256: strings.Repeat(
				"a",
				64,
			),
		},
	}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	return guide
}

func TestEntriesAutoGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildEntriesGuideForAssetTest(
			t,
			config.AutomationModeAuto,
		),
		"guide_entries_auto.txt",
	)
}

func TestEntriesReviewGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildEntriesGuideForAssetTest(
			t,
			config.AutomationModeReview,
		),
		"guide_entries_review.txt",
	)
}

func TestEntriesLegacyGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildEntriesGuideForAssetTest(
			t,
			config.AutomationModeLegacy,
		),
		"guide_entries_legacy.txt",
	)
}
