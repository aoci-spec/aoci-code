// Raw Missing的文件级语义策展分类。
//
// 分类优先级:
//
//	config.curation_exclude
//	> 与当前source_sha256匹配的文件级决策
//	> 确定性文件画像
//	> 普通ActionableMissing
//
// PendingCuration是SkippedMissing的子集，保持原有三分守恒。
// 只有有效include决策才能把empty/binary/oversize提升为Actionable。
package curation

import (
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// BuildClassification 读取当前策展资产、建立文件画像并完成Missing分类。
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

// Classify 是纯内存分类核心。
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
