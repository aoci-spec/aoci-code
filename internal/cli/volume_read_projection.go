package cli

import (
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

const volumeReadProjectionVersion = "volumes-read-projection/v1"

// volumeReadProjection is the shared read model for Status, Inventory, and
// Score. Those commands may present different slices of it, but they must not
// compute independent Volumes drift or governance facts.
type volumeReadProjection struct {
	Version           string                  `json:"version"`
	LayoutMode        string                  `json:"layout_mode"`
	CompositeIdentity string                  `json:"composite_identity"`
	ObjectCount       int                     `json:"object_count"`
	Inventory         *indexgen.Inventory     `json:"inventory"`
	Score             *indexgen.Score         `json:"score"`
	Governance        *volumegovernance.Facts `json:"governance"`
}

type volumeInventoryReport struct {
	Version           string                  `json:"version"`
	LayoutMode        string                  `json:"layout_mode"`
	CompositeIdentity string                  `json:"composite_identity"`
	ObjectCount       int                     `json:"object_count"`
	Inventory         *indexgen.Inventory     `json:"inventory"`
	Governance        *volumegovernance.Facts `json:"governance"`
}

type volumeScoreReport struct {
	Version           string                  `json:"version"`
	LayoutMode        string                  `json:"layout_mode"`
	CompositeIdentity string                  `json:"composite_identity"`
	Score             *indexgen.Score         `json:"score"`
	Governance        *volumegovernance.Facts `json:"governance"`
}

func (projection *volumeReadProjection) inventoryReport() volumeInventoryReport {
	return volumeInventoryReport{Version: projection.Version, LayoutMode: projection.LayoutMode,
		CompositeIdentity: projection.CompositeIdentity, ObjectCount: projection.ObjectCount,
		Inventory: projection.Inventory, Governance: projection.Governance}
}

func (projection *volumeReadProjection) scoreReport() volumeScoreReport {
	return volumeScoreReport{Version: projection.Version, LayoutMode: projection.LayoutMode,
		CompositeIdentity: projection.CompositeIdentity, Score: projection.Score, Governance: projection.Governance}
}

func buildVolumeReadProjection(root string, cfg *config.Config, set *cognition.Set) (*volumeReadProjection, error) {
	if cfg == nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, fmt.Errorf("volumes_read_projection_input_invalid")
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		return nil, err
	}
	inventory := &indexgen.Inventory{Items: []indexgen.Item{}}
	if code := set.Volumes[cognition.ScopeCode]; code != nil && code.State == cognition.AssetPresent && code.Document != nil {
		inventory, err = indexgen.BuildInventory(root, cfg, code.Document)
		if err != nil {
			return nil, err
		}
	}
	normalizeVolumeInventory(inventory, facts)
	score, err := buildVolumeScore(root, cfg, set, facts)
	if err != nil {
		return nil, err
	}
	return &volumeReadProjection{Version: volumeReadProjectionVersion, LayoutMode: set.LayoutMode,
		CompositeIdentity: set.CompositeIdentity, ObjectCount: volumeObjectCount(set),
		Inventory: inventory, Score: score, Governance: facts}, nil
}

func normalizeVolumeInventory(inventory *indexgen.Inventory, facts *volumegovernance.Facts) {
	if inventory == nil || facts == nil {
		return
	}
	items := inventory.Items[:0]
	for _, item := range inventory.Items {
		if _, formal := cognition.FormalAssetOwner(item.RelPath); formal {
			continue
		}
		items = append(items, item)
	}
	inventory.Items = items
	inventory.DiskTotal = facts.CodeSourceCount
	inventory.IndexRoleTotal = facts.CodeSourceCount
	inventory.ObserveTotal = facts.ManagedScope.ObserveCount
	inventory.ExcludeTotal = facts.ManagedScope.ExcludeCount
	inventory.IndexedTotal = facts.CodeSourceCount - len(facts.CodeDrift.Missing)
	if inventory.IndexedTotal < 0 {
		inventory.IndexedTotal = 0
	}
}

func buildVolumeScore(root string, cfg *config.Config, set *cognition.Set, facts *volumegovernance.Facts) (*indexgen.Score, error) {
	// Code quality continues to use the established nine-dimensional scorer.
	// Meta is prepended in memory so dictionary and quota authorities remain
	// available without inventing a second validator.
	score := &indexgen.Score{Dimensions: emptyVolumeDimensions()}
	if code := set.Volumes[cognition.ScopeCode]; code != nil && code.State == cognition.AssetPresent {
		raw := append(append([]byte{}, set.Meta.Raw...), '\n')
		raw = append(raw, code.Raw...)
		doc, warnings := index.Parse(string(raw))
		if len(warnings) != 0 {
			return nil, fmt.Errorf("volumes_score_projection_parse_warnings: %d", len(warnings))
		}
		index.ResolveRelPaths(doc, root)
		var err error
		score, err = indexgen.BuildScore(root, cfg, doc)
		if err != nil {
			return nil, err
		}
	}

	score.EntryCount = volumeObjectCount(set)
	score.DiskCount = facts.CodeSourceCount
	score.IndexTokens = facts.Budget.WholeIndexTokens
	score.Drift = indexgen.DriftSummary{
		Missing: len(facts.CodeDrift.Missing), RawMissing: len(facts.CodeDrift.Missing),
		Orphan: len(facts.CodeDrift.Orphan), Stale: len(facts.CodeDrift.Stale),
		Unbaselined: len(facts.CodeDrift.Unbaselined), LineEndingOnly: len(facts.CodeDrift.LineEndingOnly),
		ActionableMissing: len(facts.CodeDrift.Missing),
		ObservedNew:       len(facts.CodeDrift.ObservedNew), ObservedChanged: len(facts.CodeDrift.ObservedChanged),
		ObservedRemoved: len(facts.CodeDrift.ObservedRemoved),
	}
	score.ManagedScope = indexgen.ManagedScopeSummary{ScopeChangeRequired: facts.ManagedScope.ScopeChangeRequired,
		ObserveReviewRequired: cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired,
		PolicyIdentity:        facts.ManagedScope.PolicyIdentity, ActivePolicyIdentity: facts.ManagedScope.ActivePolicyIdentity,
		IndexCount: facts.ManagedScope.IndexCount, ObserveCount: facts.ManagedScope.ObserveCount,
		ExcludeCount: facts.ManagedScope.ExcludeCount, ObservedPendingReview: facts.ManagedScope.ObservedPendingReview}
	score.CognitionBudget = volumeBudgetReport(cfg, facts)
	applyVolumeGovernanceDimensions(score, set, facts)
	return score, nil
}

func emptyVolumeDimensions() []indexgen.Dimension {
	result := make([]indexgen.Dimension, 0, len(scoreDimNames))
	for _, name := range scoreDimNames {
		result = append(result, indexgen.Dimension{Name: name, Samples: []string{}, Note: "Volumes v1 shared governance projection"})
	}
	return result
}

func volumeBudgetReport(cfg *config.Config, facts *volumegovernance.Facts) *cognitionbudget.Report {
	policy, _ := cognitionbudget.Normalize(cfg.EffectiveCognitionBudget())
	return &cognitionbudget.Report{Version: machinecontract.CognitionBudgetReportV1,
		Mode: facts.Budget.Mode, Status: facts.Budget.Status, WholeIndexTokens: facts.Budget.WholeIndexTokens,
		TargetTokens: facts.Budget.TargetTokens, WarningTokens: facts.Budget.WarningTokens,
		MaxTokens: facts.Budget.MaxTokens, EntryCount: facts.CodeEntryCount + facts.DatabaseEntryCount,
		LargestEntries: []cognitionbudget.EntryCost{}, LargestR: []cognitionbudget.EntryCost{},
		LargestS: []cognitionbudget.EntryCost{}, Violations: append([]cognitionbudget.Violation{}, facts.Budget.Violations...),
		Policy: policy.WholeIndex}
}

func applyVolumeGovernanceDimensions(score *indexgen.Score, set *cognition.Set, facts *volumegovernance.Facts) {
	setDimension := func(name string, total, bad int, samples []string) {
		for index := range score.Dimensions {
			if score.Dimensions[index].Name == name {
				score.Dimensions[index].Total = total
				score.Dimensions[index].Bad = bad
				score.Dimensions[index].Samples = limitedSamples(samples, 5)
				return
			}
		}
	}
	objects := volumeObjectCount(set)
	setDimension("format", objects, 0, nil) // cognition.Load already failed closed on format errors.
	setDimension("coverage", facts.CodeSourceCount, len(facts.CodeDrift.Missing), facts.CodeDrift.Missing)
	freshness := append([]string{}, facts.CodeDrift.Missing...)
	for _, group := range [][]string{facts.CodeDrift.Orphan, facts.CodeDrift.Stale, facts.CodeDrift.Unbaselined,
		facts.CodeDrift.ObservedNew, facts.CodeDrift.ObservedChanged, facts.CodeDrift.ObservedRemoved} {
		freshness = append(freshness, group...)
	}
	setDimension("freshness", facts.CodeSourceCount, len(uniqueStrings(freshness)), uniqueStrings(freshness))
	setDimension("dict", objects, 0, nil) // scoped Meta dictionaries are validated by cognition.Load.
	setDimension("token", facts.Budget.WholeIndexTokens, len(facts.Budget.Violations), budgetViolationSamples(facts.Budget.Violations))
	agentBad := 0
	agentSamples := []string{}
	if !facts.GovernanceAligned {
		agentBad = 1
		agentSamples = append(agentSamples, facts.NextRequiredAction)
	}
	setDimension("agent_ready", 1, agentBad, agentSamples)
}

func budgetViolationSamples(values []cognitionbudget.Violation) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strings.TrimSpace(value.Code+" "+value.Path+" "+value.Field))
	}
	return result
}

func limitedSamples(values []string, limit int) []string {
	values = uniqueStrings(values)
	if len(values) > limit {
		values = values[:limit]
	}
	return values
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
