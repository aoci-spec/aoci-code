// `aoci index agent stage`测试。
//
// 覆盖:
//   - 当前Plan子集成功进入标准草稿;
//   - Manifest保存生成源、Agent、Plan和三类哈希;
//   - EntryStatus保存源码SHA-256;
//   - Stage不修改正式索引或Baseline;
//   - Ledger记录host_agent来源;
//   - 格式问题只标记warned并保留原始候选;
//   - 过期plan、计划外路径和重复路径在写入前硬拒;
//   - stdin JSON拒绝未知字段。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func agentStageCurrentPlan(
	t *testing.T,
	root string,
) (*agentPlan, string) {
	t.Helper()

	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatalf("构造计划失败: %v", err)
	}
	if plan.Stage != agentPlanStageEntriesRequired {
		t.Fatalf(
			"测试仓应可发放条目任务: %+v",
			plan,
		)
	}
	return plan, indexPath
}

func agentStageFindTarget(
	t *testing.T,
	plan *agentPlan,
	path string,
) agentPlanTarget {
	t.Helper()

	for _, target := range plan.Targets {
		if target.Path == path {
			return target
		}
	}
	t.Fatalf(
		"计划中未找到目标%s: %+v",
		path,
		plan.Targets,
	)
	return agentPlanTarget{}
}

func TestAgentStageSuccessPreservesGovernance(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	plan, indexPath := agentStageCurrentPlan(
		t,
		root,
	)
	target := agentStageFindTarget(
		t,
		plan,
		"stale.go",
	)

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

	cfg, doc, loadedIndexPath :=
		agentPlanLoadDocument(t, root)
	result, err := stageAgentEntries(
		root,
		cfg,
		doc,
		loadedIndexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Model:   "test-model",
			Entries: []agentStageEntry{
				{
					Path:         "stale.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "stale.go[XAP7T]: F:当前职责 | R:- | A:- | " +
						"S:必须保留的旧约束",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("Stage应成功: %v", err)
	}
	if result.RunID == "" ||
		result.Drafted != 1 ||
		result.Warned != 0 ||
		result.GenerationHash == "" {
		t.Fatalf("Stage结果不符: %+v", result)
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Kind != draft.KindEntries ||
		manifest.GenerationSource != draft.GenerationSourceHostAgent ||
		manifest.AgentName != "codex" ||
		manifest.PlanID != plan.PlanID ||
		manifest.IndexSHA256 != plan.IndexSHA256 ||
		manifest.HeaderSHA256 != plan.HeaderSHA256 ||
		manifest.GenerationHash != result.GenerationHash ||
		manifest.TokenSource != ledger.TokenSourceMissing {
		t.Fatalf(
			"Host-Agent manifest不符: %+v",
			manifest,
		)
	}
	if len(manifest.Entries) != 1 ||
		manifest.Entries[0].Status != "drafted" ||
		manifest.Entries[0].SourceSHA256 != target.SourceSHA256 {
		t.Fatalf(
			"generation state不符: %+v",
			manifest.Entries,
		)
	}

	draftData, err := draft.ReadFile(
		root,
		result.RunID,
		entryDraftFileName("stale.go"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(draftData),
		"必须保留的旧约束",
	) {
		t.Fatalf(
			"草稿内容不符: %s",
			draftData,
		)
	}

	indexAfter, _ := os.ReadFile(indexPath)
	baselineAfter, _ := os.ReadFile(baselinePath)
	if string(indexBefore) != string(indexAfter) {
		t.Fatal("Stage不得修改正式索引")
	}
	if string(baselineBefore) != string(baselineAfter) {
		t.Fatal("Stage不得修改Baseline")
	}

	events, _ := ledger.Recent(root, 20)
	found := false
	for _, event := range events {
		if event.Op == "agent_stage" &&
			event.Source == ledger.SourceAgent &&
			event.GenerationSource == draft.GenerationSourceHostAgent &&
			event.AgentName == "codex" &&
			event.DraftRunID == result.RunID &&
			event.TokenSrc == ledger.TokenSourceMissing {
			found = true
		}
	}
	if !found {
		t.Fatalf(
			"Ledger缺少Host-Agent Stage事件: %+v",
			events,
		)
	}
}

func TestAgentStageWarnedCandidateIsVisible(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	plan, _ := agentStageCurrentPlan(t, root)
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
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "wrong.go[XAP7T]: F:文件名故意不一致 | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"格式问题应带病进入草稿而非Stage整体失败: %v",
			err,
		)
	}
	if result.Warned != 1 ||
		result.Drafted != 0 ||
		len(result.Statuses) != 1 ||
		!strings.Contains(
			result.Statuses[0].Note,
			"文件名不一致",
		) {
		t.Fatalf(
			"warned状态不符: %+v",
			result,
		)
	}

	data, err := draft.ReadFile(
		root,
		result.RunID,
		entryDraftFileName("new.go"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(data),
		"wrong.go",
	) {
		t.Fatal("Stage不得静默修正宿主Agent原始候选")
	}
}

func TestAgentStageRejectsExpiredPlanBeforeWriting(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	plan, _ := agentStageCurrentPlan(t, root)
	target := agentStageFindTarget(
		t,
		plan,
		"new.go",
	)

	if err := os.WriteFile(
		filepath.Join(root, "new.go"),
		[]byte(
			"package main\n\nfunc ChangedAfterPlan() {}\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	_, err := stageAgentEntries(
		root,
		cfg,
		doc,
		indexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:旧计划候选 | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"plan_id已过期",
		) {
		t.Fatalf(
			"源码变化后旧计划必须被拒绝: %v",
			err,
		)
	}

	runIDs, listErr := draft.ListRunIDs(root)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(runIDs) != 0 {
		t.Fatalf(
			"计划拒绝发生在写入前，不得留下草稿run: %v",
			runIDs,
		)
	}
}

func TestAgentStageRejectsUnknownAndDuplicatePaths(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	plan, _ := agentStageCurrentPlan(t, root)
	target := agentStageFindTarget(
		t,
		plan,
		"new.go",
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)

	_, err := stageAgentEntries(
		root,
		cfg,
		doc,
		indexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "outside.go",
					SourceSHA256: strings.Repeat("0", 64),
					Entry: "outside.go[XAP7T]: F:x | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"不属于当前plan",
		) {
		t.Fatalf(
			"计划外路径必须拒绝: %v",
			err,
		)
	}

	_, err = stageAgentEntries(
		root,
		cfg,
		doc,
		indexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:x | " +
						"R:- | A:- | S:-",
				},
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:y | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"重复路径",
		) {
		t.Fatalf(
			"重复路径必须整批拒绝: %v",
			err,
		)
	}
}

func TestReadAgentStageRequestRejectsUnknownField(
	t *testing.T,
) {
	_, err := readAgentStageRequest(
		bytes.NewBufferString(
			`{
  "version": 1,
  "plan_id": "` +
				strings.Repeat("a", 64) +
				`",
  "agent": "codex",
  "entries": [],
  "unexpected": true
}`,
		),
	)
	if err == nil || !strings.Contains(err.Error(), `"unexpected"`) {
		t.Fatalf(
			"未知字段必须拒绝: %v",
			err,
		)
	}
}
