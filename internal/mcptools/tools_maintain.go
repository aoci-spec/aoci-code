// aoci_maintain高层Auto维护调度。
//
// 事实来源:
//   - 四态来自baseline.DetectWith;
//   - Missing的Raw/Actionable/Included/Excluded/Skipped/Pending/StaleDecision
//     全部来自internal/curation.BuildClassification;
//   - report仅是派发泄压阀，不是文件级exclude决策;
//   - 已有条目的Stale与Orphan仍遵循条目优先规则，不受Missing策展过滤。
//
// 换行宽容:
//   - 漂移判定消费团队line_ending_tolerance;
//   - 纯CRLF/LF表示差异不进入Stale，不派发更新任务;
//   - 团队显式严格模式和真实内容变化仍正常派发;
//   - 本工具不修改原始SHA、Baseline、正式索引或Stage绑定。
//
// 本工具只会自动前移已由规范格式器证明的format-only Baseline；真实语义变化
// 仍只派发给宿主模型，索引候选由aoci_update_entry原子批量治理。
package mcptools

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

func maintainToolDescription() string {
	return mcpContract(textassets.ContractMaintainToolDescription)
}

func registerMaintainTool(
	server *mcp.Server,
	root,
	mcpServiceVersion string,
	descriptions mcpToolDescriptions,
	inputSchemas mcpInputSchemas,
	refreshSession *cognitionRefreshSession,
) {
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "aoci_maintain",
			Description: descriptions[textassets.ContractMaintainToolDescription],
			InputSchema: inputSchemas["aoci_maintain"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			input maintainIn,
		) (
			*mcp.CallToolResult,
			any,
			error,
		) {
			result := guard(
				func() *mcp.CallToolResult {
					if input.Intent == machinecontract.MaintainIntentHumanApproval {
						return handleHumanApproval(ctx, request, root, input)
					}
					return handleMaintainInput(root, mcpServiceVersion, input, refreshSession)
				},
			)

			return result, nil, nil
		},
	)
}

// handleLegacyReadOnlyMaintain is what a Legacy single-file layout receives
// from aoci_maintain since 0.2.0: its authoring surfaces are retired, so the
// result is stopped with one finding and one next action, the migration to
// Volumes v1. Reads of the layout keep working; the receipt still describes it.
func handleLegacyReadOnlyMaintain(root, mcpServiceVersion string) *mcp.CallToolResult {
	if fail := validateWriteMessages(requiredMaintainWriteMessages); fail != nil {
		return failResult(fail)
	}
	start := time.Now()
	indexText := ""
	if repository, fail := loadRepoCtx(root); fail == nil {
		indexText = repository.text
	}
	ledger.Append(root, true, ledger.Event{Op: "maintain", DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceAgent,
		Result: machinecontract.StatusLegacyReadOnlyMigrateRequired, GovernanceResult: volumegovernance.ResultBlocked,
		Blocker: machinecontract.StatusLegacyReadOnlyMigrateRequired, NextAction: machinecontract.MaintainActionMigrateLegacyRepository,
		FormalWritesStarted: ledgerFlag(false)})
	return textResult(renderAutoResult(autoResult{
		Version: 1,
		Status:  autoStatusStopped,
		Aligned: false,
		Receipt: newCognitionReceipt(root, mcpServiceVersion, indexText, cognitionScopeRepositoryFull),
		Metrics: autoMetrics{DeterministicMs: elapsedMilliseconds(start), AOCIToolCalls: 1},
		// Legacy findings travel as cause strings, so the machine token leads
		// the sentence: a Host keys on it without parsing the prose.
		Findings: machineFindings{genericMachineFinding(
			machinecontract.StatusLegacyReadOnlyMigrateRequired,
			machinecontract.StatusLegacyReadOnlyMigrateRequired+": "+mcpMessage("mcp.error.legacy_read_only"))},
		NextAction:   machinecontract.MaintainActionMigrateLegacyRepository,
		NextCommands: legacyMigrationNextCommands(),
	}))
}

// legacyReadOnlyFail is the write-tool refusal for a Legacy layout.
func legacyReadOnlyFail() *Fail {
	return &Fail{
		Code: machinecontract.StatusLegacyReadOnlyMigrateRequired,
		Msg:  mcpMessage("mcp.error.legacy_read_only"),
		Hint: mcpMessage("mcp.error.legacy_read_only_hint"),
	}
}
