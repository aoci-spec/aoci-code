// Parallel current batches (L4): under code_cognition_parallel_batches > 1,
// Maintain keeps up to N sibling batches of one logical plan outstanding,
// delivers the first as the current batch, lists the others, and delivers
// any of them by batch_id. Everything a sibling binds is the same as a single
// batch's; what differs is the preimage rule the Apply judges it by, which
// lives with the commit (tools_write_volume_commit.go).
package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
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

type codePlanAssessment struct {
	baselineSHA256  string
	planMarker      string
	restore         bool
	recoveryPending bool
}

// beforeMaintainIndexLock lets tests interleave another session after load.
var beforeMaintainIndexLock = func(string) {}

// lockPlanOnLoadedVolume checks the loaded Code Volume and any assessment
// made before the lock against the state Apply last left. A restore also
// depends on every formal asset it would overwrite and on Recovery state.
func lockPlanOnLoadedVolume(root string, loaded *cognitionRepoCtx, assessment *codePlanAssessment) (release func(), moved bool, err error) {
	beforeMaintainIndexLock(root)
	lock, lockErr := afs.AcquireIndexLock(root)
	if lockErr != nil {
		return nil, false, lockErr
	}
	assets := []*cognition.Asset{loaded.set.Volumes[cognition.ScopeCode]}
	if assessment != nil && assessment.restore {
		assets = mustVolumeScope(loaded.set).Assets
	}
	currentState := true
	for _, asset := range assets {
		if asset == nil || asset.State != cognition.AssetPresent {
			continue
		}
		current, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(asset.Descriptor.Path)))
		if readErr != nil || codebatch.BytesSHA256(current) != asset.SHA256 {
			currentState = false
			break
		}
	}
	if currentState && assessment != nil {
		// A rebind can advance the Baseline while leaving the Code Volume
		// and the solo mark byte-identical if the mark rewrite failed.
		current, readErr := os.ReadFile(loaded.paths.BaselinePath)
		currentState = readErr == nil && codebatch.BytesSHA256(current) == assessment.baselineSHA256 &&
			codebatch.ParallelPlanMarker(root) == assessment.planMarker
	}
	if currentState && assessment != nil && assessment.restore {
		pending, pendingErr := cognitiontxn.Pending(root)
		currentState = pendingErr == nil && (len(pending) > 0) == assessment.recoveryPending
	}
	if !currentState {
		_ = lock.Release()
		return nil, true, nil
	}
	return func() { _ = lock.Release() }, false, nil
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

// parallelPlanState reads the plan on record under the caller's lock:
// whether a sibling of it is still issued or delivered, which a Maintain
// must keep serving even when what is left would fit one batch, and whether
// one session is working it alone (see siblingsNeverFetched).
func parallelPlanState(root string, ids codebatch.ParallelIdentities) (outstanding, alone bool) {
	plan := currentParallelPlan(root, ids)
	if plan == nil {
		return false, false
	}
	siblings, _ := parallelSiblingStates(root, plan)
	for _, sibling := range siblings {
		if siblingOutstanding(sibling.State) {
			outstanding = true
		}
	}
	return outstanding, siblingsNeverFetched(siblings)
}

// siblingsNeverFetched reports a plan one session is working alone: a
// sibling of it has applied while every other outstanding one was never
// handed to anyone. The caller releases those and marks the build solo, so
// a Host without subagents gets full single batches from then on instead of
// the smaller batches cut for fresh sessions. A sibling any session was
// handed keeps the plan; an Agent that asks for a released one by batch_id
// is sent back to no-argument Maintain.
func siblingsNeverFetched(siblings []codebatch.Sibling) bool {
	applied, issued := false, false
	for _, sibling := range siblings {
		switch sibling.State {
		case codebatch.SiblingApplied:
			applied = true
		case codebatch.SiblingIssued:
			issued = true
		case codebatch.SiblingDelivered:
			return false
		}
	}
	return applied && issued
}

// codeSiblingsOutstandingOnDisk reports a parallel Code plan that still holds
// over the current assets with a sibling issued or delivered and not yet
// applied. A Database batch applied then stales every such sibling, so no
// Maintain entry issues one while this holds (audit G2-05, rv34: the explicit
// Database scope went around the response-level check).
func codeSiblingsOutstandingOnDisk(root string, facts *volumegovernance.Facts, set *cognition.Set) bool {
	plan, err := codebatch.LoadParallelPlan(root)
	ids := parallelIdentitiesOf(facts, set)
	if err != nil || plan == nil || len(plan.Siblings) == 0 || !plan.PlanHolds(ids) {
		return false
	}
	// A plan the current Code Volume no longer replays from (a foreign Code
	// write landed) holds no live sibling, as currentParallelPlan judges; it
	// is only read here, never dropped, since this check takes no lock (rv35).
	if proven, replayErr := codebatch.ReplayProves(root, root, plan, ids.CodeVolumeSHA256); replayErr != nil || !proven {
		return false
	}
	siblings, _ := parallelSiblingStates(root, plan)
	for _, sibling := range siblings {
		if siblingOutstanding(sibling.State) {
			return true
		}
	}
	return false
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
		listing = append(listing, codebatch.Sibling{BatchID: strings.Repeat("0", 64), Included: len(cut), State: codebatch.SiblingInvalidated})
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
		remaining := ordered
		cuts = nil
		for position := 0; position < parallel; position++ {
			budget := aheadBudget
			if position == 0 {
				// A held batch can be recalled with the complete contract,
				// including a lead batch first sent with a reference.
				budget = firstBudget
				budget.Bytes = min(firstBudget.Bytes, aheadBudget.Bytes)
			}
			budget.Bytes -= reserve
			if budget.Bytes < 1<<10 {
				budget.Bytes = 1 << 10
			}
			selected := cutMaintainCodeBatch(remaining, limit, budget)
			if len(selected) == 0 {
				break
			}
			cuts = append(cuts, selected)
			remaining = remaining[len(selected):]
		}
		listing := siblingListingBytes(cuts)
		// A sibling delivered by batch_id also names its plan in sibling_of.
		listing += len(`,"sibling_of":""`) + 64
		if len(cuts) > 1 {
			// The response that lists siblings also carries the one line that
			// says what to do with them.
			if line, err := json.Marshal(mcpMessage("maintain.parallel_siblings_instruction", len(cuts)-1)); err == nil {
				listing += len(line) + 1
			}
		}
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
func buildParallelCodeCandidates(root string, loaded *cognitionRepoCtx, result *volumeMaintainResult, all []codebatch.Candidate, lines map[string]int, cutLimit int, contractOnWire bool, parallel int, session *cognitionRefreshSession) {
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
	var publishFresh func() error
	if plan == nil {
		fresh := codebatch.NewParallelPlan(ids)
		cuts := cutParallelSiblings(all, cutLimit, parallel, firstBudget, codeCandidateBudget(loaded.cfg, loaded.set, all, lines, true))
		if len(cuts) == 0 {
			return
		}
		receipts = map[string]codebatch.Receipt{}
		siblings = nil
		publishers := make([]func(string) error, 0, len(cuts))
		for _, cut := range cuts {
			if fail := firstUnspellableCandidate(root, loaded, cut); fail != nil {
				result.Stop = fail.GlobalStop
				return
			}
			issued, publish, err := codebatch.PrepareBatch(ids, fresh.LogicalPlan, all, cut, limit, firstBudget.Total)
			if err != nil {
				result.issueFailure = err.Error()
				return
			}
			publishers = append(publishers, publish)
			fresh.Siblings = append(fresh.Siblings, issued.BatchID)
			paths := make([]string, 0, len(cut))
			targets := make([]codebatch.Target, 0, len(cut))
			for _, candidate := range issued.Candidates {
				paths = append(paths, candidate.Path)
				targets = append(targets, candidate.Target)
			}
			receipts[issued.BatchID] = codebatch.Receipt{PlanID: issued.PlanID, BatchID: issued.BatchID, Targets: targets}
			siblings = append(siblings, codebatch.Sibling{BatchID: issued.BatchID, Targets: paths, Included: len(paths), State: codebatch.SiblingIssued})
		}
		plan = &fresh
		publishFresh = func() error {
			for _, publish := range publishers {
				if err := publish(root); err != nil {
					return err
				}
			}
			return codebatch.SaveParallelPlan(root, *plan, loaded.set.Volumes[cognition.ScopeCode].Raw)
		}
	}
	// The sibling this server was already handed, while it is still out, is
	// the current batch again: one session asking twice gets one batch, and
	// the others stay unfetched for the Agents that will ask by batch_id.
	var current *codebatch.Sibling
	if held := session.HeldSibling(); held != "" {
		for index := range siblings {
			if siblings[index].BatchID == held && siblingOutstanding(siblings[index].State) {
				current = &siblings[index]
			}
		}
	}
	for index := range siblings {
		if current == nil && siblings[index].State == codebatch.SiblingIssued {
			current = &siblings[index]
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
	deliver := current.State == codebatch.SiblingIssued
	previousID := current.BatchID
	result.codeOrphansRide = firstSiblingDelivery(plan, previousID)
	result.deliveredBatch = !deliver
	receipt := receipts[previousID]
	var publishResized func(string) error
	if deliver && cutLimit < len(receipt.Targets) {
		var err error
		receipt, publishResized, err = prepareSiblingPrefix(plan, receipt, cutLimit, limit, firstBudget.Total)
		if err != nil {
			result.issueFailure = err.Error()
			return
		}
		current.BatchID, current.Included = receipt.BatchID, len(receipt.Targets)
	}
	batchID := receipt.BatchID
	if deliver {
		current.State = codebatch.SiblingDelivered
	}
	result.publishCodeBatch = func() error {
		if deliver {
			if publishFresh != nil {
				plan.Delivered = append(plan.Delivered, batchID)
				if err := publishFresh(); err != nil {
					return err
				}
			} else if err := publishSiblingDelivery(root, plan, previousID, batchID, publishResized); err != nil {
				return err
			}
		}
		session.HoldSibling(batchID)
		return nil
	}
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
	if publishFresh != nil {
		result.checkParallelBudget = func(wire volumeMaintainResult) (int, error) {
			return parallelDeliveryCutLimit(root, loaded, wire, siblings, receipts)
		}
	} else if deliver {
		// A replacement must also fit when fetched without a contract memo.
		result.checkParallelBudget = func(wire volumeMaintainResult) (int, error) {
			return parallelDeliveryCutLimit(root, loaded, wire, nil, nil)
		}
	}
}

func firstSiblingDelivery(plan *codebatch.ParallelPlan, id string) bool {
	return len(plan.Delivered) == 0 || plan.Delivered[0] == id
}

// Only the targets held by this undelivered sibling can enter its replacement.
// Its suffix is released to later Maintain plans, and delivered siblings retain
// their original identities and replay preimage.
func prepareSiblingPrefix(plan *codebatch.ParallelPlan, receipt codebatch.Receipt, count, limit, budget int) (codebatch.Receipt, func(string) error, error) {
	if count >= len(receipt.Targets) {
		return receipt, nil, nil
	}
	all := make([]codebatch.Candidate, 0, len(receipt.Targets))
	for _, target := range receipt.Targets {
		all = append(all, codebatch.Candidate{Target: target})
	}
	ids := codebatch.ParallelIdentities{CompositeIdentity: plan.CompositeIdentity, ScopePolicyIdentity: plan.ScopePolicyIdentity,
		CodeVolumePath: plan.CodeVolumePath, CodeVolumeSHA256: plan.PreimageSHA256}
	batch, publish, err := codebatch.PrepareBatch(ids, plan.LogicalPlan, all, all[:count], limit, budget)
	if err != nil {
		return codebatch.Receipt{}, nil, err
	}
	receipt.PlanID, receipt.BatchID = batch.PlanID, batch.BatchID
	receipt.Targets = make([]codebatch.Target, 0, len(batch.Candidates))
	for _, candidate := range batch.Candidates {
		receipt.Targets = append(receipt.Targets, candidate.Target)
	}
	return receipt, publish, nil
}

// This is called only after measurement and while holding the index lock.
// Updating the plan membership invalidates the old receipt for Apply at the
// same point that the replacement is marked delivered.
func publishSiblingDelivery(root string, plan *codebatch.ParallelPlan, previousID, batchID string, publish func(string) error) error {
	if publish != nil {
		if err := publish(root); err != nil {
			return err
		}
		for i, id := range plan.Siblings {
			if id == previousID {
				plan.Siblings[i] = batchID
				break
			}
		}
	}
	plan.Delivered = append(plan.Delivered, batchID)
	return codebatch.UpdateParallelPlan(root, *plan)
}

// parallelDeliveryCutLimit tests the full-contract form a fresh worker (or a
// lead recalling the contract) can request, before publishing any sibling.
// A zero result accepts the cuts. Otherwise the result caps the next attempt.
func parallelDeliveryCutLimit(root string, loaded *cognitionRepoCtx, lead volumeMaintainResult, siblings []codebatch.Sibling, receipts map[string]codebatch.Receipt) (int, error) {
	contract, err := buildAuthoringContract(loaded.cfg, loaded.set.Meta.Raw, []string{cognition.ScopeCode})
	if err != nil {
		return 0, err
	}
	if lead.InstructionsRef != nil {
		lead.Instructions = append(append([]string{}, lead.Instructions...), contract.Instructions...)
		lead.AuthoringMeta, lead.InstructionsRef = contract.AuthoringMeta, nil
	}
	_, fits, err := maintainPayload(lead, loaded.cfg)
	if err != nil {
		return 0, err
	}
	if !fits {
		return len(lead.Candidates) - 1, nil
	}
	for _, sibling := range siblings {
		if sibling.BatchID == lead.CodePlan.BatchID {
			continue
		}
		wire := lead
		wire.OrphanRemovals = nil
		wire.Candidates = nil
		receipt := receipts[sibling.BatchID]
		appendReceiptCandidates(root, loaded, &wire, receipt)
		wire.Sets.Write = candidateRefs(wire.Candidates)
		wire.Sets.Review = reviewClosure(loaded.set, wire.Candidates)
		wire.Batch.Included = len(wire.Candidates)
		wire.Batch.SourceLines, wire.Batch.SourceLinesBudget = batchSourceLines(wire.Candidates)
		plan := *lead.CodePlan
		plan.PlanID, plan.BatchID, plan.Included = receipt.PlanID, receipt.BatchID, len(wire.Candidates)
		plan.SiblingOf = plan.LogicalPlan
		plan.Siblings = nil
		for _, other := range siblings {
			if other.BatchID != receipt.BatchID {
				plan.Siblings = append(plan.Siblings, other)
			}
		}
		wire.CodePlan = &plan
		_, fits, err := maintainPayload(wire, loaded.cfg)
		if err != nil {
			return 0, err
		}
		if !fits {
			return len(wire.Candidates) - 1, nil
		}
	}
	return 0, nil
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
	// A sibling is read, judged, and marked delivered under the index lock on
	// the Volume this call loaded; when another sibling's Apply landed in
	// between, the call starts over instead of calling the plan broken.
	for attempt := 1; ; attempt++ {
		if loaded.set.LayoutMode != cognition.LayoutVolumesV1 {
			return handleLegacyReadOnlyMaintain(root, serviceVersion)
		}
		release, moved, lockErr := lockPlanOnLoadedVolume(root, loaded, nil)
		if lockErr != nil {
			return failResult(maintainLockFailure(lockErr))
		}
		if !moved {
			cfg, fail := loadMCPConfig(root)
			if fail != nil {
				release()
				return failResult(fail)
			}
			if cfg.IndexPath == loaded.cfg.IndexPath {
				loaded.cfg = cfg
				defer release()
				break
			}
			release()
		}
		reloaded, failure := reloadMaintainContext(root, attempt)
		if failure != nil {
			return failure
		}
		loaded = reloaded
	}
	if loaded.cfg.CodeCognitionParallelBatchLimit() <= 1 {
		return failResult(&Fail{Code: errBadArgs, Msg: "maintain_batch_id_requires_parallel_batches"})
	}
	start := time.Now()
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
	deliver := false
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
	// The repository's own stop comes first, as on the no-argument path: a
	// pending recovery, a budget or Locale block, or owed Evidence leaves the
	// plan unchanged instead of handing out a batch every Apply will refuse
	// (audit G2-06). Observe evidence the batch can confirm still rides.
	observeRides := facts.NextRequiredAction == machinecontract.ActionReviewObservedEvidence && observeReviewRidesWithBatch(root, facts)
	if facts.Result == volumegovernance.ResultEvidenceRequired || (facts.Result == volumegovernance.ResultBlocked && !observeRides) {
		result.Status, result.Aligned, result.Result = autoStatusStopped, false, facts.Result
		if facts.Result == volumegovernance.ResultEvidenceRequired {
			result.NextAction = facts.NextRequiredAction
			result.NextCommands = evidenceNextCommands(root)
		} else {
			result.NextAction = blockedNextAction(facts, nil)
			result.NextCommands = blockedNextCommands(root, facts, nil)
			result.ObserveReview = observeReviewFor(facts)
		}
		result.CodePlan.NextAction = result.NextAction
		state = ""
	}
	base := result
	cutLimit := codeBatchLimit(loaded.cfg)
	var data []byte
	var receipt codebatch.Receipt
	var publish func(string) error
	for {
		result = base
		planProjection := *base.CodePlan
		result.CodePlan = &planProjection
		switch state {
		case "":
		case codebatch.SiblingIssued, codebatch.SiblingDelivered:
			original := receipts[batchID]
			receipt = original
			deliver = state == codebatch.SiblingIssued
			result.deliveredBatch = !deliver
			if deliver {
				receipt, publish, err = prepareSiblingPrefix(plan, original, cutLimit, codeBatchLimit(loaded.cfg), maintainTransportBudget(loaded.cfg))
				if err != nil {
					return failResult(&Fail{Code: errInternal, Msg: "code_parallel_plan_issue_failed", Hint: writeMessage("entry.batch.hint.replan")})
				}
			}
			appendReceiptCandidates(root, loaded, &result, receipt)
			held := 0
			for _, sibling := range siblings {
				if siblingOutstanding(sibling.State) {
					held += sibling.Included
				}
			}
			held -= len(original.Targets) - len(receipt.Targets)
			work := volumegovernance.CodeAuthoringWorkFor(loaded.set, facts.CodeDrift)
			remaining := len(work.Targets) - held
			if remaining < 0 {
				remaining = 0
			}
			result.CodePlan.BatchID = receipt.BatchID
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
			result.Batch.TransportBudgetBytes = maintainTransportBudget(loaded.cfg)
			result.CodePlan.TransportBudgetBytes = result.Batch.TransportBudgetBytes
			result.Batch.SourceLines, result.Batch.SourceLinesBudget = batchSourceLines(result.Candidates)
			result.Batch.CompleteCandidateSet, result.Batch.ContinuationRequired = true, result.CodePlan.ContinuationRequired
			result.Sets.Write = candidateRefs(result.Candidates)
			result.Sets.Review = reviewClosure(loaded.set, result.Candidates)
			contract, contractErr := buildAuthoringContract(loaded.cfg, loaded.set.Meta.Raw, []string{cognition.ScopeCode})
			if contractErr != nil {
				return failResult(&Fail{Code: errInternal, Msg: "volume_authoring_contract_invalid"})
			}
			var lead []string
			if observeRides {
				result.ObserveReview = observeReviewForBatch(facts)
				lead = append(lead, mcpMessage("maintain.observe_review_in_batch_instruction", result.ObserveReview.ReviewDigest))
			}
			addMaintainCodeOrphans(&result, facts.CodeDrift.Orphan, firstSiblingDelivery(plan, batchID))
			if len(result.OrphanRemovals) != 0 {
				lead = append(lead, mcpMessage("maintain.orphan_removals_instruction"))
			}
			attachAuthoringContract(&result, contract, lead, refreshSession, includeInstructions)
		default:
			// Applied, or invalidated by a foreign change or its own source drift:
			// the state is the answer, and no-argument Maintain plans afresh.
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
		var fits bool
		data, fits, err = maintainPayload(result, loaded.cfg)
		if err != nil {
			return failResult(&Fail{Code: errInternal, Msg: "volumes_maintain_result_invalid"})
		}
		if fits && deliver && result.InstructionsRef != nil {
			cut, err := parallelDeliveryCutLimit(root, loaded, result, nil, nil)
			if err != nil {
				return failResult(&Fail{Code: errInternal, Msg: "volumes_maintain_result_invalid"})
			}
			if cut > 0 {
				cutLimit = cut
				continue
			}
		}
		if fits {
			break
		}
		cutLimit = len(result.Candidates) - 1
	}
	if deliver {
		if err := publishSiblingDelivery(root, plan, batchID, receipt.BatchID, publish); err != nil {
			return failResult(&Fail{Code: errInternal, Msg: "code_parallel_plan_issue_failed", Hint: writeMessage("entry.batch.hint.replan")})
		}
	}
	response := maintainResponse(data, result, refreshSession)
	ledgerResult := ledger.ResultError
	if result.Status == autoStatusRepairRequired && !response.IsError {
		ledgerResult = ledger.ResultRepairRequired
	}
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "maintain", Source: ledger.SourceAgent,
		Result: ledgerResult, PathsCount: len(result.Candidates), DurationMs: result.Metrics.DeterministicMs,
		AOCIToolCalls: 1, SemanticFiles: len(result.Candidates), GovernanceResult: result.Result,
		NextAction: result.NextAction, FormalWritesStarted: ledgerFlag(false), BatchID: result.CodePlan.BatchID})
	return response
}
