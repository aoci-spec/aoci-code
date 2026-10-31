// Parallel current batches (L4): under code_cognition_parallel_batches > 1,
// Maintain keeps up to N sibling batches of one logical plan outstanding,
// delivers the first as the current batch, lists the others, and delivers
// any of them by batch_id. Everything a sibling binds is the same as a single
// batch's; what differs is the preimage rule the Apply judges it by, which
// lives with the commit (tools_write_volume_commit.go).
package mcptools

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

// parallelIdentitiesOf reads the digests a plan is issued against from the
// loaded set and the verdict: the formal assets around the Code Volume and
// the Code Volume itself.
func parallelIdentitiesOf(facts *volumegovernance.Facts, set *cognition.Set) codebatch.ParallelIdentities {
	ids := codebatch.ParallelIdentities{CompositeIdentity: facts.CompositeIdentity, ScopePolicyIdentity: facts.ManagedScope.PolicyIdentity,
		CodeVolumePath: facts.Code.Path, CodeVolumeSHA256: facts.Code.SHA256, RootSHA256: set.Root.SHA256, MetaSHA256: set.Meta.SHA256}
	if asset := set.Volumes[cognition.ScopeDatabase]; asset != nil {
		ids.DatabaseSHA256 = asset.SHA256
	}
	return ids
}

// currentParallelPlan returns the logical plan on record when it still holds
// and the replay reproduces the current Code Volume; any other record is a
// completed or invalidated plan and is dropped.
func currentParallelPlan(root string, ids codebatch.ParallelIdentities) *codebatch.ParallelPlan {
	plan, err := codebatch.LoadParallelPlan(root)
	if err != nil || plan == nil {
		return nil
	}
	if plan.PlanHolds(ids) {
		if proven, replayErr := codebatch.ReplayProves(root, root, plan, ids.CodeVolumeSHA256); replayErr == nil && proven {
			return plan
		}
	}
	_ = codebatch.RemoveParallelPlans(root)
	return nil
}

// parallelSiblingStates reads every sibling of the plan as the wire lists it:
// applied, delivered, issued, or invalidated by its own source drift. An
// invalidated sibling stays listed with its state and holds no target; its
// receipt is left for the plan's end, when the next plan issues the targets
// again. Applied ones are kept, since their records carry the replay.
func parallelSiblingStates(root string, plan *codebatch.ParallelPlan) ([]codebatch.Sibling, map[string]codebatch.Receipt) {
	applied := codebatch.AppliedBatchIDs(root, plan)
	delivered := map[string]bool{}
	for _, batchID := range plan.Delivered {
		delivered[batchID] = true
	}
	siblings := []codebatch.Sibling{}
	receipts := map[string]codebatch.Receipt{}
	for _, batchID := range plan.Siblings {
		sibling := codebatch.Sibling{BatchID: batchID, Targets: []string{}, State: codebatch.SiblingInvalidated}
		receipt, err := codebatch.LoadReceipt(root, batchID)
		if err == nil {
			for _, target := range receipt.Targets {
				sibling.Targets = append(sibling.Targets, target.Path)
			}
			sibling.Included = len(sibling.Targets)
			switch {
			case applied[batchID]:
				sibling.State = codebatch.SiblingApplied
			case !codebatch.ReceiptSourcesCurrent(root, receipt):
				sibling.State = codebatch.SiblingInvalidated
			case delivered[batchID]:
				sibling.State = codebatch.SiblingDelivered
			default:
				sibling.State = codebatch.SiblingIssued
			}
			receipts[batchID] = receipt
		}
		siblings = append(siblings, sibling)
	}
	return siblings, receipts
}

func siblingOutstanding(state string) bool {
	return state == codebatch.SiblingIssued || state == codebatch.SiblingDelivered
}

// siblingListingBytes measures what the response carries to list siblings
// beside the current batch, so the current batch is cut under the budget
// that remains.
func siblingListingBytes(cuts [][]codebatch.Candidate) int {
	listing := make([]codebatch.Sibling, 0, len(cuts))
	for _, cut := range cuts {
		sibling := codebatch.Sibling{BatchID: strings.Repeat("0", 64), Targets: []string{}, Included: len(cut), State: codebatch.SiblingDelivered}
		for _, candidate := range cut {
			sibling.Targets = append(sibling.Targets, candidate.Path)
		}
		listing = append(listing, sibling)
	}
	data, err := json.Marshal(listing)
	if err != nil {
		return 0
	}
	return len(data)
}

// cutParallelSiblings cuts up to N disjoint batches from the plan order, in
// memory: the first under this response's budget, the others as if their
// delivery carried the contract, every one under the budget less the bytes
// the listing of the siblings takes, found by iterating until the listing
// fits what was reserved for it.
func cutParallelSiblings(ordered []codebatch.Candidate, limit, parallel int, firstBudget, aheadBudget codebatch.Budget) [][]codebatch.Candidate {
	reserve := 0
	var cuts [][]codebatch.Candidate
	for iteration := 0; iteration < 4; iteration++ {
		held := map[string]bool{}
		cuts = nil
		for position := 0; position < parallel; position++ {
			budget := aheadBudget
			if position == 0 {
				budget = firstBudget
			}
			budget.Bytes -= reserve
			if budget.Bytes < 1<<10 {
				budget.Bytes = 1 << 10
			}
			selected := codebatch.CutSibling(ordered, held, limit, budget)
			if len(selected) == 0 {
				break
			}
			for _, candidate := range selected {
				held[candidate.ObjectRef] = true
			}
			cuts = append(cuts, selected)
		}
		listing := siblingListingBytes(cuts)
		if listing <= reserve {
			break
		}
		reserve = listing
	}
	return cuts
}

// buildParallelCodeCandidates is buildVolumeCodeCandidates under N > 1. A
// logical plan is the up-to-N siblings cut when it starts against one
// preimage; it lives while one of them is outstanding and completes when
// every one is applied or invalidated, and the next Maintain starts the next
// plan from the Volume as it then stands. That keeps the replay a plan's
// Apply proves, and the listing a response carries, to N siblings. The
// current batch is the first sibling no Host has been handed yet, so N
// agents calling no-argument Maintain each receive their own; when every
// outstanding sibling is already delivered, the first of them is delivered
// again, which is what a single agent sees between its Applies.
func buildParallelCodeCandidates(root string, loaded *cognitionRepoCtx, result *volumeMaintainResult, work volumegovernance.CodeAuthoringWork, snapshot map[string]baseline.Fingerprint, contractOnWire bool, parallel int) {
	all, lines := codeCandidatesForWork(root, loaded.set, work, snapshot)
	ids := parallelIdentitiesOf(result.Governance.Facts, loaded.set)
	plan := currentParallelPlan(root, ids)
	if len(all) == 0 {
		_ = codebatch.RemoveParallelPlans(root)
		return
	}
	limit := codeBatchLimit(loaded.cfg)
	var siblings []codebatch.Sibling
	var receipts map[string]codebatch.Receipt
	if plan != nil {
		siblings, receipts = parallelSiblingStates(root, plan)
		outstanding := 0
		for _, sibling := range siblings {
			if siblingOutstanding(sibling.State) {
				outstanding++
			}
		}
		if outstanding == 0 {
			_ = codebatch.RemoveParallelPlans(root)
			plan = nil
		}
	}
	firstBudget := codeCandidateBudget(loaded.cfg, loaded.set, all, lines, contractOnWire)
	if plan == nil {
		fresh := codebatch.NewParallelPlan(ids)
		ordered, _ := codebatch.Select(all, 0)
		cuts := cutParallelSiblings(ordered, limit, parallel, firstBudget, codeCandidateBudget(loaded.cfg, loaded.set, all, lines, true))
		if len(cuts) == 0 {
			return
		}
		for _, cut := range cuts {
			if fail := firstUnspellableCandidate(root, loaded, cut); fail != nil {
				result.Stop = fail.GlobalStop
				return
			}
		}
		if err := codebatch.SaveParallelPlan(root, fresh, loaded.set.Volumes[cognition.ScopeCode].Raw); err != nil {
			return
		}
		receipts = map[string]codebatch.Receipt{}
		siblings = nil
		for position, cut := range cuts {
			budget := firstBudget
			if position > 0 {
				budget = codeCandidateBudget(loaded.cfg, loaded.set, all, lines, true)
			}
			issued, err := codebatch.BuildSiblingPlan(root, fresh.LogicalPlan, ids.CompositeIdentity, ids.ScopePolicyIdentity,
				ids.CodeVolumePath, fresh.PreimageSHA256, all, cut, limit, budget)
			if err != nil {
				_ = codebatch.RemoveParallelPlans(root)
				return
			}
			receipt, loadErr := codebatch.LoadReceipt(root, issued.BatchID)
			if loadErr != nil {
				_ = codebatch.RemoveParallelPlans(root)
				return
			}
			fresh.Siblings = append(fresh.Siblings, issued.BatchID)
			receipts[issued.BatchID] = receipt
			paths := make([]string, 0, len(cut))
			for _, candidate := range cut {
				paths = append(paths, candidate.Path)
			}
			siblings = append(siblings, codebatch.Sibling{BatchID: issued.BatchID, Targets: paths, Included: len(paths), State: codebatch.SiblingIssued})
		}
		if err := codebatch.UpdateParallelPlan(root, fresh); err != nil {
			return
		}
		plan = &fresh
	}
	var current *codebatch.Sibling
	for index := range siblings {
		if siblings[index].State == codebatch.SiblingIssued {
			current = &siblings[index]
			break
		}
	}
	if current == nil {
		for index := range siblings {
			if siblingOutstanding(siblings[index].State) {
				current = &siblings[index]
				break
			}
		}
	}
	if current == nil {
		return
	}
	if current.State == codebatch.SiblingIssued {
		current.State = codebatch.SiblingDelivered
		plan.Delivered = append(plan.Delivered, current.BatchID)
		if err := codebatch.UpdateParallelPlan(root, *plan); err != nil {
			return
		}
	}
	receipt := receipts[current.BatchID]
	held := 0
	others := make([]codebatch.Sibling, 0, len(siblings))
	for _, sibling := range siblings {
		if siblingOutstanding(sibling.State) {
			held += sibling.Included
		}
		if sibling.BatchID != current.BatchID {
			others = append(others, sibling)
		}
	}
	remaining := len(all) - held
	if remaining < 0 {
		remaining = 0
	}
	outstandingOthers := 0
	for _, sibling := range others {
		if siblingOutstanding(sibling.State) {
			outstandingOthers++
		}
	}
	result.CodePlan = &codebatch.Plan{Version: codebatch.Version, PlanID: receipt.PlanID, BatchID: receipt.BatchID,
		CompositeIdentity: ids.CompositeIdentity, ScopePolicyIdentity: ids.ScopePolicyIdentity,
		CodeVolumePath: ids.CodeVolumePath, CodeVolumeSHA256: plan.PreimageSHA256,
		TotalTargets: len(all), MaxEntries: limit, Included: len(receipt.Targets), Remaining: remaining,
		CompleteCandidateSetForBatch: true, ContinuationRequired: remaining > 0 || outstandingOthers > 0,
		TransportBudgetBytes: firstBudget.Total, NextAction: "author_complete_current_machine_batch",
		LogicalPlan: plan.LogicalPlan, Siblings: others}
	result.Batch.TransportBudgetBytes = firstBudget.Total
	appendReceiptCandidates(root, loaded, result, receipt)
}

// appendReceiptCandidates turns a sibling's receipt targets into the response
// candidates, exactly as a single batch's plan candidates are.
func appendReceiptCandidates(root string, loaded *cognitionRepoCtx, result *volumeMaintainResult, receipt codebatch.Receipt) {
	thresholds := optimizationEScaleThresholds(loaded.set)
	for _, target := range receipt.Targets {
		item := volumeMaintainCandidate{Domain: cognition.ScopeCode,
			Change: target.Change, ObjectRef: target.ObjectRef, Path: target.Path,
			ExistingEntry: target.ExistingEntry, SourceSHA256: target.SourceSHA256,
			CandidateID: target.CandidateID, ModelAuthoringOnly: true}
		fillVolumeCandidateScale(root, thresholds, &item)
		result.Candidates = append(result.Candidates, item)
	}
}

// handleParallelSiblingDelivery serves aoci_maintain with batch_id: the
// issued sibling, complete, as the current batch; an applied or invalidated
// one answers its state and sends the Host back to no-argument Maintain.
func handleParallelSiblingDelivery(root, serviceVersion, batchID string, includeInstructions bool, loaded *cognitionRepoCtx, refreshSession *cognitionRefreshSession) *mcp.CallToolResult {
	start := time.Now()
	if loaded.cfg.CodeCognitionParallelBatchLimit() <= 1 {
		return failResult(&Fail{Code: errBadArgs, Msg: "maintain_batch_id_requires_parallel_batches"})
	}
	facts, _, err := volumegovernance.AssessWithSnapshot(root, loaded.cfg, loaded.set)
	if err != nil {
		return failResult(&Fail{Code: errInternal, Msg: "volumes_governance_facts_invalid"})
	}
	plan, loadErr := codebatch.LoadParallelPlan(root)
	if loadErr != nil || plan == nil || !plan.HasSibling(batchID) {
		return failResult(&Fail{Code: errBadArgs, Msg: "maintain_batch_id_unknown", Hint: writeMessage("entry.batch.hint.replan")})
	}
	ids := parallelIdentitiesOf(facts, loaded.set)
	result := volumeMaintainResult{
		Version: 1, Status: autoStatusStopped, Result: facts.Result, Aligned: false,
		RequestedScope: "", AffectedDomains: append([]string{}, facts.AffectedDomains...),
		Candidates: []volumeMaintainCandidate{}, OrphanRemovals: []string{},
		Sets:       volumeMaintainSets{Review: []string{}, Write: []string{}, Guard: []string{"root", "meta", cognition.ScopeCode}},
		Governance: &volumegovernance.TransportFacts{Facts: facts}, Receipt: newVolumeCognitionReceipt(root, serviceVersion, loaded.set, mustVolumeScope(loaded.set)),
		Metrics: autoMetrics{AOCIToolCalls: 1}, SemanticGenerated: false, NetworkAccessed: false,
		ServiceBinaryReplacedOnDisk: serviceBinaryReplacedOnDisk(),
		Batch: volumeAuthoringBatch{MaxEntries: codeBatchLimit(loaded.cfg),
			CompositeIdentity: facts.CompositeIdentity, ScopePolicyIdentity: facts.ManagedScope.PolicyIdentity},
	}
	proven := false
	if plan.PlanHolds(ids) {
		if ok, replayErr := codebatch.ReplayProves(root, root, plan, ids.CodeVolumeSHA256); replayErr == nil && ok {
			proven = true
		}
	}
	siblings, receipts := parallelSiblingStates(root, plan)
	changed := false
	var requested *codebatch.Sibling
	for index := range siblings {
		if siblings[index].BatchID == batchID {
			requested = &siblings[index]
		}
	}
	state := codebatch.SiblingInvalidated
	if proven && requested != nil {
		state = requested.State
	}
	others := make([]codebatch.Sibling, 0, len(siblings))
	for _, sibling := range siblings {
		if sibling.BatchID != batchID {
			others = append(others, sibling)
		}
	}
	result.CodePlan = &codebatch.Plan{Version: codebatch.Version, BatchID: batchID, CompositeIdentity: ids.CompositeIdentity,
		ScopePolicyIdentity: ids.ScopePolicyIdentity, CodeVolumePath: ids.CodeVolumePath, CodeVolumeSHA256: plan.PreimageSHA256,
		MaxEntries: codeBatchLimit(loaded.cfg), LogicalPlan: plan.LogicalPlan, SiblingOf: plan.LogicalPlan, Siblings: others}
	switch state {
	case codebatch.SiblingIssued, codebatch.SiblingDelivered:
		receipt := receipts[batchID]
		if state == codebatch.SiblingIssued {
			plan.Delivered = append(plan.Delivered, batchID)
			changed = true
		}
		if changed {
			_ = codebatch.UpdateParallelPlan(root, *plan)
		}
		appendReceiptCandidates(root, loaded, &result, receipt)
		held := 0
		for _, sibling := range siblings {
			if siblingOutstanding(sibling.State) {
				held += sibling.Included
			}
		}
		work := volumegovernance.CodeAuthoringWorkFor(loaded.set, facts.CodeDrift)
		remaining := len(work.Targets) - held
		if remaining < 0 {
			remaining = 0
		}
		result.CodePlan.PlanID, result.CodePlan.Included, result.CodePlan.Remaining = receipt.PlanID, len(receipt.Targets), remaining
		result.CodePlan.TotalTargets, result.CodePlan.CompleteCandidateSetForBatch = len(work.Targets), true
		outstandingOthers := 0
		for _, sibling := range others {
			if siblingOutstanding(sibling.State) {
				outstandingOthers++
			}
		}
		result.CodePlan.ContinuationRequired = remaining > 0 || outstandingOthers > 0
		result.CodePlan.NextAction = "author_complete_current_machine_batch"
		result.Status, result.Result, result.NextAction = autoStatusRepairRequired, volumegovernance.ResultAuthoringRequired, "author_complete_current_machine_batch"
		result.Batch.TotalTargets, result.Batch.Included, result.Batch.Remaining = len(work.Targets), len(receipt.Targets), remaining
		result.Batch.CompleteCandidateSet, result.Batch.ContinuationRequired = true, result.CodePlan.ContinuationRequired
		result.Sets.Write = candidateRefs(result.Candidates)
		result.Sets.Review = reviewClosure(loaded.set, result.Candidates)
		contract, contractErr := authoringcontract.Build(loaded.set.Meta.Raw, []string{cognition.ScopeCode}, textassets.ActiveLocale())
		if contractErr != nil {
			return failResult(&Fail{Code: errInternal, Msg: "volume_authoring_contract_invalid"})
		}
		attachAuthoringContract(&result, contract, nil, refreshSession, includeInstructions)
	default:
		// Applied, or invalidated by a foreign change or its own source drift:
		// the state is the answer, and no-argument Maintain plans afresh.
		if changed {
			_ = codebatch.UpdateParallelPlan(root, *plan)
		}
		listed := codebatch.Sibling{BatchID: batchID, Targets: []string{}, State: state}
		if requested != nil {
			listed.Targets, listed.Included = requested.Targets, requested.Included
		}
		result.CodePlan.Siblings = append([]codebatch.Sibling{listed}, others...)
		result.Result, result.NextAction = volumegovernance.ResultBlocked, machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch
		result.CodePlan.NextAction = result.NextAction
	}
	result.Batch.LogicalPlan, result.Batch.BatchIdentity = volumeBatchIdentities(result)
	result.Batch.NextAction = result.NextAction
	result.Metrics.DeterministicMs = elapsedMilliseconds(start)
	result.Metrics.SemanticFiles = len(result.Candidates)
	ledgerResult := ledger.ResultError
	if result.Status == autoStatusRepairRequired {
		ledgerResult = ledger.ResultRepairRequired
	}
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "maintain", Source: ledger.SourceAgent,
		Result: ledgerResult, PathsCount: len(result.Candidates), DurationMs: result.Metrics.DeterministicMs,
		AOCIToolCalls: 1, SemanticFiles: len(result.Candidates), GovernanceResult: result.Result,
		NextAction: result.NextAction, FormalWritesStarted: ledgerFlag(false), BatchID: batchID})
	boundMaintainTransport(&result)
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return errResult(errInternal, "volume_maintain_result_invalid", "")
	}
	return textResult(string(data) + "\n")
}
