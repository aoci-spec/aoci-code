// Aligned Guide对RawMissing与未解决治理路径的终态语义测试。
package cli

import (
	"strings"
	"testing"
)

func TestBuildAlignedGuideRetainsRawMissingWithoutFalseVerifyFailure(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageAligned,
	)
	plan.AutomationMode = "auto"
	plan.Summary.Missing = 1
	plan.Summary.CurationExcluded = 1
	plan.CurationExcluded =
		[]string{"docs/img/example.png"}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	if guide.Mode != agentGuideModeComplete ||
		!guide.Complete ||
		guide.ApprovalRequired ||
		guide.StopBeforeApply {
		t.Fatalf(
			"Aligned Guide终态不符: %+v",
			guide,
		)
	}

	instructions := strings.Join(
		guide.Instructions,
		"\n",
	)

	for _, anchor := range []string{
		"verify会保留RawMissing原始事实",
		"CurationExcludedMissing与非Pending SkippedMissing不导致失败",
		"只有ActionableMissing、PendingCurationMissing、Orphan、Stale或Unbaselined未解决时才返回非零",
	} {
		if !strings.Contains(
			instructions,
			anchor,
		) {
			t.Fatalf(
				"Aligned Guide缺少终态语义%q:\n%s",
				anchor,
				instructions,
			)
		}
	}

	if strings.Contains(
		instructions,
		"verify可能因原始Missing返回exit 1",
	) {
		t.Fatalf(
			"Aligned Guide仍包含旧Verify失败语义:\n%s",
			instructions,
		)
	}
}
