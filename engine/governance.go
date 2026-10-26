package engine

import (
	"encoding/json"
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// GovernanceFinding is one fact that keeps the repository from alignment or
// that governance reports for review.
type GovernanceFinding struct {
	Code             string
	Domain           string
	Target           string
	Cause            string
	SafeRepairAction string
}

// DriftCounts counts the Code drift classes the Baseline comparison found.
type DriftCounts struct {
	Missing        int
	Orphan         int
	Stale          int
	Unbaselined    int
	LineEndingOnly int
}

// Governance is the deterministic fact set Verify, Check, Guide, Maintain,
// and Overview share, projected to its decision and its reasons. Facts holds
// the complete fact set as the JSON verify --json reports under governance.
type Governance struct {
	Aligned             bool
	Result              string
	NextRequiredAction  string
	AffectedDomains     []string
	Findings            []GovernanceFinding
	CodeSourceCount     int
	CodeEntryCount      int
	Drift               DriftCounts
	PendingTransactions int
	RecoveryPending     bool
	ThirdPartyConflict  bool
	ScopeChangeRequired bool
	Facts               json.RawMessage
}

// Governance assesses the repository exactly as verify, check, and the Guide
// do, without writing Verify History or a Ledger event. It returns
// ErrLayoutUnsupported on a Legacy layout.
func (r *Repository) Governance() (*Governance, error) {
	set, err := r.load()
	if err != nil {
		return nil, err
	}
	if set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, ErrLayoutUnsupported
	}
	facts, err := volumegovernance.Assess(r.root, r.cfg, set)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	out := &Governance{
		Aligned:             facts.GovernanceAligned,
		Result:              facts.Result,
		NextRequiredAction:  facts.NextRequiredAction,
		AffectedDomains:     append([]string(nil), facts.AffectedDomains...),
		CodeSourceCount:     facts.CodeSourceCount,
		CodeEntryCount:      facts.CodeEntryCount,
		PendingTransactions: facts.PendingTransactions,
		RecoveryPending:     facts.RecoveryPending,
		ThirdPartyConflict:  facts.ThirdPartyConflict,
		ScopeChangeRequired: facts.ManagedScope.ScopeChangeRequired,
		Drift: DriftCounts{
			Missing:        len(facts.CodeDrift.Missing),
			Orphan:         len(facts.CodeDrift.Orphan),
			Stale:          len(facts.CodeDrift.Stale),
			Unbaselined:    len(facts.CodeDrift.Unbaselined),
			LineEndingOnly: len(facts.CodeDrift.LineEndingOnly),
		},
		Facts: raw,
	}
	for _, finding := range facts.Findings {
		out.Findings = append(out.Findings, GovernanceFinding{
			Code:             finding.Code,
			Domain:           finding.Domain,
			Target:           finding.Target,
			Cause:            finding.Cause,
			SafeRepairAction: finding.SafeRepairAction,
		})
	}
	return out, nil
}
