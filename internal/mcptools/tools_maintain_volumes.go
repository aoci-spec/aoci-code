package mcptools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

type volumeMaintainCandidate struct {
	Domain             string `json:"domain"`
	Change             string `json:"change"`
	ObjectRef          string `json:"object_ref"`
	Path               string `json:"path,omitempty"`
	ExistingEntry      string `json:"existing_entry,omitempty"`
	SourceSHA256       string `json:"source_sha256,omitempty"`
	EvidenceVersion    string `json:"evidence_version,omitempty"`
	EvidenceSHA256     string `json:"evidence_sha256,omitempty"`
	CandidateID        string `json:"candidate_id,omitempty"`
	BatchID            string `json:"batch_id,omitempty"`
	ModelAuthoringOnly bool   `json:"model_authoring_only"`
}

type volumeMaintainSets struct {
	Review []string `json:"review"`
	Write  []string `json:"write"`
	Guard  []string `json:"guard"`
}

type volumeAuthoringBatch struct {
	LogicalPlan          string `json:"logical_plan"`
	BatchIdentity        string `json:"batch_identity"`
	TotalTargets         int    `json:"total_targets"`
	MaxEntries           int    `json:"max_entries"`
	Included             int    `json:"included"`
	Remaining            int    `json:"remaining"`
	CompleteCandidateSet bool   `json:"complete_candidate_set_for_current_batch"`
	ContinuationRequired bool   `json:"continuation_required"`
	CompositeIdentity    string `json:"composite_identity"`
	ScopePolicyIdentity  string `json:"scope_policy_identity"`
	NextAction           string `json:"next_action"`
}

type volumeMaintainResult struct {
	Version           int                       `json:"version"`
	Status            string                    `json:"status"`
	Result            string                    `json:"result"`
	Aligned           bool                      `json:"aligned"`
	RequestedScope    string                    `json:"requested_scope"`
	AffectedDomains   []string                  `json:"affected_domains"`
	Candidates        []volumeMaintainCandidate `json:"candidates"`
	OrphanRemovals    []string                  `json:"orphan_remove_candidates"`
	Sets              volumeMaintainSets        `json:"sets"`
	DatabasePlan      *dbcognition.Plan         `json:"database_plan,omitempty"`
	CodePlan          *codebatch.Plan           `json:"code_plan,omitempty"`
	Batch             volumeAuthoringBatch      `json:"authoring_batch"`
	Governance        *volumegovernance.Facts   `json:"governance"`
	Receipt           cognitionReceipt          `json:"cognition_receipt"`
	Metrics           autoMetrics               `json:"metrics"`
	SemanticGenerated bool                      `json:"semantic_generated"`
	NetworkAccessed   bool                      `json:"network_accessed"`
	NextAction        string                    `json:"next_action"`
	Instructions      []string                  `json:"instructions,omitempty"`
	AuthoringMeta     string                    `json:"authoring_meta,omitempty"`
}

func handleVolumeMaintain(root, serviceVersion, requestedScope string, loaded *cognitionRepoCtx, refreshSession *cognitionRefreshSession) *mcp.CallToolResult {
	start := time.Now()
	if requestedScope != "" && requestedScope != cognition.ScopeCode && requestedScope != cognition.ScopeDatabase && requestedScope != cognition.ScopeAll {
		return failResult(&Fail{Code: errBadArgs, Msg: "maintain_scope_invalid"})
	}
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return failResult(&Fail{Code: errInternal, Msg: "volumes_governance_facts_invalid"})
	}
	result := volumeMaintainResult{
		Version: 1, Status: autoStatusStopped, Result: facts.Result, Aligned: facts.GovernanceAligned,
		RequestedScope: requestedScope, AffectedDomains: append([]string{}, facts.AffectedDomains...),
		Candidates: []volumeMaintainCandidate{}, OrphanRemovals: []string{},
		Sets:       volumeMaintainSets{Review: []string{}, Write: []string{}, Guard: []string{"root", "meta"}},
		Governance: facts, Receipt: newVolumeCognitionReceipt(root, serviceVersion, loaded.set, mustVolumeScope(loaded.set)),
		Metrics: autoMetrics{AOCIToolCalls: 1}, SemanticGenerated: false, NetworkAccessed: false,
		NextAction: facts.NextRequiredAction,
		Batch: volumeAuthoringBatch{MaxEntries: machinecontract.EntriesBatchMaxItems,
			CompositeIdentity: facts.CompositeIdentity, ScopePolicyIdentity: facts.ManagedScope.PolicyIdentity},
	}
	requested := map[string]bool{cognition.ScopeCode: requestedScope == "" || requestedScope == cognition.ScopeAll || requestedScope == cognition.ScopeCode,
		cognition.ScopeDatabase: requestedScope == "" || requestedScope == cognition.ScopeAll || requestedScope == cognition.ScopeDatabase}

	if facts.Result != volumegovernance.ResultBlocked && facts.Result != volumegovernance.ResultEvidenceRequired {
		if requested[cognition.ScopeCode] && facts.Code.Enabled {
			buildVolumeCodeCandidates(root, loaded, &result)
		}
		if requested[cognition.ScopeDatabase] && facts.Database.Enabled {
			buildVolumeDatabaseCandidates(root, loaded, &result, machinecontract.EntriesBatchMaxItems-len(result.Candidates))
		}
	}
	result.Batch.TotalTargets = volumeAuthoringTargetCount(facts, requested)
	result.Batch.Included = len(result.Candidates)
	result.Batch.Remaining = result.Batch.TotalTargets - result.Batch.Included
	if result.Batch.Remaining < 0 {
		result.Batch.Remaining = 0
	}
	result.Batch.CompleteCandidateSet = len(result.Candidates) > 0
	result.Batch.ContinuationRequired = result.Batch.Remaining > 0
	result.Batch.LogicalPlan, result.Batch.BatchIdentity = volumeBatchIdentities(result)
	result.Batch.NextAction = "author_complete_current_machine_batch"
	for _, orphan := range facts.CodeDrift.Orphan {
		if requested[cognition.ScopeCode] {
			result.OrphanRemovals = append(result.OrphanRemovals, "code:"+orphan)
		}
	}
	for _, item := range facts.DatabaseCognition.Items {
		if requested[cognition.ScopeDatabase] && item.State == machinecontract.DatabaseCognitionOrphan {
			result.OrphanRemovals = append(result.OrphanRemovals, item.ObjectRef)
		}
	}
	sort.Strings(result.OrphanRemovals)
	result.Sets.Write = candidateRefs(result.Candidates)
	result.Sets.Review = reviewClosure(loaded.set, result.Candidates)
	if facts.Code.Enabled {
		result.Sets.Guard = append(result.Sets.Guard, cognition.ScopeCode)
	}
	if facts.Database.Enabled {
		result.Sets.Guard = append(result.Sets.Guard, cognition.ScopeDatabase, "database_evidence", "database_binding")
	}
	result.Sets.Guard = sortedUniqueStrings(result.Sets.Guard)

	switch {
	case facts.Result == volumegovernance.ResultEvidenceRequired:
		result.Status, result.Aligned = autoStatusStopped, false
	case facts.Result == volumegovernance.ResultBlocked || len(result.OrphanRemovals) > 0:
		result.Status, result.Aligned, result.Result, result.NextAction = autoStatusStopped, false, volumegovernance.ResultBlocked, "explicit_orphan_remove_or_resolve_blocker"
	case len(result.Candidates) > 0:
		result.Status, result.Aligned, result.Result, result.NextAction = autoStatusRepairRequired, false, volumegovernance.ResultAuthoringRequired, result.Batch.NextAction
		affected := candidateDomains(result.Candidates)
		contract, contractErr := authoringcontract.Build(loaded.set.Meta.Raw, affected, textassets.ActiveLocale())
		if contractErr != nil {
			return failResult(&Fail{Code: errInternal, Msg: "volume_authoring_contract_invalid"})
		}
		result.AuthoringMeta = contract.AuthoringMeta
		result.Instructions = contract.Instructions
	default:
		result.Status, result.Aligned, result.Result, result.NextAction = autoStatusApplied, true, volumegovernance.ResultAligned, "none"
	}
	result.Metrics.DeterministicMs = elapsedMilliseconds(start)
	result.Metrics.SemanticFiles = len(result.Candidates)
	if refreshSession != nil {
		refreshSession.noteSemanticThreshold(len(result.Candidates), loaded.cfg.CognitionRefreshThreshold)
	}
	ledgerResult := ledger.ResultOK
	if result.Status == autoStatusStopped {
		ledgerResult = ledger.ResultError
	} else if result.Status == autoStatusRepairRequired {
		ledgerResult = ledger.ResultRepairRequired
	}
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "maintain", Source: ledger.SourceAgent,
		Result: ledgerResult, PathsCount: len(result.Candidates), DurationMs: result.Metrics.DeterministicMs,
		AOCIToolCalls: 1, SemanticFiles: len(result.Candidates)})
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return failResult(&Fail{Code: errInternal, Msg: "volumes_maintain_result_invalid"})
	}
	return textResult(string(data) + "\n")
}

func candidateDomains(candidates []volumeMaintainCandidate) []string {
	values := make([]string, 0, 2)
	for _, domain := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		for _, candidate := range candidates {
			if candidate.Domain == domain {
				values = append(values, domain)
				break
			}
		}
	}
	return values
}

func mustVolumeScope(set *cognition.Set) cognition.ScopeView {
	view, _ := set.Scope(cognition.ScopeAll)
	return view
}

func buildVolumeCodeCandidates(root string, loaded *cognitionRepoCtx, result *volumeMaintainResult) {
	all := []codebatch.Candidate{}
	missing := append([]string{}, result.Governance.CodeDrift.Missing...)
	classification, _, _, err := curation.BuildClassification(root, loaded.cfg, missing)
	if err == nil {
		missing = append([]string{}, classification.Actionable...)
		for _, pending := range classification.Pending {
			result.OrphanRemovals = append(result.OrphanRemovals, "pending_curation:"+pending.Path)
		}
	}
	for _, path := range missing {
		if fingerprint, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(path))); hashErr == nil {
			all = append(all, codebatch.Candidate{Target: codebatch.Target{Change: cognition.ImpactChangeCreate,
				ObjectRef: "code:" + path, Path: path, SourceSHA256: fingerprint.SHA256}})
		}
	}
	for _, item := range []struct {
		change string
		paths  []string
	}{{cognition.ImpactChangeUpdate, result.Governance.CodeDrift.Stale}, {cognition.ImpactChangeUpdate, result.Governance.CodeDrift.Unbaselined}} {
		for _, path := range item.paths {
			object := cognitionObjectByRef(loaded.set.Volumes[cognition.ScopeCode], "code:"+path)
			fingerprint, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(path)))
			if object == nil || hashErr != nil {
				continue
			}
			all = append(all, codebatch.Candidate{Target: codebatch.Target{Change: item.change,
				ObjectRef: object.CanonicalRef, Path: path, ExistingEntry: object.CanonicalLine,
				SourceSHA256: fingerprint.SHA256}})
		}
	}
	if len(all) == 0 {
		return
	}
	plan, err := codebatch.BuildPlan(root, result.Governance.CompositeIdentity,
		result.Governance.ManagedScope.PolicyIdentity, result.Governance.Code.Path,
		result.Governance.Code.SHA256, all, machinecontract.EntriesBatchMaxItems)
	if err != nil {
		return
	}
	result.CodePlan = &plan
	for _, candidate := range plan.Candidates {
		result.Candidates = append(result.Candidates, volumeMaintainCandidate{Domain: cognition.ScopeCode,
			Change: candidate.Change, ObjectRef: candidate.ObjectRef, Path: candidate.Path,
			ExistingEntry: candidate.ExistingEntry, SourceSHA256: candidate.SourceSHA256,
			CandidateID: candidate.CandidateID, BatchID: plan.BatchID, ModelAuthoringOnly: true})
	}
}

func buildVolumeDatabaseCandidates(root string, loaded *cognitionRepoCtx, result *volumeMaintainResult, capacity int) {
	if capacity <= 0 {
		return
	}
	assessment := result.Governance.DatabaseCognition
	objectLimit, evidenceLimit := loaded.cfg.DatabaseCognitionBatchLimits()
	if objectLimit > capacity {
		objectLimit = capacity
	}
	plan, err := dbcognition.BuildPlan(root, assessment, loaded.set, objectLimit, evidenceLimit)
	if err != nil {
		return
	}
	result.DatabasePlan = &plan
	for _, candidate := range plan.Candidates {
		change := cognition.ImpactChangeUpdate
		if candidate.CognitionState == machinecontract.DatabaseCognitionMissing {
			change = cognition.ImpactChangeCreate
		}
		result.Candidates = append(result.Candidates, volumeMaintainCandidate{Domain: cognition.ScopeDatabase,
			Change: change, ObjectRef: candidate.ObjectRef, ExistingEntry: candidate.ExistingDatabaseEntry,
			EvidenceVersion: candidate.EvidenceVersion, EvidenceSHA256: candidate.TableEvidenceSHA256,
			CandidateID: candidate.CandidateID, BatchID: plan.BatchID, ModelAuthoringOnly: true})
	}
}

func volumeAuthoringTargetCount(facts *volumegovernance.Facts, requested map[string]bool) int {
	total := 0
	if requested[cognition.ScopeCode] {
		total += len(facts.CodeDrift.Missing) + len(facts.CodeDrift.Stale) + len(facts.CodeDrift.Unbaselined)
	}
	if requested[cognition.ScopeDatabase] {
		total += facts.DatabaseCognition.Summary.Missing + facts.DatabaseCognition.Summary.Stale + facts.DatabaseCognition.Summary.Unbaselined
	}
	return total
}

func volumeBatchIdentities(result volumeMaintainResult) (string, string) {
	codePlanID := "-"
	if result.CodePlan != nil {
		codePlanID = result.CodePlan.PlanID
	}
	databasePlanID := "-"
	if result.DatabasePlan != nil {
		databasePlanID = result.DatabasePlan.BatchID
	}
	logical := sha256.Sum256([]byte(fmt.Sprintf("volumes-authoring-plan/v1\x00%s\x00%s\x00%d\x00%s\x00%s",
		result.Batch.CompositeIdentity, result.Batch.ScopePolicyIdentity, result.Batch.TotalTargets,
		codePlanID, databasePlanID)))
	parts := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		parts = append(parts, strings.Join([]string{candidate.Domain, candidate.ObjectRef, candidate.BatchID,
			candidate.CandidateID, candidate.SourceSHA256, candidate.EvidenceSHA256}, "\x00"))
	}
	batch := sha256.Sum256([]byte(fmt.Sprintf("volumes-authoring-batch/v1\x00%x\x00%s\x00%d",
		logical, strings.Join(parts, "\x1e"), result.Batch.Included)))
	return fmt.Sprintf("%x", logical), fmt.Sprintf("%x", batch)
}

func candidateRefs(candidates []volumeMaintainCandidate) []string {
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.ObjectRef)
	}
	return sortedUniqueStrings(values)
}

func reviewClosure(set *cognition.Set, candidates []volumeMaintainCandidate) []string {
	review := candidateRefs(candidates)
	impactCandidates := []cognition.ImpactCandidate{}
	for candidateIndex, candidate := range candidates {
		if candidate.ExistingEntry == "" {
			continue
		}
		impactCandidates = append(impactCandidates, cognition.ImpactCandidate{Change: cognition.ImpactChangeUpdate,
			ObjectRef: candidate.ObjectRef, CanonicalLine: candidate.ExistingEntry, OriginalCandidateIndex: candidateIndex + 1})
	}
	if len(impactCandidates) == 0 {
		return review
	}
	impact, err := cognition.ResolveImpact(set, impactCandidates)
	if err != nil {
		return review
	}
	for _, item := range impact.ReviewSet {
		review = append(review, item.Object)
	}
	return sortedUniqueStrings(review)
}

func sortedUniqueStrings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
