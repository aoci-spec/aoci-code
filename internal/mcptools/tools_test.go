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
			getEntriesIn{
				Paths: []string{"src/a.go"},
			},
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
			getEntriesIn{
				Paths: []string{"src/a.go"},
			},
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
			getEntriesIn{
				Paths: []string{"src/n.go"},
			},
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
			getEntriesIn{
				Paths: []string{"../etc/passwd"},
			},
		),
	)

	if !strings.Contains(
		output,
		errPathUnsafe,
	) {
		t.Fatal("逃逸路径应含 path_unsafe 拒绝")
	}
}

func TestApplyUpdateEntryPipeline(
	t *testing.T,
) {
	root := buildRepo(t)

	outcome, fail := ApplyUpdateEntry(
		root,
		"src/b.go",
		"b.go[X.Y.5.T]: F:乙 | R:- | A:- | S:新条目",
		"agent",
		false,
	)

	if fail != nil ||
		outcome.Action != "新增" {
		t.Fatalf(
			"新增失败: %+v %+v",
			outcome,
			fail,
		)
	}

	outcome, fail = ApplyUpdateEntry(
		root,
		"src/a.go",
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:替换后",
		"agent",
		false,
	)

	if fail != nil ||
		outcome.Action != "替换" {
		t.Fatalf(
			"替换失败: %+v %+v",
			outcome,
			fail,
		)
	}

	indexPath := filepath.Join(
		root,
		".aoci",
		"index.txt",
	)

	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	_, fail = ApplyUpdateEntry(
		root,
		"src/a.go",
		"wrong.go[X.Y.5.T]: F:- | R:- | A:- | S:-",
		"agent",
		false,
	)

	if fail == nil ||
		fail.Code != errBadArgs {
		t.Fatalf(
			"文件名不符应 bad_args 硬拒: %+v",
			fail,
		)
	}

	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Fatal("被拒的回写不得改动索引")
	}

	duplicateLine :=
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:替换后"

	if err := os.WriteFile(
		indexPath,
		[]byte(string(after)+duplicateLine+"\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	_, fail = ApplyUpdateEntry(
		root,
		"src/a.go",
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:再改",
		"agent",
		false,
	)

	if fail == nil ||
		fail.Code != errWriteConflict {
		t.Fatalf(
			"重复条目应 write_conflict: %+v",
			fail,
		)
	}

	if err := os.WriteFile(
		indexPath,
		before,
		0644,
	); err != nil {
		t.Fatal(err)
	}

	beforePreview, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	outcome, fail = ApplyUpdateEntry(
		root,
		"src/a.go",
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:预览",
		"human",
		true,
	)

	if fail != nil ||
		!outcome.DryRun {
		t.Fatalf(
			"干跑异常: %+v %+v",
			outcome,
			fail,
		)
	}

	afterPreview, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(beforePreview) != string(afterPreview) {
		t.Fatal("干跑不得落盘")
	}
}

func TestReportAppendOnly(
	t *testing.T,
) {
	root := buildRepo(t)

	indexPath := filepath.Join(
		root,
		".aoci",
		"index.txt",
	)

	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	output := resText(
		t,
		handleReport(
			root,
			reportIn{
				Path: "src/a.go",
				Note: "多行\n折叠",
			},
		),
	)

	if !strings.Contains(
		output,
		"已登记待办",
	) {
		t.Fatal("report 应确认登记")
	}

	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Fatal("report 不得改动索引")
	}

	data, err := os.ReadFile(
		filepath.Join(
			root,
			".aoci",
			"reports.jsonl",
		),
	)

	if err != nil ||
		strings.Contains(
			string(data),
			"\n折叠",
		) {
		t.Fatalf(
			"待办应落盘且 note 换行折叠: %v %q",
			err,
			string(data),
		)
	}
}

func TestSearchAndOverview(
	t *testing.T,
) {
	root := buildRepo(t)

	output := resText(
		t,
		handleSearch(
			root,
			searchIn{
				Keyword:   "必读",
				TagFilter: "C>=5",
			},
		),
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
		handleSearch(
			root,
			searchIn{},
		),
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
