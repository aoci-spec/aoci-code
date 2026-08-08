package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func buildCurationGuideForAssetTest(
	t *testing.T,
	mode string,
) *agentGuide {
	t.Helper()

	plan := guideTestPlan(
		agentPlanStageCurationRequired,
	)

	plan.AutomationMode = mode

	plan.CurationTargets = []agentPlanCurationTarget{
		{
			Path: "marker.empty",
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

func TestCurationAutoGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildCurationGuideForAssetTest(
			t,
			config.AutomationModeAuto,
		),
		"guide_curation_auto.txt",
	)
}

func TestCurationReviewGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildCurationGuideForAssetTest(
			t,
			config.AutomationModeReview,
		),
		"guide_curation_review.txt",
	)
}

func TestCurationLegacyGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildCurationGuideForAssetTest(
			t,
			config.AutomationModeLegacy,
		),
		"guide_curation_legacy.txt",
	)
}
