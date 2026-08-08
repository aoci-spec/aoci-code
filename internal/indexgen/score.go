// Package indexgen —— 本文件是score评分核心。
//
// 定位: 纯离线、纯读、零AI的索引质量九维度评分。全部判定复用既有机器闸
// (ValidateEntryLineWith/CheckSQuotaWith/CheckTagsAgainstDict/CheckEScale/
// BuildInventory/DetectWith/BuildMissingClassification)，本文件不建立第二套权威。
//
// 事实层与治理层分离:
//
//   - DriftSummary完整保留baseline.Detect原始四态、LineEndingOnly和Missing三分;
//
//   - coverage只统计ActionableMissing;
//
//   - freshness统计尚未解决的治理路径:
//     ActionableMissing、PendingCurationMissing、Orphan、Stale、Unbaselined;
//
//   - LineEndingOnly是换行表示信息态，不计freshness欠账;
//
//   - Included是Actionable子集，Pending是Skipped子集;
//
//   - CurationExcluded与非Pending技术跳过属于已经解释的排除治理结果，
//     不计coverage或freshness欠账;
//
//   - 三分守恒:
//
//     ActionableMissing + CurationExcludedMissing + SkippedMissing = Missing。
//
// E规模只评价仓库源码与静态文本实体；路径是否参与判定统一由
// index.ShouldCheckEScalePath裁决，Score不得持有本地路径排除副本。
//
// 九维固定顺序:
// format/coverage/freshness/squota/dict/token/agent_ready/escale/tagparse。
// 新维度只允许尾部追加。
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

// BuildScore计算九维度评分。
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

// BuildScoreWithLimit计算九维度评分，sampleLimit<=0表示样本不截断。
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

	// BuildInventory已经填充RelPath，重新采集统一样本路径口径。
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

	// 生产评分必须读取正式curation.json，不能回退到旧纯内存分类。
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

	// freshness回答“当前还有多少未解决治理路径”，而不是重新复制原始四态。
	//
	// 原始Missing、Orphan、Stale、Unbaselined、LineEndingOnly及Missing三分
	// 完整保存在score.Drift，并可通过aoci verify查看。
	// LineEndingOnly不进入本维度。
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
