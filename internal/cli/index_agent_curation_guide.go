// Curation Guide construction.
//
// Every decision, role, reason, and confidence value is semantic cognition. The
// current host model must investigate each target independently. Physical
// profiles, paths, and extensions are evidence only and cannot generate a
// decision directly.
package cli

import (
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func buildCurationGuide(
	guide *agentGuide,
	plan *agentPlan,
	policy agentAutomationPolicy,
) {
	if !policy.AllowStage {
		buildObserveGuide(
			guide,
			"CurationRequired",
		)
		return
	}

	guide.Mode = policy.GuideMode
	guide.ApprovalRequired = policy.ApprovalRequired
	guide.StopBeforeApply = policy.StopBeforeApply

	guide.Commands.CurationStage =
		"aoci index agent curation stage --agent " + guide.Agent + " --request-file {request_file} --json"
	guide.Commands.CurationDiff =
		"aoci index agent curation diff {run_id}"
	guide.Commands.CurationApply =
		"aoci index agent curation apply {run_id}"

	targets := currentAgentCurationBatch(
		plan,
	)

	guide.CurationBatch = &agentGuideCurationBatch{
		MaxDecisions: machinecontract.CurationBatchMaxItems,
		Included:     len(targets),
		Remaining:    len(plan.CurationTargets) - len(targets),
		Targets:      targets,
	}

	decisions := make(
		[]agentCurationDecision,
		0,
		len(targets),
	)

	for _, target := range targets {
		decisions = append(
			decisions,
			agentCurationDecision{
				Path:         target.Path,
				SourceSHA256: target.SourceSHA256,
				Confidence:   -1,
			},
		)
	}

	guide.CurationStageRequest = &agentCurationStageRequest{
		Version:   agentCurationStageVersion,
		PlanID:    plan.PlanID,
		Agent:     guide.Agent,
		Decisions: decisions,
	}

	baseInstructions := renderGuideLines(
		textassets.ActiveLocale(),
		textassets.ContractGuideCurationBaseInstructions,
		nil,
	)

	switch policy.Mode {
	case config.AutomationModeAuto:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideCurationAutoMessage,
			nil,
		)

		guide.Instructions = append(
			guide.Instructions,
			baseInstructions...,
		)

		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideCurationAutoInstructions,
				nil,
			)...,
		)

	case config.AutomationModeReview:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideCurationReviewMessage,
			nil,
		)

		guide.Instructions = append(
			guide.Instructions,
			baseInstructions...,
		)

		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideCurationReviewInstructions,
				nil,
			)...,
		)

	default:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideCurationLegacyMessage,
			nil,
		)

		guide.Instructions = append(
			guide.Instructions,
			baseInstructions...,
		)

		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideCurationLegacyInstructions,
				nil,
			)...,
		)
	}
}
