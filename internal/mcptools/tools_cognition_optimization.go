package mcptools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/cognitionoptimization"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

const cognitionOptimizationVersion = "cognition-optimization/v1"

var advanceCognitionOptimizationCheckpoint = cognitionoptimization.Advance

type cognitionOptimizationCost struct {
	FTokens       int `json:"f_tokens"`
	RTokens       int `json:"r_tokens"`
	ATokens       int `json:"a_tokens"`
	STokens       int `json:"s_tokens"`
	TotalTokens   int `json:"total_tokens"`
	RTargetTokens int `json:"r_target_tokens"`
	RMaxTokens    int `json:"r_max_tokens"`
	STargetTokens int `json:"s_target_tokens"`
	SMaxTokens    int `json:"s_max_tokens"`
}

type cognitionOptimizationStatus struct {
	Version              string `json:"version"`
	OptimizationID       string `json:"optimization_id,omitempty"`
	State                string `json:"state"`
	CurrentBatchID       string `json:"current_batch_id,omitempty"`
	TotalTargets         int    `json:"total_targets"`
	Included             int    `json:"included"`
	Reviewed             int    `json:"reviewed"`
	NoChange             int    `json:"no_change"`
	Replaced             int    `json:"replaced"`
	Remaining            int    `json:"remaining"`
	ContinuationRequired bool   `json:"continuation_required"`
}

func handleCognitionOptimizationMaintain(
	root, serviceVersion string,
	input maintainIn,
	loaded *cognitionRepoCtx,
) *mcp.CallToolResult {
	start := time.Now()
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return failResult(&Fail{Code: errInternal, Msg: "cognition_optimization_governance_invalid"})
	}
	if facts.Result != volumegovernance.ResultAligned || !facts.GovernanceAligned {
		return failResult(&Fail{Code: errCandidateInvalid, Msg: "cognition_optimization_requires_ordinary_alignment"})
	}
	if !facts.Code.Enabled || loaded.set.Volumes[cognition.ScopeCode] == nil {
		return failResult(&Fail{Code: errCandidateInvalid, Msg: "cognition_optimization_code_volume_absent"})
	}
	if len(facts.Budget.Violations) != 0 {
		return failResult(&Fail{Code: errCandidateInvalid, Msg: "cognition_optimization_requires_budget_repair_first"})
	}

	stored, loadErr := cognitionoptimization.Load(root)
	switch {
	case loadErr == nil && !stored.Checkpoint.Completed:
		if len(input.ObjectRefs) != 0 {
			return failResult(&Fail{Code: errBadArgs, Msg: "cognition_optimization_object_refs_only_allowed_on_initial_request"})
		}
	case loadErr == nil && stored.Checkpoint.Completed:
		// A completed checkpoint is only a local anti-duplication boundary for
		// the preceding request. A later explicit intent deliberately starts a
		// fresh review without inventing a Policy or Capability epoch.
	case errors.Is(loadErr, cognitionoptimization.ErrCheckpointNotFound):
	case loadErr != nil:
		return failResult(&Fail{Code: errCandidateInvalid, Msg: "cognition_optimization_checkpoint_invalid"})
	}

	var selection cognitionoptimization.Selection
	var plan codebatch.Plan
	var checkpoint cognitionoptimization.StoredCheckpoint
	if loadErr == nil && !stored.Checkpoint.Completed {
		checkpoint = stored
		if stored.Checkpoint.CurrentBatchID != "" {
			plan, selection, err = loadOptimizationCurrentBatch(root, loaded, facts, stored.Checkpoint)
			if err != nil {
				// Ordinary governance is already aligned and recovery-free here. A
				// stale or missing draft receipt can therefore be safely replaced by
				// a fresh Code Batch over the exact unchanged checkpoint prefix.
				plan, selection, err = planOptimizationBatch(root, loaded, facts, stored.Checkpoint.RemainingObjectRefs)
				if err == nil {
					checkpoint, err = cognitionoptimization.RebindBatch(root, stored.SHA256, stored.Checkpoint.OptimizationID,
						stored.Checkpoint.CurrentBatchID, plan.BatchID)
				}
			}
		} else {
			plan, selection, err = planOptimizationBatch(root, loaded, facts, stored.Checkpoint.RemainingObjectRefs)
			if err == nil {
				checkpoint, err = cognitionoptimization.BindBatch(root, stored.SHA256, stored.Checkpoint.OptimizationID, plan.BatchID)
			}
		}
	} else {
		selection, err = selectOptimizationTargets(root, loaded, input.ObjectRefs)
		if err == nil && selection.TotalTargets > 0 {
			plan, _, err = buildOptimizationCodePlan(root, facts, selection.Batch)
		}
		if err == nil && selection.TotalTargets > 0 {
			orderedRefs := optimizationSelectionRefs(selection)
			optimizationID, idErr := cognitionOptimizationID(loaded, facts, orderedRefs)
			if idErr != nil {
				err = idErr
			} else {
				create := cognitionoptimization.CreateInput{OptimizationID: optimizationID,
					CurrentBatchID: plan.BatchID, RemainingObjectRefs: orderedRefs}
				if loadErr == nil && stored.Checkpoint.Completed {
					checkpoint, err = cognitionoptimization.RestartCompleted(root, stored.SHA256, create)
				} else {
					checkpoint, err = cognitionoptimization.Create(root, create)
				}
			}
		}
	}
	if err != nil {
		return failResult(&Fail{Code: errCandidateInvalid, Msg: "cognition_optimization_plan_invalid: " + err.Error()})
	}
	if selection.TotalTargets == 0 {
		return renderEmptyCognitionOptimization(root, serviceVersion, loaded, facts, start)
	}

	orderedPlan := reorderOptimizationPlan(plan, selection.Batch)
	result := volumeMaintainResult{
		Version: 1, Status: autoStatusApplied, Result: volumegovernance.ResultAligned, Aligned: true,
		RequestedScope: input.Scope, AffectedDomains: []string{}, Candidates: []volumeMaintainCandidate{},
		OrphanRemovals: []string{}, Sets: volumeMaintainSets{Review: []string{}, Write: []string{}, Guard: []string{"root", "meta", cognition.ScopeCode}},
		CodePlan: &orderedPlan, Governance: facts,
		Receipt: newVolumeCognitionReceipt(root, serviceVersion, loaded.set, mustVolumeScope(loaded.set)),
		Metrics: autoMetrics{AOCIToolCalls: 1}, SemanticGenerated: false, NetworkAccessed: false,
		NextAction: "review_complete_cognition_optimization_batch",
	}
	for _, candidate := range selection.Batch {
		issued := optimizationPlanCandidate(orderedPlan, candidate.ObjectRef)
		if issued == nil {
			return failResult(&Fail{Code: errInternal, Msg: "cognition_optimization_candidate_identity_missing"})
		}
		result.Candidates = append(result.Candidates, optimizationMaintainCandidate(candidate, *issued, orderedPlan.BatchID, len(input.ObjectRefs) != 0))
	}
	result.Sets.Write = candidateRefs(result.Candidates)
	result.Sets.Review = reviewClosure(loaded.set, result.Candidates)
	result.Batch = volumeAuthoringBatch{
		LogicalPlan: orderedPlan.PlanID, BatchIdentity: orderedPlan.BatchID,
		TotalTargets: checkpoint.Checkpoint.ReviewedCount + len(checkpoint.Checkpoint.RemainingObjectRefs),
		MaxEntries:   machinecontract.EntriesBatchMaxItems, Included: len(selection.Batch),
		Remaining:            len(checkpoint.Checkpoint.RemainingObjectRefs) - len(selection.Batch),
		CompleteCandidateSet: true,
		ContinuationRequired: len(checkpoint.Checkpoint.RemainingObjectRefs) > len(selection.Batch),
		CompositeIdentity:    facts.CompositeIdentity, ScopePolicyIdentity: facts.ManagedScope.PolicyIdentity,
		NextAction: "review_complete_cognition_optimization_batch",
	}
	result.Optimization = optimizationStatus(checkpoint.Checkpoint, len(selection.Batch), "review_required")
	contract, contractErr := authoringcontract.Build(loaded.set.Meta.Raw, []string{cognition.ScopeCode}, textassets.ActiveLocale())
	if contractErr != nil {
		return failResult(&Fail{Code: errInternal, Msg: "cognition_optimization_authoring_contract_invalid"})
	}
	result.AuthoringMeta = contract.AuthoringMeta
	result.Instructions = contract.Instructions
	result.Metrics.DeterministicMs = elapsedMilliseconds(start)
	result.Metrics.SemanticFiles = len(result.Candidates)
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "maintain", Source: ledger.SourceAgent,
		Result: ledger.ResultOK, PathsCount: len(result.Candidates), DurationMs: result.Metrics.DeterministicMs,
		AOCIToolCalls: 1, SemanticFiles: len(result.Candidates)})
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return failResult(&Fail{Code: errInternal, Msg: "cognition_optimization_result_invalid"})
	}
	return textResult(string(data) + "\n")
}

func renderEmptyCognitionOptimization(root, serviceVersion string, loaded *cognitionRepoCtx, facts *volumegovernance.Facts, start time.Time) *mcp.CallToolResult {
	result := volumeMaintainResult{
		Version: 1, Status: autoStatusApplied, Result: volumegovernance.ResultAligned, Aligned: true,
		AffectedDomains: []string{}, Candidates: []volumeMaintainCandidate{}, OrphanRemovals: []string{},
		Sets:       volumeMaintainSets{Review: []string{}, Write: []string{}, Guard: []string{"root", "meta", cognition.ScopeCode}},
		Governance: facts, Receipt: newVolumeCognitionReceipt(root, serviceVersion, loaded.set, mustVolumeScope(loaded.set)),
		Metrics:           autoMetrics{AOCIToolCalls: 1, DeterministicMs: elapsedMilliseconds(start)},
		SemanticGenerated: false, NetworkAccessed: false, NextAction: "none",
		Optimization: &cognitionOptimizationStatus{Version: cognitionOptimizationVersion, State: "complete"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		return failResult(&Fail{Code: errInternal, Msg: "cognition_optimization_result_invalid"})
	}
	return textResult(string(data) + "\n")
}

func selectOptimizationTargets(root string, loaded *cognitionRepoCtx, objectRefs []string) (cognitionoptimization.Selection, error) {
	entries, err := optimizationAlignedEntries(root, loaded.set.Volumes[cognition.ScopeCode], objectRefs)
	if err != nil {
		return cognitionoptimization.Selection{}, err
	}
	return cognitionoptimization.Select(entries, loaded.cfg.EffectiveCognitionBudget(), cognitionoptimization.SelectOptions{
		ObjectRefs: objectRefs, MaxEntries: machinecontract.EntriesBatchMaxItems,
	})
}

func planOptimizationBatch(root string, loaded *cognitionRepoCtx, facts *volumegovernance.Facts, remaining []string) (codebatch.Plan, cognitionoptimization.Selection, error) {
	limit := machinecontract.EntriesBatchMaxItems
	if len(remaining) < limit {
		limit = len(remaining)
	}
	if limit == 0 {
		return codebatch.Plan{}, cognitionoptimization.Selection{}, fmt.Errorf("optimization checkpoint has no remaining objects")
	}
	refs := append([]string{}, remaining[:limit]...)
	entries, err := optimizationAlignedEntries(root, loaded.set.Volumes[cognition.ScopeCode], refs)
	if err != nil {
		return codebatch.Plan{}, cognitionoptimization.Selection{}, err
	}
	measured, err := cognitionoptimization.Select(entries, loaded.cfg.EffectiveCognitionBudget(), cognitionoptimization.SelectOptions{
		ObjectRefs: refs, MaxEntries: limit,
	})
	if err != nil {
		return codebatch.Plan{}, cognitionoptimization.Selection{}, err
	}
	ordered := reorderOptimizationSelection(measured, refs)
	plan, _, err := buildOptimizationCodePlan(root, facts, ordered.Batch)
	return plan, ordered, err
}

func loadOptimizationCurrentBatch(root string, loaded *cognitionRepoCtx, facts *volumegovernance.Facts, checkpoint cognitionoptimization.Checkpoint) (codebatch.Plan, cognitionoptimization.Selection, error) {
	receipt, err := codebatch.LoadReceipt(root, checkpoint.CurrentBatchID)
	if err != nil {
		return codebatch.Plan{}, cognitionoptimization.Selection{}, err
	}
	if receipt.CompositeIdentity != facts.CompositeIdentity || receipt.ScopePolicyIdentity != facts.ManagedScope.PolicyIdentity ||
		receipt.CodeVolumePath != facts.Code.Path || receipt.CodeVolumeSHA256 != facts.Code.SHA256 {
		return codebatch.Plan{}, cognitionoptimization.Selection{}, fmt.Errorf("optimization candidate receipt is stale")
	}
	if len(receipt.Targets) > len(checkpoint.RemainingObjectRefs) {
		return codebatch.Plan{}, cognitionoptimization.Selection{}, fmt.Errorf("optimization checkpoint batch exceeds remaining objects")
	}
	entries := make([]cognitionoptimization.AlignedEntry, 0, len(receipt.Targets))
	wanted := map[string]bool{}
	for _, ref := range checkpoint.RemainingObjectRefs[:len(receipt.Targets)] {
		wanted[ref] = true
	}
	for _, target := range receipt.Targets {
		if !wanted[target.ObjectRef] {
			return codebatch.Plan{}, cognitionoptimization.Selection{}, fmt.Errorf("optimization receipt does not match checkpoint prefix")
		}
		entries = append(entries, cognitionoptimization.AlignedEntry{ObjectRef: target.ObjectRef, Path: target.Path,
			SourceSHA256: target.SourceSHA256, ExistingEntry: target.ExistingEntry})
	}
	measured, err := cognitionoptimization.Select(entries, loaded.cfg.EffectiveCognitionBudget(), cognitionoptimization.SelectOptions{MaxEntries: len(entries)})
	if err != nil {
		return codebatch.Plan{}, cognitionoptimization.Selection{}, err
	}
	selection := reorderOptimizationSelection(measured, checkpoint.RemainingObjectRefs[:len(receipt.Targets)])
	plan := codebatch.Plan{Version: receipt.Version, PlanID: receipt.PlanID, BatchID: receipt.BatchID,
		CompositeIdentity: receipt.CompositeIdentity, ScopePolicyIdentity: receipt.ScopePolicyIdentity,
		CodeVolumePath: receipt.CodeVolumePath, CodeVolumeSHA256: receipt.CodeVolumeSHA256,
		TotalTargets: len(receipt.Targets), MaxEntries: machinecontract.EntriesBatchMaxItems,
		Included: len(receipt.Targets), CompleteCandidateSetForBatch: true,
		Candidates: make([]codebatch.Candidate, 0, len(receipt.Targets)), NextAction: "author_complete_current_machine_batch"}
	for _, target := range receipt.Targets {
		plan.Candidates = append(plan.Candidates, codebatch.Candidate{Target: target})
	}
	return reorderOptimizationPlan(plan, selection.Batch), selection, nil
}

func optimizationAlignedEntries(root string, asset *cognition.Asset, objectRefs []string) ([]cognitionoptimization.AlignedEntry, error) {
	if asset == nil {
		return nil, fmt.Errorf("Code Volume is absent")
	}
	wanted := map[string]bool{}
	for _, ref := range objectRefs {
		wanted[strings.TrimSpace(ref)] = true
	}
	result := make([]cognitionoptimization.AlignedEntry, 0, len(asset.Objects))
	for _, object := range asset.Objects {
		if len(wanted) != 0 && !wanted[object.CanonicalRef] {
			continue
		}
		if object.Entry == nil || object.Entry.RelPath == "" {
			return nil, fmt.Errorf("Code object %s has no repository path", object.CanonicalRef)
		}
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(object.Entry.RelPath)))
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", object.CanonicalRef, err)
		}
		result = append(result, cognitionoptimization.AlignedEntry{ObjectRef: object.CanonicalRef,
			Path: object.Entry.RelPath, SourceSHA256: fingerprint.SHA256, ExistingEntry: object.CanonicalLine})
	}
	return result, nil
}

func buildOptimizationCodePlan(root string, facts *volumegovernance.Facts, candidates []cognitionoptimization.Candidate) (codebatch.Plan, []codebatch.Candidate, error) {
	values := make([]codebatch.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, codebatch.Candidate{Target: codebatch.Target{Change: cognition.ImpactChangeUpdate,
			ObjectRef: candidate.ObjectRef, Path: candidate.Path, SourceSHA256: candidate.SourceSHA256,
			ExistingEntry: candidate.ExistingEntry}})
	}
	plan, err := codebatch.BuildPlan(root, facts.CompositeIdentity, facts.ManagedScope.PolicyIdentity,
		facts.Code.Path, facts.Code.SHA256, values, machinecontract.EntriesBatchMaxItems)
	return plan, values, err
}

func optimizationMaintainCandidate(measured cognitionoptimization.Candidate, issued codebatch.Candidate, batchID string, explicit bool) volumeMaintainCandidate {
	reason := "c_importance_and_entry_cost"
	if measured.TargetOverageTokens > 0 {
		reason = "c_band_target_overage"
	}
	if explicit {
		reason = "explicit_object_ref"
	}
	return volumeMaintainCandidate{Domain: cognition.ScopeCode, Change: cognition.ImpactChangeUpdate,
		ObjectRef: measured.ObjectRef, Path: measured.Path, ExistingEntry: measured.ExistingEntry,
		ExistingEntrySHA256: entrySHA256(measured.ExistingEntry), SourceSHA256: measured.SourceSHA256,
		CandidateID: issued.CandidateID, BatchID: batchID, ModelAuthoringOnly: true,
		Importance: measured.Importance, SelectionReason: reason,
		Cost: &cognitionOptimizationCost{FTokens: measured.Cost.FTokens, RTokens: measured.Cost.RTokens,
			ATokens: measured.Cost.ATokens, STokens: measured.Cost.STokens, TotalTokens: measured.Cost.TotalTokens,
			RTargetTokens: measured.RTargetTokens, RMaxTokens: measured.RMaxTokens,
			STargetTokens: measured.STargetTokens, SMaxTokens: measured.SMaxTokens}}
}

func optimizationPlanCandidate(plan codebatch.Plan, objectRef string) *codebatch.Candidate {
	for index := range plan.Candidates {
		if plan.Candidates[index].ObjectRef == objectRef {
			return &plan.Candidates[index]
		}
	}
	return nil
}

func reorderOptimizationPlan(plan codebatch.Plan, ordered []cognitionoptimization.Candidate) codebatch.Plan {
	byRef := map[string]codebatch.Candidate{}
	for _, candidate := range plan.Candidates {
		byRef[candidate.ObjectRef] = candidate
	}
	plan.Candidates = plan.Candidates[:0]
	for _, candidate := range ordered {
		if value, ok := byRef[candidate.ObjectRef]; ok {
			plan.Candidates = append(plan.Candidates, value)
		}
	}
	return plan
}

func reorderOptimizationSelection(selection cognitionoptimization.Selection, refs []string) cognitionoptimization.Selection {
	byRef := map[string]cognitionoptimization.Candidate{}
	for _, candidate := range selection.Batch {
		byRef[candidate.ObjectRef] = candidate
	}
	ordered := make([]cognitionoptimization.Candidate, 0, len(refs))
	for _, ref := range refs {
		if candidate, ok := byRef[ref]; ok {
			ordered = append(ordered, candidate)
		}
	}
	selection.Batch = ordered
	selection.TotalTargets = len(ordered)
	selection.RemainingObjectRefs = []string{}
	return selection
}

func optimizationSelectionRefs(selection cognitionoptimization.Selection) []string {
	result := make([]string, 0, selection.TotalTargets)
	for _, candidate := range selection.Batch {
		result = append(result, candidate.ObjectRef)
	}
	return append(result, selection.RemainingObjectRefs...)
}

func cognitionOptimizationID(loaded *cognitionRepoCtx, facts *volumegovernance.Facts, refs []string) (string, error) {
	budgetID, err := cognitionbudget.Identity(loaded.cfg.EffectiveCognitionBudget())
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, value := range append([]string{cognitionOptimizationVersion, facts.CompositeIdentity,
		facts.ManagedScope.PolicyIdentity, facts.Code.SHA256, budgetID}, refs...) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func optimizationStatus(checkpoint cognitionoptimization.Checkpoint, included int, state string) *cognitionOptimizationStatus {
	remainingAfterBatch := len(checkpoint.RemainingObjectRefs) - included
	if remainingAfterBatch < 0 {
		remainingAfterBatch = 0
	}
	return &cognitionOptimizationStatus{Version: cognitionOptimizationVersion,
		OptimizationID: checkpoint.OptimizationID, State: state, CurrentBatchID: checkpoint.CurrentBatchID,
		TotalTargets: checkpoint.ReviewedCount + len(checkpoint.RemainingObjectRefs), Included: included,
		Reviewed: checkpoint.ReviewedCount, NoChange: checkpoint.NoChangeCount, Replaced: checkpoint.ReplacedCount,
		Remaining: remainingAfterBatch, ContinuationRequired: remainingAfterBatch > 0}
}

func entrySHA256(line string) string {
	digest := sha256.Sum256([]byte(line))
	return hex.EncodeToString(digest[:])
}

type cognitionOptimizationUpdateContext struct {
	Checkpoint       cognitionoptimization.StoredCheckpoint
	BatchID          string
	Reviewed         int
	NoChange         int
	Replaced         int
	SubmissionSHA256 string
	AlreadyAdvanced  bool
}

func cognitionOptimizationSubmissionIdentity(
	optimizationID string,
	receipt codebatch.Receipt,
	input []updateEntryItemIn,
) (string, int, int, error) {
	if len(input) != len(receipt.Targets) || len(input) == 0 {
		return "", 0, 0, fmt.Errorf("cognition_optimization_batch_incomplete")
	}
	byPath := make(map[string]updateEntryItemIn, len(input))
	for _, item := range input {
		if strings.TrimSpace(item.ObjectRef) != "" || strings.TrimSpace(item.Path) == "" {
			return "", 0, 0, fmt.Errorf("cognition_optimization_batch_mismatch")
		}
		rel, err := afs.NormalizeRelPath(item.Path)
		if err != nil || byPath[rel].Path != "" {
			return "", 0, 0, fmt.Errorf("cognition_optimization_batch_mismatch")
		}
		item.Path = rel
		byPath[rel] = item
	}

	hash := sha256.New()
	writeValue := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	writeValue("cognition-optimization-submission/v1")
	writeValue(optimizationID)
	writeValue(receipt.BatchID)
	writeValue(fmt.Sprintf("%d", len(receipt.Targets)))
	noChange, replaced := 0, 0
	for _, target := range receipt.Targets {
		item, ok := byPath[target.Path]
		if !ok || strings.ToLower(strings.TrimSpace(item.BatchID)) != receipt.BatchID ||
			strings.ToLower(strings.TrimSpace(item.SourceSHA256)) != target.SourceSHA256 ||
			strings.ToLower(strings.TrimSpace(item.CandidateID)) != target.CandidateID {
			return "", 0, 0, fmt.Errorf("cognition_optimization_batch_mismatch")
		}
		entry, ok := index.ParseEntryLine(canonicalVolumeCandidateLine(target.Path, item.NewEntry), 1)
		if !ok {
			return "", 0, 0, fmt.Errorf("cognition_optimization_entry_invalid")
		}
		for _, value := range []string{target.ObjectRef, target.Path, target.Change, target.SourceSHA256, target.CandidateID, entry.FullLine} {
			writeValue(value)
		}
		if entry.FullLine == target.ExistingEntry {
			noChange++
		} else {
			replaced++
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), noChange, replaced, nil
}

func prepareCognitionOptimizationUpdate(root string, input []updateEntryItemIn) (*cognitionOptimizationUpdateContext, error) {
	stored, err := cognitionoptimization.Load(root)
	if errors.Is(err, cognitionoptimization.ErrCheckpointNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cognition_optimization_checkpoint_invalid: %w", err)
	}
	checkpoint := stored.Checkpoint
	if len(input) == 0 {
		return nil, nil
	}
	currentMatches, previousMatches := 0, 0
	for _, item := range input {
		if strings.EqualFold(strings.TrimSpace(item.BatchID), checkpoint.CurrentBatchID) {
			currentMatches++
		}
		if strings.EqualFold(strings.TrimSpace(item.BatchID), checkpoint.LastCompletedBatchID) {
			previousMatches++
		}
	}
	if previousMatches == len(input) && checkpoint.LastCompletedBatchID != "" {
		receipt, receiptErr := codebatch.LoadReceipt(root, checkpoint.LastCompletedBatchID)
		if receiptErr != nil {
			return nil, fmt.Errorf("cognition_optimization_completed_batch_receipt_invalid: %w", receiptErr)
		}
		submissionSHA256, noChange, replaced, identityErr := cognitionOptimizationSubmissionIdentity(
			checkpoint.OptimizationID, receipt, input,
		)
		if identityErr != nil || submissionSHA256 != checkpoint.LastSubmissionSHA256 ||
			len(input) != checkpoint.LastReviewedCount || noChange != checkpoint.LastNoChangeCount || replaced != checkpoint.LastReplacedCount {
			return nil, fmt.Errorf("cognition_optimization_completed_batch_payload_mismatch")
		}
		return &cognitionOptimizationUpdateContext{Checkpoint: stored, BatchID: checkpoint.LastCompletedBatchID,
			Reviewed: checkpoint.LastReviewedCount, NoChange: checkpoint.LastNoChangeCount,
			Replaced: checkpoint.LastReplacedCount, SubmissionSHA256: submissionSHA256, AlreadyAdvanced: true}, nil
	}
	if currentMatches == 0 && previousMatches == 0 {
		return nil, nil
	}
	if currentMatches != len(input) || checkpoint.Completed || checkpoint.CurrentBatchID == "" {
		return nil, fmt.Errorf("cognition_optimization_batch_mixed")
	}
	receipt, err := codebatch.LoadReceipt(root, checkpoint.CurrentBatchID)
	if err != nil {
		return nil, fmt.Errorf("cognition_optimization_receipt_invalid: %w", err)
	}
	submissionSHA256, noChange, replaced, identityErr := cognitionOptimizationSubmissionIdentity(
		checkpoint.OptimizationID, receipt, input,
	)
	if identityErr != nil {
		return nil, identityErr
	}
	return &cognitionOptimizationUpdateContext{Checkpoint: stored, BatchID: checkpoint.CurrentBatchID,
		Reviewed: len(input), NoChange: noChange, Replaced: replaced, SubmissionSHA256: submissionSHA256}, nil
}

func advanceCognitionOptimizationUpdate(root string, current *cognitionOptimizationUpdateContext) (*cognitionOptimizationStatus, error) {
	if current == nil {
		return nil, nil
	}
	if current.AlreadyAdvanced {
		checkpoint := current.Checkpoint.Checkpoint
		state := "in_progress"
		if checkpoint.Completed {
			state = "complete"
		}
		return &cognitionOptimizationStatus{Version: cognitionOptimizationVersion,
			OptimizationID: checkpoint.OptimizationID, State: state, CurrentBatchID: checkpoint.CurrentBatchID,
			TotalTargets: checkpoint.ReviewedCount + len(checkpoint.RemainingObjectRefs), Included: current.Reviewed,
			Reviewed: checkpoint.ReviewedCount, NoChange: checkpoint.NoChangeCount, Replaced: checkpoint.ReplacedCount,
			Remaining: len(checkpoint.RemainingObjectRefs), ContinuationRequired: len(checkpoint.RemainingObjectRefs) > 0}, nil
	}
	if current.NoChange+current.Replaced != current.Reviewed {
		return nil, fmt.Errorf("cognition_optimization_batch_classification_incomplete")
	}
	advanced, err := advanceCognitionOptimizationCheckpoint(root, current.Checkpoint.SHA256, cognitionoptimization.AdvanceInput{
		OptimizationID:   current.Checkpoint.Checkpoint.OptimizationID,
		CurrentBatchID:   current.BatchID,
		SubmissionSHA256: current.SubmissionSHA256,
		ReviewedDelta:    current.Reviewed,
		NoChangeDelta:    current.NoChange,
		ReplacedDelta:    current.Replaced,
	})
	if err != nil {
		return nil, err
	}
	checkpoint := advanced.Checkpoint
	state := "in_progress"
	if checkpoint.Completed {
		state = "complete"
	}
	return &cognitionOptimizationStatus{Version: cognitionOptimizationVersion,
		OptimizationID: checkpoint.OptimizationID, State: state, CurrentBatchID: checkpoint.CurrentBatchID,
		TotalTargets: checkpoint.ReviewedCount + len(checkpoint.RemainingObjectRefs), Included: current.Reviewed,
		Reviewed: checkpoint.ReviewedCount, NoChange: checkpoint.NoChangeCount, Replaced: checkpoint.ReplacedCount,
		Remaining: len(checkpoint.RemainingObjectRefs), ContinuationRequired: len(checkpoint.RemainingObjectRefs) > 0}, nil
}
