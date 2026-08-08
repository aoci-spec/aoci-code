// Guide对四种automation.mode的权限、停点与Auto收口/修复合同测试。
package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestAgentGuideEntriesAutomationModes(
	t *testing.T,
) {
	tests := []struct {
		mode               string
		guideMode          string
		approvalRequired   bool
		stopBeforeApply    bool
		hasStage           bool
		hasManualPipeline  bool
		messageAnchor      string
		instructionAnchors []string
		forbiddenAnchors   []string
	}{
		{
			mode:              config.AutomationModeAuto,
			guideMode:         agentGuideModeExecute,
			approvalRequired:  false,
			stopBeforeApply:   false,
			hasStage:          true,
			hasManualPipeline: false,
			messageAnchor:     "自动修复",
			instructionAnchors: []string{
				"正常路径不得再调用commands.check",
				"auto_finalize.status=applied",
				"auto_finalize.status=repair_required",
				"只修正其中失败条目",
				"其他候选保持原样",
				"不得要求用户回复“继续”",
				"auto_finalize.status=stopped",
				"普通回复边界、用户提问、repair_required和可自动恢复的stopped都不是结束条件",
				"证明零写入则记录closure并重新Plan",
			},
			forbiddenAnchors: []string{
				"再执行entries check和entries diff",
				"直接执行原子entries apply",
				"repair_required时立即停止",
			},
		},
		{
			mode:              config.AutomationModeReview,
			guideMode:         agentGuideModePrepareReview,
			approvalRequired:  true,
			stopBeforeApply:   true,
			hasStage:          true,
			hasManualPipeline: true,
			messageAnchor:     "等待用户批准",
			instructionAnchors: []string{
				"再执行entries check和entries diff",
				"stop_before_apply=true",
			},
		},
		{
			mode:              config.AutomationModeLegacy,
			guideMode:         agentGuideModePrepareReview,
			approvalRequired:  true,
			stopBeforeApply:   true,
			hasStage:          true,
			hasManualPipeline: true,
			messageAnchor:     "旧仓兼容",
			instructionAnchors: []string{
				"再执行entries check和entries diff",
				"legacy兼容态要求",
			},
		},
		{
			mode:              config.AutomationModeOff,
			guideMode:         agentGuideModeObserve,
			approvalRequired:  false,
			stopBeforeApply:   false,
			hasStage:          false,
			hasManualPipeline: false,
			messageAnchor:     "只报告",
			instructionAnchors: []string{
				"不得直接调用agent stage",
			},
		},
	}

	for _, current := range tests {
		t.Run(
			current.mode,
			func(t *testing.T) {
				plan := guideTestPlan(
					agentPlanStageEntriesRequired,
				)
				plan.AutomationMode = current.mode
				plan.Targets = []agentPlanTarget{
					{
						Path:         "x.go",
						Kind:         "create",
						SourceSHA256: strings.Repeat("a", 64),
					},
				}

				guide, err := buildAgentGuide(
					"codex",
					plan,
				)
				if err != nil {
					t.Fatal(err)
				}

				if guide.Mode != current.guideMode ||
					guide.ApprovalRequired != current.approvalRequired ||
					guide.StopBeforeApply != current.stopBeforeApply {
					t.Fatalf(
						"Entries Guide策略不符: %+v",
						guide,
					)
				}

				hasStage :=
					guide.EntriesStageRequest != nil &&
						guide.Commands.EntriesStage != ""
				if hasStage != current.hasStage {
					t.Fatalf(
						"Entries Stage权限不符: %+v",
						guide,
					)
				}

				if !strings.Contains(
					guide.Message,
					current.messageAnchor,
				) {
					t.Fatalf(
						"Entries模式文案不符: %s",
						guide.Message,
					)
				}

				instructions := strings.Join(
					guide.Instructions,
					"\n",
				)

				for _, anchor := range current.instructionAnchors {
					if !strings.Contains(
						instructions,
						anchor,
					) {
						t.Fatalf(
							"%s模式缺少指令锚点%q:\n%s",
							current.mode,
							anchor,
							instructions,
						)
					}
				}

				for _, anchor := range current.forbiddenAnchors {
					if strings.Contains(
						instructions,
						anchor,
					) {
						t.Fatalf(
							"%s模式不得包含旧合同锚点%q:\n%s",
							current.mode,
							anchor,
							instructions,
						)
					}
				}

				manualCommands := []string{
					guide.Commands.Check,
					guide.Commands.Diff,
					guide.Commands.Apply,
				}
				for _, command := range manualCommands {
					if current.hasManualPipeline && command == "" {
						t.Fatalf(
							"%s必须公开独立Check/Diff/Apply命令: %+v",
							current.mode,
							guide.Commands,
						)
					}
					if !current.hasManualPipeline && command != "" {
						t.Fatalf(
							"%s不得公开正文禁止的独立命令: %+v",
							current.mode,
							guide.Commands,
						)
					}
				}

				if current.mode == config.AutomationModeOff &&
					guide.Batch != nil {
					t.Fatalf(
						"off不得发放Batch: %+v",
						guide,
					)
				}
			},
		)
	}
}

func TestAgentGuideJSONCommandsMatchCurrentInstructions(
	t *testing.T,
) {
	headerPlan := guideTestPlan(
		agentPlanStageHeaderRequired,
	)
	headerPlan.AutomationMode = config.AutomationModeAuto
	headerPlan.HeaderState = agentPlanHeaderMissing
	headerGuide, err := buildAgentGuide(
		"codex",
		headerPlan,
	)
	if err != nil {
		t.Fatal(err)
	}

	entriesPlan := guideTestPlan(
		agentPlanStageEntriesRequired,
	)
	entriesPlan.AutomationMode = config.AutomationModeAuto
	entriesPlan.Targets = []agentPlanTarget{
		{
			Path:         "x.go",
			Kind:         "create",
			SourceSHA256: strings.Repeat("a", 64),
		},
	}
	entriesGuide, err := buildAgentGuide(
		"codex",
		entriesPlan,
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		guide     *agentGuide
		required  []string
		forbidden []string
	}{
		{
			name:      "header-auto",
			guide:     headerGuide,
			required:  []string{"header_stage", "diff", "apply"},
			forbidden: []string{"entries_stage", "check", "curation_stage"},
		},
		{
			name:      "entries-auto",
			guide:     entriesGuide,
			required:  []string{"header_show", "entries_stage"},
			forbidden: []string{"check", "diff", "apply", "curation_stage"},
		},
		{
			name: "curation-auto",
			guide: buildCurationGuideForAssetTest(
				t,
				config.AutomationModeAuto,
			),
			required: []string{
				"curation_stage",
				"curation_diff",
				"curation_apply",
			},
			forbidden: []string{"header_stage", "entries_stage", "check", "diff", "apply"},
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				var output strings.Builder
				if err := writeAgentGuideJSON(
					&output,
					current.guide,
				); err != nil {
					t.Fatal(err)
				}

				var rendered struct {
					Commands map[string]json.RawMessage `json:"commands"`
				}
				if err := json.Unmarshal(
					[]byte(output.String()),
					&rendered,
				); err != nil {
					t.Fatal(err)
				}
				for _, name := range current.required {
					if _, ok := rendered.Commands[name]; !ok {
						t.Fatalf(
							"Guide JSON缺少当前instructions允许的命令%s: %s",
							name,
							output.String(),
						)
					}
				}
				for _, name := range current.forbidden {
					if _, ok := rendered.Commands[name]; ok {
						t.Fatalf(
							"Guide JSON公开当前instructions未允许的命令%s: %s",
							name,
							output.String(),
						)
					}
				}
			},
		)
	}
}

func TestAgentGuideHeaderAutomationModes(
	t *testing.T,
) {
	for _, current := range []struct {
		mode             string
		guideMode        string
		approvalRequired bool
		hasPipeline      bool
	}{
		{
			mode:             config.AutomationModeAuto,
			guideMode:        agentGuideModeExecute,
			approvalRequired: false,
			hasPipeline:      true,
		},
		{
			mode:             config.AutomationModeReview,
			guideMode:        agentGuideModePrepareReview,
			approvalRequired: true,
			hasPipeline:      true,
		},
		{
			mode:             config.AutomationModeLegacy,
			guideMode:        agentGuideModePrepareReview,
			approvalRequired: true,
			hasPipeline:      true,
		},
		{
			mode:             config.AutomationModeOff,
			guideMode:        agentGuideModeObserve,
			approvalRequired: false,
			hasPipeline:      false,
		},
	} {
		t.Run(
			current.mode,
			func(t *testing.T) {
				plan := guideTestPlan(
					agentPlanStageHeaderRequired,
				)
				plan.AutomationMode = current.mode
				plan.HeaderState =
					agentPlanHeaderMissing

				guide, err := buildAgentGuide(
					"codex",
					plan,
				)
				if err != nil {
					t.Fatal(err)
				}

				if guide.Mode != current.guideMode ||
					guide.ApprovalRequired != current.approvalRequired {
					t.Fatalf(
						"Header Guide策略不符: %+v",
						guide,
					)
				}

				hasPipeline :=
					guide.HeaderStageRequest != nil &&
						guide.Commands.HeaderStage != "" &&
						guide.Commands.Diff != "" &&
						guide.Commands.Apply != ""
				if hasPipeline != current.hasPipeline {
					t.Fatalf(
						"Header Stage/Diff/Apply权限不符: %+v",
						guide,
					)
				}
				if !current.hasPipeline &&
					(guide.Commands.HeaderStage != "" ||
						guide.Commands.Diff != "" ||
						guide.Commands.Apply != "") {
					t.Fatalf(
						"Header observe不得公开部分写命令: %+v",
						guide.Commands,
					)
				}
			},
		)
	}
}

func TestAgentGuideCurationAutomationModes(
	t *testing.T,
) {
	for _, current := range []struct {
		mode             string
		guideMode        string
		approvalRequired bool
		hasPipeline      bool
	}{
		{
			mode:             config.AutomationModeAuto,
			guideMode:        agentGuideModeExecute,
			approvalRequired: false,
			hasPipeline:      true,
		},
		{
			mode:             config.AutomationModeReview,
			guideMode:        agentGuideModePrepareReview,
			approvalRequired: true,
			hasPipeline:      true,
		},
		{
			mode:             config.AutomationModeLegacy,
			guideMode:        agentGuideModePrepareReview,
			approvalRequired: true,
			hasPipeline:      true,
		},
		{
			mode:             config.AutomationModeOff,
			guideMode:        agentGuideModeObserve,
			approvalRequired: false,
			hasPipeline:      false,
		},
	} {
		t.Run(
			current.mode,
			func(t *testing.T) {
				guide := buildCurationGuideForAssetTest(
					t,
					current.mode,
				)
				if guide.Mode != current.guideMode ||
					guide.ApprovalRequired != current.approvalRequired {
					t.Fatalf(
						"Curation Guide策略不符: %+v",
						guide,
					)
				}

				hasPipeline :=
					guide.CurationStageRequest != nil &&
						guide.Commands.CurationStage != "" &&
						guide.Commands.CurationDiff != "" &&
						guide.Commands.CurationApply != ""
				if hasPipeline != current.hasPipeline {
					t.Fatalf(
						"Curation Stage/Diff/Apply权限不符: %+v",
						guide,
					)
				}
				if !current.hasPipeline &&
					(guide.Commands.CurationStage != "" ||
						guide.Commands.CurationDiff != "" ||
						guide.Commands.CurationApply != "") {
					t.Fatalf(
						"Curation observe不得公开部分写命令: %+v",
						guide.Commands,
					)
				}
			},
		)
	}
}
