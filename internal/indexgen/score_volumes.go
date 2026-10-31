// The nine-dimension score over a Cognition Volumes repository: the same
// dimensions, names, and JSON shape as the Legacy score, with every drift,
// budget, and E scale number projected from the governance facts Verify and
// Check already report, so the score never computes those a second time.
package indexgen

import (
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// VolumeScoreDrift carries the Code drift lists a Volumes score reads: the
// one CodeDrift projection Verify, Check, and scope status share. Missing
// holds only the paths that await an Entry; Skipped and CurationExcluded are
// the held sources beside it, which are reported and never counted as debt.
type VolumeScoreDrift struct {
	Missing, Orphan, Stale, Unbaselined, LineEndingOnly []string
	Skipped, CurationExcluded                           []string
	ObservedNew, ObservedChanged, ObservedRemoved       []string
}

// VolumeScoreBudget is the Whole-Index token budget and the S coverage of the
// Code Volume as the governance budget facts measure them; the token
// dimension and the score's budget block are this value.
type VolumeScoreBudget struct {
	Mode                         string                          `json:"mode"`
	Status                       string                          `json:"status"`
	WholeIndexTokens             int                             `json:"whole_index_tokens"`
	TargetTokens                 int                             `json:"target_tokens"`
	WarningTokens                int                             `json:"warning_tokens"`
	MaxTokens                    int                             `json:"max_tokens"`
	Violations                   int                             `json:"violations"`
	SCoverage                    []cognitionbudget.SCoverageBand `json:"s_coverage"`
	HighImportanceEntries        int                             `json:"high_importance_entries"`
	HighImportanceSAbsent        int                             `json:"high_importance_s_absent"`
	HighImportanceSAbsentPercent int                             `json:"high_importance_s_absent_percent"`
}

// VolumeScoreEScale is the E scale audit as the governance audit reports it:
// how many Code Entries it judged, how many disagree with their source, and
// the paths of those.
type VolumeScoreEScale struct {
	Checked, Mismatched int
	Paths               []string
}

// VolumeScoreInput is what a Volumes score reads beside the loaded set. The
// caller fills it from the governance facts; nothing here is recomputed.
type VolumeScoreInput struct {
	CodeSourceCount int
	BaselineMissing bool
	Drift           VolumeScoreDrift
	ManagedScope    ManagedScopeSummary
	Budget          VolumeScoreBudget
	EScale          VolumeScoreEScale
}

// VolumeAssetSummary names one formal asset of the scored layout.
type VolumeAssetSummary struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	AssetState  string `json:"asset_state"`
	SHA256      string `json:"sha256,omitempty"`
	ObjectCount int    `json:"object_count"`
}

// scoredObject is one Volume object as the dimensions see it: the line the
// checks read, the path the format gate compares the name against, the name
// a sample shows, and the dictionary its Volume is tagged under.
type scoredObject struct {
	display, rel, line string
	dict               *index.TagDict
	code               bool
}

// BuildVolumeScore scores a Volumes v1 set. Code and Database objects enter
// format, squota, dict, and tagparse, each judged under its own scoped
// dictionary; coverage, freshness, and escale are Code facts, since only Code
// objects have a source file; token is the Whole-Index budget the governance
// facts measured over every formal asset. sampleLimit<=0 keeps every sample.
func BuildVolumeScore(set *cognition.Set, in VolumeScoreInput, sampleLimit int) *Score {
	score := &Score{Dimensions: []Dimension{}, Layout: set.LayoutMode, Volumes: volumeAssetSummaries(set)}
	meta := string(set.Meta.Raw)
	dicts := map[string]*index.TagDict{
		cognition.ScopeCode:     index.ExtractScopedTagDict(meta, cognition.ScopeCode),
		cognition.ScopeDatabase: index.ExtractScopedTagDict(meta, cognition.ScopeDatabase),
	}
	sQuota := index.ExtractSQuotaThresholds(meta)
	eScale := index.ExtractEScaleThresholds(index.ScopedDictionaryText(meta, cognition.ScopeCode))

	objects := []scoredObject{}
	codeEntries := 0
	for _, id := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		asset := set.Volumes[id]
		if asset == nil || asset.Descriptor.State == machinecontract.CognitionVolumeDisabled {
			continue
		}
		for _, object := range asset.Objects {
			scored := scoredObject{display: object.CanonicalRef, rel: object.Name, line: object.CanonicalLine, dict: dicts[id], code: id == cognition.ScopeCode}
			if scored.code {
				codeEntries++
				if object.Entry != nil && object.Entry.RelPath != "" {
					scored.display, scored.rel = object.Entry.RelPath, object.Entry.RelPath
				}
			}
			objects = append(objects, scored)
		}
	}
	score.EntryCount = len(objects)
	score.DiskCount = in.CodeSourceCount
	score.ManagedScope = in.ManagedScope
	score.Drift = volumeDriftSummary(in.Drift)
	budget := in.Budget
	if budget.SCoverage == nil {
		budget.SCoverage = []cognitionbudget.SCoverageBand{}
	}
	score.Budget = &budget

	violations := make([][]index.Violation, len(objects))
	for position, object := range objects {
		violations[position] = index.ValidateEntryLineWith(object.rel, object.line, sQuota)
	}

	format := Dimension{Name: "format", Total: len(objects), Note: indexgenMessage("score.note_format")}
	formatSamples := newSink(sampleLimit)
	for position, object := range objects {
		if index.HasError(violations[position]) {
			format.Bad++
			formatSamples.add(object.display)
		}
	}
	format.Samples = formatSamples.items
	score.Dimensions = append(score.Dimensions, format)

	coverage := Dimension{Name: "coverage", Total: in.CodeSourceCount, Note: indexgenMessage("score.note_coverage")}
	coverageSamples := newSink(sampleLimit)
	for _, rel := range in.Drift.Missing {
		coverage.Bad++
		coverageSamples.add(rel)
	}
	if count := len(in.Drift.CurationExcluded); count > 0 {
		coverage.Note += indexgenMessage("score.note_coverage_excluded", count)
	}
	if count := len(in.Drift.Skipped); count > 0 {
		coverage.Note += indexgenMessage("score.note_coverage_skipped", count)
	}
	coverage.Samples = coverageSamples.items
	score.Dimensions = append(score.Dimensions, coverage)

	// freshness is the unresolved governance paths, each once: the same lists
	// Verify counts, with Observe changes only where the policy makes them
	// a review.
	freshness := Dimension{Name: "freshness", Total: in.CodeSourceCount, Note: indexgenMessage("score.note_freshness")}
	freshnessSamples := newSink(sampleLimit)
	seen := map[string]bool{}
	groups := [][]string{in.Drift.Missing, in.Drift.Orphan, in.Drift.Stale, in.Drift.Unbaselined}
	if in.ManagedScope.ObserveReviewRequired {
		groups = append(groups, in.Drift.ObservedNew, in.Drift.ObservedChanged, in.Drift.ObservedRemoved)
	}
	for _, group := range groups {
		for _, rel := range group {
			if rel == "" || seen[rel] {
				continue
			}
			seen[rel] = true
			freshness.Bad++
			freshnessSamples.add(rel)
		}
	}
	freshness.Samples = freshnessSamples.items
	score.Dimensions = append(score.Dimensions, freshness)

	squota := Dimension{Name: "squota", Total: len(objects), Note: sQuotaNote(sQuota)}
	squotaSamples := newSink(sampleLimit)
	for _, object := range objects {
		if index.CheckSQuotaWith(object.line, sQuota) != nil {
			squota.Bad++
			squotaSamples.add(object.display)
		}
	}
	squota.Samples = squotaSamples.items
	score.Dimensions = append(score.Dimensions, squota)

	dict := Dimension{Name: "dict", Note: indexgenMessage("score.note_dict")}
	dictSamples := newSink(sampleLimit)
	for _, object := range objects {
		if !object.dict.HasDict() {
			continue
		}
		dict.Total++
		if index.CheckTagsAgainstDict(object.line, object.dict) != nil {
			dict.Bad++
			dictSamples.add(object.display)
		}
	}
	dict.Samples = dictSamples.items
	score.Dimensions = append(score.Dimensions, dict)

	score.IndexTokens = in.Budget.WholeIndexTokens
	score.Dimensions = append(score.Dimensions, Dimension{Name: "token", Total: score.IndexTokens, Samples: []string{}, Note: indexgenMessage("score.note_token")})

	agent := Dimension{Name: "agent_ready", Total: 3, Samples: []string{}, Note: indexgenMessage("score.note_agent")}
	if !dicts[cognition.ScopeCode].HasDict() {
		agent.Bad++
		agent.Samples = append(agent.Samples, indexgenMessage("score.sample_agent_dict"))
	}
	if in.BaselineMissing {
		agent.Bad++
		agent.Samples = append(agent.Samples, indexgenMessage("score.sample_agent_baseline"))
	}
	if len(objects) == 0 {
		agent.Bad++
		agent.Samples = append(agent.Samples, indexgenMessage("score.sample_agent_entries"))
	}
	score.Dimensions = append(score.Dimensions, agent)

	escale := Dimension{Name: "escale", Note: eScaleNote(eScale) + indexgenMessage("score.note_escale_suffix")}
	escaleSamples := newSink(sampleLimit)
	if eScale.HasThresholds() {
		escale.Total = codeEntries
		escale.Bad = in.EScale.Mismatched
		for _, rel := range in.EScale.Paths {
			escaleSamples.add(rel)
		}
	}
	escale.Samples = escaleSamples.items
	score.Dimensions = append(score.Dimensions, escale)

	tagparse := Dimension{Name: "tagparse", Total: len(objects), Note: indexgenMessage("score.note_tagparse")}
	tagparseSamples := newSink(sampleLimit)
	for position, object := range objects {
		if index.HasTagParseWarning(violations[position]) {
			tagparse.Bad++
			tagparseSamples.add(object.display)
		}
	}
	tagparse.Samples = tagparseSamples.items
	score.Dimensions = append(score.Dimensions, tagparse)
	return score
}

// volumeDriftSummary fills the Legacy drift fields from the Code drift lists.
// Raw Missing is every index-role file without an Entry; Actionable is the
// part that awaits one; the held sources are reported under the Legacy names
// for a technical skip and a curation exclusion. Volumes has no pending
// Curation, so those fields stay zero.
func volumeDriftSummary(drift VolumeScoreDrift) DriftSummary {
	raw := len(drift.Missing) + len(drift.Skipped) + len(drift.CurationExcluded)
	return DriftSummary{
		Missing: raw, RawMissing: raw,
		Orphan: len(drift.Orphan), Stale: len(drift.Stale), Unbaselined: len(drift.Unbaselined),
		LineEndingOnly:          len(drift.LineEndingOnly),
		ActionableMissing:       len(drift.Missing),
		CurationExcludedMissing: len(drift.CurationExcluded),
		SkippedMissing:          len(drift.Skipped),
		ObservedNew:             len(drift.ObservedNew), ObservedChanged: len(drift.ObservedChanged), ObservedRemoved: len(drift.ObservedRemoved),
	}
}

// volumeAssetSummaries lists the Root, the Meta, and every declared Volume in
// declaration order, each with the state and digest the loader recorded.
func volumeAssetSummaries(set *cognition.Set) []VolumeAssetSummary {
	summaries := []VolumeAssetSummary{}
	add := func(asset *cognition.Asset) {
		if asset == nil {
			return
		}
		summaries = append(summaries, VolumeAssetSummary{ID: asset.Descriptor.ID, Kind: asset.Descriptor.Kind, Path: asset.Descriptor.Path,
			AssetState: asset.State, SHA256: asset.SHA256, ObjectCount: asset.ObjectCount})
	}
	add(&set.Root)
	add(&set.Meta)
	for _, id := range set.DeclaredOrder {
		if id == cognition.ScopeMeta {
			continue
		}
		add(set.Volumes[id])
	}
	return summaries
}
