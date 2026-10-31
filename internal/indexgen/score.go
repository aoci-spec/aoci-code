// Package indexgen: this file is the score computation core.
//
// Position: the purely offline, pure-read, zero-AI nine-dimension index quality score. Every judgement reuses the existing machine gates
// (ValidateEntryLineWith/CheckSQuotaWith/CheckTagsAgainstDict/CheckEScale/
// BuildInventory/DetectWith/BuildMissingClassification); this file establishes no second authority.
//
// The fact layer and the governance layer are separate:
//
//   - DriftSummary keeps baseline.Detect's raw four states, LineEndingOnly, and the Missing three-way split in full;
//
//   - coverage counts only ActionableMissing;
//
//   - freshness counts the governance paths not yet resolved:
//     ActionableMissing, PendingCurationMissing, Orphan, Stale, Unbaselined;
//
//   - LineEndingOnly is the line-ending representation informational state and counts as no freshness debt;
//
//   - Included is a subset of Actionable, Pending a subset of Skipped;
//
//   - CurationExcluded and non-Pending technical skips are explained exclusion governance results
//     and count as neither coverage nor freshness debt;
//
//   - three-way conservation:
//
//     ActionableMissing + CurationExcludedMissing + SkippedMissing = Missing.
//
// E scale evaluates only the repository's source and static text entities; whether a path takes part is decided uniformly by
// index.ShouldCheckEScalePath, and Score must hold no local path exclusion copy.
//
// The fixed order of the nine dimensions:
// format/coverage/freshness/squota/dict/token/agent_ready/escale/tagparse.
// A new dimension may only be appended at the end.
package indexgen

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

// BuildScore computes the nine-dimension score.
func BuildScore(
	root string,
	cfg *config.Config,
	doc *index.Document,
) (*Score, error) {
	return BuildScoreWithLimit(
		root,
		cfg,
		doc,
		scoreSampleLimit,
	)
}

// BuildScoreWithLimit computes the nine-dimension score; sampleLimit<=0 means the samples are not truncated.
func BuildScoreWithLimit(
	root string,
	cfg *config.Config,
	doc *index.Document,
	sampleLimit int,
) (*Score, error) {
	score := &Score{
		Dimensions: []Dimension{},
	}

	headerText, _ := index.ExtractHeader(
		doc.RawText,
	)
	dict := index.ExtractTagDict(
		headerText,
	)
	sQuotaThresholds := index.ExtractSQuotaThresholds(
		headerText,
	)

	type entryRef struct {
		name string
		rel  string
		line string
	}

	collectEntries := func() []entryRef {
		entries := []entryRef{}

		for _, section := range doc.Sections {
			for _, entry := range section.Entries {
				entries = append(
					entries,
					entryRef{
						name: entry.Filename,
						rel:  entry.RelPath,
						line: entry.FullLine,
					},
				)
			}
		}

		return entries
	}

	display := func(
		entry entryRef,
	) string {
		if entry.rel != "" {
			return entry.rel
		}

		return entry.name
	}

	entries := collectEntries()
	score.EntryCount = len(entries)

	inventory, err := BuildInventory(
		root,
		cfg,
		doc,
	)
	if err != nil {
		return nil, err
	}

	score.DiskCount = inventory.DiskTotal

	// BuildInventory has already filled RelPath; recollect to unify the sample path reading.
	entries = collectEntries()

	state, err := managedstate.Load(root, cfg)
	if err != nil {
		return nil, err
	}
	currentBaseline := state.Baseline
	detected := &baseline.DetectResult{Missing: []string{}, Orphan: []string{}, Stale: []string{}, Unbaselined: []string{},
		LineEndingOnly: []string{}, ObservedNew: []string{}, ObservedChanged: []string{}, ObservedRemoved: []string{}, Warnings: []string{}}
	if !state.ScopeChangeRequired {
		detected, err = managedstate.Detect(root, cfg, doc, state)
		if err != nil {
			return nil, err
		}
	}
	score.ManagedScope = ManagedScopeSummary{ScopeChangeRequired: state.ScopeChangeRequired,
		PolicyIdentity: state.DesiredPolicyIdentity, ActivePolicyIdentity: state.ActivePolicyIdentity,
		IndexCount: inventory.IndexRoleTotal, ObserveCount: inventory.ObserveTotal, ExcludeCount: inventory.ExcludeTotal}
	policy := cfg.EffectiveManagedScope()
	score.ManagedScope.ObserveReviewRequired = !state.Legacy && policy.ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired
	if score.ManagedScope.ObserveReviewRequired {
		score.ManagedScope.ObservedPendingReview = len(detected.ObservedNew) + len(detected.ObservedChanged) + len(detected.ObservedRemoved)
	}
	budgetReport, budgetErr := cognitionbudget.Build(root, []byte(doc.RawText), cfg.EffectiveCognitionBudget())
	if budgetErr != nil {
		return nil, budgetErr
	}
	score.CognitionBudget = budgetReport

	// A production score must read the formal curation.json and must not fall back to the old pure in-memory classification.
	missingClassification, _, err :=
		BuildMissingClassification(
			root,
			cfg,
			doc,
			detected.Missing,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"构建score策展分类失败: %w",
			err,
		)
	}

	score.CurationSHA256 =
		missingClassification.CurationSHA256

	score.Drift =
		buildDriftSummaryWithClassification(
			detected,
			missingClassification,
		)

	entryViolations := make(
		[][]index.Violation,
		len(entries),
	)

	for position, entry := range entries {
		rel := entry.rel
		if rel == "" {
			rel = entry.name
		}

		entryViolations[position] =
			index.ValidateEntryLineWith(
				rel,
				entry.line,
				sQuotaThresholds,
			)
	}

	formatSamples := newSink(
		sampleLimit,
	)
	formatDimension := Dimension{
		Name:  "format",
		Total: len(entries),
		Note:  indexgenMessage("score.note_format"),
	}

	for position, entry := range entries {
		if index.HasError(
			entryViolations[position],
		) {
			formatDimension.Bad++
			formatSamples.add(
				display(entry),
			)
		}
	}

	formatDimension.Samples =
		formatSamples.items

	score.Dimensions = append(
		score.Dimensions,
		formatDimension,
	)

	coverageSamples := newSink(
		sampleLimit,
	)
	coverageDimension := Dimension{
		Name:  "coverage",
		Total: inventory.DiskTotal,
		Note:  indexgenMessage("score.note_coverage"),
	}

	for _, rel := range missingClassification.Actionable {
		coverageDimension.Bad++
		coverageSamples.add(rel)
	}

	if count := len(
		missingClassification.Included,
	); count > 0 {
		coverageDimension.Note += indexgenMessage("score.note_coverage_included", count)
	}

	if count := len(
		missingClassification.CurationExcluded,
	); count > 0 {
		coverageDimension.Note += indexgenMessage("score.note_coverage_excluded", count)
	}

	if count := len(
		missingClassification.Pending,
	); count > 0 {
		coverageDimension.Note += indexgenMessage("score.note_coverage_pending", count)
	}

	nonPendingSkipped :=
		len(missingClassification.Skipped) -
			len(missingClassification.Pending)

	if nonPendingSkipped > 0 {
		coverageDimension.Note += indexgenMessage("score.note_coverage_skipped", nonPendingSkipped)
	}

	coverageDimension.Samples =
		coverageSamples.items

	score.Dimensions = append(
		score.Dimensions,
		coverageDimension,
	)

	// freshness answers "how many unresolved governance paths remain now" rather than copying the raw four states again.
	//
	// The raw Missing, Orphan, Stale, Unbaselined, LineEndingOnly, and the Missing three-way split
	// are kept in full in score.Drift and can be seen through aoci verify.
	// LineEndingOnly does not enter this dimension.
	freshnessSamples := newSink(
		sampleLimit,
	)
	freshnessDimension := Dimension{
		Name:  "freshness",
		Total: inventory.DiskTotal,
		Note:  indexgenMessage("score.note_freshness"),
	}
	freshnessSeen := map[string]bool{}

	addFreshness := func(
		rel string,
	) {
		if rel == "" ||
			freshnessSeen[rel] {
			return
		}

		freshnessSeen[rel] = true
		freshnessDimension.Bad++
		freshnessSamples.add(rel)
	}

	for _, rel := range missingClassification.Actionable {
		addFreshness(rel)
	}

	for _, item := range missingClassification.Pending {
		addFreshness(item.Path)
	}

	for _, group := range [][]string{
		detected.Orphan,
		detected.Stale,
		detected.Unbaselined,
	} {
		for _, rel := range group {
			addFreshness(rel)
		}
	}
	if cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired {
		for _, group := range [][]string{detected.ObservedNew, detected.ObservedChanged, detected.ObservedRemoved} {
			for _, rel := range group {
				addFreshness(rel)
			}
		}
	}

	freshnessDimension.Samples =
		freshnessSamples.items

	score.Dimensions = append(
		score.Dimensions,
		freshnessDimension,
	)

	sQuotaSamples := newSink(
		sampleLimit,
	)
	sQuotaDimension := Dimension{
		Name:  "squota",
		Total: len(entries),
		Note: sQuotaNote(
			sQuotaThresholds,
		),
	}

	for _, entry := range entries {
		if violation := index.CheckSQuotaWith(
			entry.line,
			sQuotaThresholds,
		); violation != nil {
			sQuotaDimension.Bad++
			sQuotaSamples.add(
				display(entry),
			)
		}
	}

	sQuotaDimension.Samples =
		sQuotaSamples.items

	score.Dimensions = append(
		score.Dimensions,
		sQuotaDimension,
	)

	dictSamples := newSink(
		sampleLimit,
	)
	dictDimension := Dimension{
		Name: "dict",
		Note: indexgenMessage("score.note_dict"),
	}

	if dict.HasDict() {
		dictDimension.Total = len(entries)

		for _, entry := range entries {
			if violation := index.CheckTagsAgainstDict(
				entry.line,
				dict,
			); violation != nil {
				dictDimension.Bad++
				dictSamples.add(
					display(entry),
				)
			}
		}
	}

	dictDimension.Samples =
		dictSamples.items

	score.Dimensions = append(
		score.Dimensions,
		dictDimension,
	)

	score.IndexTokens =
		ledger.EstimateTokens(
			doc.RawText,
		)

	score.Dimensions = append(
		score.Dimensions,
		Dimension{
			Name:    "token",
			Total:   score.IndexTokens,
			Samples: []string{},
			Note:    indexgenMessage("score.note_token"),
		},
	)

	agentDimension := Dimension{
		Name:    "agent_ready",
		Total:   3,
		Samples: []string{},
		Note:    indexgenMessage("score.note_agent"),
	}

	if !dict.HasDict() {
		agentDimension.Bad++
		agentDimension.Samples = append(
			agentDimension.Samples,
			indexgenMessage("score.sample_agent_dict"),
		)
	}

	if currentBaseline == nil {
		agentDimension.Bad++
		agentDimension.Samples = append(
			agentDimension.Samples,
			indexgenMessage("score.sample_agent_baseline"),
		)
	}

	if len(entries) == 0 {
		agentDimension.Bad++
		agentDimension.Samples = append(
			agentDimension.Samples,
			indexgenMessage("score.sample_agent_entries"),
		)
	}

	score.Dimensions = append(
		score.Dimensions,
		agentDimension,
	)

	eScaleThresholds :=
		index.ExtractEScaleThresholds(
			headerText,
		)

	eScaleSamples := newSink(
		sampleLimit,
	)
	eScaleDimension := Dimension{
		Name: "escale",
		Note: eScaleNote(
			eScaleThresholds,
		) + indexgenMessage("score.note_escale_suffix"),
	}

	if eScaleThresholds.HasThresholds() {
		eScaleDimension.Total =
			len(entries)

		for _, entry := range entries {
			if !index.ShouldCheckEScalePath(
				entry.rel,
			) {
				continue
			}

			absolutePath := filepath.Join(
				root,
				filepath.FromSlash(
					entry.rel,
				),
			)

			info, statErr := os.Stat(
				absolutePath,
			)
			if statErr != nil ||
				info.IsDir() {
				continue
			}

			lines, countErr :=
				fs.CountFileLines(
					absolutePath,
				)
			if countErr != nil {
				continue
			}

			if violation := index.CheckEScale(
				entry.line,
				lines,
				eScaleThresholds,
			); violation != nil {
				eScaleDimension.Bad++
				eScaleSamples.add(
					display(entry),
				)
			}
		}
	}

	eScaleDimension.Samples =
		eScaleSamples.items

	score.Dimensions = append(
		score.Dimensions,
		eScaleDimension,
	)

	tagParseSamples := newSink(
		sampleLimit,
	)
	tagParseDimension := Dimension{
		Name:  "tagparse",
		Total: len(entries),
		Note:  indexgenMessage("score.note_tagparse"),
	}

	for position, entry := range entries {
		if index.HasTagParseWarning(
			entryViolations[position],
		) {
			tagParseDimension.Bad++
			tagParseSamples.add(
				display(entry),
			)
		}
	}

	tagParseDimension.Samples =
		tagParseSamples.items

	score.Dimensions = append(
		score.Dimensions,
		tagParseDimension,
	)

	return score, nil
}
