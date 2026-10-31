// The file-level semantic curation classification of Raw Missing.
//
// Classification priority:
//
//	config.curation_exclude
//	> a file-level decision matching the current source_sha256
//	> the deterministic file profile
//	> ordinary ActionableMissing
//
// PendingCuration is a subset of SkippedMissing, keeping the existing three-way conservation.
// Only a valid include decision can promote empty/binary/oversize to Actionable.
package curation

import (
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// BuildClassification reads the current curation asset, builds the file profiles, and completes the Missing classification.
func BuildClassification(
	root string,
	cfg *config.Config,
	rawMissing []string,
) (
	Classification,
	*Document,
	string,
	error,
) {
	document, _, documentHash, err :=
		Load(root)
	if err != nil {
		return Classification{},
			nil,
			"",
			err
	}

	profiles := BuildProfiles(
		root,
		rawMissing,
	)

	return Classify(
			cfg,
			rawMissing,
			profiles,
			document,
		),
		document,
		documentHash,
		nil
}

// Classify is the pure in-memory classification core.
func Classify(
	cfg *config.Config,
	rawMissing []string,
	profiles map[string]Profile,
	document *Document,
) Classification {
	result := Classification{
		Missing:          []string{},
		Actionable:       []string{},
		Included:         []string{},
		CurationExcluded: []ExcludedMissing{},
		Skipped:          []SkippedMissing{},
		Pending:          []PendingCandidate{},
		StaleDecisions:   []string{},
		Profiles:         profiles,
	}

	paths := append(
		[]string{},
		rawMissing...,
	)
	sort.Strings(paths)
	result.Missing = append(
		result.Missing,
		paths...,
	)

	for _, rel := range paths {
		if cfg != nil &&
			cfg.CurationExcluded(rel) {
			result.CurationExcluded = append(
				result.CurationExcluded,
				ExcludedMissing{
					Path:   rel,
					Reason: "命中团队config.curation_exclude路径策略",
					Source: "config",
				},
			)
			continue
		}

		profile, found := profiles[rel]
		if !found {
			profile = Profile{
				Path: rel,
				Reason: ProfileReasonUnreadablePrefix +
					"缺少文件画像",
			}
		}

		decision, hasDecision :=
			DecisionByPath(
				document,
				rel,
			)

		validDecision :=
			hasDecision &&
				profile.SourceSHA256 != "" &&
				decision.SourceSHA256 ==
					profile.SourceSHA256

		if validDecision {
			switch decision.Decision {
			case DecisionInclude:
				result.Actionable = append(
					result.Actionable,
					rel,
				)
				result.Included = append(
					result.Included,
					rel,
				)
				continue

			case DecisionExclude:
				result.CurationExcluded = append(
					result.CurationExcluded,
					ExcludedMissing{
						Path:       rel,
						Reason:     decision.Reason,
						Confidence: decision.Confidence,
						Agent:      decision.Agent,
						Source:     "curation.json",
					},
				)
				continue
			}
		}

		if hasDecision &&
			!validDecision {
			result.StaleDecisions = append(
				result.StaleDecisions,
				rel,
			)
		}

		switch {
		case profile.Reason == ProfileReasonEmpty ||
			profile.Reason == ProfileReasonBinary ||
			profile.Reason == ProfileReasonOversize:
			result.Skipped = append(
				result.Skipped,
				SkippedMissing{
					Path:   rel,
					Reason: profile.Reason,
				},
			)
			result.Pending = append(
				result.Pending,
				PendingCandidate{
					Path:          rel,
					SourceSHA256:  profile.SourceSHA256,
					SizeBytes:     profile.SizeBytes,
					Lines:         profile.Lines,
					Ext:           profile.Ext,
					ProfileReason: profile.Reason,
				},
			)

		case strings.HasPrefix(
			profile.Reason,
			ProfileReasonUnreadablePrefix,
		):
			result.Skipped = append(
				result.Skipped,
				SkippedMissing{
					Path:   rel,
					Reason: profile.Reason,
				},
			)

		default:
			result.Actionable = append(
				result.Actionable,
				rel,
			)
		}
	}

	return result
}
