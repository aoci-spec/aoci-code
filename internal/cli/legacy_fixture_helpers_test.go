// Shared fixture helpers for CLI tests. They used to live in the Legacy
// authoring test files that left with the Legacy writing surfaces (0.2.0);
// the tests that still build repositories with them stay here.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// (from internal/cli/index_agent_plan_test.go)
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

// (from internal/cli/index_agent_plan_test.go)
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

// (from internal/cli/index_entries_apply_atomic_test.go)
// buildManualAtomicEntriesRepo构造不启用Host-Agent Plan防线的Endpoint草稿仓。
//
// 该夹具专门隔离人工Apply原子语义；Generation Plan漂移另由R60-D测试覆盖。
func buildManualAtomicEntriesRepo(
	t *testing.T,
) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(
		filepath.Join(root, ".aoci"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	indexText := "#====人工原子Apply测试索引====\n" +
		"#===头部索引===\n" +
		"#【系统】人工原子Apply测试仓\n" +
		"#A层级: X-程序入口\n" +
		"#B模块: UT-单元测试\n" +
		"#C重要度: 9核心 8高频 7业务 5常规 3辅助 1边缘\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		"#S配额: " + machinecontract.NumericText().SQuotaDefaultCompact + "\n" +
		"#===头部索引完毕===\n\n" +
		"#代码索引\n" +
		"===" + filepath.ToSlash(root) + "/===\n" +
		manualAtomicOldA + "\n" +
		manualAtomicOldB + "\n\n" +
		"#代码索引完毕\n"

	manualAtomicWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)
	manualAtomicWriteFile(
		t,
		root,
		"a.go",
		"package demo\n",
	)
	manualAtomicWriteFile(
		t,
		root,
		"b.go",
		"package demo\n",
	)

	cfg := legacyTestConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.LedgerEnabled = true
	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, _, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	// The Legacy draft run this fixture once minted left with the Legacy
	// authoring surfaces; every caller discards the second value.
	return root, ""
}

// (from internal/cli/index_update_test.go)
// buildUpdateRepo 造最小全对齐仓库。
func buildUpdateRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")

	if err := os.WriteFile(
		filepath.Join(root, "f.go"),
		[]byte("package f\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	indexText := "#头部第一行\n#头部第二行\n" +
		"===段" + rootSlash + "/===\n" +
		"f.go[XC5T]: F:x | R:- | A:- | S:-\n" +
		"aoci.txt[XC9T]: F:索引本体 | R:- | A:- | S:-\n"
	if err := os.WriteFile(
		filepath.Join(root, "aoci.txt"),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, warnings, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatalf("快照失败: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("快照不应有警告: %v", warnings)
	}
	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatalf("建线失败: %v", err)
	}
	return root
}

// (from internal/cli/index_entries_apply_atomic_test.go)
func manualAtomicWriteFile(
	t *testing.T,
	root,
	rel,
	content string,
) {
	t.Helper()

	absolutePath := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)
	if err := os.MkdirAll(
		filepath.Dir(absolutePath),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		absolutePath,
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

// Fixture Entry lines for the manual atomic update tests.
const (
	manualAtomicOldA = "a.go[XUT5T]: F:旧职责甲 | R:- | A:- | S:-"
	manualAtomicOldB = "b.go[XUT5T]: F:旧职责乙 | R:- | A:- | S:-"

	manualAtomicNewA = "a.go[XUT5T]: F:原子新职责甲 | R:b.go | A:- | S:-"
)
