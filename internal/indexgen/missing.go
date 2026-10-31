// The Missing action-typing adapter layer of package indexgen.
//
// The raw physical fact is still produced by baseline.Detect:
//
//	Missing = exists on disk, absent from the formal index.
//
// File profiles, persistent include/exclude decisions, and PendingCuration are all delegated to
// internal/curation. indexgen is only responsible for mapping the core classification into the stable protocols of the CLI, Score, and the like, and
// for adding SuggestedSection to new files.
//
// Three-way conservation:
//
//	ActionableMissing
//	+ CurationExcludedMissing
//	+ SkippedMissing
//	= raw Missing.
//
// IncludedMissing is a subset of ActionableMissing.
// PendingCurationMissing is a subset of SkippedMissing.
// Neither subset may take part in the total as a fourth exclusive set.
package indexgen

import (
	"sort"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// SkippedMissing are the raw Missing that do not currently enter the automatic entry drafting queue.
type SkippedMissing struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// MissingClassification is the stable interpretation of the raw Missing.
type MissingClassification struct {
	Missing                 []string                    `json:"missing"`
	Actionable              []string                    `json:"actionable_missing"`
	Included                []string                    `json:"included_missing"`
	CurationExcluded        []string                    `json:"curation_excluded_missing"`
	CurationExcludedDetails []curation.ExcludedMissing  `json:"curation_excluded_details"`
	Skipped                 []SkippedMissing            `json:"skipped_missing"`
	Pending                 []curation.PendingCandidate `json:"pending_curation_missing"`
	StaleDecisions          []string                    `json:"stale_curation_decisions"`
	CurationSHA256          string                      `json:"curation_sha256"`
}

// ClassifyMissing keeps the historical pure in-memory call surface without repository context.
//
// This function cannot read curation.json and recognizes only team path exclusions and Inventory skip profiles.
// The formal production path must prefer BuildMissingClassification.
func ClassifyMissing(
	cfg *config.Config,
	rawMissing []string,
	inventory *Inventory,
) MissingClassification {
	result := MissingClassification{
		Missing:                 []string{},
		Actionable:              []string{},
		Included:                []string{},
		CurationExcluded:        []string{},
		CurationExcludedDetails: []curation.ExcludedMissing{},
		Skipped:                 []SkippedMissing{},
		Pending:                 []curation.PendingCandidate{},
		StaleDecisions:          []string{},
		CurationSHA256:          curation.HashBytes(nil),
	}

	skipReasonByPath := map[string]string{}
	if inventory != nil {
		for _, item := range inventory.Items {
			if item.RelPath == "" || item.SkipReason == "" {
				continue
			}
			skipReasonByPath[item.RelPath] = item.SkipReason
		}
	}

	paths := append([]string{}, rawMissing...)
	sort.Strings(paths)
	result.Missing = append(result.Missing, paths...)

	for _, rel := range paths {
		if cfg != nil && cfg.CurationExcluded(rel) {
			result.CurationExcluded = append(result.CurationExcluded, rel)
			result.CurationExcludedDetails = append(
				result.CurationExcludedDetails,
				curation.ExcludedMissing{
					Path:   rel,
					Reason: indexgenMessage("missing.reason_policy"),
					Source: "config",
				},
			)
			continue
		}

		if reason := skipReasonByPath[rel]; reason != "" {
			result.Skipped = append(
				result.Skipped,
				SkippedMissing{Path: rel, Reason: reason},
			)
			continue
		}

		result.Actionable = append(result.Actionable, rel)
	}

	return result
}

// BuildMissingClassification reads curation.json, builds the unified file profiles, and completes the production classification.
//
// The returned Inventory covers only this round's Raw Missing, for the Plan to fill Lines, Ext, and SuggestedSection.
func BuildMissingClassification(
	root string,
	cfg *config.Config,
	doc *index.Document,
	rawMissing []string,
) (
	MissingClassification,
	*Inventory,
	error,
) {
	coreClassification, _, curationSHA256, err := curation.BuildClassification(
		root,
		cfg,
		rawMissing,
	)
	if err != nil {
		return MissingClassification{}, nil, err
	}

	inventory := &Inventory{
		Items:        []Item{},
		DiskTotal:    len(rawMissing),
		IndexedTotal: 0,
	}

	sections := []sectionRef{}
	if doc != nil {
		index.ResolveRelPaths(doc, root)
		indexed, refs := collectIndexPaths(root, doc)
		inventory.IndexedTotal = len(indexed)
		sections = refs
	}

	paths := append([]string{}, rawMissing...)
	sort.Strings(paths)

	for _, rel := range paths {
		profile := coreClassification.Profiles[rel]
		inventory.Items = append(
			inventory.Items,
			Item{
				RelPath:          rel,
				SizeBytes:        profile.SizeBytes,
				Lines:            profile.Lines,
				Ext:              profile.Ext,
				SuggestedSection: suggestSection(rel, sections),
				SkipReason:       profile.Reason,
			},
		)
	}

	skipped := make([]SkippedMissing, 0, len(coreClassification.Skipped))
	for _, item := range coreClassification.Skipped {
		skipped = append(
			skipped,
			SkippedMissing{Path: item.Path, Reason: item.Reason},
		)
	}

	return MissingClassification{
		Missing:                 append([]string{}, coreClassification.Missing...),
		Actionable:              append([]string{}, coreClassification.Actionable...),
		Included:                append([]string{}, coreClassification.Included...),
		CurationExcluded:        append([]string{}, coreClassification.ExcludedPaths()...),
		CurationExcludedDetails: append([]curation.ExcludedMissing{}, coreClassification.CurationExcluded...),
		Skipped:                 skipped,
		Pending:                 append([]curation.PendingCandidate{}, coreClassification.Pending...),
		StaleDecisions:          append([]string{}, coreClassification.StaleDecisions...),
		CurationSHA256:          curationSHA256,
	}, inventory, nil
}
