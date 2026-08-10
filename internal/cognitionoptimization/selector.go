// Package cognitionoptimization provides deterministic, semantics-free
// selection and progress primitives for explicitly requested cognition
// optimization. It measures existing model-authored entries, but never creates,
// truncates, retags, or rewrites F/R/A/S content.
package cognitionoptimization

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// MaxBatchEntries is the existing AOCI entry-update transaction boundary.
const MaxBatchEntries = 200

// AlignedEntry is one complete, current Code cognition entry supplied by the
// caller after ordinary governance has proved it aligned with its source.
// Selection deliberately does not attempt to reproduce Baseline alignment.
type AlignedEntry struct {
	ObjectRef     string `json:"object_ref"`
	Path          string `json:"path"`
	SourceSHA256  string `json:"source_sha256"`
	ExistingEntry string `json:"existing_entry"`
}

// EntryCost uses the same deterministic byte/3 estimate as cognitionbudget.
type EntryCost struct {
	FTokens     int `json:"f_tokens"`
	RTokens     int `json:"r_tokens"`
	ATokens     int `json:"a_tokens"`
	STokens     int `json:"s_tokens"`
	TotalTokens int `json:"total_tokens"`
}

// Candidate contains only measurements and the unchanged current Entry. It is
// safe transport for a model review, not authored cognition.
type Candidate struct {
	AlignedEntry
	Importance               int       `json:"importance"`
	Cost                     EntryCost `json:"cost"`
	RTargetTokens            int       `json:"r_target_tokens"`
	RMaxTokens               int       `json:"r_max_tokens"`
	STargetTokens            int       `json:"s_target_tokens"`
	SMaxTokens               int       `json:"s_max_tokens"`
	TargetOverageTokens      int       `json:"target_overage_tokens"`
	MaxOverageTokens         int       `json:"max_overage_tokens"`
	TargetPressureBasisPoint int64     `json:"target_pressure_basis_points"`
	MaxPressureBasisPoint    int64     `json:"max_pressure_basis_points"`
}

// SelectOptions optionally restricts optimization to exact canonical Code
// object identities. MaxEntries defaults to MaxBatchEntries and may not exceed
// it. Explicit ObjectRefs select membership; their input order is not priority.
type SelectOptions struct {
	ObjectRefs []string `json:"object_refs,omitempty"`
	MaxEntries int      `json:"max_entries,omitempty"`
}

// Selection is one bounded model-review batch plus the stable ordered tail.
type Selection struct {
	TotalTargets        int         `json:"total_targets"`
	Batch               []Candidate `json:"batch"`
	RemainingObjectRefs []string    `json:"remaining_object_refs"`
}

// Select measures and deterministically orders complete aligned Code entries.
// Entries exceeding their current C-band max come first, then entries exceeding
// target, followed by higher-C and larger entries. ObjectRef is the final stable
// tie-breaker. No entry text is synthesized or modified.
func Select(entries []AlignedEntry, policy cognitionbudget.Policy, options SelectOptions) (Selection, error) {
	normalized, err := cognitionbudget.Normalize(policy)
	if err != nil {
		return Selection{}, fmt.Errorf("normalize cognition budget: %w", err)
	}
	limit := options.MaxEntries
	if limit == 0 {
		limit = MaxBatchEntries
	}
	if limit < 1 || limit > MaxBatchEntries {
		return Selection{}, fmt.Errorf("optimization batch limit must be between 1 and %d", MaxBatchEntries)
	}

	explicit, err := normalizeExplicitRefs(options.ObjectRefs)
	if err != nil {
		return Selection{}, err
	}
	seen := make(map[string]bool, len(entries))
	foundExplicit := make(map[string]bool, len(explicit))
	candidates := make([]Candidate, 0, len(entries))
	for position, current := range entries {
		candidate, err := measureCandidate(current, normalized)
		if err != nil {
			return Selection{}, fmt.Errorf("aligned entry %d: %w", position+1, err)
		}
		if seen[candidate.ObjectRef] {
			return Selection{}, fmt.Errorf("duplicate aligned object_ref %q", candidate.ObjectRef)
		}
		seen[candidate.ObjectRef] = true
		if len(explicit) != 0 {
			if !explicit[candidate.ObjectRef] {
				continue
			}
			foundExplicit[candidate.ObjectRef] = true
		}
		candidates = append(candidates, candidate)
	}
	if len(explicit) != len(foundExplicit) {
		missing := make([]string, 0, len(explicit)-len(foundExplicit))
		for objectRef := range explicit {
			if !foundExplicit[objectRef] {
				missing = append(missing, objectRef)
			}
		}
		sort.Strings(missing)
		return Selection{}, fmt.Errorf("explicit optimization object_ref not aligned: %s", strings.Join(missing, ","))
	}

	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.MaxPressureBasisPoint != right.MaxPressureBasisPoint {
			return left.MaxPressureBasisPoint > right.MaxPressureBasisPoint
		}
		if left.TargetPressureBasisPoint != right.TargetPressureBasisPoint {
			return left.TargetPressureBasisPoint > right.TargetPressureBasisPoint
		}
		if left.Importance != right.Importance {
			return left.Importance > right.Importance
		}
		if left.Cost.TotalTokens != right.Cost.TotalTokens {
			return left.Cost.TotalTokens > right.Cost.TotalTokens
		}
		if left.Cost.STokens != right.Cost.STokens {
			return left.Cost.STokens > right.Cost.STokens
		}
		if left.Cost.RTokens != right.Cost.RTokens {
			return left.Cost.RTokens > right.Cost.RTokens
		}
		return left.ObjectRef < right.ObjectRef
	})

	included := len(candidates)
	if included > limit {
		included = limit
	}
	batch := append([]Candidate{}, candidates[:included]...)
	remaining := make([]string, 0, len(candidates)-included)
	for _, candidate := range candidates[included:] {
		remaining = append(remaining, candidate.ObjectRef)
	}
	return Selection{TotalTargets: len(candidates), Batch: batch, RemainingObjectRefs: remaining}, nil
}

func measureCandidate(current AlignedEntry, policy cognitionbudget.Policy) (Candidate, error) {
	path, err := afs.NormalizeRelPath(current.Path)
	if err != nil || path != current.Path || current.ObjectRef != "code:"+path {
		return Candidate{}, fmt.Errorf("invalid Code object identity %q", current.ObjectRef)
	}
	if !validSHA256(current.SourceSHA256) {
		return Candidate{}, fmt.Errorf("invalid source_sha256 for %q", current.ObjectRef)
	}
	entry, ok := index.ParseEntryLine(current.ExistingEntry, 1)
	if !ok || entry.FullLine != current.ExistingEntry {
		return Candidate{}, fmt.Errorf("invalid complete Entry for %q", current.ObjectRef)
	}
	importance, err := strconv.Atoi(entry.TagsParsed["C"])
	if err != nil || importance < 1 || importance > 9 {
		return Candidate{}, fmt.Errorf("invalid C importance for %q", current.ObjectRef)
	}
	rBand, rOK := cognitionbudget.LimitFor(policy.R, importance)
	sBand, sOK := cognitionbudget.LimitFor(policy.S, importance)
	if !rOK || !sOK {
		return Candidate{}, fmt.Errorf("missing C-band budget for %q", current.ObjectRef)
	}
	cost := EntryCost{
		FTokens:     cognitionbudget.EstimateTokens([]byte(entry.F)),
		RTokens:     cognitionbudget.EstimateTokens([]byte(entry.R)),
		ATokens:     cognitionbudget.EstimateTokens([]byte(entry.Api)),
		STokens:     cognitionbudget.EstimateTokens([]byte(entry.S)),
		TotalTokens: cognitionbudget.EstimateTokens([]byte(entry.FullLine)),
	}
	rTargetOver := positiveDifference(cost.RTokens, rBand.TargetTokens)
	sTargetOver := positiveDifference(cost.STokens, sBand.TargetTokens)
	rMaxOver := positiveDifference(cost.RTokens, rBand.MaxTokens)
	sMaxOver := positiveDifference(cost.STokens, sBand.MaxTokens)
	return Candidate{
		AlignedEntry:             current,
		Importance:               importance,
		Cost:                     cost,
		RTargetTokens:            rBand.TargetTokens,
		RMaxTokens:               rBand.MaxTokens,
		STargetTokens:            sBand.TargetTokens,
		SMaxTokens:               sBand.MaxTokens,
		TargetOverageTokens:      rTargetOver + sTargetOver,
		MaxOverageTokens:         rMaxOver + sMaxOver,
		TargetPressureBasisPoint: pressureBasisPoints(cost.RTokens, rBand.TargetTokens) + pressureBasisPoints(cost.STokens, sBand.TargetTokens),
		MaxPressureBasisPoint:    pressureBasisPoints(cost.RTokens, rBand.MaxTokens) + pressureBasisPoints(cost.STokens, sBand.MaxTokens),
	}, nil
}

func normalizeExplicitRefs(values []string) (map[string]bool, error) {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		objectRef := strings.TrimSpace(value)
		if !canonicalCodeObjectRef(objectRef) {
			return nil, fmt.Errorf("invalid explicit Code object_ref %q", value)
		}
		if result[objectRef] {
			return nil, fmt.Errorf("duplicate explicit object_ref %q", objectRef)
		}
		result[objectRef] = true
	}
	return result, nil
}

func canonicalCodeObjectRef(objectRef string) bool {
	if !strings.HasPrefix(objectRef, "code:") {
		return false
	}
	path := strings.TrimPrefix(objectRef, "code:")
	normalized, err := afs.NormalizeRelPath(path)
	return err == nil && normalized == path
}

func positiveDifference(actual, limit int) int {
	if actual > limit {
		return actual - limit
	}
	return 0
}

func pressureBasisPoints(actual, limit int) int64 {
	overage := positiveDifference(actual, limit)
	if overage == 0 {
		return 0
	}
	denominator := limit
	if denominator < 1 {
		denominator = 1
	}
	return int64(overage) * 10000 / int64(denominator)
}

func validSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
