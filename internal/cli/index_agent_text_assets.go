package cli

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

// validateGuideAssets只预检当前Plan分支将实际消费的资源。未选择的Guide阶段
// 或automation.mode文案损坏不会扩大本次命令的失败域。
func validateGuideAssets(ids ...textassets.ID) error {
	for _, id := range ids {
		if _, err := textassets.Load(textassets.ActiveLocale(), id); err != nil {
			return fmt.Errorf("%s", cliMessage(
				"guide.asset_invalid",
				id,
				localeSafeCLIDetail(err.Error()),
			))
		}
	}

	return nil
}

// renderGuideText和renderGuideLines是Guide领域构造器的无panic读取桥。
// buildAgentGuide已预检当前分支的精确资源集合；内部测试若绕过入口，读取失败
// 也只生成带ID的诊断，不会panic或回退到源码副本。
func renderGuideText(locale string, id textassets.ID, data any) string {
	value, err := textassets.RenderScalar(locale, id, data)
	if err != nil {
		return cliMessage("guide.asset_load_failed", id, localeSafeCLIDetail(err.Error()))
	}

	return value
}

func renderGuideLines(locale string, id textassets.ID, data any) []string {
	value, err := textassets.RenderLines(locale, id, data)
	if err != nil {
		return []string{cliMessage("guide.asset_load_failed", id, localeSafeCLIDetail(err.Error()))}
	}

	return value
}

func requiredGuideAssetIDs(
	plan *agentPlan,
	policy agentAutomationPolicy,
) ([]textassets.ID, error) {
	ids := []textassets.ID{textassets.ContractGuideBaseInstructions}
	add := func(values ...textassets.ID) []textassets.ID {
		return append(ids, values...)
	}
	observe := func() []textassets.ID {
		return add(
			textassets.ContractGuideObserveMessage,
			textassets.ContractGuideObserveInstructions,
		)
	}
	modePair := func(
		autoMessage, autoInstructions,
		reviewMessage, reviewInstructions,
		legacyMessage, legacyInstructions textassets.ID,
	) []textassets.ID {
		switch policy.Mode {
		case config.AutomationModeAuto:
			return add(autoMessage, autoInstructions)
		case config.AutomationModeReview:
			return add(reviewMessage, reviewInstructions)
		default:
			return add(legacyMessage, legacyInstructions)
		}
	}

	switch plan.Stage {
	case agentPlanStageBaselineRequired:
		if plan.BaselineExists {
			return add(
				textassets.ContractGuideBaselineBlockedMessage,
				textassets.ContractGuideBaselineBlockedInstructions,
			), nil
		}
		return add(
			textassets.ContractGuideBaselineFirstMessage,
			textassets.ContractGuideBaselineFirstInstructions,
		), nil
	case agentPlanStageHeaderRequired:
		if !policy.AllowStage {
			return observe(), nil
		}
		ids = add(textassets.ContractGuideHeaderBaseInstructions)
		return modePair(
			textassets.ContractGuideHeaderAutoMessage,
			textassets.ContractGuideHeaderAutoInstructions,
			textassets.ContractGuideHeaderReviewMessage,
			textassets.ContractGuideHeaderReviewInstructions,
			textassets.ContractGuideHeaderLegacyMessage,
			textassets.ContractGuideHeaderLegacyInstructions,
		), nil
	case agentPlanStageIndexReviewRequired:
		return add(
			textassets.ContractGuideIndexReviewBlockedMessage,
			textassets.ContractGuideIndexReviewBlockedInstructions,
		), nil
	case agentPlanStageEntriesRequired:
		if !policy.AllowStage {
			return observe(), nil
		}
		ids = add(textassets.ContractGuideEntriesBaseInstructions)
		return modePair(
			textassets.ContractGuideEntriesAutoMessage,
			textassets.ContractGuideEntriesAutoInstructions,
			textassets.ContractGuideEntriesReviewMessage,
			textassets.ContractGuideEntriesReviewInstructions,
			textassets.ContractGuideEntriesLegacyMessage,
			textassets.ContractGuideEntriesLegacyInstructions,
		), nil
	case agentPlanStageCurationRequired:
		if !policy.AllowStage {
			return observe(), nil
		}
		ids = add(textassets.ContractGuideCurationBaseInstructions)
		return modePair(
			textassets.ContractGuideCurationAutoMessage,
			textassets.ContractGuideCurationAutoInstructions,
			textassets.ContractGuideCurationReviewMessage,
			textassets.ContractGuideCurationReviewInstructions,
			textassets.ContractGuideCurationLegacyMessage,
			textassets.ContractGuideCurationLegacyInstructions,
		), nil
	case agentPlanStageOrphanReview:
		return add(
			textassets.ContractGuideOrphanReviewBlockedMessage,
			textassets.ContractGuideOrphanReviewBlockedInstructions,
		), nil
	case agentPlanStageScopeChangeRequired, agentPlanStageObservedReview,
		agentPlanStageCompressionRequired, agentPlanStageBudgetExceeded:
		return ids, nil
	case agentPlanStageAligned:
		if plan.Summary.Missing == 0 &&
			plan.Summary.Orphan == 0 &&
			plan.Summary.Unbaselined == 0 &&
			plan.Summary.Changed == 0 {
			return add(
				textassets.ContractGuideAlignedCleanMessage,
				textassets.ContractGuideAlignedCleanInstructions,
			), nil
		}
		return add(
			textassets.ContractGuideAlignedExplainedMessage,
			textassets.ContractGuideAlignedExplainedInstructions,
		), nil
	default:
		return nil, fmt.Errorf("%s", cliMessage("guide.stage_unknown", plan.Stage))
	}
}
