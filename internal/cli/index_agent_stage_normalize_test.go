// Host-Agent Stage元数据归一化集成测试。
//
// 防止出现“临时副本校验通过，但Manifest仍保存未归一原始值”的审计分裂。
package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
)

func TestAgentStageNormalizesMetadataBeforeManifest(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	plan, _ := agentStageCurrentPlan(
		t,
		root,
	)
	target := agentStageFindTarget(
		t,
		plan,
		"new.go",
	)

	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)

	result, err := stageAgentEntries(
		root,
		cfg,
		doc,
		indexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  "  " + plan.PlanID + "  ",
			Agent:   "  codex  ",
			Model:   "  test-model  ",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:归一化测试 | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"带首尾空格的合法元数据应归一后成功: %v",
			err,
		)
	}

	if result.Agent != "codex" ||
		result.Model != "test-model" ||
		result.PlanID != plan.PlanID {
		t.Fatalf(
			"Stage结果元数据未归一: %+v",
			result,
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if manifest.AgentName != "codex" ||
		manifest.Model != "test-model" ||
		manifest.PlanID != plan.PlanID {
		t.Fatalf(
			"Manifest保存了未归一元数据: %+v",
			manifest,
		)
	}
}
