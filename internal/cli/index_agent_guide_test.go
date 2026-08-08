// R60-E1 Host-Agent Guide测试。
//
// 覆盖:
//   - 六类Plan阶段的安全行为映射;
//   - 无Baseline允许普通scan，已有Baseline的Unbaselined必须阻断;
//   - Header请求模板与首次人工批准停点;
//   - Entries请求模板遵守机器合同批次上限并保留path/source_sha256;
//   - Index外部漂移和Orphan必须交维护者;
//   - Aligned明确完成;
//   - 真实Plan到Guide的全链路纯读、零Ledger;
//   - guide命令已挂载且JSON输出可解析;
//   - 非法Agent标识拒绝。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func guideTestPlan(
	stage string,
) *agentPlan {
	return &agentPlan{
		Version:          agentPlanVersion,
		PlanID:           strings.Repeat("a", 64),
		Stage:            stage,
		NextAction:       agentPlanActionNone,
		RepositoryRoot:   "/repo",
		IndexPath:        "aoci.txt",
		IndexSHA256:      strings.Repeat("b", 64),
		HeaderSHA256:     strings.Repeat("c", 64),
		RepositorySHA256: strings.Repeat("d", 64),
		HeaderState:      agentPlanHeaderReady,
		BaselineExists:   true,
		AutomationMode:   "legacy",
		Targets:          []agentPlanTarget{},
		CurationExcluded: []string{},
		SkippedMissing:   []agentPlanSkipped{},
		Orphans:          []string{},
		Unbaselined:      []string{},
		Warnings:         []string{},
	}
}

func TestBuildAgentGuideBaselineWithoutBaselineAllowsScan(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageBaselineRequired,
	)
	plan.BaselineExists = false
	plan.NextAction = agentPlanActionScan

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if guide.Mode != agentGuideModeExecute ||
		guide.ApprovalRequired ||
		guide.Commands.Scan != "aoci scan" {
		t.Fatalf(
			"首次无Baseline指南不符: %+v",
			guide,
		)
	}
	for _, instruction := range guide.Instructions {
		if strings.Contains(
			instruction,
			"scan --force",
		) {
			t.Fatalf(
				"首次Baseline也不得指导--force: %+v",
				guide.Instructions,
			)
		}
	}
}

func TestBuildAgentGuideExistingBaselineUnbaselinedBlocks(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageBaselineRequired,
	)
	plan.BaselineExists = true
	plan.Unbaselined = []string{"new.go"}
	plan.Summary.Unbaselined = 1

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if guide.Mode != agentGuideModeBlocked ||
		!guide.ApprovalRequired ||
		guide.Commands.Scan != "" {
		t.Fatalf(
			"已有Baseline的Unbaselined必须阻断: %+v",
			guide,
		)
	}
	if !strings.Contains(
		strings.Join(guide.Instructions, "\n"),
		"不要执行scan --force",
	) {
		t.Fatalf(
			"阻断指南必须明确防洗白: %+v",
			guide.Instructions,
		)
	}
}

func TestBuildAgentGuideHeaderRequiresApproval(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageHeaderRequired,
	)
	plan.NextAction = agentPlanActionGenerateHead
	plan.HeaderState = agentPlanHeaderMissing

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if guide.Mode != agentGuideModePrepareReview ||
		!guide.ApprovalRequired ||
		!guide.StopBeforeApply {
		t.Fatalf(
			"Header指南必须停在批准点: %+v",
			guide,
		)
	}
	if guide.HeaderStageRequest == nil ||
		guide.HeaderStageRequest.PlanID != plan.PlanID ||
		guide.HeaderStageRequest.Agent != "codex" ||
		guide.HeaderStageRequest.Header != "" {
		t.Fatalf(
			"Header请求模板不符: %+v",
			guide.HeaderStageRequest,
		)
	}
	if guide.EntriesStageRequest != nil ||
		guide.Commands.HeaderStage == "" ||
		!strings.Contains(
			guide.Commands.Apply,
			"{run_id}",
		) {
		t.Fatalf(
			"Header命令面不符: %+v",
			guide,
		)
	}
}

func TestBuildAgentGuideEntriesBatchesAtProtocolLimit(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageEntriesRequired,
	)
	plan.NextAction = agentPlanActionStageEntries

	for index := 0; index < machinecontract.EntriesBatchMaxItems+5; index++ {
		plan.Targets = append(
			plan.Targets,
			agentPlanTarget{
				Path: fmt.Sprintf(
					"src/file_%03d.go",
					index,
				),
				Kind: "create",
				SourceSHA256: fmt.Sprintf(
					"%064x",
					index+1,
				),
			},
		)
	}
	plan.Summary.ExecutableTargets =
		len(plan.Targets)

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if guide.Mode != agentGuideModePrepareReview ||
		!guide.ApprovalRequired ||
		!guide.StopBeforeApply {
		t.Fatalf(
			"Entries指南必须停在批准点: %+v",
			guide,
		)
	}
	if guide.Batch == nil ||
		guide.Batch.Included != machinecontract.EntriesBatchMaxItems ||
		guide.Batch.Remaining != 5 ||
		len(guide.Batch.Targets) != machinecontract.EntriesBatchMaxItems {
		t.Fatalf(
			"Entries批次边界不符: %+v",
			guide.Batch,
		)
	}
	if guide.EntriesStageRequest == nil ||
		len(guide.EntriesStageRequest.Entries) !=
			machinecontract.EntriesBatchMaxItems {
		t.Fatalf(
			"Entries请求模板数量不符: %+v",
			guide.EntriesStageRequest,
		)
	}

	firstTarget := plan.Targets[0]
	firstRequest :=
		guide.EntriesStageRequest.Entries[0]
	if firstRequest.Path != firstTarget.Path ||
		firstRequest.SourceSHA256 !=
			firstTarget.SourceSHA256 ||
		firstRequest.Entry != "" {
		t.Fatalf(
			"Entries模板必须只留entry待填: target=%+v request=%+v",
			firstTarget,
			firstRequest,
		)
	}
	if guide.HeaderStageRequest != nil ||
		guide.Commands.HeaderShow == "" ||
		guide.Commands.EntriesStage == "" ||
		guide.Commands.Check == "" ||
		guide.Commands.Diff == "" ||
		guide.Commands.Apply == "" {
		t.Fatalf(
			"Entries命令面不完整: %+v",
			guide.Commands,
		)
	}
}

func TestBuildAgentGuideBlockedAndCompleteStages(
	t *testing.T,
) {
	tests := []struct {
		name     string
		stage    string
		mode     string
		approval bool
		complete bool
	}{
		{
			name:     "index_review",
			stage:    agentPlanStageIndexReviewRequired,
			mode:     agentGuideModeBlocked,
			approval: true,
		},
		{
			name:     "orphan_review",
			stage:    agentPlanStageOrphanReview,
			mode:     agentGuideModeBlocked,
			approval: true,
		},
		{
			name:     "aligned",
			stage:    agentPlanStageAligned,
			mode:     agentGuideModeComplete,
			complete: true,
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				plan := guideTestPlan(
					current.stage,
				)
				if current.stage ==
					agentPlanStageOrphanReview {
					plan.Orphans =
						[]string{"ghost.go"}
				}

				guide, err := buildAgentGuide(
					"codex",
					plan,
				)
				if err != nil {
					t.Fatal(err)
				}
				if guide.Mode != current.mode ||
					guide.ApprovalRequired !=
						current.approval ||
					guide.Complete !=
						current.complete {
					t.Fatalf(
						"阶段指南不符: %+v",
						guide,
					)
				}
				if guide.HeaderStageRequest != nil ||
					guide.EntriesStageRequest != nil {
					t.Fatalf(
						"阻断/完成阶段不得发放Stage模板: %+v",
						guide,
					)
				}
			},
		)
	}
}

func TestBuildAgentGuideManagedGovernanceStages(t *testing.T) {
	budget := cognitionbudget.Report{WholeIndexTokens: 101000, MaxTokens: 100000,
		Violations: []cognitionbudget.Violation{{Code: "whole_index_budget_exceeded", Actual: 101000, Maximum: 100000}}}
	for _, stage := range []string{agentPlanStageScopeChangeRequired, agentPlanStageObservedReview,
		agentPlanStageCompressionRequired, agentPlanStageBudgetExceeded} {
		t.Run(stage, func(t *testing.T) {
			plan := guideTestPlan(stage)
			plan.Governance = &agentPlanGovernance{CognitionBudget: &budget}
			guide, err := buildAgentGuide("codex", plan)
			if err != nil {
				t.Fatal(err)
			}
			if guide.Mode != agentGuideModeBlocked || guide.Complete || guide.HeaderStageRequest != nil || guide.EntriesStageRequest != nil {
				t.Fatalf("managed governance stage issued unsafe work: %+v", guide)
			}
			switch stage {
			case agentPlanStageScopeChangeRequired:
				if guide.Commands.ScopePreview == "" || guide.Commands.ScopeStatus == "" {
					t.Fatalf("scope change guide lacks preview/status: %+v", guide.Commands)
				}
			case agentPlanStageObservedReview:
				if guide.Commands.ScopeAcknowledge == "" || guide.Commands.ScopeStatus == "" {
					t.Fatalf("observe review guide lacks explicit acknowledgement: %+v", guide.Commands)
				}
			default:
				if guide.Commands.ScopeStatus == "" || !strings.Contains(guide.Message, "101000") {
					t.Fatalf("budget guide lacks exact report facts: %+v", guide)
				}
			}
		})
	}
}

func TestBuildAgentGuideRejectsInvalidInput(
	t *testing.T,
) {
	if _, err := buildAgentGuide(
		"bad agent",
		guideTestPlan(agentPlanStageAligned),
	); err == nil {
		t.Fatal("非法Agent标识应拒绝")
	}

	if _, err := buildAgentGuide(
		"codex",
		nil,
	); err == nil {
		t.Fatal("空Plan应拒绝")
	}

	unknown := guideTestPlan("future_stage")
	if _, err := buildAgentGuide(
		"codex",
		unknown,
	); err == nil {
		t.Fatal("未知Stage应拒绝")
	}
}

func TestAgentGuideRealPlanIsPure(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)

	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	baselinePath := filepath.Join(
		root,
		".aoci",
		"baseline.json",
	)
	baselineBefore, err := os.ReadFile(
		baselinePath,
	)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if guide.Plan.PlanID != plan.PlanID ||
		guide.EntriesStageRequest == nil ||
		len(guide.EntriesStageRequest.Entries) != 2 {
		t.Fatalf(
			"真实Plan指南不符: %+v",
			guide,
		)
	}

	indexAfter, _ := os.ReadFile(indexPath)
	baselineAfter, _ := os.ReadFile(
		baselinePath,
	)
	if string(indexBefore) != string(indexAfter) {
		t.Fatal("Guide不得修改正式索引")
	}
	if string(baselineBefore) !=
		string(baselineAfter) {
		t.Fatal("Guide不得修改Baseline")
	}
	if _, err := os.Stat(
		filepath.Join(
			root,
			".aoci",
			"ledger.jsonl",
		),
	); !os.IsNotExist(err) {
		t.Fatalf(
			"Guide必须零Ledger: %v",
			err,
		)
	}
}

func TestIndexAgentCommandMountsGuide(
	t *testing.T,
) {
	command := newIndexAgentCmd()
	found, _, err := command.Find(
		[]string{"guide"},
	)
	if err != nil ||
		found == nil ||
		found.Name() != "guide" {
		t.Fatalf(
			"agent命令组未挂载guide: found=%v err=%v",
			found,
			err,
		)
	}
}

func TestAgentGuideCommandJSON(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	oldRepo := flagRepo
	oldJSON := flagJSON
	flagRepo = root
	flagJSON = true
	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})

	command := newIndexAgentGuideCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true

	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"--agent",
		"codex",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf(
			"Guide JSON命令失败: %v\n%s",
			err,
			output.String(),
		)
	}

	var result agentGuide
	if err := json.Unmarshal(
		output.Bytes(),
		&result,
	); err != nil {
		t.Fatalf(
			"Guide JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}
	if result.Agent != "codex" ||
		result.Plan == nil ||
		result.Plan.Stage !=
			agentPlanStageEntriesRequired ||
		result.EntriesStageRequest == nil {
		t.Fatalf(
			"Guide JSON内容不符: %+v",
			result,
		)
	}
}

func TestAgentGuideCommandRejectsInvalidAgent(
	t *testing.T,
) {
	oldRepo := flagRepo
	oldJSON := flagJSON
	flagRepo = t.TempDir()
	flagJSON = false
	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})

	command := newIndexAgentGuideCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs([]string{
		"--agent",
		"bad agent",
	})

	err := command.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitConfig {
		t.Fatalf(
			"非法Agent应为ExitConfig: %v",
			err,
		)
	}
}
