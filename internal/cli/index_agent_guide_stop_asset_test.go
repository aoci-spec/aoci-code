// Guide阻断停点文本资产与真实领域构造的字节级测试。
package cli

import (
	"reflect"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestAgentGuideBlockedStagesUseTextAssets(
	t *testing.T,
) {
	tests := []struct {
		name           string
		stage          string
		messageID      textassets.ID
		instructionsID textassets.ID
	}{
		{
			name:           "index-review",
			stage:          agentPlanStageIndexReviewRequired,
			messageID:      textassets.ContractGuideIndexReviewBlockedMessage,
			instructionsID: textassets.ContractGuideIndexReviewBlockedInstructions,
		},
		{
			name:           "orphan-review",
			stage:          agentPlanStageOrphanReview,
			messageID:      textassets.ContractGuideOrphanReviewBlockedMessage,
			instructionsID: textassets.ContractGuideOrphanReviewBlockedInstructions,
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				plan := &agentPlan{
					Stage:          current.stage,
					AutomationMode: config.AutomationModeReview,
				}

				guide, err := buildAgentGuide(
					"codex",
					plan,
				)
				if err != nil {
					t.Fatalf(
						"构造阻断Guide失败: %v",
						err,
					)
				}

				if guide.Mode != agentGuideModeBlocked {
					t.Fatalf(
						"阻断Guide模式错误: got=%s",
						guide.Mode,
					)
				}

				if !guide.ApprovalRequired {
					t.Fatal(
						"阻断Guide必须要求维护者批准",
					)
				}

				expectedMessage := textassets.MustRender(
					textassets.LegacyLocale,
					current.messageID,
					nil,
				)

				if guide.Message != expectedMessage {
					t.Fatalf(
						"阻断Message未使用文本资产:\nwant=%q\ngot=%q",
						expectedMessage,
						guide.Message,
					)
				}

				expectedInstructions := append(
					textassets.MustRenderLines(
						textassets.LegacyLocale,
						textassets.ContractGuideBaseInstructions,
						nil,
					),
					textassets.MustRenderLines(
						textassets.LegacyLocale,
						current.instructionsID,
						nil,
					)...,
				)

				if !reflect.DeepEqual(
					guide.Instructions,
					expectedInstructions,
				) {
					t.Fatalf(
						"阻断Instructions未使用文本资产:\nwant=%v\ngot=%v",
						expectedInstructions,
						guide.Instructions,
					)
				}
			},
		)
	}
}
