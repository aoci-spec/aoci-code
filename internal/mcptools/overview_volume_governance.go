package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// inspectVolumeGovernance adapts the shared Volumes governance assessment to
// the refresh counters used by Overview. The snapshot identity deliberately
// excludes BusinessSourceSHA256 because that aggregate includes Git HEAD;
// commit-only changes are not cognition or governance changes. The Baseline
// byte identity is added separately so a newly aligned Baseline cannot be
// combined with an already delivered Whole-Index body.
func inspectVolumeGovernance(
	root string,
	loaded *cognitionRepoCtx,
) (semanticChangeFacts, string, *Fail) {
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return semanticChangeFacts{}, "", &Fail{
			Code: errInternal,
			Msg:  mcpMessage("maintain.snapshot_failed", localeSafeMCPDetail(err.Error())),
		}
	}
	baselineIdentity := "absent"
	fingerprint, hashErr := baseline.HashFile(loaded.paths.BaselinePath)
	switch {
	case hashErr == nil:
		baselineIdentity = fingerprint.SHA256
	case os.IsNotExist(hashErr):
	default:
		return semanticChangeFacts{}, "", &Fail{
			Code: errInternal,
			Msg:  mcpMessage("maintain.snapshot_failed", localeSafeMCPDetail(hashErr.Error())),
		}
	}

	identityFacts := *facts
	identityFacts.BusinessSourceSHA256 = ""
	// Runtime audit files live under .aoci and are safety-excluded. Their
	// appearance may change the diagnostic exclude count, but never the set of
	// governed Code sources or the governance verdict.
	identityFacts.ManagedScope.ExcludeCount = 0
	encoded, err := json.Marshal(struct {
		Facts          volumegovernance.Facts `json:"facts"`
		BaselineSHA256 string                 `json:"baseline_sha256"`
	}{Facts: identityFacts, BaselineSHA256: baselineIdentity})
	if err != nil {
		return semanticChangeFacts{}, "", &Fail{
			Code: errInternal,
			Msg:  mcpMessage("mcp.error.internal_recovered"),
		}
	}
	digest := sha256.Sum256(encoded)
	return semanticFactsFromVolumeGovernance(loaded, facts), hex.EncodeToString(digest[:]), nil
}

func semanticFactsFromVolumeGovernance(
	loaded *cognitionRepoCtx,
	facts *volumegovernance.Facts,
) semanticChangeFacts {
	result := semanticChangeFacts{
		Threshold:              loaded.cfg.CognitionRefreshThreshold,
		SemanticStale:          len(facts.CodeDrift.Stale),
		ActionableMissing:      len(facts.CodeDrift.Missing),
		LineEndingOnly:         len(facts.CodeDrift.LineEndingOnly),
		Orphan:                 len(facts.CodeDrift.Orphan),
		Unbaselined:            len(facts.CodeDrift.Unbaselined),
		Warnings:               len(facts.RelationFindings),
		GovernanceBlockerCount: len(facts.Findings),
		GovernanceAligned:      facts.GovernanceAligned,
		ScopeChangeRequired:    facts.ManagedScope.ScopeChangeRequired,
		ScopePolicyIdentity:    facts.ManagedScope.PolicyIdentity,
		ObservedNew:            len(facts.CodeDrift.ObservedNew),
		ObservedChanged:        len(facts.CodeDrift.ObservedChanged),
		ObservedRemoved:        len(facts.CodeDrift.ObservedRemoved),
		ObservedPendingReview:  facts.ManagedScope.ObservedPendingReview,
		WholeIndexTokens:       facts.Budget.WholeIndexTokens,
		BudgetMode:             facts.Budget.Mode,
		BudgetStatus:           facts.Budget.Status,
		BudgetViolationCount:   len(facts.Budget.Violations),
	}
	paths := make(map[string]bool)
	for _, group := range [][]string{facts.CodeDrift.Missing, facts.CodeDrift.Stale} {
		for _, path := range group {
			paths[path] = true
		}
	}
	for _, item := range facts.DatabaseCognition.Items {
		switch item.State {
		case machinecontract.DatabaseCognitionMissing:
			result.ActionableMissing++
			paths[item.ObjectRef] = true
		case machinecontract.DatabaseCognitionStale:
			result.SemanticStale++
			paths[item.ObjectRef] = true
		case machinecontract.DatabaseCognitionUnbaselined:
			result.Unbaselined++
			paths[item.ObjectRef] = true
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	pathDigest := sha256.Sum256([]byte(strings.Join(ordered, "\x00")))
	result.Count = len(ordered)
	result.PathSetSHA256 = hex.EncodeToString(pathDigest[:])
	return result
}

func confirmVolumeGovernanceSnapshot(
	root string,
	loaded *cognitionRepoCtx,
	expected string,
) *Fail {
	_, actual, fail := inspectVolumeGovernance(root, loaded)
	if fail != nil {
		return fail
	}
	if actual != expected {
		return &Fail{
			Code: errCognitionSnapshotUnavailable,
			Msg:  mcpMessage("overview.delivery.snapshot_changed"),
			Hint: mcpMessage("overview.delivery.snapshot_retry"),
		}
	}
	return nil
}
