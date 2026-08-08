// Plan/Guide公共Missing字段别名与plan_id兼容测试。
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// planDigestWithoutAuditTimestampFixture固定不包含BaselineUpdatedAt的摘要结果。
//
// 任何仅用于输出的字段别名或审计时间变化都不得改变该值；真实任务绑定字段
// 变化仍必须改变摘要，不能在术语清理中静默修改。
const planDigestWithoutAuditTimestampFixture = "cd9897f0db2c5e0d429a56946e876d61422f2fbb1fb8c657f86c0e3553a6d58f"

func agentMissingViewFixture() *agentPlan {
	return &agentPlan{
		Version:           agentPlanVersion,
		PlanID:            strings.Repeat("a", 64),
		GeneratedAt:       "2026-07-20T00:00:00Z",
		Stage:             agentPlanStageEntriesRequired,
		NextAction:        agentPlanActionStageEntries,
		RepositoryRoot:    "/repo",
		IndexPath:         "aoci.txt",
		IndexSHA256:       strings.Repeat("b", 64),
		HeaderSHA256:      strings.Repeat("c", 64),
		CurationSHA256:    strings.Repeat("e", 64),
		RepositorySHA256:  strings.Repeat("d", 64),
		HeaderState:       agentPlanHeaderReady,
		BaselineExists:    true,
		BaselineUpdatedAt: "2026-07-19T00:00:00Z",
		AutomationMode:    "auto",
		Summary: agentPlanSummary{
			Changed:           1,
			Missing:           3,
			ActionableNew:     1,
			CurationExcluded:  1,
			SkippedMissing:    1,
			PendingCuration:   1,
			Orphan:            1,
			ExecutableTargets: 2,
		},
		Targets: []agentPlanTarget{
			{
				Path:         "x.go",
				Kind:         "update",
				SourceSHA256: strings.Repeat("1", 64),
				SizeBytes:    10,
				Lines:        3,
				ExpectedE:    []string{"T"},
				Ext:          ".go",
				OldEntry: "x.go[XAP7T]: F:x | " +
					"R:- | A:- | S:-",
			},
			{
				Path:             "new.go",
				Kind:             "create",
				SourceSHA256:     strings.Repeat("2", 64),
				SizeBytes:        20,
				Lines:            4,
				ExpectedE:        []string{"T"},
				Ext:              ".go",
				SuggestedSection: "代码索引",
			},
		},
		CurationTargets: []agentPlanCurationTarget{
			{
				Path:          "empty.txt",
				SourceSHA256:  strings.Repeat("3", 64),
				Ext:           ".txt",
				ProfileReason: "empty",
			},
		},
		CurationExcluded: []string{
			"docs/x.md",
		},
		SkippedMissing: []agentPlanSkipped{
			{
				Path:   "empty.txt",
				Reason: "empty",
			},
		},
		Orphans: []string{
			"ghost.go",
		},
		Unbaselined: []string{},
		Warnings:    []string{},
	}
}

func TestAgentPlanPublicJSONAddsCanonicalAliasesWithoutChangingDigest(
	t *testing.T,
) {
	plan := agentMissingViewFixture()

	before, err := calculateAgentPlanID(plan)
	if err != nil {
		t.Fatal(err)
	}

	if before != planDigestWithoutAuditTimestampFixture {
		t.Fatalf(
			"内部Plan摘要形态已变化: got=%s want=%s",
			before,
			planDigestWithoutAuditTimestampFixture,
		)
	}

	var output bytes.Buffer
	if err := writeAgentPlanJSON(
		&output,
		plan,
	); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Summary struct {
			Missing                 int `json:"missing"`
			RawMissing              int `json:"raw_missing"`
			ActionableNew           int `json:"actionable_new"`
			ActionableMissing       int `json:"actionable_missing"`
			CurationExcluded        int `json:"curation_excluded"`
			CurationExcludedMissing int `json:"curation_excluded_missing"`
			PendingCuration         int `json:"pending_curation"`
			PendingCurationMissing  int `json:"pending_curation_missing"`
		} `json:"summary"`

		CurationTargets         []agentPlanCurationTarget `json:"curation_targets"`
		PendingCurationMissing  []agentPlanCurationTarget `json:"pending_curation_missing"`
		CurationExcluded        []string                  `json:"curation_excluded"`
		CurationExcludedMissing []string                  `json:"curation_excluded_missing"`
	}

	if err := json.Unmarshal(
		output.Bytes(),
		&payload,
	); err != nil {
		t.Fatalf(
			"公共Plan JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	if payload.Summary.Missing !=
		payload.Summary.RawMissing ||
		payload.Summary.Missing != 3 {
		t.Fatalf(
			"missing与raw_missing兼容别名不符: %+v",
			payload.Summary,
		)
	}

	if payload.Summary.ActionableNew !=
		payload.Summary.ActionableMissing ||
		payload.Summary.ActionableMissing != 1 {
		t.Fatalf(
			"actionable_new与actionable_missing兼容别名不符: %+v",
			payload.Summary,
		)
	}

	if payload.Summary.CurationExcluded !=
		payload.Summary.CurationExcludedMissing ||
		payload.Summary.CurationExcludedMissing != 1 {
		t.Fatalf(
			"curation_excluded兼容别名不符: %+v",
			payload.Summary,
		)
	}

	if payload.Summary.PendingCuration !=
		payload.Summary.PendingCurationMissing ||
		payload.Summary.PendingCurationMissing != 1 {
		t.Fatalf(
			"pending_curation兼容别名不符: %+v",
			payload.Summary,
		)
	}

	if len(payload.CurationTargets) != 1 ||
		len(payload.PendingCurationMissing) != 1 ||
		payload.CurationTargets[0].Path !=
			payload.PendingCurationMissing[0].Path {
		t.Fatalf(
			"pending_curation_missing列表别名不符: old=%+v canonical=%+v",
			payload.CurationTargets,
			payload.PendingCurationMissing,
		)
	}

	if strings.Join(
		payload.CurationExcluded,
		"\n",
	) != strings.Join(
		payload.CurationExcludedMissing,
		"\n",
	) {
		t.Fatalf(
			"curation_excluded_missing列表别名不符: old=%v canonical=%v",
			payload.CurationExcluded,
			payload.CurationExcludedMissing,
		)
	}

	after, err := calculateAgentPlanID(plan)
	if err != nil {
		t.Fatal(err)
	}

	if before != after ||
		after != planDigestWithoutAuditTimestampFixture {
		t.Fatalf(
			"公共JSON编码不得改变plan_id摘要: before=%s after=%s",
			before,
			after,
		)
	}
}

func TestAgentGuidePublicJSONUsesCanonicalNestedPlanView(
	t *testing.T,
) {
	plan := agentMissingViewFixture()

	digest, err := calculateAgentPlanID(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanID = digest

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := writeAgentGuideJSON(
		&output,
		guide,
	); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Plan struct {
			PlanID  string `json:"plan_id"`
			Summary struct {
				Missing                int `json:"missing"`
				RawMissing             int `json:"raw_missing"`
				ActionableNew          int `json:"actionable_new"`
				ActionableMissing      int `json:"actionable_missing"`
				PendingCuration        int `json:"pending_curation"`
				PendingCurationMissing int `json:"pending_curation_missing"`
			} `json:"summary"`
		} `json:"plan"`
	}

	if err := json.Unmarshal(
		output.Bytes(),
		&payload,
	); err != nil {
		t.Fatalf(
			"公共Guide JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	if payload.Plan.PlanID != planDigestWithoutAuditTimestampFixture ||
		payload.Plan.Summary.Missing !=
			payload.Plan.Summary.RawMissing ||
		payload.Plan.Summary.ActionableNew !=
			payload.Plan.Summary.ActionableMissing ||
		payload.Plan.Summary.PendingCuration !=
			payload.Plan.Summary.PendingCurationMissing {
		t.Fatalf(
			"Guide嵌套Plan规范别名或plan_id不符: %+v",
			payload,
		)
	}

	// 旧内部结构仍可解析新JSON，新增字段会被encoding/json安全忽略。
	var legacyGuide agentGuide
	if err := json.Unmarshal(
		output.Bytes(),
		&legacyGuide,
	); err != nil {
		t.Fatalf(
			"旧Guide消费者应继续可解析新增别名JSON: %v",
			err,
		)
	}

	if legacyGuide.Plan == nil ||
		legacyGuide.Plan.PlanID != planDigestWithoutAuditTimestampFixture ||
		legacyGuide.Plan.Summary.Missing != 3 ||
		legacyGuide.Plan.Summary.ActionableNew != 1 ||
		legacyGuide.Plan.Summary.PendingCuration != 1 {
		t.Fatalf(
			"旧Guide消费者兼容字段不符: %+v",
			legacyGuide.Plan,
		)
	}
}
