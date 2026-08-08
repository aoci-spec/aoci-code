// `aoci index agent header stage`测试。
//
// 覆盖:
//   - header_required阶段成功写入标准Header草稿;
//   - Manifest保存Host-Agent、Plan及三类摘要;
//   - Stage不修改正式索引或Baseline;
//   - Ledger记录agent_header_stage;
//   - 首次头部明确要求人工批准;
//   - 结构、字典和Safety问题带病进入草稿并形成Warning;
//   - 全仓任意文件变化使旧Header Plan失效;
//   - 非header_required阶段拒绝;
//   - stdin未知字段拒绝。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func agentHeaderStagePlan(
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
		t.Fatalf("构造Header计划失败: %v", err)
	}
	if plan.Stage != agentPlanStageHeaderRequired {
		t.Fatalf(
			"测试仓应处于header_required: %+v",
			plan,
		)
	}
	if plan.RepositorySHA256 == "" {
		t.Fatal("Header计划必须携带repository_sha256")
	}
	return plan, indexPath
}

func validAgentHeaderCandidate() string {
	return "#【系统】测试仓 — Go命令行演示项目\n" +
		"#【部署】go test ./...; go run .\n" +
		"#【整体规范】文件级职责、关联、接口和高熵约束进入AOCI条目\n" +
		"#A层级: X-入口 C-核心 D-文档\n" +
		"#B模块: RT-根命令 GR-问候语 CF-配置\n" +
		"#C重要度: 9核心 8高频 7业务 5常规 3辅助 1边缘\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		"#S配额: " + machinecontract.NumericText().SQuotaDefaultCompact + "\n" +
		"#【负空间】无网络服务、数据库和远程模型调用\n"
}

func TestAgentHeaderStageSuccessRequiresApproval(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
		true,
	)
	plan, indexPath := agentHeaderStagePlan(
		t,
		root,
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
	result, err := stageAgentHeader(
		root,
		cfg,
		doc,
		loadedIndexPath,
		agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  "  " + plan.PlanID + "  ",
			Agent:   "  codex  ",
			Model:   "  host-model  ",
			Header:  validAgentHeaderCandidate(),
		},
	)
	if err != nil {
		t.Fatalf("Header Stage应成功: %v", err)
	}

	if result.RunID == "" ||
		result.GenerationHash == "" ||
		result.PlanID != plan.PlanID ||
		result.Agent != "codex" ||
		result.Model != "host-model" ||
		!result.ApprovalRequired {
		t.Fatalf(
			"Header Stage结果不符: %+v",
			result,
		)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf(
			"合规候选不应有Warning: %+v",
			result.Warnings,
		)
	}
	if !strings.Contains(
		result.NextCommand,
		"aoci index header diff",
	) ||
		!strings.Contains(
			result.ApplyCommand,
			"aoci index header apply",
		) {
		t.Fatalf(
			"结果必须给出审阅和批准命令: %+v",
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
	if manifest.Kind != draft.KindHeader ||
		manifest.GenerationSource != draft.GenerationSourceHostAgent ||
		manifest.AgentName != "codex" ||
		manifest.PlanID != plan.PlanID ||
		manifest.IndexSHA256 != plan.IndexSHA256 ||
		manifest.HeaderSHA256 != plan.HeaderSHA256 ||
		manifest.GenerationHash != result.GenerationHash ||
		manifest.TokenSource != ledger.TokenSourceMissing ||
		len(manifest.Files) != 1 ||
		manifest.Files[0] != draft.HeaderFileName {
		t.Fatalf(
			"Header Manifest不符: %+v",
			manifest,
		)
	}

	headerBytes, err := draft.ReadFile(
		root,
		result.RunID,
		draft.HeaderFileName,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(headerBytes),
		"#A层级: X-入口 C-核心 D-文档",
	) {
		t.Fatalf(
			"头部草稿内容不符: %s",
			headerBytes,
		)
	}

	indexAfter, _ := os.ReadFile(indexPath)
	baselineAfter, _ := os.ReadFile(baselinePath)
	if string(indexBefore) != string(indexAfter) {
		t.Fatal("Header Stage不得修改正式索引")
	}
	if string(baselineBefore) != string(baselineAfter) {
		t.Fatal("Header Stage不得修改Baseline")
	}

	events, _ := ledger.Recent(root, 20)
	found := false
	for _, event := range events {
		if event.Op == "agent_header_stage" &&
			event.Source == ledger.SourceAgent &&
			event.GenerationSource == draft.GenerationSourceHostAgent &&
			event.AgentName == "codex" &&
			event.DraftRunID == result.RunID {
			found = true
		}
	}
	if !found {
		t.Fatalf(
			"Ledger缺少agent_header_stage事件: %+v",
			events,
		)
	}
}

func TestAgentHeaderStageWarnsButPreservesCandidate(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
		true,
	)
	plan, _ := agentHeaderStagePlan(t, root)

	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	badHeader := "这行没有#，结构非法\n" +
		"#普通说明，无A/B字典\n" +
		"#达到zero defects\n"

	result, err := stageAgentHeader(
		root,
		cfg,
		doc,
		indexPath,
		agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Header:  badHeader,
		},
	)
	if err != nil {
		t.Fatalf(
			"带病候选应进入草稿并Warning，而非Stage整体失败: %v",
			err,
		)
	}
	if len(result.Warnings) < 3 {
		t.Fatalf(
			"应同时产生结构、字典和Safety Warning: %+v",
			result.Warnings,
		)
	}

	data, err := draft.ReadFile(
		root,
		result.RunID,
		draft.HeaderFileName,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(data),
		"这行没有#",
	) ||
		!strings.Contains(
			string(data),
			"zero defects",
		) {
		t.Fatal("Header Stage不得静默修改原始候选")
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Warnings) != len(result.Warnings) {
		t.Fatalf(
			"Warning必须同步进入Manifest: %+v",
			manifest,
		)
	}
}

func TestAgentHeaderStageRejectsPlanAfterRepositoryChange(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
		true,
	)
	plan, _ := agentHeaderStagePlan(t, root)

	if err := os.WriteFile(
		filepath.Join(root, "new.go"),
		[]byte(
			"package main\n\nfunc ChangedAfterHeaderPlan() {}\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	currentPlan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if currentPlan.RepositorySHA256 ==
		plan.RepositorySHA256 {
		t.Fatal(
			"源码变化后repository_sha256必须变化",
		)
	}
	if currentPlan.PlanID == plan.PlanID {
		t.Fatal(
			"header_required阶段源码变化后plan_id必须变化",
		)
	}

	_, err = stageAgentHeader(
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
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"plan_id已过期",
		) {
		t.Fatalf(
			"旧Header计划必须在写草稿前被拒绝: %v",
			err,
		)
	}

	runIDs, listErr := draft.ListRunIDs(root)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(runIDs) != 0 {
		t.Fatalf(
			"计划拒绝不得留下草稿Run: %v",
			runIDs,
		)
	}
}

func TestAgentHeaderStageRejectsWrongPlanStage(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)
	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Stage != agentPlanStageEntriesRequired {
		t.Fatalf(
			"夹具应处于entries_required: %+v",
			plan,
		)
	}

	_, err = stageAgentHeader(
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
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"不能接收头部候选",
		) {
		t.Fatalf(
			"非header_required阶段必须拒绝: %v",
			err,
		)
	}
}

func TestAgentHeaderStageSemanticRefreshStageBoundary(
	t *testing.T,
) {
	t.Run("aligned_requires_explicit_intent", func(t *testing.T) {
		root := buildAgentPlanAlignedRepo(t)
		cfg, doc, indexPath := agentPlanLoadDocument(t, root)
		plan, err := buildAgentPlan(root, cfg, doc, indexPath)
		if err != nil {
			t.Fatal(err)
		}

		_, err = stageAgentHeader(root, cfg, doc, indexPath, agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Header:  validAgentHeaderCandidate(),
		})
		if err == nil || !strings.Contains(err.Error(), "header_required") {
			t.Fatalf("aligned阶段省略intent必须保持原有拒绝行为: %v", err)
		}
	})

	t.Run("semantic_refresh_rejects_non_aligned", func(t *testing.T) {
		root := buildAgentPlanMixedRepo(t, true, true)
		cfg, doc, indexPath := agentPlanLoadDocument(t, root)
		plan, err := buildAgentPlan(root, cfg, doc, indexPath)
		if err != nil {
			t.Fatal(err)
		}

		_, err = stageAgentHeader(root, cfg, doc, indexPath, agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Intent:  agentHeaderStageIntentSemanticRefresh,
			Header:  validAgentHeaderCandidate(),
		})
		if err == nil || !strings.Contains(err.Error(), "aligned") {
			t.Fatalf("semantic_refresh必须只接受aligned计划: %v", err)
		}
	})
}

func TestAgentHeaderStageRejectsUnknownIntent(
	t *testing.T,
) {
	request := agentHeaderStageRequest{
		Version: agentHeaderStageVersion,
		PlanID:  strings.Repeat("a", 64),
		Agent:   "codex",
		Intent:  "rewrite_anyway",
		Header:  validAgentHeaderCandidate(),
	}
	if err := normalizeAndValidateAgentHeaderStageRequest(&request); err == nil ||
		!strings.Contains(err.Error(), "semantic_refresh") {
		t.Fatalf("未知Header intent必须拒绝: %v", err)
	}
}

func TestReadAgentHeaderStageRequestRejectsUnknownField(
	t *testing.T,
) {
	_, err := readAgentHeaderStageRequest(
		bytes.NewBufferString(
			`{
  "version": 1,
  "plan_id": "` +
				strings.Repeat("a", 64) +
				`",
  "agent": "codex",
  "header": "#A层级: X-x\n#B模块: Y-y",
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

func TestReadAgentHeaderStageRequestAcceptsSemanticRefresh(
	t *testing.T,
) {
	request, err := readAgentHeaderStageRequest(bytes.NewBufferString(`{
  "version": 1,
  "plan_id": "` + strings.Repeat("a", 64) + `",
  "agent": "codex",
  "intent": "semantic_refresh",
  "header": "#A层级: X-x\n#B模块: Y-y"
}`))
	if err != nil || request.Intent != agentHeaderStageIntentSemanticRefresh {
		t.Fatalf("semantic_refresh请求应按协议读取: err=%v request=%+v", err, request)
	}
}
