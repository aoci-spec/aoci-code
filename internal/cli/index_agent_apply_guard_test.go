// R60-D Host-Agent Apply前Generation Plan一致性测试。
//
// 覆盖:
//   - Entries Stage和Review后源码变化，Apply在正式写入前拒绝;
//   - Header Stage和Diff后仓库变化，Apply在正式写入前拒绝;
//   - 显式run_id不能绕过Generation Plan核对;
//   - 拒绝时索引、Baseline、Application和applied_at均不变化。
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func TestEntriesApplyRejectsExpiredHostAgentGenerationPlan(
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
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:新文件职责 | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"Entries Stage应成功: %v",
			err,
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}

	hash, err := draft.HashFiles(
		root,
		result.RunID,
		entryDraftNames(manifest),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.AppendReview(
		root,
		result.RunID,
		draft.ReviewRecord{
			Action:     draft.ReviewActionCheck,
			DraftHash:  hash,
			PathsCount: 1,
			Passed:     1,
		},
	); err != nil {
		t.Fatal(err)
	}

	indexBefore := readEntriesIndex(
		t,
		root,
	)
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

	if err := os.WriteFile(
		filepath.Join(root, "new.go"),
		[]byte(
			"package main\n\nfunc ChangedAfterReview() {}\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err := runEntriesApplyForAudit(
		t,
		root,
		result.RunID,
	)

	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitInvalid {
		t.Fatalf(
			"旧Generation Plan应ExitInvalid: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		err.Error(),
		"宿主Agent生成计划防线",
	) ||
		!strings.Contains(
			err.Error(),
			"重新运行aoci index agent plan",
		) {
		t.Fatalf(
			"拒绝错误应点明Generation Plan已过期: %v",
			err,
		)
	}

	if readEntriesIndex(t, root) != indexBefore {
		t.Fatal(
			"Generation Plan拒绝前不得修改正式索引",
		)
	}

	baselineAfter, err := os.ReadFile(
		baselinePath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(baselineAfter) !=
		string(baselineBefore) {
		t.Fatal(
			"Generation Plan拒绝不得前移Baseline",
		)
	}

	after, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Applications) != 0 ||
		after.AppliedAt != "" {
		t.Fatalf(
			"Generation Plan拒绝不得形成Application授权: %+v",
			after,
		)
	}
}

func TestHeaderApplyRejectsExpiredHostAgentGenerationPlan(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
		true,
	)

	plan, _ := agentHeaderStagePlan(
		t,
		root,
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)

	result, err := stageAgentHeader(
		root,
		cfg,
		doc,
		indexPath,
		agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Header:  validAgentHeaderCandidate(),
		},
	)
	if err != nil {
		t.Fatalf(
			"Header Stage应成功: %v",
			err,
		)
	}

	hash, err := draft.HashFiles(
		root,
		result.RunID,
		[]string{draft.HeaderFileName},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.AppendReview(
		root,
		result.RunID,
		draft.ReviewRecord{
			Action:     draft.ReviewActionDiff,
			DraftHash:  hash,
			PathsCount: 1,
			Passed:     1,
		},
	); err != nil {
		t.Fatal(err)
	}

	indexBefore := readIndex(
		t,
		root,
	)
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

	if err := os.WriteFile(
		filepath.Join(root, "new.go"),
		[]byte(
			"package main\n\nfunc ChangedAfterHeaderReview() {}\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err := runHeaderApplyForP23(
		t,
		root,
		result.RunID,
	)

	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitInvalid {
		t.Fatalf(
			"旧Header Generation Plan应ExitInvalid: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		err.Error(),
		"宿主Agent生成计划防线",
	) {
		t.Fatalf(
			"拒绝错误应点明Generation Plan防线: %v",
			err,
		)
	}

	if readIndex(t, root) != indexBefore {
		t.Fatal(
			"Header Generation Plan拒绝前不得修改正式索引",
		)
	}

	baselineAfter, err := os.ReadFile(
		baselinePath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(baselineAfter) !=
		string(baselineBefore) {
		t.Fatal(
			"Header Generation Plan拒绝不得前移Baseline",
		)
	}

	backups, _ := filepath.Glob(
		filepath.Join(
			root,
			"aoci.txt.backup.*",
		),
	)
	if len(backups) != 0 {
		t.Fatalf(
			"Generation Plan拒绝不得产生索引备份: %v",
			backups,
		)
	}

	after, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if after.AppliedAt != "" {
		t.Fatalf(
			"Generation Plan拒绝不得写applied_at: %s",
			after.AppliedAt,
		)
	}
}

func TestHeaderSemanticRefreshUsesAlignedGenerationPlan(
	t *testing.T,
) {
	root := buildAgentPlanAlignedRepo(t)
	cfg, doc, indexPath := agentPlanLoadDocument(t, root)
	plan, err := buildAgentPlan(root, cfg, doc, indexPath)
	if err != nil || plan.Stage != agentPlanStageAligned {
		t.Fatalf("测试仓必须初始aligned: err=%v plan=%+v", err, plan)
	}

	indexBefore := readIndex(t, root)
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineBefore, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := stageAgentHeader(root, cfg, doc, indexPath, agentHeaderStageRequest{
		Version: agentHeaderStageVersion,
		PlanID:  plan.PlanID,
		Agent:   "codex",
		Intent:  "  SEMANTIC_REFRESH  ",
		Header:  validAgentHeaderCandidate(),
	})
	if err != nil {
		t.Fatalf("aligned语义刷新Stage应成功: %v", err)
	}
	if result.Intent != agentHeaderStageIntentSemanticRefresh {
		t.Fatalf("结果必须返回规范化intent: %+v", result)
	}
	manifest, err := draft.LoadManifest(root, result.RunID)
	if err != nil || manifest.HeaderIntent != "" {
		t.Fatalf("新Manifest不得扩展稳定JSON合同: err=%v manifest=%+v", err, manifest)
	}
	intentBytes, err := draft.ReadFile(root, result.RunID, draft.HeaderIntentFileName)
	if err != nil || string(intentBytes) != agentHeaderStageIntentSemanticRefresh+"\n" {
		t.Fatalf("独立intent草稿必须保留精确语义: err=%v bytes=%q", err, intentBytes)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, ".aoci", "drafts", result.RunID, draft.ManifestFileName))
	if err != nil || strings.Contains(string(manifestBytes), "header_intent") {
		t.Fatalf("稳定Manifest不得序列化header_intent: err=%v manifest=%s", err, manifestBytes)
	}
	if readIndex(t, root) != indexBefore {
		t.Fatal("semantic_refresh Stage不得修改正式索引")
	}
	baselineAfterStage, err := os.ReadFile(baselinePath)
	if err != nil || string(baselineAfterStage) != string(baselineBefore) {
		t.Fatalf("semantic_refresh Stage不得前移Baseline: %v", err)
	}

	if output, err := runHeaderDiffForP23(t, root, result.RunID); err != nil {
		t.Fatalf("semantic_refresh Diff应成功: %v\n%s", err, output)
	}
	output, err := runHeaderApplyForP23(t, root, result.RunID)
	if err != nil {
		t.Fatalf("semantic_refresh Apply应成功: %v\n%s", err, output)
	}
	if !strings.Contains(readIndex(t, root), "#【系统】测试仓") {
		t.Fatal("Apply必须通过正式Header链写入候选")
	}
	baselineAfterApply, err := os.ReadFile(baselinePath)
	if err != nil || string(baselineAfterApply) == string(baselineBefore) {
		t.Fatalf("Apply必须通过正式事务前移Baseline: %v", err)
	}

	manifest, err = draft.LoadManifest(root, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	manifest.HeaderIntent = agentHeaderStageIntentSemanticRefresh
	if err := draft.SaveManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	if output, err = runHeaderApplyForP23(t, root, result.RunID); err != nil {
		t.Fatalf("已完成Run的幂等Apply应清理过渡字段: %v\n%s", err, output)
	}
	manifest, err = draft.LoadManifest(root, result.RunID)
	if err != nil || manifest.HeaderIntent != "" {
		t.Fatalf("幂等Apply必须移除过渡Manifest字段: err=%v manifest=%+v", err, manifest)
	}

	cfg, doc, indexPath = agentPlanLoadDocument(t, root)
	plan, err = buildAgentPlan(root, cfg, doc, indexPath)
	if err != nil || plan.Stage != agentPlanStageAligned {
		t.Fatalf("正式Header语义刷新后必须保持aligned: err=%v plan=%+v", err, plan)
	}
}

func TestHostAgentHeaderRetryRecoversAfterBaselineFailure(t *testing.T) {
	root := buildAgentPlanMixedRepo(t, false, true)
	plan, _ := agentHeaderStagePlan(t, root)
	cfg, doc, indexPath := agentPlanLoadDocument(t, root)
	result, err := stageAgentHeader(root, cfg, doc, indexPath, agentHeaderStageRequest{
		Version: agentHeaderStageVersion,
		PlanID:  plan.PlanID,
		Agent:   "codex",
		Header:  validAgentHeaderCandidate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := draft.HashFiles(root, result.RunID, []string{draft.HeaderFileName})
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.AppendReview(root, result.RunID, draft.ReviewRecord{
		Action: draft.ReviewActionDiff, DraftHash: hash, PathsCount: 1, Passed: 1,
	}); err != nil {
		t.Fatal(err)
	}

	previousSave := saveHeaderBaseline
	saveHeaderBaseline = func(string, *baseline.Baseline) error {
		return errors.New("injected host-agent baseline failure")
	}
	_, firstErr := runHeaderApplyForP23(t, root, result.RunID)
	if firstErr == nil || !strings.Contains(firstErr.Error(), "header已写入") {
		t.Fatalf("首次写后Baseline故障应形成可恢复错误: %v", firstErr)
	}
	intentPath, err := headerRecoveryPath(root, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(intentPath); err != nil {
		t.Fatalf("写后故障必须保留Header恢复意图: %v", err)
	}
	backupsBefore, _ := filepath.Glob(filepath.Join(root, "aoci.txt.backup.*"))

	saveHeaderBaseline = previousSave
	t.Cleanup(func() { saveHeaderBaseline = previousSave })
	output, retryErr := runHeaderApplyForP23(t, root, result.RunID)
	if retryErr != nil || !strings.Contains(output, "头部事务已恢复") {
		t.Fatalf("同一Host-Agent run应零写入补齐治理事务: err=%v\n%s", retryErr, output)
	}
	backupsAfter, _ := filepath.Glob(filepath.Join(root, "aoci.txt.backup.*"))
	if len(backupsAfter) != len(backupsBefore) {
		t.Fatalf("恢复不得重复写Header或创建备份: before=%v after=%v", backupsBefore, backupsAfter)
	}
	if _, err := os.Stat(intentPath); !os.IsNotExist(err) {
		t.Fatalf("恢复完成后意图必须清理: %v", err)
	}
	manifest, err := draft.LoadManifest(root, result.RunID)
	if err != nil || manifest.AppliedAt == "" {
		t.Fatalf("恢复必须补齐Application终态: err=%v manifest=%+v", err, manifest)
	}
}
