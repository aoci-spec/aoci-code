package mcptools

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// renderMaintainTransport checks the UTF-8 tool text including its newline.
// The planned candidate set is already receipt-bound. If the ordinary list
// samples do not fit, reduce only the advisory lists and preserve their totals.
func renderMaintainTransport(result volumeMaintainResult, cfg *config.Config, session *cognitionRefreshSession) *mcp.CallToolResult {
	limit := machinecontract.MaintainTransportListLimit
	if len(result.Candidates) > 0 {
		limit = maintainCandidateSampleLimit
	}
	budget := maintainTransportBudget(cfg)
	var data []byte
	for {
		wire := result
		boundMaintainTransport(&wire, limit)
		var err error
		data, err = json.Marshal(wire)
		if err != nil {
			return failResult(&Fail{Code: errInternal, Msg: "volumes_maintain_result_invalid"})
		}
		// Database-only candidate responses use their own Evidence limit.
		databaseOnly := result.CodePlan == nil && result.DatabasePlan != nil && len(result.OrphanRemovals) == 0
		if len(data)+1 <= budget || databaseOnly {
			break
		}
		if limit > 0 {
			limit /= 2
			continue
		}
		if len(result.Candidates)+len(result.OrphanRemovals) != 1 || result.ObserveReview != nil {
			return failResult(&Fail{Code: errInternal,
				Msg: fmt.Sprintf("maintain_transport_budget_exceeded: %d > %d", len(data)+1, budget)})
		}
		wire.Batch.TransportBudgetBytes = budget
		wire.Batch.TransportBudgetExceeded = true
		data, err = json.Marshal(wire)
		if err != nil {
			return failResult(&Fail{Code: errInternal, Msg: "volumes_maintain_result_invalid"})
		}
		break
	}
	if result.authoringContractDigest != "" {
		session.recordAuthoringContract(result.authoringContractDigest)
	}
	return textResult(string(data) + "\n")
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
