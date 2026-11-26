package mcptools

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// maintainPayload measures the exact tool text, including its newline. The
// caller can cut the candidate prefix when even the sample-free response is
// too wide. One authoring target and its required evidence travel together.
func maintainPayload(result volumeMaintainResult, cfg *config.Config) (data []byte, fits bool, err error) {
	limit := machinecontract.MaintainTransportListLimit
	if len(result.Candidates) > 0 {
		limit = maintainCandidateSampleLimit
	}
	budget := maintainTransportBudget(cfg)
	// The independent Evidence limit applies only to Database-only deliveries.
	databaseOnly := result.CodePlan == nil && result.DatabasePlan != nil &&
		len(result.OrphanRemovals) == 0 && result.ObserveReview == nil
	for {
		wire := result
		boundMaintainTransport(&wire, limit)
		data, err = json.Marshal(wire)
		if err != nil {
			return nil, false, err
		}
		if len(data)+1 <= budget || databaseOnly {
			return data, true, nil
		}
		if limit > 0 {
			limit /= 2
			continue
		}
		if len(result.Candidates) > 1 {
			return data, false, nil
		}
		wire.Batch.TransportBudgetBytes = budget
		wire.Batch.TransportBudgetExceeded = true
		data, err = json.Marshal(wire)
		return data, err == nil, err
	}
}

func maintainResponse(data []byte, result volumeMaintainResult, session *cognitionRefreshSession) *mcp.CallToolResult {
	if result.authoringContractDigest != "" {
		session.recordAuthoringContract(result.authoringContractDigest)
	}
	return textResult(string(data) + "\n")
}

func renderMaintainTransport(result volumeMaintainResult, cfg *config.Config, session *cognitionRefreshSession) *mcp.CallToolResult {
	data, fits, err := maintainPayload(result, cfg)
	if err != nil {
		return failResult(&Fail{Code: errInternal, Msg: "volumes_maintain_result_invalid"})
	}
	if !fits {
		return maintainReplanResponse(result, cfg)
	}
	return maintainResponse(data, result, session)
}

// A previously issued receipt cannot be shortened after delivery. If its
// immutable batch no longer fits, route to current governance or a fresh plan.
func maintainReplanResponse(result volumeMaintainResult, cfg *config.Config) *mcp.CallToolResult {
	result.Candidates = []volumeMaintainCandidate{}
	result.CodePlan, result.DatabasePlan = nil, nil
	result.Status, result.Result, result.Aligned = autoStatusStopped, volumegovernance.ResultBlocked, false
	result.AuthoringMeta, result.Instructions, result.InstructionsRef = "", nil, nil
	result.Optimization = nil
	result.authoringContractDigest = ""
	result.Sets.Review, result.Sets.Write = []string{}, []string{}
	result.NextAction = machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch
	if result.ObserveReview != nil {
		result.ObserveReview = observeReviewFor(result.Governance.Facts)
		result.NextAction = machinecontract.ActionReviewObservedEvidence
	}
	result.Batch = volumeAuthoringBatch{NextAction: result.NextAction}
	data, _, err := maintainPayload(result, cfg)
	if err != nil {
		return failResult(&Fail{Code: errInternal, Msg: "volumes_maintain_result_invalid"})
	}
	return maintainResponse(data, result, nil)
}

func boundMaintainTransport(result *volumeMaintainResult, limit int) {
	if len(result.Sets.Write) > limit {
		result.Sets.WriteTotal = len(result.Sets.Write)
		result.Sets.Write = result.Sets.Write[:limit]
	}
	if len(result.Sets.Review) > limit {
		result.Sets.ReviewTotal = len(result.Sets.Review)
		result.Sets.Review = result.Sets.Review[:limit]
	}
	if result.Governance != nil {
		result.Governance = volumegovernance.BoundForMaintainTransport(result.Governance.Facts, limit)
	}
}
