package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func buildHeaderGuideForAssetTest(
	t *testing.T,
	mode string,
) *agentGuide {
	t.Helper()

	plan := guideTestPlan(
		agentPlanStageHeaderRequired,
	)
	plan.AutomationMode = mode
	plan.HeaderState =
		agentPlanHeaderMissing

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	return guide
}

func TestHeaderAutoGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildHeaderGuideForAssetTest(
			t,
			config.AutomationModeAuto,
		),
		"guide_header_auto.txt",
	)
}

func TestHeaderReviewGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildHeaderGuideForAssetTest(
			t,
			config.AutomationModeReview,
		),
		"guide_header_review.txt",
	)
}

func TestHeaderLegacyGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertGuideMatchesGolden(
		t,
		buildHeaderGuideForAssetTest(
			t,
			config.AutomationModeLegacy,
		),
		"guide_header_legacy.txt",
	)
}
