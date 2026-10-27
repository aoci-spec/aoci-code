// Host capacity (#114): the loop breaker for context-compaction reloads.
//
// After a Host compacts its context, the runtime contract asks for one fresh
// complete Overview before the business task continues. When the Whole-Index
// does not fit the Host's post-compaction context, that reload is itself
// compacted before it can finish, the reload is asked for again, and the
// session never reaches its task. The MCP server process outlives the Host's
// compactions, so it can see what the Host cannot: a reload that started under
// a declared context_compaction was interrupted by the next declared
// context_compaction before any confirmed, attested delivery consumed it.
// That interruption is the capacity verdict. From then on, for the life of
// this server session, a fresh reload of a scope at least that large answers
// the capacity stop instead of another chain: no body, the generation
// consumed, an uncertain receipt, and source-bound continuation without an
// unqualified complete-system claim. An explicit Overview requested while no
// compaction refresh is pending still delivers, and a smaller scope still
// delivers, so nothing is withheld that the Host can hold.
package mcptools

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// compactionDeliveryAttempt is the fresh complete delivery started while a
// declared context_compaction refresh was pending. It closes when a confirmed,
// attested delivery or a capacity stop consumes the pending events.
type compactionDeliveryAttempt struct {
	ChainID         string
	ScopeIdentity   string
	EstimatedTokens int
	ChunkCount      int
	ChunksServed    int
}

// hostCapacityVerdict is what this server session learned about the Host: the
// smallest scope whose compaction reload was interrupted by compaction.
type hostCapacityVerdict struct {
	FailedEstimatedTokens int
	Interruptions         int
	InterruptedChainID    string
	InterruptedChunks     int
	InterruptedChunkCount int
}

// hostCapacityInterruption reports, to the handler that holds the Ledger, the
// interruption one evaluate call just recorded.
type hostCapacityInterruption struct {
	ChainID         string
	EstimatedTokens int
	ChunksServed    int
	ChunkCount      int
	EventID         string
}

// overviewDeliveryPlan is what a fresh complete delivery of one scope would
// transport, the cumulative cost a Host weighs before it starts the chain:
// chunk_tokens bounds one response, estimated_tokens is the whole chain.
type overviewDeliveryPlan struct {
	Scope           string `json:"scope"`
	ScopeIdentity   string `json:"scope_identity"`
	EntryCount      int    `json:"entry_count"`
	EstimatedTokens int    `json:"estimated_tokens"`
	BodyUTF8Bytes   int    `json:"body_utf8_bytes"`
	ChunkTokens     int    `json:"chunk_tokens"`
	ChunkCount      int    `json:"chunk_count"`
}

// hostCapacityFacts is the compact capacity view check_only and the stop
// itself report.
type hostCapacityFacts struct {
	Verdict                    bool   `json:"verdict"`
	FailedEstimatedTokens      int    `json:"failed_estimated_tokens,omitempty"`
	Interruptions              int    `json:"interruptions,omitempty"`
	InterruptedDeliveryChainID string `json:"interrupted_delivery_chain_id,omitempty"`
	InterruptedChunksServed    int    `json:"interrupted_chunks_served,omitempty"`
	InterruptedChunkCount      int    `json:"interrupted_chunk_count,omitempty"`
	// CompactionReload is what a fresh context-compaction reload of the
	// requested scope gets now: deliver or capacity_stop.
	CompactionReload string `json:"compaction_reload"`
}

// planOverviewDelivery measures one framed body with the delivery path's own
// chunk planner, so the plan a Host reads before the chain and the chain it
// then receives are one computation.
func planOverviewDelivery(framed framedOverviewBody, sequence []overviewChallengeTarget, scope, scopeIdentity string, entryCount, estimatedTokens, chunkTokens int) (overviewDeliveryPlan, error) {
	plan := overviewDeliveryPlan{
		Scope: scope, ScopeIdentity: scopeIdentity, EntryCount: entryCount,
		EstimatedTokens: estimatedTokens, BodyUTF8Bytes: framed.Receipt.BodyUTF8Bytes,
		ChunkTokens: chunkTokens, ChunkCount: 1,
	}
	if len(framed.Text)/3 <= chunkTokens {
		return plan, nil
	}
	spans, err := planOverviewChunks(framed.Text, chunkTokens, len(framed.Receipt.StartMarker)+1, sequence)
	if err != nil {
		return overviewDeliveryPlan{}, err
	}
	plan.ChunkCount = len(spans)
	return plan, nil
}

// noteCompactionDeclaration runs at the top of evaluate, before the request's
// own reasons join the pending set: a context_compaction declared with a
// fresh event while a compaction reload is still open is the interruption.
// The caller holds session.mu.
func (session *cognitionRefreshSession) noteCompactionDeclaration(hostReasons []string, eventID string) *hostCapacityInterruption {
	attempt := session.compactionAttempt
	if attempt == nil || eventID == "" || session.pendingEvents[eventID] || session.consumedEvents[eventID] ||
		!containsString(hostReasons, machinecontract.RefreshReasonContextCompaction) {
		return nil
	}
	verdict := session.capacity
	if verdict == nil {
		verdict = &hostCapacityVerdict{}
		session.capacity = verdict
	}
	if verdict.FailedEstimatedTokens == 0 || attempt.EstimatedTokens < verdict.FailedEstimatedTokens {
		verdict.FailedEstimatedTokens = attempt.EstimatedTokens
	}
	verdict.Interruptions++
	verdict.InterruptedChainID = attempt.ChainID
	verdict.InterruptedChunks = attempt.ChunksServed
	verdict.InterruptedChunkCount = attempt.ChunkCount
	// The interrupted attempt is over: the new declaration either stops or
	// opens an attempt of its own.
	session.compactionAttempt = nil
	return &hostCapacityInterruption{
		ChainID: attempt.ChainID, EstimatedTokens: attempt.EstimatedTokens,
		ChunksServed: attempt.ChunksServed, ChunkCount: attempt.ChunkCount, EventID: eventID,
	}
}

// compactionPending reports whether a declared context_compaction refresh is
// still waiting for its delivery. The caller holds session.mu.
func (session *cognitionRefreshSession) compactionPending() bool {
	return session.pendingReasons[machinecontract.RefreshReasonContextCompaction]
}

// noteFreshDelivery records the delivery chain a fresh complete request just
// started. Under a pending compaction refresh it opens (or replaces) the
// compaction reload attempt. The caller holds session.mu.
func (session *cognitionRefreshSession) noteFreshDelivery(chainID string, plan overviewDeliveryPlan, chunkIndex int) {
	if !session.compactionPending() {
		return
	}
	session.compactionAttempt = &compactionDeliveryAttempt{
		ChainID: chainID, ScopeIdentity: plan.ScopeIdentity,
		EstimatedTokens: plan.EstimatedTokens, ChunkCount: plan.ChunkCount, ChunksServed: chunkIndex,
	}
}

// noteChunkServed advances the open attempt's progress for the Ledger and the
// verdict. The caller holds session.mu.
func (session *cognitionRefreshSession) noteChunkServed(chainID string, chunkIndex int) {
	if attempt := session.compactionAttempt; attempt != nil && attempt.ChainID == chainID && chunkIndex > attempt.ChunksServed {
		attempt.ChunksServed = chunkIndex
	}
}

// capacityStopApplies decides the stop for a fresh complete request: a
// compaction refresh is pending and the verdict covers a scope this large.
// The caller holds session.mu.
func (session *cognitionRefreshSession) capacityStopApplies(estimatedTokens int) bool {
	return session.capacity != nil && session.compactionPending() &&
		estimatedTokens >= session.capacity.FailedEstimatedTokens
}

// capacityFacts reports the verdict and what a compaction reload of a scope
// this large would get. The caller holds session.mu.
func (session *cognitionRefreshSession) capacityFacts(estimatedTokens int) *hostCapacityFacts {
	facts := &hostCapacityFacts{CompactionReload: machinecontract.OverviewCompactionReloadDeliver}
	verdict := session.capacity
	if verdict == nil {
		return facts
	}
	facts.Verdict = true
	facts.FailedEstimatedTokens = verdict.FailedEstimatedTokens
	facts.Interruptions = verdict.Interruptions
	facts.InterruptedDeliveryChainID = verdict.InterruptedChainID
	facts.InterruptedChunksServed = verdict.InterruptedChunks
	facts.InterruptedChunkCount = verdict.InterruptedChunkCount
	if estimatedTokens >= verdict.FailedEstimatedTokens {
		facts.CompactionReload = machinecontract.OverviewCompactionReloadCapacityStop
	}
	return facts
}

// consumeCapacityStop answers the pending compaction refresh with the stop:
// the generation advances with an uncertain receipt, exactly like a complete
// transport whose Attestation fell short, so the Host continues source-bound
// instead of asking again. The caller holds session.mu.
func (session *cognitionRefreshSession) consumeCapacityStop(current cognitionReceipt, eventID string) cognitionReceipt {
	receipt := receiptWithState(session.deliveredReceipt(current, eventID), cognitionStateUncertain, false)
	session.markDeliveryAttempt(receipt)
	return receipt
}

// noteDeliveryChain stamps a rendered response with its chain ID and the
// refresh reasons it answered, and keeps the compaction reload attempt in
// step. Only a fresh delivery of the complete scope opens an attempt, because
// the contract's compaction reload is the Whole-Index: a scoped read proves
// nothing about whether the whole fits, and an unattested small one must
// never lower the verdict. A later chunk advances the attempt; a proof half
// for its body closes it, since the Host then held every chunk.
// The caller holds session.mu.
func (session *cognitionRefreshSession) noteDeliveryChain(delivery *overviewRendered, bodySHA256 string, fresh, fullScope bool, reasons []string, estimatedTokens int) {
	chainID := session.deliveryChainID(bodySHA256, fresh)
	delivery.Facts.ChainID = chainID
	delivery.Facts.RefreshReasons = append([]string(nil), reasons...)
	if delivery.Facts.DeliveryMode == overviewDeliveryAttestation {
		// An incomplete confirmation alone proves no transport. Keep the
		// attempt open without changing the independent model-proof path.
		if delivery.Attestation.DeliveryIntegrity == deliveryIntegrityIncomplete && !delivery.Attestation.ReportProvided {
			return
		}
		if attempt := session.compactionAttempt; attempt != nil && attempt.ScopeIdentity == delivery.Facts.IndexSHA256 {
			session.compactionAttempt = nil
		}
		return
	}
	if !delivery.Facts.ChunkKnown {
		return
	}
	if fresh {
		if fullScope {
			session.noteFreshDelivery(chainID, overviewDeliveryPlan{
				ScopeIdentity: delivery.Facts.IndexSHA256, EstimatedTokens: estimatedTokens,
				ChunkCount: delivery.Facts.ChunkCount,
			}, delivery.Facts.ChunkIndex)
		}
		return
	}
	session.noteChunkServed(chainID, delivery.Facts.ChunkIndex)
}

var deliveryChainSequence atomic.Uint64

// deliveryChainID names one delivery chain for the Ledger. A fresh complete
// delivery mints the ID; its continuations and proof calls inherit it by body
// identity. A continuation this session never started (a v1 cursor carried
// across a server restart) gets an ID of its own. The caller holds session.mu.
func (session *cognitionRefreshSession) deliveryChainID(bodySHA256 string, fresh bool) string {
	if session.chains == nil {
		session.chains = map[string]string{}
	}
	if !fresh {
		if id := session.chains[bodySHA256]; id != "" {
			return id
		}
	}
	id := newDeliveryChainID()
	if _, exists := session.chains[bodySHA256]; !exists {
		session.chainOrder = append(session.chainOrder, bodySHA256)
	}
	session.chains[bodySHA256] = id
	for len(session.chainOrder) > 64 {
		delete(session.chains, session.chainOrder[0])
		session.chainOrder = session.chainOrder[1:]
	}
	return id
}

func newDeliveryChainID() string {
	sequence := deliveryChainSequence.Add(1)
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("chain-%016x", sequence)
	}
	return fmt.Sprintf("chain-%s-%04x", hex.EncodeToString(random[:]), sequence&0xffff)
}

// capacityStopView is everything the stop response states.
type capacityStopView struct {
	Root, MCPServiceVersion, LayoutMode string
	RequestedScope, EffectiveScope      string
	Plan                                overviewDeliveryPlan
	Capacity                            *hostCapacityFacts
	GovernanceAligned                   bool
	Receipt                             cognitionReceipt
	CognitionStateV2                    bool
	FullScope                           bool
}

// renderOverviewCapacityStop renders the stop in the metadata form full and
// attestation responses use. It carries no body and no cognition level: the
// level vocabulary describes a delivered index, and none was delivered.
func renderOverviewCapacityStop(view capacityStopView) string {
	receiptJSON, _ := json.Marshal(view.Receipt)
	receiptLabel := "cognition_receipt"
	if view.Receipt.Version == 2 {
		receiptLabel = "cognition_receipt_v2"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder,
		"AOCI Overview Metadata:\nrequest_mode: full\ndelivery_mode: %s\ncompleted: false\ncontinuation_required: false\nfull_text_included: false\nfallback_reason: %s\n"+
			"runtime_repository_root: %s\nmcp_service_version: %s\nlayout_mode: %s\nrequested_scope: %s\neffective_scope: %s\n"+
			"index_sha256: %s\nentry_count: %d\nestimated_tokens: %d\nbody_utf8_bytes: %d\nchunk_tokens: %d\nchunk_count: %d\n"+
			"capacity_failed_estimated_tokens: %d\ncapacity_interruptions: %d\ninterrupted_delivery_chain_id: %s\n"+
			"interrupted_chunks_served: %d\ninterrupted_chunk_count: %d\n"+
			"governance_aligned: %t\nrefresh_generation: %d\nrefresh_status: %s\ncognition_state: %s\nmodel_full_cognition_reliable: false\n"+
			"delivery_guidance: %s\n%s: %s\nnext_action: %s\n",
		machinecontract.OverviewDeliveryModeCapacityStop, machinecontract.OverviewFallbackHostContextCapacityExceeded,
		view.Root, view.MCPServiceVersion, view.LayoutMode, view.RequestedScope, view.EffectiveScope,
		view.Plan.ScopeIdentity, view.Plan.EntryCount, view.Plan.EstimatedTokens, view.Plan.BodyUTF8Bytes,
		view.Plan.ChunkTokens, view.Plan.ChunkCount,
		view.Capacity.FailedEstimatedTokens, view.Capacity.Interruptions, view.Capacity.InterruptedDeliveryChainID,
		view.Capacity.InterruptedChunksServed, view.Capacity.InterruptedChunkCount,
		view.GovernanceAligned, view.Receipt.RefreshGeneration, machinecontract.RefreshStatusNotRequired,
		view.Receipt.State, machinecontract.OverviewDeliveryGuidanceSourceBound,
		receiptLabel, receiptJSON,
		overviewMetadataLineValue(mcpMessage("overview.capacity_stop.next", view.Plan.EstimatedTokens, view.Capacity.FailedEstimatedTokens)),
	)
	if view.CognitionStateV2 {
		state := assessOverviewCognitionStateV2(
			false, view.FullScope, view.GovernanceAligned, true,
			overviewAttestationResult{
				DeliveryIntegrity: deliveryIntegrityIncomplete,
				ModelAttestation:  modelAttestationNotProvided,
			},
		)
		encoded, _ := json.Marshal(state)
		fmt.Fprintf(&builder, "cognition_state_v2: %s\n", encoded)
	}
	return builder.String()
}
