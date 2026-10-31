// The high-level Auto maintenance scheduling of aoci_maintain.
//
// Sources of fact:
//   - the four states come from baseline.DetectWith;
//   - Missing's Raw/Actionable/Included/Excluded/Skipped/Pending/StaleDecision
//     all come from internal/curation.BuildClassification;
//   - report is only a dispatch relief valve, not a file-level exclude decision;
//   - Stale and Orphan of existing entries still follow the entry-first rule and are not filtered by Missing curation.
//
// Line-ending tolerance:
//   - drift judgement consumes the team line_ending_tolerance;
//   - a pure CRLF/LF representation difference does not enter Stale and dispatches no update task;
//   - the team's explicit strict mode and real content changes still dispatch normally;
//   - this tool does not change the raw SHA, the Baseline, the formal index, or the Stage binding.
//
// This tool only auto-advances a format-only Baseline proven by the canonical formatter; a real semantic change
// is still dispatched to the host model only, and the index candidates are governed atomically in batch by aoci_update_entry.
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
