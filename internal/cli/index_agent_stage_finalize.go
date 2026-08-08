// Host-Agent Entries Stage提交后的可选Auto收口与人读渲染。
//
// 本文件不负责创建草稿。只有stageAgentEntries已经完整保存草稿、Manifest和
// Stage Ledger后，auto模式才进入共享Entries Auto内核。这样任一后续硬闸、
// CAS或审计失败都保留当前Run，宿主可按结构化恢复动作继续处理。
//
// 候选内容错误使用repair_required：保持正式资产零写入、退出码0并要求宿主
// 只修正findings中的失败条目后重新Stage。真正的一致性或运行故障才返回错误。
package cli

import (
	"fmt"
	"io"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

// finalizeHostAgentEntriesStageAuto只在auto模式下执行草稿提交后的治理收口。
//
// Generation Plan在Check前重新计算，防止Stage完成后到Apply前仓库事实变化。
// 本函数不删除Run；失败时草稿、Check、Diff及Application证据按实际阶段保留。
func finalizeHostAgentEntriesStageAuto(
	cmd *cobra.Command,
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	result *agentStageResult,
) error {
	if result == nil {
		return &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("entries.stage.result_nil")),
		}
	}

	if result.AutomationMode !=
		config.AutomationModeAuto {
		return nil
	}

	prefix, err := currentAgentCommandPrefix()
	if err != nil {
		return &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.stage.executable_bind_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	stopped := &entriesAutoFinalizeResult{
		Status:     entriesAutoStatusStopped,
		FailedStep: entriesAutoStepGenerationPlan,
		RunID:      result.RunID,
		Attempted:  len(result.Statuses),
		Recovery:   cliMessage("entries.stage.recovery.plan"),
	}
	result.AutoFinalize = stopped
	result.NextCommand = ""

	manifest, err := draft.LoadManifest(
		repoRoot,
		result.RunID,
	)
	if err != nil {
		return &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.stage.manifest_read_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	_, err = guardHostAgentGenerationPlan(
		cmd,
		repoRoot,
		cfg,
		manifest,
		draft.KindEntries,
		agentPlanStageEntriesRequired,
	)
	if err != nil {
		closureErr := draft.AppendZeroWriteClosure(
			repoRoot,
			result.RunID,
			draft.ZeroWriteClosure{
				Version:           1,
				Step:              draft.ZeroWriteStepGenerationPlan,
				Reason:            draft.ZeroWriteReasonPlanGuard,
				DraftHash:         manifest.GenerationHash,
				PreIndexSHA256:    manifest.IndexSHA256,
				FormalAssetWrites: 0,
			},
		)
		if closureErr != nil {
			return &ExitError{Code: ExitInternal, Err: fmt.Errorf(
				"generation_plan_failed_and_zero_write_closure_failed: plan=%v closure=%v",
				err, closureErr,
			)}
		}
		return err
	}

	autoResult, autoErr := runEntriesAutoFinalize(
		repoRoot,
		cfg,
		doc,
		result.RunID,
		len(result.Statuses),
		ledger.SourceAgent,
		io.Discard,
	)
	if autoResult == nil {
		autoResult = &entriesAutoFinalizeResult{
			Status:     entriesAutoStatusStopped,
			FailedStep: entriesAutoStepAudit,
			RunID:      result.RunID,
			Attempted:  len(result.Statuses),
			Recovery:   cliMessage("entries.stage.recovery.no_status"),
		}
	}

	result.AutoFinalize = autoResult

	if autoErr != nil {
		result.NextCommand = ""

		if autoResult.Status ==
			entriesAutoStatusRepairRequired {
			return nil
		}

		return autoErr
	}

	result.NextCommand = bindAgentCommand(
		"aoci verify --json",
		prefix,
	)

	return nil
}

// renderAgentStageHuman输出Stage及可选Auto收口的紧凑人读结果。
func renderAgentStageHuman(
	out io.Writer,
	result *agentStageResult,
) {
	if out == nil ||
		result == nil {
		return
	}

	fmt.Fprint(out, cliMessage("entries.stage.created", result.RunID))
	fmt.Fprintf(
		out,
		"automation.mode: %s | approval_required: %t | stop_before_apply: %t\n",
		result.AutomationMode,
		result.ApprovalRequired,
		result.StopBeforeApply,
	)
	fmt.Fprintf(
		out,
		"generation_hash: %s\n",
		result.GenerationHash,
	)
	fmt.Fprint(out, cliMessage("entries.stage.total", result.Drafted, result.Warned))

	for _, status := range result.Statuses {
		fmt.Fprintf(
			out,
			"[%s] %s",
			status.Status,
			status.Path,
		)
		if status.Note != "" {
			fmt.Fprint(
				out,
				" —— "+status.Note,
			)
		}
		fmt.Fprintln(
			out,
		)
	}

	if result.AutoFinalize != nil {
		auto := result.AutoFinalize

		fmt.Fprint(out, cliMessage(
			"entries.stage.auto_result",
			auto.Status,
			auto.Checked,
			auto.Warned,
			auto.Rejected,
			auto.DiffReviewed,
			auto.Applied,
			auto.AssetWritten,
			auto.AuditRecorded,
		))

		if auto.FailedStep != "" {
			label := cliMessage("entries.stage.failed_step")
			if auto.Status ==
				entriesAutoStatusRepairRequired {
				label = cliMessage("entries.stage.repair_step")
			}

			fmt.Fprintln(
				out,
				label+auto.FailedStep,
			)
		}
		if auto.Recovery != "" {
			fmt.Fprintln(
				out,
				cliMessage("entries.stage.recovery", localeSafeCLIDetail(auto.Recovery)),
			)
		}
	}

	if result.NextCommand != "" {
		fmt.Fprintln(
			out,
			cliMessage("agent.next", result.NextCommand),
		)
	}
}
