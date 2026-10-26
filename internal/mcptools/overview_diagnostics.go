package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The SDK validates arguments before the typed handler. This receiving
// boundary observes its schema errors without running the semantic scorer.
func overviewSchemaDiagnostics(root string, enabled bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			params, ok := request.GetParams().(*mcp.CallToolParamsRaw)
			if !ok || method != "tools/call" || params.Name != "aoci_overview" {
				return result, err
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(params.Arguments, &fields) != nil || fields["model_cognition_attestation"] == nil {
				return result, err
			}
			call, ok := result.(*mcp.CallToolResult)
			if !ok || call.GetError() == nil || !strings.HasPrefix(call.GetError().Error(), "validating \"arguments\":") {
				return result, err
			}
			call.Content = nil
			call.SetError(fmt.Errorf("%s; %s", call.GetError(), mcpMessage("overview.attestation.schema_expected")))
			ledger.Append(root, enabled, ledger.Event{Op: "overview_attestation_schema_rejected", Source: ledger.SourceAgent, Result: "schema_rejected"})
			return result, err
		}
	}
}

func recordOverviewProofEvents(root string, enabled bool, input overviewIn, delivery overviewRendered, body framedOverviewBody) {
	chain := delivery.Facts.ChainID
	if delivery.Facts.DeliveryMode == overviewDeliveryFull || (delivery.Facts.DeliveryMode == overviewDeliveryChunked && strings.HasSuffix(delivery.Output, body.Receipt.EndMarker+"\n")) {
		ledger.Append(root, enabled, ledger.Event{Op: "overview_delivery_completed", Source: ledger.SourceAgent, IndexSHA256: delivery.Facts.IndexSHA256, Result: "completed", DeliveryChainID: chain})
	}
	if input.HostConfirmation != nil && hostDeliveryStatus(input.HostConfirmation, body) == hostDeliveryConfirmed {
		ledger.Append(root, enabled, ledger.Event{Op: "overview_host_confirmation_accepted", Source: ledger.SourceAgent, IndexSHA256: delivery.Facts.IndexSHA256, Result: "accepted", DeliveryChainID: chain})
	}
}

// recordCapacityEvents writes the Host-capacity facts one request produced: the
// interruption its compaction declaration recorded and the stop it received.
func recordCapacityEvents(root string, enabled bool, indexSHA256 string, interruption *hostCapacityInterruption, stop *hostCapacityFacts, plan overviewDeliveryPlan, reasons []string) {
	if interruption != nil {
		completed := false
		ledger.Append(root, enabled, ledger.Event{
			Op: "overview_delivery_interrupted", Source: ledger.SourceAgent, IndexSHA256: indexSHA256,
			DeliveryChainID: interruption.ChainID, ChunkIndex: interruption.ChunksServed, ChunkCount: interruption.ChunkCount,
			DeliveryCompleted: &completed, EstimatedTokens: interruption.EstimatedTokens, RefreshEventID: interruption.EventID,
			RefreshReasons: []string{machinecontract.RefreshReasonContextCompaction}, Result: machinecontract.OverviewFallbackHostContextCapacityExceeded,
		})
	}
	if stop != nil {
		ledger.Append(root, enabled, ledger.Event{
			Op: "overview_capacity_stop", Source: ledger.SourceAgent, IndexSHA256: indexSHA256,
			DeliveryMode: machinecontract.OverviewDeliveryModeCapacityStop, FallbackReason: machinecontract.OverviewFallbackHostContextCapacityExceeded,
			EstimatedTokens: plan.EstimatedTokens, ChunkCount: plan.ChunkCount, EntryCount: plan.EntryCount,
			DeliveryChainID: stop.InterruptedDeliveryChainID, RefreshReasons: reasons, AOCIToolCalls: 1,
			Result: machinecontract.OverviewDeliveryModeCapacityStop,
		})
	}
}
