// MCP工具handler单元测试(不起真实MCP client,直测handler主体)。
package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func buildRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()

	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}

	must(
		os.MkdirAll(
			filepath.Join(root, "src"),
			0755,
		),
	)
	must(
		os.MkdirAll(
			filepath.Join(root, ".aoci"),
			0755,
		),
	)
	must(
		os.WriteFile(
			filepath.Join(root, "src", "a.go"),
			[]byte("A1"),
			0644,
		),
	)

	rootSlash := filepath.ToSlash(root)
	indexText := "====测====\n" +
		"===段 " + rootSlash + "/src/===\n" +
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:改前必读\n" +
		"====完====\n"

	must(
		os.WriteFile(
			filepath.Join(root, ".aoci", "index.txt"),
			[]byte(indexText),
			0644,
		),
	)

	cfg := legacyTestConfig()
	cfg.IndexPath = ".aoci/index.txt"

	must(
		config.Save(
			root,
			cfg,
		),
	)

	snapshot, _, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	must(err)

	must(
		baseline.Save(
			root,
			baseline.NewBaseline(snapshot),
		),
	)

	return root
}

func resText(
	t *testing.T,
	result *mcp.CallToolResult,
) string {
	t.Helper()

	if len(result.Content) == 0 {
		t.Fatal("结果无内容")
	}

	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("结果非纯文本(违反 MCP 纪律)")
	}

	return content.Text
}

func TestGetEntriesStaleWarning(
	t *testing.T,
) {
	root := buildRepo(t)

	output := resText(
		t,
		handleGetEntries(
			root,
			"test-version",
			getEntriesIn{
				Paths: []string{"src/a.go"},
			},
			nil,
		),
	)

	if strings.Contains(output, "STALE") {
		t.Fatal("未改动不应出 STALE")
	}

	if err := os.WriteFile(
		filepath.Join(root, "src", "a.go"),
		[]byte("A2"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	output = resText(
		t,
		handleGetEntries(
			root,
			"test-version",
			getEntriesIn{
				Paths: []string{"src/a.go"},
			},
			nil,
		),
	)

	if !strings.Contains(output, "⚠ STALE") {
		t.Fatal("漂移警告必须出现")
	}

	if err := os.WriteFile(
		filepath.Join(root, "src", "n.go"),
		[]byte("N"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	output = resText(
		t,
		handleGetEntries(
			root,
			"test-version",
			getEntriesIn{
				Paths: []string{"src/n.go"},
			},
			nil,
		),
	)

	if !strings.Contains(output, "未收录") {
		t.Fatal("未收录文件应提示补录")
	}
}

func TestGetEntriesPathEscape(
	t *testing.T,
) {
	root := buildRepo(t)

	output := resText(
		t,
		handleGetEntries(
			root,
			"test-version",
			getEntriesIn{
				Paths: []string{"../etc/passwd"},
			},
			nil,
		),
	)

	if !strings.Contains(
		output,
		errPathUnsafe,
	) {
		t.Fatal("逃逸路径应含 path_unsafe 拒绝")
	}
}

func TestSearchAndOverview(
	t *testing.T,
) {
	root := buildRepo(t)

	output := resText(
		t,
		handleSearch(root, "test-version", searchIn{
			Keyword:   "必读",
			TagFilter: "C>=5",
		}, nil),
	)

	if !strings.Contains(
		output,
		"1 条",
	) {
		t.Fatalf(
			"检索应命中 1 条: %q",
			output,
		)
	}

	output = resText(
		t,
		handleSearch(root, "test-version", searchIn{}, nil),
	)

	if !strings.Contains(
		output,
		errBadArgs,
	) {
		t.Fatal("空参检索应 bad_args")
	}

	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatalf(
			"loadRepoCtx 失败: %+v",
			fail,
		)
	}

	overview := renderLegacyOverviewForTest(
		t, root, "test", repository.text, repository.doc,
		overviewIn{}, machinecontract.OverviewChunkTokensDefault,
	).Output
	for _, anchor := range []string{
		"full_text_included: true",
		"a.go[X.Y.5.T]",
		"<<<AOCI_OVERVIEW_BODY_BEGIN/v1",
	} {
		if !strings.Contains(
			overview,
			anchor,
		) {
			t.Fatalf(
				"完整总览缺少锚点%q:\n%s",
				anchor,
				overview,
			)
		}
	}

	if strings.Contains(
		overview,
		"修改文件前仍用 aoci_get_entries",
	) {
		t.Fatalf(
			"完整总览不得继续要求重复读取目标Entry:\n%s",
			overview,
		)
	}

	if strings.Contains(overview, "AOCI Index Overview") || strings.Contains(overview, "A Layer distribution") {
		t.Fatal("ordinary Overview retained the removed directory wrapper")
	}
}

// TestOverviewRelocatesHistoricalSectionPaths验证Overview明确区分当前仓库根与
// 索引正文中的历史绝对路径，同时不改动正式索引字节。
// TestOverviewRelocatesHistoricalSectionPaths验证Overview明确区分当前仓库根与
// 索引正文中的历史绝对路径，同时不改动正式索引字节。
// TestOverviewRelocatesHistoricalSectionPaths验证Overview明确区分当前仓库根与
// 索引正文中的历史绝对路径，同时不改动正式索引字节。
// TestOverviewRelocatesHistoricalSectionPaths验证Overview明确区分当前仓库根与
// 索引正文中的历史绝对路径，同时不改动正式索引字节。
func TestOverviewRelocatesHistoricalSectionPaths(
	t *testing.T,
) {
	root := buildRepo(t)
	indexPath := filepath.Join(
		root,
		".aoci",
		"index.txt",
	)
	historicalRoot := "/opt/aoci-code"
	historicalIndex := "====测====\n" +
		"===根 " + historicalRoot + "/===\n" +
		"root.go[X.Y.5.T]: F:根 | R:- | A:- | S:-\n" +
		"===段 " + historicalRoot + "/src/===\n" +
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:改前必读\n" +
		"====完====\n"

	if err := os.WriteFile(
		indexPath,
		[]byte(historicalIndex),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatalf(
			"loadRepoCtx失败: %+v",
			fail,
		)
	}

	entry := index.FindEntry(
		repository.doc,
		"src/a.go",
	)
	if entry == nil || entry.RelPath != "src/a.go" {
		t.Fatalf(
			"历史目录段应重定位到当前仓库相对路径: %+v",
			entry,
		)
	}

	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	overview := renderLegacyOverviewForTest(
		t, root, "v-migration-test", repository.text, repository.doc,
		overviewIn{}, machinecontract.OverviewChunkTokensDefault,
	).Output

	for _, anchor := range []string{
		"runtime_repository_root: " + root,
		"===根 " + historicalRoot + "/===",
		"===段 " + historicalRoot + "/src/===",
	} {
		if !strings.Contains(
			overview,
			anchor,
		) {
			t.Fatalf(
				"迁移Overview缺少锚点%q:\n%s",
				anchor,
				overview,
			)
		}
	}
	if strings.Contains(overview, filepath.ToSlash(filepath.Join(root, "src"))+"(段)") ||
		strings.Contains(overview, "目录总览") {
		t.Fatal("Overview must not synthesize a relocated directory summary")
	}

	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("Overview纯读不得改写历史目录段或索引正文")
	}
}
