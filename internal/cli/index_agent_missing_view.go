// Host-Agent Plan与Guide的公共JSON视图。
//
// 设计目的:
//   - 内部agentPlan和agentPlanSummary继续保持既有字段与JSON形态，
//     calculateAgentPlanID仍对原始内部结构做摘要，保证相同仓库事实的plan_id不变;
//   - 对外JSON同时保留旧字段和新增规范字段，给旧消费者一个兼容周期;
//   - 规范字段只作为同一事实的别名，不建立第二套分类或计数算法。
//
// 兼容映射:
//   - missing                 = raw_missing;
//   - actionable_new          = actionable_missing;
//   - curation_excluded       = curation_excluded_missing;
//   - pending_curation        = pending_curation_missing.
//
// 顶层列表兼容映射:
//   - curation_excluded       = curation_excluded_missing;
//   - curation_targets        = pending_curation_missing.
//
// 本文件只做内存视图和JSON编码，不读取仓库、不修改Plan、不写正式资产。
package cli

import (
	"encoding/json"
	"io"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// agentPlanSummaryView同时暴露历史字段与规范Missing字段。
type agentPlanSummaryView struct {
	Changed int `json:"changed"`

	// Missing是历史兼容字段，语义固定为原始物理Missing。
	Missing    int `json:"missing"`
	RawMissing int `json:"raw_missing"`

	// ActionableNew是历史兼容字段，语义固定为ActionableMissing。
	ActionableNew     int `json:"actionable_new"`
	ActionableMissing int `json:"actionable_missing"`

	IncludedMissing int `json:"included_missing"`

	// CurationExcluded是历史兼容字段。
	CurationExcluded        int `json:"curation_excluded"`
	CurationExcludedMissing int `json:"curation_excluded_missing"`

	SkippedMissing int `json:"skipped_missing"`

	// PendingCuration是历史兼容字段。
	PendingCuration        int `json:"pending_curation"`
	PendingCurationMissing int `json:"pending_curation_missing"`

	StaleCurationDecisions int `json:"stale_curation_decisions"`
	Orphan                 int `json:"orphan"`
	Unbaselined            int `json:"unbaselined"`
	ExecutableTargets      int `json:"executable_targets"`
}

// agentPlanView是Plan的公共JSON编码模型。
//
// 它不参与plan_id计算。内部agentPlan仍是安全摘要唯一输入。
type agentPlanView struct {
	Version           int                       `json:"version"`
	PlanID            string                    `json:"plan_id"`
	GeneratedAt       string                    `json:"generated_at"`
	Stage             string                    `json:"stage"`
	NextAction        string                    `json:"next_action"`
	RepositoryRoot    string                    `json:"repository_root"`
	IndexPath         string                    `json:"index_path"`
	IndexSHA256       string                    `json:"index_sha256"`
	HeaderSHA256      string                    `json:"header_sha256"`
	CurationSHA256    string                    `json:"curation_sha256"`
	RepositorySHA256  string                    `json:"repository_sha256,omitempty"`
	HeaderState       string                    `json:"header_state"`
	HeaderMessage     string                    `json:"header_message,omitempty"`
	BaselineExists    bool                      `json:"baseline_exists"`
	BaselineUpdatedAt string                    `json:"baseline_updated_at,omitempty"`
	AutomationMode    string                    `json:"automation_mode"`
	IndexSelfStale    bool                      `json:"index_self_stale"`
	LocaleMigration   localeMigrationCoverage   `json:"locale_migration"`
	SafeInventory     *afs.SafeInventorySummary `json:"safe_inventory,omitempty"`
	Governance        *agentPlanGovernance      `json:"governance,omitempty"`

	Summary agentPlanSummaryView `json:"summary"`

	Targets         []agentPlanTarget         `json:"targets"`
	CurationTargets []agentPlanCurationTarget `json:"curation_targets"`

	// PendingCurationMissing是CurationTargets的规范列表别名。
	PendingCurationMissing []agentPlanCurationTarget `json:"pending_curation_missing"`

	CurationExcluded []string `json:"curation_excluded"`

	// CurationExcludedMissing是CurationExcluded的规范列表别名。
	CurationExcludedMissing []string `json:"curation_excluded_missing"`

	SkippedMissing []agentPlanSkipped `json:"skipped_missing"`
	Orphans        []string           `json:"orphans"`
	Unbaselined    []string           `json:"unbaselined"`
	Warnings       []string           `json:"warnings"`
}

// agentGuideView保持Guide领域结构，只把嵌套Plan替换为公共Plan视图。
type agentGuideView struct {
	Version int            `json:"version"`
	Agent   string         `json:"agent"`
	Mode    string         `json:"mode"`
	Plan    *agentPlanView `json:"plan"`

	Complete         bool `json:"complete"`
	ApprovalRequired bool `json:"approval_required"`
	StopBeforeApply  bool `json:"stop_before_apply"`

	Message            string               `json:"message"`
	Instructions       []string             `json:"instructions"`
	Commands           agentGuideCommands   `json:"commands"`
	NextActionContract agentGuideNextAction `json:"next_action_contract"`

	HeaderStageRequest   *agentHeaderStageRequest   `json:"header_stage_request,omitempty"`
	EntriesStageRequest  *agentStageRequest         `json:"entries_stage_request,omitempty"`
	CurationStageRequest *agentCurationStageRequest `json:"curation_stage_request,omitempty"`

	Batch         *agentGuideBatch         `json:"batch,omitempty"`
	CurationBatch *agentGuideCurationBatch `json:"curation_batch,omitempty"`
}

func newAgentPlanSummaryView(
	summary agentPlanSummary,
) agentPlanSummaryView {
	return agentPlanSummaryView{
		Changed:                 summary.Changed,
		Missing:                 summary.Missing,
		RawMissing:              summary.Missing,
		ActionableNew:           summary.ActionableNew,
		ActionableMissing:       summary.ActionableNew,
		IncludedMissing:         summary.IncludedMissing,
		CurationExcluded:        summary.CurationExcluded,
		CurationExcludedMissing: summary.CurationExcluded,
		SkippedMissing:          summary.SkippedMissing,
		PendingCuration:         summary.PendingCuration,
		PendingCurationMissing:  summary.PendingCuration,
		StaleCurationDecisions:  summary.StaleCurationDecisions,
		Orphan:                  summary.Orphan,
		Unbaselined:             summary.Unbaselined,
		ExecutableTargets:       summary.ExecutableTargets,
	}
}

func newAgentPlanView(
	plan *agentPlan,
) *agentPlanView {
	if plan == nil {
		return nil
	}

	return &agentPlanView{
		Version:           plan.Version,
		PlanID:            plan.PlanID,
		GeneratedAt:       plan.GeneratedAt,
		Stage:             plan.Stage,
		NextAction:        plan.NextAction,
		RepositoryRoot:    plan.RepositoryRoot,
		IndexPath:         plan.IndexPath,
		IndexSHA256:       plan.IndexSHA256,
		HeaderSHA256:      plan.HeaderSHA256,
		CurationSHA256:    plan.CurationSHA256,
		RepositorySHA256:  plan.RepositorySHA256,
		HeaderState:       plan.HeaderState,
		HeaderMessage:     plan.HeaderMessage,
		BaselineExists:    plan.BaselineExists,
		BaselineUpdatedAt: plan.BaselineUpdatedAt,
		AutomationMode:    plan.AutomationMode,
		IndexSelfStale:    plan.IndexSelfStale,
		LocaleMigration:   plan.LocaleMigration,
		SafeInventory:     plan.SafeInventory,
		Governance:        plan.Governance,
		Summary:           newAgentPlanSummaryView(plan.Summary),
		Targets: append(
			[]agentPlanTarget{},
			plan.Targets...,
		),
		CurationTargets: append(
			[]agentPlanCurationTarget{},
			plan.CurationTargets...,
		),
		PendingCurationMissing: append(
			[]agentPlanCurationTarget{},
			plan.CurationTargets...,
		),
		CurationExcluded: append(
			[]string{},
			plan.CurationExcluded...,
		),
		CurationExcludedMissing: append(
			[]string{},
			plan.CurationExcluded...,
		),
		SkippedMissing: append(
			[]agentPlanSkipped{},
			plan.SkippedMissing...,
		),
		Orphans: append(
			[]string{},
			plan.Orphans...,
		),
		Unbaselined: append(
			[]string{},
			plan.Unbaselined...,
		),
		Warnings: append(
			[]string{},
			plan.Warnings...,
		),
	}
}

func newAgentGuideView(
	guide *agentGuide,
) *agentGuideView {
	if guide == nil {
		return nil
	}

	return &agentGuideView{
		Version:              guide.Version,
		Agent:                guide.Agent,
		Mode:                 guide.Mode,
		Plan:                 newAgentPlanView(guide.Plan),
		Complete:             guide.Complete,
		ApprovalRequired:     guide.ApprovalRequired,
		StopBeforeApply:      guide.StopBeforeApply,
		Message:              guide.Message,
		Instructions:         append([]string{}, guide.Instructions...),
		Commands:             guide.Commands,
		NextActionContract:   guide.NextActionContract,
		HeaderStageRequest:   guide.HeaderStageRequest,
		EntriesStageRequest:  guide.EntriesStageRequest,
		CurationStageRequest: guide.CurationStageRequest,
		Batch:                guide.Batch,
		CurationBatch:        guide.CurationBatch,
	}
}

func writeAgentPlanJSON(
	writer io.Writer,
	plan *agentPlan,
) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	return encoder.Encode(
		newAgentPlanView(plan),
	)
}

func writeAgentGuideJSON(
	writer io.Writer,
	guide *agentGuide,
) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	return encoder.Encode(
		newAgentGuideView(guide),
	)
}
