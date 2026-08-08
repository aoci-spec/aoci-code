// aoci_maintain消费正式curation.json的集成测试。
package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
)

func buildMaintainFileCurationRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"marker.empty",
		"",
	)

	indexText := maintainHeader(true) +
		"\n===代码索引" +
		filepath.ToSlash(root) +
		"/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n"

	maintainWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	snapshot, warnings, err := baseline.Snapshot(
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

func saveMaintainFileDecision(
	t *testing.T,
	root,
	decision,
	role,
	reason string,
) {
	t.Helper()

	profile, err := curation.ProfilePath(
		root,
		"marker.empty",
	)
	if err != nil {
		t.Fatal(err)
	}

	document := &curation.Document{
		Version: curation.Version,
		Decisions: []curation.Decision{
			{
				Path:         "marker.empty",
				Decision:     decision,
				Role:         role,
				Reason:       reason,
				Confidence:   98,
				SourceSHA256: profile.SourceSHA256,
				Agent:        "codex",
				Model:        "test-model",
				UpdatedAt:    "2026-07-15T00:00:00Z",
			},
		},
	}

	if err := curation.Save(
		root,
		document,
	); err != nil {
		t.Fatal(err)
	}
}

func TestMaintainPendingCurationNotDispatched(
	t *testing.T,
) {
	root := buildMaintainFileCurationRepo(t)

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusStopped || len(result.Candidates) != 0 ||
		!hasFinding(result, "pending_curation: marker.empty") {
		t.Fatalf("Pending特殊文件只能返回真实策展停点: %+v", result)
	}
}

func TestMaintainIncludedSpecialFileDispatched(
	t *testing.T,
) {
	root := buildMaintainFileCurationRepo(t)

	saveMaintainFileDecision(
		t,
		root,
		curation.DecisionInclude,
		"声明包级协议能力",
		"空文件存在本身会被运行时识别",
	)

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusRepairRequired || len(result.Candidates) != 1 {
		t.Fatalf("有效include应仅派发一个语义候选: %+v", result)
	}
	candidate := result.Candidates[0]
	if candidate.Path != "marker.empty" || candidate.Kind != "新增·include" ||
		candidate.CurationRole != "声明包级协议能力" ||
		candidate.CurationReason != "空文件存在本身会被运行时识别" ||
		candidate.ProfileReason != "empty" || candidate.SourceSHA256 == "" {
		t.Fatalf("include候选证据不完整: %+v", candidate)
	}
}

func TestMaintainExcludedSpecialFileNotDispatched(
	t *testing.T,
) {
	root := buildMaintainFileCurationRepo(t)

	saveMaintainFileDecision(
		t,
		root,
		curation.DecisionExclude,
		"无独立文件级职责",
		"该文件无需形成独立索引条目",
	)

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusApplied || !result.Aligned || len(result.Candidates) != 0 {
		t.Fatalf("有效exclude应被内部治理后直接对齐: %+v", result)
	}
}

func TestMaintainStaleDecisionBecomesPending(
	t *testing.T,
) {
	root := buildMaintainFileCurationRepo(t)

	saveMaintainFileDecision(
		t,
		root,
		curation.DecisionInclude,
		"旧角色",
		"旧摘要绑定决策",
	)

	if err := os.WriteFile(
		filepath.Join(root, "marker.empty"),
		[]byte{0},
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusStopped || len(result.Candidates) != 0 ||
		!hasFinding(result, "pending_curation: marker.empty") {
		t.Fatalf("过期include只能回到逐项策展裁决: %+v", result)
	}
}
