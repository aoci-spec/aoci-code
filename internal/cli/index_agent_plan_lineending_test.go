// Host-Agent Plan换行宽容与原始字节绑定测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func buildAgentPlanLineEndingRepo(
	t *testing.T,
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
		"x.go",
		"package x\n\nvar Value = 1\n",
	)

	indexText := agentPlanHeader(true) +
		"\n===代码索引" +
		rootSlash +
		"/===\n" +
		"aoci.txt[XRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"x.go[XAP7T]: F:测试文件 | R:- | A:- | S:保留约束\n"

	agentPlanWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, warnings, err :=
		baseline.Snapshot(
			root,
			cfg.WalkOptions(),
		)
	if err != nil {
		t.Fatal(err)
	}

	if len(warnings) != 0 {
		t.Fatalf(
			"测试快照不应有警告: %v",
			warnings,
		)
	}

	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	return root
}

// TestAgentPlanLineEndingToleranceKeepsDispatchQuietButBindingStrict验证：
// 判定层不派发假任务，绑定层仍感知原始字节变化。
func TestAgentPlanLineEndingToleranceKeepsDispatchQuietButBindingStrict(
	t *testing.T,
) {
	root := buildAgentPlanLineEndingRepo(t)

	cfg, document, indexPath :=
		agentPlanLoadDocument(
			t,
			root,
		)

	initialPlan, err := buildAgentPlan(
		root,
		cfg,
		document,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	if initialPlan.Stage !=
		agentPlanStageAligned {
		t.Fatalf(
			"初始仓库应aligned: %+v",
			initialPlan,
		)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"x.go",
		),
		[]byte(
			"package x\r\n\r\nvar Value = 1\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	tolerantPlan, err := buildAgentPlan(
		root,
		cfg,
		document,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	if tolerantPlan.Stage !=
		agentPlanStageAligned ||
		tolerantPlan.NextAction !=
			agentPlanActionNone ||
		tolerantPlan.Summary.Changed != 0 ||
		len(tolerantPlan.Targets) != 0 {
		t.Fatalf(
			"默认宽容时纯换行差异不得派发任务: %+v",
			tolerantPlan,
		)
	}

	if initialPlan.RepositorySHA256 ==
		tolerantPlan.RepositorySHA256 {
		t.Fatal(
			"原始字节变化必须改变repository_sha256，使旧Plan失效",
		)
	}

	currentFingerprint, err :=
		baseline.HashFile(
			filepath.Join(
				root,
				"x.go",
			),
		)
	if err != nil {
		t.Fatal(err)
	}

	strictConfig := *cfg
	strictConfig.LineEndingTolerance = false

	strictPlan, err := buildAgentPlan(
		root,
		&strictConfig,
		document,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	if strictPlan.Stage !=
		agentPlanStageEntriesRequired ||
		strictPlan.NextAction !=
			agentPlanActionStageEntries ||
		strictPlan.Summary.Changed != 1 ||
		len(strictPlan.Targets) != 1 {
		t.Fatalf(
			"团队严格模式应派发一个更新目标: %+v",
			strictPlan,
		)
	}

	target := strictPlan.Targets[0]

	if target.Path != "x.go" ||
		target.Kind != "update" ||
		target.SourceSHA256 !=
			currentFingerprint.SHA256 {
		t.Fatalf(
			"严格模式目标必须绑定当前原始字节SHA: %+v",
			target,
		)
	}

	if strictPlan.RepositorySHA256 !=
		tolerantPlan.RepositorySHA256 {
		t.Fatal(
			"同一当前快照的repository_sha256不应受判定模式影响",
		)
	}

	if strictPlan.PlanID ==
		tolerantPlan.PlanID {
		t.Fatal(
			"治理状态和目标不同，plan_id必须不同",
		)
	}
}

// TestAgentPlanLineEndingToleranceDoesNotHideRealChange验证真实内容变化仍派发。
func TestAgentPlanLineEndingToleranceDoesNotHideRealChange(
	t *testing.T,
) {
	root := buildAgentPlanLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"x.go",
		),
		[]byte(
			"package x\r\n\r\nvar Value = 2\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, document, indexPath :=
		agentPlanLoadDocument(
			t,
			root,
		)

	plan, err := buildAgentPlan(
		root,
		cfg,
		document,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Stage !=
		agentPlanStageEntriesRequired ||
		plan.Summary.Changed != 1 ||
		len(plan.Targets) != 1 ||
		plan.Targets[0].Path != "x.go" {
		t.Fatalf(
			"真实内容变化不得被换行宽容吞掉: %+v",
			plan,
		)
	}
}
