// score协议、样本收集与漂移摘要辅助。
package indexgen

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// scoreSampleLimit 每维度违规样本默认最大展示条数。
const scoreSampleLimit = 5

// Dimension 是一个评分维度的结果。
type Dimension struct {
	Name    string   `json:"name"`
	Total   int      `json:"total"`
	Bad     int      `json:"bad"`
	Samples []string `json:"samples"`
	Note    string   `json:"note"`
}

// DriftSummary 是score对原始四态、换行信息态、Missing三分与策展子集的稳定暴露面。
//
// 向后兼容:
//   - Missing继续保留原始历史字段语义，始终等于RawMissing;
//   - PendingCuration继续保留历史字段语义，始终等于PendingCurationMissing。
//
// 新机器消费者应优先使用raw_missing和pending_curation_missing，避免把物理事实
// 误解为尚未解决的治理任务。
type DriftSummary struct {
	Missing                 int `json:"missing"`
	RawMissing              int `json:"raw_missing"`
	Orphan                  int `json:"orphan"`
	Stale                   int `json:"stale"`
	Unbaselined             int `json:"unbaselined"`
	LineEndingOnly          int `json:"line_ending_only"`
	ActionableMissing       int `json:"actionable_missing"`
	IncludedMissing         int `json:"included_missing"`
	CurationExcludedMissing int `json:"curation_excluded_missing"`
	SkippedMissing          int `json:"skipped_missing"`
	PendingCuration         int `json:"pending_curation"`
	PendingCurationMissing  int `json:"pending_curation_missing"`
	StaleCurationDecisions  int `json:"stale_curation_decisions"`
	ObservedNew             int `json:"observed_new"`
	ObservedChanged         int `json:"observed_changed"`
	ObservedRemoved         int `json:"observed_removed"`
}

type ManagedScopeSummary struct {
	ScopeChangeRequired   bool   `json:"scope_change_required"`
	ObserveReviewRequired bool   `json:"observe_review_required"`
	PolicyIdentity        string `json:"scope_policy_identity,omitempty"`
	ActivePolicyIdentity  string `json:"active_scope_policy_identity,omitempty"`
	IndexCount            int    `json:"index_count"`
	ObserveCount          int    `json:"observe_count"`
	ExcludeCount          int    `json:"exclude_count"`
	ObservedPendingReview int    `json:"observed_pending_review"`
}

// Score 是一次完整评分结果。
type Score struct {
	Dimensions      []Dimension             `json:"dimensions"`
	EntryCount      int                     `json:"entry_count"`
	DiskCount       int                     `json:"disk_count"`
	IndexTokens     int                     `json:"index_tokens_estimated"`
	CurationSHA256  string                  `json:"curation_sha256"`
	Drift           DriftSummary            `json:"drift"`
	ManagedScope    ManagedScopeSummary     `json:"managed_scope"`
	CognitionBudget *cognitionbudget.Report `json:"cognition_budget,omitempty"`
}

type sampleSink struct {
	limit int
	items []string
}

func newSink(limit int) *sampleSink {
	return &sampleSink{
		limit: limit,
		items: []string{},
	}
}

func (sink *sampleSink) add(value string) {
	if sink.limit > 0 &&
		len(sink.items) >= sink.limit {
		return
	}

	sink.items = append(
		sink.items,
		value,
	)
}

// buildDriftSummary 保留旧包内测试调用口。
// 没有仓库根上下文时只能识别config路径政策，不能读取curation.json。
func buildDriftSummary(
	cfg *config.Config,
	detected *baseline.DetectResult,
) DriftSummary {
	if detected == nil {
		return DriftSummary{}
	}

	classification := ClassifyMissing(
		cfg,
		detected.Missing,
		nil,
	)

	return buildDriftSummaryWithClassification(
		detected,
		classification,
	)
}

// buildDriftSummaryWithClassification 构建原始四态、换行信息态、
// Missing三分和策展子集。
func buildDriftSummaryWithClassification(
	detected *baseline.DetectResult,
	classification MissingClassification,
) DriftSummary {
	if detected == nil {
		return DriftSummary{}
	}

	rawMissing := len(detected.Missing)
	pendingCurationMissing := len(classification.Pending)

	return DriftSummary{
		Missing:                 rawMissing,
		RawMissing:              rawMissing,
		Orphan:                  len(detected.Orphan),
		Stale:                   len(detected.Stale),
		Unbaselined:             len(detected.Unbaselined),
		LineEndingOnly:          len(detected.LineEndingOnly),
		ActionableMissing:       len(classification.Actionable),
		IncludedMissing:         len(classification.Included),
		CurationExcludedMissing: len(classification.CurationExcluded),
		SkippedMissing:          len(classification.Skipped),
		PendingCuration:         pendingCurationMissing,
		PendingCurationMissing:  pendingCurationMissing,
		StaleCurationDecisions:  len(classification.StaleDecisions),
		ObservedNew:             len(detected.ObservedNew),
		ObservedChanged:         len(detected.ObservedChanged),
		ObservedRemoved:         len(detected.ObservedRemoved),
	}
}

func sQuotaNote(
	thresholds *index.SQuotaThresholds,
) string {
	switch {
	case thresholds.HasQuotas():
		return indexgenMessage("score.note_squota_header")

	case thresholds.SawSQuotaLine():
		return indexgenMessage("score.note_squota_invalid", machinecontract.NumericText().SQuotaDefaultExample)

	default:
		return indexgenMessage("score.note_squota_default")
	}
}

func eScaleNote(
	thresholds *index.EScaleThresholds,
) string {
	switch {
	case thresholds.HasThresholds():
		return indexgenMessage("score.note_escale_header")

	case thresholds.SawEScaleLine():
		return indexgenMessage("score.note_escale_invalid")

	default:
		return indexgenMessage("score.note_escale_missing")
	}
}
