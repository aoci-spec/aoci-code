// Host-Agent Guide的阶段工作流构建。
//
// Stage命令默认使用--request-file {request_file}。宿主Agent应把Guide请求模板
// 保存为UTF-8 JSON文件后执行；--stdin-json仅保留为可靠字节流环境的兼容入口。
//
// R63纯模型语义合同:
//   - 标签、F/R/A/S必须由当前宿主模型阅读索引头和目标源码后逐项生成；
//   - AST、路径、文件名、正则、模板、规则引擎或批量脚本不得生成或预填语义；
//   - 工具只负责读取原文、传输、校验、审计和落盘。
//
// R65/R65-03 Auto合同:
//   - Entries Stage先安全提交标准草稿，再在内部完成Check、Diff审计和原子Apply；
//   - applied表示批次已写入，宿主只需Verify并重新Guide；
//   - repair_required表示候选内容可修复，宿主只修失败条目并自动重新Stage；
//   - stopped结束本次写尝试；宿主依据既有zero-write/Recovery证据决定重新Plan、
//     Resume、Rollback或真实停点，不建立第二套运行状态；
//   - 单批成功不等于完整任务完成，只有Guide终态或真正停点才能结束循环。
package cli

import (
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func newAgentGuideBase(
	agentName string,
	plan *agentPlan,
) *agentGuide {
	resumeCommand :=
		"aoci index agent guide --agent " +
			agentName +
			" --json"

	return &agentGuide{
		Version: agentGuideVersion,
		Agent:   agentName,
		Plan:    plan,
		Instructions: renderGuideLines(
			textassets.ActiveLocale(),
			textassets.ContractGuideBaseInstructions,
			nil,
		),
		Commands: agentGuideCommands{
			Guide:  resumeCommand,
			Plan:   "aoci index agent plan --json",
			Verify: "aoci verify --json",
		},
	}
}

func buildBaselineGuide(
	guide *agentGuide,
	plan *agentPlan,
) {
	if !plan.BaselineExists {
		guide.Mode = agentGuideModeExecute
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideBaselineFirstMessage,
			nil,
		)
		guide.Commands.Scan = "aoci scan"
		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideBaselineFirstInstructions,
				nil,
			)...,
		)
		return
	}

	guide.Mode = agentGuideModeBlocked
	guide.ApprovalRequired = true
	guide.Message = renderGuideText(
		textassets.ActiveLocale(),
		textassets.ContractGuideBaselineBlockedMessage,
		nil,
	)
	guide.Instructions = append(
		guide.Instructions,
		renderGuideLines(
			textassets.ActiveLocale(),
			textassets.ContractGuideBaselineBlockedInstructions,
			nil,
		)...,
	)
}

func buildObserveGuide(
	guide *agentGuide,
	scope string,
) {
	guide.Mode = agentGuideModeObserve
	guide.ApprovalRequired = false
	guide.StopBeforeApply = false
	guide.Message = renderGuideText(
		textassets.ActiveLocale(),
		textassets.ContractGuideObserveMessage,
		struct {
			Scope string
		}{
			Scope: scope,
		},
	)
	guide.Instructions = append(
		guide.Instructions,
		renderGuideLines(
			textassets.ActiveLocale(),
			textassets.ContractGuideObserveInstructions,
			nil,
		)...,
	)
}

func buildHeaderGuide(
	guide *agentGuide,
	plan *agentPlan,
	policy agentAutomationPolicy,
) {
	if !policy.AllowStage {
		buildObserveGuide(
			guide,
			"HeaderRequired",
		)
		return
	}

	guide.Mode = policy.GuideMode
	guide.ApprovalRequired =
		policy.ApprovalRequired
	guide.StopBeforeApply =
		policy.StopBeforeApply

	guide.Commands.HeaderStage =
		"aoci index agent header stage --agent " + guide.Agent + " --request-file {request_file} --json"
	guide.Commands.Diff =
		"aoci index header diff {run_id}"
	guide.Commands.Apply =
		"aoci index header apply {run_id}"

	guide.HeaderStageRequest =
		&agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   guide.Agent,
			Header:  "",
		}

	baseInstructions := renderGuideLines(
		textassets.ActiveLocale(),
		textassets.ContractGuideHeaderBaseInstructions,
		nil,
	)

	switch policy.Mode {
	case config.AutomationModeAuto:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideHeaderAutoMessage,
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
				textassets.ContractGuideHeaderAutoInstructions,
				nil,
			)...,
		)

	case config.AutomationModeReview:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideHeaderReviewMessage,
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
				textassets.ContractGuideHeaderReviewInstructions,
				nil,
			)...,
		)

	default:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideHeaderLegacyMessage,
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
				textassets.ContractGuideHeaderLegacyInstructions,
				nil,
			)...,
		)
	}
}

func buildEntriesGuide(
	guide *agentGuide,
	plan *agentPlan,
	policy agentAutomationPolicy,
) {
	if !policy.AllowStage {
		buildObserveGuide(
			guide,
			"EntriesRequired",
		)
		return
	}

	guide.Mode = policy.GuideMode
	guide.ApprovalRequired =
		policy.ApprovalRequired
	guide.StopBeforeApply =
		policy.StopBeforeApply

	guide.Commands.HeaderShow =
		"aoci index header show"
	guide.Commands.EntriesStage =
		"aoci index agent stage --agent " + guide.Agent + " --request-file {request_file} --json"

	// Entries Auto completes Check, Diff, and Apply inside Stage, so standalone
	// commands are exposed only when review or legacy allows the host to run them.
	if policy.Mode != config.AutomationModeAuto {
		guide.Commands.Check =
			"aoci index entries check {run_id}"
		guide.Commands.Diff =
			"aoci index entries diff {run_id}"
		guide.Commands.Apply =
			"aoci index entries apply {run_id}"
	}

	targets := agentGuideTargets(plan)

	guide.Batch = &agentGuideBatch{
		MaxEntries: machinecontract.EntriesBatchMaxItems,
		Included:   len(targets),
		Remaining:  len(plan.Targets) - len(targets),
		Targets:    targets,
	}

	guide.EntriesStageRequest =
		buildEntriesStageTemplate(
			guide.Agent,
			plan.PlanID,
			targets,
		)

	generationInstructions := renderGuideLines(
		textassets.ActiveLocale(),
		textassets.ContractGuideEntriesBaseInstructions,
		nil,
	)

	switch policy.Mode {
	case config.AutomationModeAuto:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideEntriesAutoMessage,
			nil,
		)

		guide.Instructions = append(
			guide.Instructions,
			generationInstructions...,
		)
		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideEntriesAutoInstructions,
				nil,
			)...,
		)

	case config.AutomationModeReview:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideEntriesReviewMessage,
			nil,
		)

		guide.Instructions = append(
			guide.Instructions,
			generationInstructions...,
		)
		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideEntriesReviewInstructions,
				nil,
			)...,
		)

	default:
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideEntriesLegacyMessage,
			nil,
		)

		guide.Instructions = append(
			guide.Instructions,
			generationInstructions...,
		)
		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideEntriesLegacyInstructions,
				nil,
			)...,
		)
	}
}

func agentGuideTargets(
	plan *agentPlan,
) []agentPlanTarget {
	limit := len(plan.Targets)
	if limit > machinecontract.EntriesBatchMaxItems {
		limit = machinecontract.EntriesBatchMaxItems
	}

	return append(
		[]agentPlanTarget{},
		plan.Targets[:limit]...,
	)
}

func buildEntriesStageTemplate(
	agentName,
	planID string,
	targets []agentPlanTarget,
) *agentStageRequest {
	requestEntries := make(
		[]agentStageEntry,
		0,
		len(targets),
	)

	for _, target := range targets {
		requestEntries = append(
			requestEntries,
			agentStageEntry{
				Path:         target.Path,
				SourceSHA256: target.SourceSHA256,
				Entry:        "",
			},
		)
	}

	return &agentStageRequest{
		Version: agentStageVersion,
		PlanID:  planID,
		Agent:   agentName,
		Entries: requestEntries,
	}
}

func buildAlignedGuide(
	guide *agentGuide,
	plan *agentPlan,
) {
	guide.Mode = agentGuideModeComplete
	guide.Complete = true

	if plan.Summary.Missing == 0 &&
		plan.Summary.Orphan == 0 &&
		plan.Summary.Unbaselined == 0 &&
		plan.Summary.Changed == 0 {
		guide.Message = renderGuideText(
			textassets.ActiveLocale(),
			textassets.ContractGuideAlignedCleanMessage,
			nil,
		)
		guide.Instructions = append(
			guide.Instructions,
			renderGuideLines(
				textassets.ActiveLocale(),
				textassets.ContractGuideAlignedCleanInstructions,
				nil,
			)...,
		)
		return
	}

	guide.Message = renderGuideText(
		textassets.ActiveLocale(),
		textassets.ContractGuideAlignedExplainedMessage,
		nil,
	)

	guide.Instructions = append(
		guide.Instructions,
		renderGuideLines(
			textassets.ActiveLocale(),
			textassets.ContractGuideAlignedExplainedInstructions,
			nil,
		)...,
	)
}
