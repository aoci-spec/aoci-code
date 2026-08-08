// Host-Agent确定性计划的内部稳定协议与plan_id摘要结构。
//
// Plan同时承载:
//   - 原始四态事实;
//   - Missing三分;
//   - Entries目标;
//   - PendingCuration目标;
//   - 当前curation.json摘要。
//
// 兼容纪律:
//   - 本文件中的agentPlan和agentPlanSummary保持历史JSON字段形态，
//     它们是plan_id稳定摘要的输入，不直接增加RawMissing等公共别名;
//   - 公共JSON别名位于index_agent_missing_view.go，仅负责输出;
//   - 相同仓库事实不得因术语别名增加而改变plan_id。
//
// plan_id不包含生成时间和绝对仓库路径，但包含索引、头部、策展资产、
// automation模式和全部任务集合。
//
// expected_e字段(R60-F.9-A4,2026-07-18 操作者裁决路b): 由目标行数与头部
// E阈值表经index.ExpectedEScaleSymbols确定性导出 —— 确定性字段不让模型猜。
// 阈值不可用或行数落入字典空隙时省略；该真实任务字段参与plan_id摘要。
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

const (
	agentPlanVersion = 1

	agentPlanStageBaselineRequired    = "baseline_required"
	agentPlanStageHeaderRequired      = "header_required"
	agentPlanStageIndexReviewRequired = "index_review_required"
	agentPlanStageEntriesRequired     = "entries_required"
	agentPlanStageCurationRequired    = "curation_required"
	agentPlanStageOrphanReview        = "orphan_review"
	agentPlanStageScopeChangeRequired = "scope_change_required"
	agentPlanStageObservedReview      = "observed_evidence_review_required"
	agentPlanStageCompressionRequired = "cognition_compression_required"
	agentPlanStageBudgetExceeded      = "budget_exceeded"
	agentPlanStageAligned             = "aligned"

	agentPlanActionScan              = "scan"
	agentPlanActionGenerateHead      = "generate_header"
	agentPlanActionReviewIndex       = "review_index"
	agentPlanActionStageEntries      = "stage_entries"
	agentPlanActionStageCuration     = "stage_curation"
	agentPlanActionReviewOrphans     = "review_orphans"
	agentPlanActionScopePreview      = "scope_preview"
	agentPlanActionReviewObserved    = "review_observed_evidence"
	agentPlanActionCompressCognition = "compress_cognition"
	agentPlanActionNone              = "none"

	agentPlanHeaderReady       = "ready"
	agentPlanHeaderMissing     = "missing"
	agentPlanHeaderUnparseable = "unparseable"
)

type agentPlanGovernance struct {
	ScopeChangeRequired   bool                    `json:"scope_change_required"`
	DesiredPolicyIdentity string                  `json:"desired_policy_identity"`
	ActivePolicyIdentity  string                  `json:"active_policy_identity,omitempty"`
	IndexCount            int                     `json:"index_count"`
	ObserveCount          int                     `json:"observe_count"`
	ExcludeCount          int                     `json:"exclude_count"`
	ObserveReviewRequired bool                    `json:"observe_review_required"`
	ObservedNew           []string                `json:"observed_new"`
	ObservedChanged       []string                `json:"observed_changed"`
	ObservedRemoved       []string                `json:"observed_removed"`
	CognitionBudget       *cognitionbudget.Report `json:"cognition_budget,omitempty"`
}

// agentPlanTarget 是可生成索引条目的文件目标。
type agentPlanTarget struct {
	Path             string   `json:"path"`
	Kind             string   `json:"kind"`
	SourceSHA256     string   `json:"source_sha256"`
	SizeBytes        int64    `json:"size_bytes"`
	Lines            int      `json:"lines,omitempty"`
	ExpectedE        []string `json:"expected_e,omitempty"`
	Ext              string   `json:"ext,omitempty"`
	SuggestedSection string   `json:"suggested_section,omitempty"`
	OldEntry         string   `json:"old_entry,omitempty"`
}

// agentPlanCurationTarget 是等待include/exclude语义裁决的物理特殊文件。
type agentPlanCurationTarget struct {
	Path          string `json:"path"`
	SourceSHA256  string `json:"source_sha256"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	Lines         int    `json:"lines,omitempty"`
	Ext           string `json:"ext,omitempty"`
	ProfileReason string `json:"profile_reason"`
}

// agentPlanSkipped 是当前不进入条目生成队列的Missing。
type agentPlanSkipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type localeMigrationCoverageCount struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
}

type localeMigrationCoverageStatus struct {
	Total   int    `json:"total"`
	Pending int    `json:"pending"`
	Status  string `json:"status"`
}

type localeMigrationExclusion struct {
	Surface string `json:"surface"`
	Reason  string `json:"reason"`
}

type localeMigrationUnresolved struct {
	Target string `json:"target"`
	Count  int    `json:"count"`
	Reason string `json:"reason"`
}

// localeMigrationCoverage is the machine-readable completion proof for every
// formal natural-language surface owned by AOCI. It reports category totals,
// not a repository-wide character heuristic.
type localeMigrationCoverage struct {
	Active             bool                          `json:"active"`
	FromLocale         string                        `json:"from_locale,omitempty"`
	ToLocale           string                        `json:"to_locale,omitempty"`
	Header             localeMigrationCoverageCount  `json:"header"`
	OrdinaryEntries    localeMigrationCoverageCount  `json:"ordinary_entries"`
	GovernanceEntries  localeMigrationCoverageCount  `json:"governance_entries"`
	Curation           localeMigrationCoverageCount  `json:"curation"`
	ManagedIndexText   localeMigrationCoverageCount  `json:"managed_index_text"`
	AgentsManagedBlock localeMigrationCoverageStatus `json:"agents_managed_block"`
	RuntimeContracts   localeMigrationCoverageStatus `json:"runtime_contracts"`
	Excluded           []localeMigrationExclusion    `json:"excluded"`
	Unresolved         []localeMigrationUnresolved   `json:"unresolved"`
}

// agentPlanSummary是参与plan_id摘要的历史内部计数结构。
//
// 字段语义固定:
//   - Missing = RawMissing;
//   - ActionableNew = ActionableMissing;
//   - CurationExcluded = CurationExcludedMissing;
//   - PendingCuration = PendingCurationMissing.
//
// 不得为了输出别名直接在本结构增加重复JSON字段。
type agentPlanSummary struct {
	Changed                int `json:"changed"`
	Missing                int `json:"missing"`
	ActionableNew          int `json:"actionable_new"`
	IncludedMissing        int `json:"included_missing"`
	CurationExcluded       int `json:"curation_excluded"`
	SkippedMissing         int `json:"skipped_missing"`
	PendingCuration        int `json:"pending_curation"`
	StaleCurationDecisions int `json:"stale_curation_decisions"`
	Orphan                 int `json:"orphan"`
	Unbaselined            int `json:"unbaselined"`
	ExecutableTargets      int `json:"executable_targets"`
}

// agentPlan 是plan_id摘要使用的内部稳定结构。
//
// 命令JSON输出必须经newAgentPlanView/writeAgentPlanJSON，不能直接编码本结构。
type agentPlan struct {
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
	LocaleMigration   localeMigrationCoverage   `json:"-"`
	SafeInventory     *afs.SafeInventorySummary `json:"safe_inventory,omitempty"`
	Governance        *agentPlanGovernance      `json:"governance,omitempty"`

	Summary agentPlanSummary `json:"summary"`

	Targets          []agentPlanTarget         `json:"targets"`
	CurationTargets  []agentPlanCurationTarget `json:"curation_targets"`
	CurationExcluded []string                  `json:"curation_excluded"`
	SkippedMissing   []agentPlanSkipped        `json:"skipped_missing"`
	Orphans          []string                  `json:"orphans"`
	Unbaselined      []string                  `json:"unbaselined"`
	Warnings         []string                  `json:"warnings"`
}

type agentPlanBuildError struct {
	Code int
	Err  error
}

func (e *agentPlanBuildError) Error() string {
	if e == nil ||
		e.Err == nil {
		return ""
	}

	return e.Err.Error()
}

func (e *agentPlanBuildError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// calculateAgentPlanID对历史内部Plan结构做稳定摘要。
//
// 公共JSON视图和Missing别名绝不进入该摘要面。
func calculateAgentPlanID(
	plan *agentPlan,
) (string, error) {
	copyForDigest := *plan
	copyForDigest.PlanID = ""
	copyForDigest.GeneratedAt = ""
	copyForDigest.BaselineUpdatedAt = ""
	copyForDigest.RepositoryRoot = ""
	if copyForDigest.SafeInventory != nil {
		summary := *copyForDigest.SafeInventory
		// Stage creates ignored .aoci Draft and audit files before the
		// pre-Apply Generation Plan guard. Their display-only counts must not
		// invalidate the Plan that authorized their creation. Safety remains
		// bound by rules/selection identities, managed counts, sensitive and
		// configured exclusions, unsafe objects, and required review facts.
		summary.Ignored = 0
		summary.RuntimeExcluded = 0
		summary.GeneratedExcluded = 0
		copyForDigest.SafeInventory = &summary
	}
	if copyForDigest.Governance != nil {
		governance := *copyForDigest.Governance
		// ExcludeCount includes the same hard-excluded runtime names summarized
		// above. Desired policy identity and Safe Inventory safety facts bind
		// meaningful exclusion changes; the aggregate display count does not.
		governance.ExcludeCount = 0
		copyForDigest.Governance = &governance
	}

	data, err := json.Marshal(
		copyForDigest,
	)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(
		sum[:],
	), nil
}

func sha256Hex(
	data []byte,
) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(
		sum[:],
	)
}

func renderAgentPlan(
	plan *agentPlan,
) string {
	var builder strings.Builder

	fmt.Fprintf(
		&builder,
		"AOCI agent plan @ %s\n",
		plan.GeneratedAt,
	)
	fmt.Fprintf(
		&builder,
		"stage: %s | next_action: %s\n",
		plan.Stage,
		plan.NextAction,
	)
	fmt.Fprintf(
		&builder,
		"plan_id: %s\n",
		plan.PlanID,
	)
	fmt.Fprintf(
		&builder,
		"automation.mode: %s | header: %s | baseline: %t\n",
		plan.AutomationMode,
		plan.HeaderState,
		plan.BaselineExists,
	)
	fmt.Fprintf(
		&builder,
		"index_sha256: %s\nheader_sha256: %s\ncuration_sha256: %s\n"+
			"repository_sha256: %s\n",
		plan.IndexSHA256,
		plan.HeaderSHA256,
		plan.CurationSHA256,
		plan.RepositorySHA256,
	)
	fmt.Fprint(&builder, cliMessage(
		"plan.render.facts",
		plan.Summary.Changed,
		plan.Summary.Missing,
		plan.Summary.Orphan,
		plan.Summary.Unbaselined,
	))
	fmt.Fprint(&builder, cliMessage(
		"plan.render.actions",
		plan.Summary.ExecutableTargets,
		plan.Summary.ActionableNew,
		plan.Summary.IncludedMissing,
		plan.Summary.PendingCuration,
		plan.Summary.CurationExcluded,
		plan.Summary.SkippedMissing,
	))

	if plan.HeaderMessage != "" {
		builder.WriteString(cliMessage(
			"plan.render.header_note",
			localeSafeCLIDetail(plan.HeaderMessage),
		))
	}
	if plan.IndexSelfStale {
		builder.WriteString(cliMessage("plan.render.index_stale"))
	}

	const displayLimit = 20

	for position, target := range plan.Targets {
		if position >= displayLimit {
			fmt.Fprint(&builder, cliMessage("plan.render.entries_truncated", len(plan.Targets)))
			break
		}

		fmt.Fprintf(
			&builder,
			"  [%s] %s sha256=%s\n",
			target.Kind,
			target.Path,
			target.SourceSHA256,
		)
	}

	for position, target := range plan.CurationTargets {
		if position >= displayLimit {
			fmt.Fprint(&builder, cliMessage(
				"plan.render.curation_truncated",
				len(plan.CurationTargets),
			))
			break
		}

		fmt.Fprintf(
			&builder,
			"  [curation:%s] %s sha256=%s\n",
			target.ProfileReason,
			target.Path,
			target.SourceSHA256,
		)
	}

	switch plan.NextAction {
	case agentPlanActionScan:
		builder.WriteString(cliMessage("plan.next.scan"))

	case agentPlanActionGenerateHead:
		builder.WriteString(cliMessage("plan.next.header"))

	case agentPlanActionReviewIndex:
		builder.WriteString(cliMessage("plan.next.review_index"))

	case agentPlanActionStageEntries:
		builder.WriteString(cliMessage("plan.next.entries"))

	case agentPlanActionStageCuration:
		builder.WriteString(cliMessage("plan.next.curation"))

	case agentPlanActionReviewOrphans:
		builder.WriteString(cliMessage("plan.next.orphans"))

	default:
		builder.WriteString(cliMessage("plan.next.none"))
	}

	return builder.String()
}
