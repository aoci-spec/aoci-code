// Host-Agent Plan物理事实与目标组装辅助。
//
// 本文件只处理原始快照摘要、头部状态、目标物理画像和稳定列表转换。
// 是否派发任务的阶段裁决仍位于index_agent_plan.go。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
)

func populateLocaleMigrationTargets(
	plan *agentPlan,
	cfg *config.Config,
	document *index.Document,
	repoRoot string,
	snapshot map[string]baseline.Fingerprint,
	thresholds *index.EScaleThresholds,
) error {
	if cfg == nil || cfg.LocaleMigration == nil {
		return nil
	}
	existing := make(map[string]struct{}, len(plan.Targets))
	for _, target := range plan.Targets {
		existing[target.Path] = struct{}{}
	}
	ordinaryPaths, _ := splitLocaleMigrationEntryPaths(cfg.LocaleMigration)
	for _, relPath := range ordinaryPaths {
		if _, present := existing[relPath]; present {
			continue
		}
		fingerprint, present := snapshot[relPath]
		if !present {
			return &agentPlanBuildError{
				Code: ExitConfig,
				Err:  fmt.Errorf("%s", cliMessage("locale.plan.target_missing_disk", relPath)),
			}
		}
		entry := index.FindEntry(document, relPath)
		if entry == nil {
			return &agentPlanBuildError{
				Code: ExitConfig,
				Err:  fmt.Errorf("%s", cliMessage("locale.plan.target_missing_index", relPath)),
			}
		}
		target := agentPlanTarget{
			Path:         relPath,
			Kind:         "update",
			SourceSHA256: fingerprint.SHA256,
			SizeBytes:    fingerprint.Size,
			Ext:          filepath.Ext(relPath),
			OldEntry:     entry.FullLine,
		}
		fillTargetEScale(&target, repoRoot, thresholds)
		plan.Targets = append(plan.Targets, target)
		existing[relPath] = struct{}{}
	}
	sort.Slice(plan.Targets, func(left, right int) bool {
		return plan.Targets[left].Path < plan.Targets[right].Path
	})
	return nil
}

func populateLocaleMigrationCurationTargets(
	plan *agentPlan,
	cfg *config.Config,
	repoRoot string,
) {
	if cfg == nil || cfg.LocaleMigration == nil {
		return
	}
	existing := make(map[string]struct{}, len(plan.CurationTargets))
	for _, target := range plan.CurationTargets {
		existing[target.Path] = struct{}{}
	}
	profiles := curation.BuildProfiles(repoRoot, cfg.LocaleMigration.CurationPaths)
	for _, relPath := range cfg.LocaleMigration.CurationPaths {
		if _, present := existing[relPath]; present {
			continue
		}
		profile := profiles[relPath]
		reason := "locale_migration"
		if profile.Reason != "" {
			reason += ":" + profile.Reason
		}
		plan.CurationTargets = append(plan.CurationTargets, agentPlanCurationTarget{
			Path:          relPath,
			SourceSHA256:  profile.SourceSHA256,
			SizeBytes:     profile.SizeBytes,
			Lines:         profile.Lines,
			Ext:           profile.Ext,
			ProfileReason: reason,
		})
	}
	sort.Slice(plan.CurationTargets, func(left, right int) bool {
		return plan.CurationTargets[left].Path < plan.CurationTargets[right].Path
	})
}

// calculateRepositorySnapshotHash对当前仓库快照的路径、原始SHA和实际字节数
// 进行稳定摘要。
//
// NormalizedSHA256刻意不进入摘要：Plan与Stage绑定必须反映原始字节变化。
func calculateRepositorySnapshotHash(
	snapshot map[string]baseline.Fingerprint,
) string {
	keys := make(
		[]string,
		0,
		len(snapshot),
	)

	for relPath := range snapshot {
		keys = append(
			keys,
			relPath,
		)
	}

	sort.Strings(keys)

	var builder strings.Builder

	for _, relPath := range keys {
		fingerprint := snapshot[relPath]

		builder.WriteString(relPath)
		builder.WriteByte(0)
		builder.WriteString(
			fingerprint.SHA256,
		)
		builder.WriteByte(0)
		builder.WriteString(
			strconv.FormatInt(
				fingerprint.Size,
				10,
			),
		)
		builder.WriteByte(0)
	}

	return sha256Hex(
		[]byte(builder.String()),
	)
}

func inspectAgentPlanHeader(
	headerText string,
) (
	string,
	string,
) {
	dict := index.ExtractTagDict(
		headerText,
	)

	if dict != nil &&
		dict.HasDict() {
		return agentPlanHeaderReady, ""
	}

	if strings.Contains(
		headerText,
		"A层级",
	) ||
		strings.Contains(
			headerText,
			"B模块",
		) ||
		strings.Contains(
			headerText,
			"A Layer",
		) ||
		strings.Contains(
			headerText,
			"B Module",
		) {
		return agentPlanHeaderUnparseable,
			cliMessage("plan.header_unparseable")
	}

	return agentPlanHeaderMissing,
		cliMessage("plan.header_missing")
}

// fillTargetEScale为单个目标补齐Lines并导出ExpectedE。
// 统计失败或阈值不可判定时留空，不阻断计划。
func fillTargetEScale(
	target *agentPlanTarget,
	repoRoot string,
	thresholds *index.EScaleThresholds,
) {
	if target.Lines == 0 {
		absolutePath := filepath.Join(
			repoRoot,
			filepath.FromSlash(
				target.Path,
			),
		)

		if stat, statErr := os.Stat(
			absolutePath,
		); statErr == nil &&
			!stat.IsDir() {
			if lines, countErr :=
				afs.CountFileLines(
					absolutePath,
				); countErr == nil {
				target.Lines = lines
			}
		}
	}

	if target.Lines > 0 {
		target.ExpectedE =
			index.ExpectedEScaleSymbols(
				target.Lines,
				thresholds,
			)
	}
}

// populateAgentPlanTargets组装Entries更新与新增目标。
//
// 所有SourceSHA256都来自当前snapshot的原始字节指纹。
func populateAgentPlanTargets(
	plan *agentPlan,
	document *index.Document,
	repoRoot string,
	snapshot map[string]baseline.Fingerprint,
	classified updateClassification,
	inventory *indexgen.Inventory,
	thresholds *index.EScaleThresholds,
) error {
	for _, relPath := range classified.Changed {
		fingerprint := snapshot[relPath]
		oldEntry := ""

		if hit := index.FindEntry(
			document,
			relPath,
		); hit != nil {
			oldEntry = hit.FullLine
		}

		target := agentPlanTarget{
			Path:         relPath,
			Kind:         "update",
			SourceSHA256: fingerprint.SHA256,
			SizeBytes:    fingerprint.Size,
			Ext: filepath.Ext(
				relPath,
			),
			OldEntry: oldEntry,
		}

		fillTargetEScale(
			&target,
			repoRoot,
			thresholds,
		)

		plan.Targets = append(
			plan.Targets,
			target,
		)
	}

	inventoryByPath :=
		map[string]indexgen.Item{}

	if inventory != nil {
		for _, item := range inventory.Items {
			inventoryByPath[item.RelPath] =
				item
		}
	}

	for _, relPath := range classified.NewFiles {
		fingerprint := snapshot[relPath]

		target := agentPlanTarget{
			Path:         relPath,
			Kind:         "create",
			SourceSHA256: fingerprint.SHA256,
			SizeBytes:    fingerprint.Size,
			Ext: filepath.Ext(
				relPath,
			),
		}

		if item, found :=
			inventoryByPath[relPath]; found {
			target.Lines = item.Lines
			target.Ext = item.Ext
			target.SuggestedSection =
				item.SuggestedSection
		}

		fillTargetEScale(
			&target,
			repoRoot,
			thresholds,
		)

		plan.Targets = append(
			plan.Targets,
			target,
		)
	}

	return nil
}

func populateAgentPlanCurationTargets(
	plan *agentPlan,
	classification indexgen.MissingClassification,
) {
	for _, pending := range classification.Pending {
		plan.CurationTargets = append(
			plan.CurationTargets,
			agentPlanCurationTarget{
				Path: pending.Path,
				SourceSHA256: pending.
					SourceSHA256,
				SizeBytes: pending.SizeBytes,
				Lines:     pending.Lines,
				Ext:       pending.Ext,
				ProfileReason: pending.
					ProfileReason,
			},
		)
	}
}

func convertAgentPlanSkipped(
	items []indexgen.SkippedMissing,
) []agentPlanSkipped {
	result := make(
		[]agentPlanSkipped,
		0,
		len(items),
	)

	for _, item := range items {
		result = append(
			result,
			agentPlanSkipped{
				Path:   item.Path,
				Reason: item.Reason,
			},
		)
	}

	return result
}
