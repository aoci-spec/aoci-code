// Host-Agent automation.mode 策略映射测试。
package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestResolveAgentAutomationPolicy(
	t *testing.T,
) {
	tests := []struct {
		mode             string
		guideMode        string
		allowStage       bool
		approvalRequired bool
		stopBeforeApply  bool
		continueApply    bool
	}{
		{
			mode:             config.AutomationModeAuto,
			guideMode:        agentGuideModeExecute,
			allowStage:       true,
			approvalRequired: false,
			stopBeforeApply:  false,
			continueApply:    true,
		},
		{
			mode:             config.AutomationModeReview,
			guideMode:        agentGuideModePrepareReview,
			allowStage:       true,
			approvalRequired: true,
			stopBeforeApply:  true,
			continueApply:    false,
		},
		{
			mode:             config.AutomationModeLegacy,
			guideMode:        agentGuideModePrepareReview,
			allowStage:       true,
			approvalRequired: true,
			stopBeforeApply:  true,
			continueApply:    false,
		},
		{
			mode:             config.AutomationModeOff,
			guideMode:        agentGuideModeObserve,
			allowStage:       false,
			approvalRequired: false,
			stopBeforeApply:  false,
			continueApply:    false,
		},
	}

	for _, current := range tests {
		policy, err := resolveAgentAutomationPolicy(
			current.mode,
		)
		if err != nil {
			t.Fatalf(
				"解析%s失败: %v",
				current.mode,
				err,
			)
		}

		if policy.GuideMode != current.guideMode ||
			policy.AllowStage != current.allowStage ||
			policy.ApprovalRequired != current.approvalRequired ||
			policy.StopBeforeApply != current.stopBeforeApply ||
			policy.ContinueThroughApply != current.continueApply {
			t.Fatalf(
				"策略不符: mode=%s policy=%+v",
				current.mode,
				policy,
			)
		}
	}
}
