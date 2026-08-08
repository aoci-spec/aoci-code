// Package indexgen 的Missing行动分型适配层。
//
// 原始物理事实仍由baseline.Detect产生:
//
//	Missing = 磁盘存在、正式索引不存在。
//
// 文件画像、持久化include/exclude决策和PendingCuration统一委托
// internal/curation。indexgen只负责把核心分类映射为CLI、Score等稳定协议，并为
// 新文件补充SuggestedSection。
//
// 三分守恒:
//
//	ActionableMissing
//	+ CurationExcludedMissing
//	+ SkippedMissing
//	= 原始Missing。
//
// IncludedMissing是ActionableMissing的子集。
// PendingCurationMissing是SkippedMissing的子集。
// 两个子集都不得作为第四个互斥集合参与总数相加。
package indexgen

import (
	"sort"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// SkippedMissing 是当前不进入自动条目起草队列的原始Missing。
type SkippedMissing struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// MissingClassification 是原始Missing的稳定解释。
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

// ClassifyMissing 保留无仓库上下文的历史纯内存调用面。
//
// 该函数无法读取curation.json，只识别团队路径排除与Inventory跳过画像。
// 正式生产路径必须优先使用BuildMissingClassification。
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

// BuildMissingClassification 读取curation.json、建立统一文件画像并完成生产分类。
//
// 返回Inventory只覆盖本轮Raw Missing，用于Plan填充Lines、Ext和SuggestedSection。
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
