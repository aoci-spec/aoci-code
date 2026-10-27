package mcptools

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

type maintainIn struct {
	Scope           string                `json:"scope,omitempty"`
	Intent          string                `json:"intent,omitempty"`
	ObjectRefs      []string              `json:"object_refs,omitempty"`
	ApprovalRequest *humanApprovalRequest `json:"approval_request,omitempty"`
}

const maintainIntentCognitionOptimization = "cognition_optimization"

type databaseMaintainResult struct {
	Version           int                    `json:"version"`
	Status            string                 `json:"status"`
	Aligned           bool                   `json:"aligned"`
	RequestedScope    string                 `json:"requested_scope"`
	EffectiveScope    string                 `json:"effective_scope"`
	Assessment        dbcognition.Assessment `json:"database_cognition"`
	Plan              *dbcognition.Plan      `json:"candidate_plan,omitempty"`
	AuthoringContract string                 `json:"authoring_contract,omitempty"`
	Receipt           cognitionReceipt       `json:"cognition_receipt"`
	Metrics           autoMetrics            `json:"metrics"`
	Findings          []string               `json:"findings,omitempty"`
	NextAction        string                 `json:"next_action"`
}

func handleMaintainInput(root, mcpServiceVersion string, input maintainIn, refreshSession *cognitionRefreshSession) *mcp.CallToolResult {
	// human_approval opens a host dialog, so it is served where the MCP
	// session is in hand (registerMaintainTool); reaching here means there is
	// no session to ask through.
	if input.Intent == machinecontract.MaintainIntentHumanApproval {
		return failResult(&Fail{Code: errBadArgs, Msg: "human_approval_requires_mcp_session"})
	}
	if input.ApprovalRequest != nil {
		return failResult(&Fail{Code: errBadArgs, Msg: "maintain_approval_request_requires_human_approval"})
	}
	loaded, fail := loadCognitionCtx(root)
	if input.Intent != "" {
		if input.Intent != maintainIntentCognitionOptimization {
			return failResult(&Fail{Code: errBadArgs, Msg: "maintain_intent_invalid"})
		}
		if fail != nil {
			return failResult(fail)
		}
		if loaded.set.LayoutMode != cognition.LayoutVolumesV1 {
			return failResult(&Fail{Code: errBadArgs, Msg: "cognition_optimization_requires_volumes_v1"})
		}
		if input.Scope != "" && input.Scope != cognition.ScopeCode {
			return failResult(&Fail{Code: errBadArgs, Msg: "cognition_optimization_scope_must_be_code"})
		}
		return handleCognitionOptimizationMaintain(root, mcpServiceVersion, input, loaded)
	}
	if len(input.ObjectRefs) != 0 {
		return failResult(&Fail{Code: errBadArgs, Msg: "maintain_object_refs_require_cognition_optimization"})
	}
	if fail == nil && loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		switch input.Scope {
		case "", cognition.ScopeCode, cognition.ScopeAll:
			return handleVolumeMaintain(root, mcpServiceVersion, input.Scope, loaded, refreshSession)
		case cognition.ScopeDatabase:
			return handleDatabaseMaintain(root, mcpServiceVersion, input.Scope)
		default:
			return failResult(&Fail{Code: errBadArgs, Msg: "maintain_scope_invalid"})
		}
	}
	// Not a Volumes v1 layout: a Legacy single-file index, or none. Legacy is
	// read-only since 0.2.0, so Code maintenance answers the migration; the
	// Database scope still reads its saved Evidence.
	switch input.Scope {
	case "", cognition.ScopeCode, cognition.ScopeAll:
		return handleLegacyReadOnlyMaintain(root, mcpServiceVersion)
	case cognition.ScopeDatabase:
		return handleDatabaseMaintain(root, mcpServiceVersion, input.Scope)
	default:
		return failResult(&Fail{Code: errBadArgs, Msg: "maintain_scope_invalid"})
	}
}

func handleDatabaseMaintain(root, mcpServiceVersion, requestedScope string) *mcp.CallToolResult {
	start := time.Now()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return failResult(fail)
	}
	view, _ := loaded.set.Scope(cognition.ScopeDatabase)
	receipt := newVolumeCognitionReceipt(root, mcpServiceVersion, loaded.set, view)
	state, exists, err := baseline.Load(root)
	if err != nil {
		return failResult(&Fail{Code: errIndexInvalid, Msg: "database_cognition_baseline_invalid"})
	}
	if !exists || state == nil {
		state = baseline.NewBaseline(nil)
	}
	assessment := dbcognition.Assess(root, loaded.cfg.DatabaseSources, loaded.set, state)
	result := databaseMaintainResult{
		Version: 1, Status: autoStatusStopped, RequestedScope: requestedScope,
		EffectiveScope: cognition.ScopeDatabase, Assessment: assessment, Receipt: receipt,
		Metrics: autoMetrics{AOCIToolCalls: 1}, Findings: []string{}, NextAction: assessment.NextAction,
	}
	switch {
	case assessment.CognitionCurrent:
		result.Status = autoStatusApplied
		result.Aligned = true
	case assessment.DatabaseVolumeState == cognition.AssetAbsent:
		result.Findings = append(result.Findings, assessment.NextAction)
	case assessment.BlockingSourceCount > 0 || assessment.Summary.Orphan+assessment.Summary.EvidenceUnavailable+assessment.Summary.EvidenceInvalid+assessment.Summary.SourceDisabled > 0:
		for _, source := range assessment.Sources {
			if source.State != machinecontract.DatabaseCognitionCurrent {
				result.Findings = append(result.Findings, source.State+": "+source.SourceID)
			}
		}
		for _, item := range assessment.Items {
			switch item.State {
			case machinecontract.DatabaseCognitionOrphan, machinecontract.DatabaseCognitionEvidenceUnavailable, machinecontract.DatabaseCognitionEvidenceInvalid, machinecontract.DatabaseCognitionSourceDisabled:
				result.Findings = append(result.Findings, item.State+": "+item.ObjectRef)
			}
		}
	default:
		objectLimit, evidenceLimit := loaded.cfg.DatabaseCognitionBatchLimits()
		plan, planErr := dbcognition.BuildPlan(root, assessment, loaded.set, objectLimit, evidenceLimit)
		if planErr != nil {
			result.Findings = append(result.Findings, planErr.Error())
			break
		}
		result.Status = autoStatusRepairRequired
		result.Plan = &plan
		result.NextAction = plan.NextAction
		assembled, contractErr := authoringcontract.Build(loaded.set.Meta.Raw, []string{cognition.ScopeDatabase}, textassets.ActiveLocale())
		if contractErr != nil {
			return failResult(&Fail{Code: errInternal, Msg: "database_authoring_contract_invalid"})
		}
		result.AuthoringContract = strings.Join(assembled.Instructions[1:], "\n\n")
	}
	result.Metrics.DeterministicMs = elapsedMilliseconds(start)
	result.Metrics.SemanticFiles = assessment.Summary.Missing + assessment.Summary.Stale + assessment.Summary.Unbaselined
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return errResult(errInternal, "database_maintain_result_invalid", "")
	}
	ledgerResult := ledger.ResultOK
	if result.Status == autoStatusStopped {
		ledgerResult = ledger.ResultError
	} else if result.Status == autoStatusRepairRequired {
		ledgerResult = ledger.ResultRepairRequired
	}
	event := ledger.Event{
		Op: "maintain", Source: ledger.SourceAgent, Result: ledgerResult,
		PathsCount: result.Metrics.SemanticFiles, DurationMs: result.Metrics.DeterministicMs,
		AOCIToolCalls: 1, SemanticFiles: result.Metrics.SemanticFiles,
		GovernanceResult: volumegovernance.ResultBlocked, NextAction: result.NextAction, FormalWritesStarted: ledgerFlag(false),
	}
	switch {
	case result.Aligned:
		event.GovernanceResult = volumegovernance.ResultAligned
	case result.Plan != nil:
		event.GovernanceResult, event.BatchID = volumegovernance.ResultAuthoringRequired, result.Plan.BatchID
	case len(result.Findings) > 0:
		// A database finding is "<state>: <identity>" or a bare action token;
		// the Ledger keeps the token and never the source or table identity.
		event.Blocker = strings.TrimSpace(strings.SplitN(result.Findings[0], ":", 2)[0])
	}
	ledger.Append(root, loaded.cfg.LedgerEnabled, event)
	return textResult(string(data) + "\n")
}
