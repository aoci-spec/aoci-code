// `aoci index agent plan` 确定性计划器测试。
//
// 覆盖:
//   - 混合态中 Stale 优先、Missing 过滤、策展和跳过事实可见;
//   - plan_id 在状态不变时稳定;
//   - 计划器不修改索引、基线且不写 ledger;
//   - 基线缺失阻断;
//   - 头部字典缺失阻断;
//   - 索引自身漂移阻断;
//   - 全对齐仓返回 aligned。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

func agentPlanWriteFile(
	t *testing.T,
	root,
	rel,
	content string,
) {
	t.Helper()

	path := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)
	if err := os.MkdirAll(
		filepath.Dir(path),
		0o755,
	); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(
		path,
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

func agentPlanHeader(
	withDict bool,
) string {
	header := "#====Agent Plan测试索引====\n"
	if withDict {
		header += "#A层级: C-命令 X构建\n" +
			"#B模块: RT根 AP计划\n" +
			"#C重要度: 9核心 7业务 3辅助\n" +
			"#E规模: L大>400 M中200-400 S小100-200 T微<100\n"
	}
	return header
}

func agentPlanLoadDocument(
	t *testing.T,
	root string,
) (*config.Config, *index.Document, string) {
	t.Helper()

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	indexPath := filepath.Join(
		root,
		cfg.IndexPath,
	)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("读取索引失败: %v", err)
	}
	doc, warnings := index.Parse(string(data))
	if len(warnings) != 0 {
		t.Fatalf("测试索引不应产生警告: %+v", warnings)
	}
	index.ResolveRelPaths(doc, root)
	return cfg, doc, indexPath
}

// buildAgentPlanMixedRepo 构造:
//   - stale.go: 已收录且指纹变化;
//   - new.go: 可执行Missing;
//   - docs/skip.md: 被策展排除的Missing;
//   - empty.txt: 工具看见但建议跳过的Missing;
//   - orphan.go: 仅索引存在;
//   - aoci.txt: 当前对齐。
func buildAgentPlanMixedRepo(
	t *testing.T,
	withDict,
	withBaseline bool,
) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	agentPlanWriteFile(
		t,
		root,
		"stale.go",
		"package main\n// 当前实现已经变化\n",
	)
	agentPlanWriteFile(
		t,
		root,
		"new.go",
		"package main\n\nfunc NewFeature() {}\n",
	)
	agentPlanWriteFile(
		t,
		root,
		"docs/skip.md",
		"# 维护者明确不收录\n",
	)
	agentPlanWriteFile(
		t,
		root,
		"empty.txt",
		"",
	)

	indexText := agentPlanHeader(withDict) +
		"\n===代码索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"stale.go[XAP7T]: F:旧职责 | R:- | A:- | S:必须保留的旧约束\n" +
		"orphan.go[XAP3T]: F:孤儿条目 | R:- | A:- | S:-\n"
	agentPlanWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatalf("加载团队配置失败: %v", err)
	}
	cfg.CurationExclude = []string{"docs"}
	if err := config.Save(root, cfg); err != nil {
		t.Fatalf("保存团队配置失败: %v", err)
	}

	if withBaseline {
		indexFP, err := baseline.HashFile(
			filepath.Join(root, "aoci.txt"),
		)
		if err != nil {
			t.Fatalf("索引哈希失败: %v", err)
		}
		baselineState := baseline.NewBaseline(
			map[string]baseline.Fingerprint{
				"aoci.txt": indexFP,
				"stale.go": {
					SHA256: "0000旧指纹",
					Size:   1,
				},
			},
		)
		if err := baseline.Save(
			root,
			baselineState,
		); err != nil {
			t.Fatalf("保存基线失败: %v", err)
		}
	}

	return root
}

func TestAgentPlanMixedStates(
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
	baselineBefore, err := os.ReadFile(baselinePath)
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
		t.Fatalf("构造计划失败: %v", err)
	}

	if plan.Stage != agentPlanStageEntriesRequired ||
		plan.NextAction != agentPlanActionStageEntries {
		t.Fatalf(
			"混合态应进入entries_required: %+v",
			plan,
		)
	}
	if plan.HeaderState != agentPlanHeaderReady {
		t.Fatalf("头部应可用: %+v", plan)
	}
	if !plan.BaselineExists {
		t.Fatal("基线应存在")
	}
	if plan.Summary.Changed != 1 ||
		plan.Summary.Missing != 3 ||
		plan.Summary.ActionableNew != 1 ||
		plan.Summary.CurationExcluded != 1 ||
		plan.Summary.SkippedMissing != 1 ||
		plan.Summary.Orphan != 1 ||
		plan.Summary.ExecutableTargets != 2 {
		t.Fatalf(
			"事实/行动计数不符: %+v",
			plan.Summary,
		)
	}

	if len(plan.Targets) != 2 {
		t.Fatalf("应有2个可执行目标: %+v", plan.Targets)
	}
	if plan.Targets[0].Kind != "update" ||
		plan.Targets[0].Path != "stale.go" {
		t.Fatalf(
			"Stale必须排在Missing之前: %+v",
			plan.Targets,
		)
	}
	if !strings.Contains(
		plan.Targets[0].OldEntry,
		"必须保留的旧约束",
	) {
		t.Fatalf(
			"更新目标必须携带旧条目: %+v",
			plan.Targets[0],
		)
	}
	if plan.Targets[0].SourceSHA256 == "" {
		t.Fatal("更新目标必须携带当前源码SHA-256")
	}

	if plan.Targets[1].Kind != "create" ||
		plan.Targets[1].Path != "new.go" {
		t.Fatalf(
			"第二目标应为可执行Missing: %+v",
			plan.Targets,
		)
	}
	if plan.Targets[1].SuggestedSection != "代码索引" ||
		plan.Targets[1].Lines != 3 {
		t.Fatalf(
			"新目标画像不符: %+v",
			plan.Targets[1],
		)
	}

	if len(plan.CurationExcluded) != 1 ||
		plan.CurationExcluded[0] != "docs/skip.md" {
		t.Fatalf(
			"策展Missing应可见但不派发: %+v",
			plan.CurationExcluded,
		)
	}
	if len(plan.SkippedMissing) != 1 ||
		plan.SkippedMissing[0].Path != "empty.txt" ||
		plan.SkippedMissing[0].Reason != "empty" {
		t.Fatalf(
			"建议跳过Missing不符: %+v",
			plan.SkippedMissing,
		)
	}
	if len(plan.Orphans) != 1 ||
		plan.Orphans[0] != "orphan.go" {
		t.Fatalf("孤儿列表不符: %+v", plan.Orphans)
	}

	secondPlan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatalf("第二次构造计划失败: %v", err)
	}
	if plan.PlanID == "" ||
		plan.PlanID != secondPlan.PlanID {
		t.Fatalf(
			"状态不变时plan_id必须稳定: %q != %q",
			plan.PlanID,
			secondPlan.PlanID,
		)
	}

	indexAfter, _ := os.ReadFile(indexPath)
	baselineAfter, _ := os.ReadFile(baselinePath)
	if string(indexBefore) != string(indexAfter) {
		t.Error("agent plan不得修改索引")
	}
	if string(baselineBefore) != string(baselineAfter) {
		t.Error("agent plan不得修改基线")
	}
	if _, err := os.Stat(
		filepath.Join(root, ".aoci", "ledger.jsonl"),
	); !os.IsNotExist(err) {
		t.Fatalf(
			"agent plan必须零落账，ledger不应被创建: %v",
			err,
		)
	}
}

func TestAgentPlanBaselineRequired(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		false,
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
		t.Fatalf("构造计划失败: %v", err)
	}
	if plan.Stage != agentPlanStageBaselineRequired ||
		plan.NextAction != agentPlanActionScan {
		t.Fatalf(
			"无基线应要求scan: %+v",
			plan,
		)
	}
	if plan.BaselineExists {
		t.Fatal("BaselineExists应为false")
	}
	if len(plan.Targets) != 0 {
		t.Fatalf(
			"基线未建立时不得发放任务: %+v",
			plan.Targets,
		)
	}
}

func TestAgentPlanIDIgnoresOnlyVolatileExcludedRuntimeCounts(t *testing.T) {
	root := buildAgentPlanMixedRepo(t, true, true)
	cfg, doc, indexPath := agentPlanLoadDocument(t, root)
	plan, err := buildAgentPlan(root, cfg, doc, indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SafeInventory == nil {
		t.Fatal("fixture must expose Safe Inventory summary")
	}

	volatile := *plan
	volatile.GeneratedAt = "2099-01-01T00:00:00Z"
	volatile.BaselineUpdatedAt = "2099-01-01T00:00:01Z"
	volatileSummary := *plan.SafeInventory
	volatileSummary.Ignored += 100
	volatileSummary.RuntimeExcluded += 90
	volatileSummary.GeneratedExcluded += 10
	volatile.SafeInventory = &volatileSummary
	if volatile.Governance != nil {
		governance := *volatile.Governance
		governance.ExcludeCount += 100
		volatile.Governance = &governance
	}
	volatileID, err := calculateAgentPlanID(&volatile)
	if err != nil {
		t.Fatal(err)
	}
	if volatileID != plan.PlanID {
		t.Fatalf("audit timestamps and hard-excluded runtime churn must not expire their own Plan: %s != %s", volatileID, plan.PlanID)
	}

	sensitive := volatile
	sensitiveSummary := volatileSummary
	sensitiveSummary.BuiltinSensitiveExcluded++
	sensitive.SafeInventory = &sensitiveSummary
	sensitiveID, err := calculateAgentPlanID(&sensitive)
	if err != nil {
		t.Fatal(err)
	}
	if sensitiveID == plan.PlanID {
		t.Fatal("sensitive exclusion changes must remain Plan-bound")
	}

	managed := volatile
	if managed.Governance != nil {
		governance := *managed.Governance
		governance.IndexCount++
		managed.Governance = &governance
		managedID, err := calculateAgentPlanID(&managed)
		if err != nil {
			t.Fatal(err)
		}
		if managedID == plan.PlanID {
			t.Fatal("managed Index count changes must remain Plan-bound")
		}
	}
}

func TestAgentPlanHeaderRequired(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		false,
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
		t.Fatalf("构造计划失败: %v", err)
	}
	if plan.Stage != agentPlanStageHeaderRequired ||
		plan.NextAction != agentPlanActionGenerateHead {
		t.Fatalf(
			"无字典头部应要求生成头部: %+v",
			plan,
		)
	}
	if plan.HeaderState != agentPlanHeaderMissing {
		t.Fatalf(
			"头部状态应为missing: %+v",
			plan,
		)
	}
	if len(plan.Targets) != 0 {
		t.Fatalf(
			"字典未立约时不得发放任务: %+v",
			plan.Targets,
		)
	}
}

func TestAgentPlanIndexSelfStaleRequiresReview(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)
	indexPath := filepath.Join(root, "aoci.txt")
	file, err := os.OpenFile(
		indexPath,
		os.O_APPEND|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(
		"#基线建立后由外部通道改动\n",
	); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, doc, loadedIndexPath :=
		agentPlanLoadDocument(t, root)
	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		loadedIndexPath,
	)
	if err != nil {
		t.Fatalf("构造计划失败: %v", err)
	}
	if plan.Stage != agentPlanStageIndexReviewRequired ||
		plan.NextAction != agentPlanActionReviewIndex ||
		!plan.IndexSelfStale {
		t.Fatalf(
			"索引自身漂移应要求人工核对: %+v",
			plan,
		)
	}
	if len(plan.Targets) != 0 {
		t.Fatalf(
			"索引自身未确认时不得发放任务: %+v",
			plan.Targets,
		)
	}
}

func buildAgentPlanAlignedRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	agentPlanWriteFile(
		t,
		root,
		"keep.go",
		"package main\n",
	)
	if err := ensureManagedAgentsLocale(root, textassets.LegacyLocale); err != nil {
		t.Fatalf("创建受管AGENTS夹具失败: %v", err)
	}
	indexText := agentPlanHeader(true) +
		"\n===代码索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"AGENTS.md[XAP7T]: F:Agent规则 | R:- | A:- | S:-\n" +
		"keep.go[XAP7T]: F:对齐文件 | R:- | A:- | S:-\n"
	agentPlanWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	indexFP, err := baseline.HashFile(
		filepath.Join(root, "aoci.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	keepFP, err := baseline.HashFile(
		filepath.Join(root, "keep.go"),
	)
	if err != nil {
		t.Fatal(err)
	}
	agentsFP, err := baseline.HashFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(
		root,
		baseline.NewBaseline(
			map[string]baseline.Fingerprint{
				"aoci.txt":  indexFP,
				"AGENTS.md": agentsFP,
				"keep.go":   keepFP,
			},
		),
	); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestAgentPlanAligned(
	t *testing.T,
) {
	root := buildAgentPlanAlignedRepo(t)

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
	if plan.Stage != agentPlanStageAligned ||
		plan.NextAction != agentPlanActionNone {
		t.Fatalf(
			"全对齐仓应返回aligned: %+v",
			plan,
		)
	}
	if len(plan.Targets) != 0 ||
		plan.Summary.ExecutableTargets != 0 {
		t.Fatalf(
			"全对齐仓不得有目标: %+v",
			plan,
		)
	}
}
