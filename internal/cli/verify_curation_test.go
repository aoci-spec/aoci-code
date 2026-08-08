// verify策展型Missing的事实层与治理终态测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// buildVerifyCurationRepo 构造已经由团队策略排除docs/x.md的仓库。
//
// withActionable=true时额外加入src/main.go普通Missing，用于验证：
// 原始Missing完整保留，但只有尚未解决的普通Missing触发ExitDrift。
func buildVerifyCurationRepo(
	t *testing.T,
	withActionable bool,
) string {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: T测试\n" +
		"#C重要度: 5常规\n" +
		"#E规模: T微<100\n" +
		"===配置索引" +
		rootSlash +
		"/===\n" +
		"aoci.txt[XT5T]: F:索引 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(
			root,
			"aoci.txt",
		),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(
		filepath.Join(
			root,
			"docs",
		),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"docs",
			"x.md",
		),
		[]byte("# doc\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if withActionable {
		if err := os.MkdirAll(
			filepath.Join(
				root,
				"src",
			),
			0o755,
		); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			filepath.Join(
				root,
				"src",
				"main.go",
			),
			[]byte("package main\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	cfg := legacyTestConfig()
	cfg.CurationExclude = []string{"docs"}
	cfg.LedgerEnabled = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, warnings, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatalf(
			"建立测试基线时采集快照失败: %v",
			err,
		)
	}

	if len(warnings) != 0 {
		t.Fatalf(
			"测试仓快照不应产生警告: %v",
			warnings,
		)
	}

	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatalf(
			"建立测试基线失败: %v",
			err,
		)
	}

	return root
}

func withVerifyFlags(
	t *testing.T,
	root string,
	jsonMode bool,
) {
	t.Helper()

	oldRepo := flagRepo
	oldJSON := flagJSON
	oldQuiet := flagQuiet

	flagRepo = root
	flagJSON = jsonMode
	flagQuiet = false

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
		flagQuiet = oldQuiet
	})
}

func TestCollectCurationExcludedMissingKeepsOrder(
	t *testing.T,
) {
	cfg := &config.Config{
		CurationExclude: []string{
			"docs",
			"README.md",
		},
	}

	missing := []string{
		"src/main.go",
		"docs/a.md",
		"README.md",
		"docs2/not-matched.md",
	}

	got := collectCurationExcludedMissing(
		cfg,
		missing,
	)
	want := []string{
		"docs/a.md",
		"README.md",
	}

	if len(got) != len(want) {
		t.Fatalf(
			"策展子集数量不符: got=%v want=%v",
			got,
			want,
		)
	}

	for position := range want {
		if got[position] != want[position] {
			t.Fatalf(
				"策展子集顺序或内容不符: got=%v want=%v",
				got,
				want,
			)
		}
	}
}

func TestRenderVerifyHumanSeparatesRawAndUnresolved(
	t *testing.T,
) {
	report := &verifyReport{
		Root:           "/repo",
		IndexEntries:   1,
		DiskFiles:      3,
		BaselineExists: true,
		Result: &baseline.DetectResult{
			Missing: []string{
				"docs/x.md",
				"src/main.go",
			},
			Orphan:      []string{},
			Stale:       []string{},
			Unbaselined: []string{},
		},
		ActionableMissing: []string{
			"src/main.go",
		},
		CurationExcludedMissing: []string{
			"docs/x.md",
		},
		FormatWarnings: []string{},
		GeneratedAt:    "2026-07-13T00:00:00Z",
	}

	got := renderVerifyHuman(
		report,
	)

	for _, anchor := range []string{
		"Missing 原始事实(2)",
		"CurationExcludedMissing 策展排除型Missing(1)",
		"未解决治理漂移共 1 项(exit 1)",
		"原始四态事实共 2 项",
	} {
		if !strings.Contains(
			got,
			anchor,
		) {
			t.Fatalf(
				"人读报告缺少事实/治理分层锚点%q:\n%s",
				anchor,
				got,
			)
		}
	}
}

func TestVerifyMixedCurationAndActionableStillDrifts(
	t *testing.T,
) {
	root := buildVerifyCurationRepo(
		t,
		true,
	)
	withVerifyFlags(
		t,
		root,
		false,
	)

	cmd := newVerifyCmd()
	var out bytes.Buffer
	var errOut bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.RunE(
		cmd,
		nil,
	)

	var exitErr *ExitError

	if !errors.As(
		err,
		&exitErr,
	) ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"仍有ActionableMissing时应为ExitDrift: %v",
			err,
		)
	}

	text := out.String()

	for _, anchor := range []string{
		"CurationExcludedMissing 策展排除型Missing(1)",
		"docs/x.md",
		"src/main.go",
		"未解决治理漂移共 1 项(exit 1)",
		"原始四态事实共 2 项",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"生产命令人读输出缺少锚点%q:\n%s",
				anchor,
				text,
			)
		}
	}

	historyDir := filepath.Join(
		root,
		".aoci",
		"verify_history",
	)

	entries, readErr := os.ReadDir(
		historyDir,
	)
	if readErr != nil {
		t.Fatalf(
			"verify_history应已创建: %v",
			readErr,
		)
	}

	if len(entries) != 1 {
		t.Fatalf(
			"应产生1份verify快照,实得%d",
			len(entries),
		)
	}

	snapshot, readErr := os.ReadFile(
		filepath.Join(
			historyDir,
			entries[0].Name(),
		),
	)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if !strings.Contains(
		string(snapshot),
		"未解决治理漂移共 1 项(exit 1)",
	) {
		t.Fatalf(
			"历史快照应包含同一治理结论:\n%s",
			snapshot,
		)
	}
}

func TestVerifyOnlyCurationExcludedReturnsSuccess(
	t *testing.T,
) {
	root := buildVerifyCurationRepo(
		t,
		false,
	)
	withVerifyFlags(
		t,
		root,
		true,
	)

	cmd := newVerifyCmd()
	var out bytes.Buffer
	var errOut bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.RunE(
		cmd,
		nil,
	); err != nil {
		t.Fatalf(
			"只有已exclude的原始Missing时verify应exit=0: %v\n%s",
			err,
			out.String(),
		)
	}

	var report verifyReport

	if err := json.Unmarshal(
		out.Bytes(),
		&report,
	); err != nil {
		t.Fatalf(
			"JSON输出不可解析: %v\n%s",
			err,
			out.String(),
		)
	}

	if len(report.Result.Missing) != 1 ||
		report.Result.Missing[0] != "docs/x.md" {
		t.Fatalf(
			"原始Missing事实必须保留: %+v",
			report.Result.Missing,
		)
	}

	if len(report.CurationExcludedMissing) != 1 ||
		report.CurationExcludedMissing[0] != "docs/x.md" {
		t.Fatalf(
			"策展排除子集不符: %+v",
			report.CurationExcludedMissing,
		)
	}

	if verifyRawDriftCount(&report) != 1 ||
		verifyUnresolvedDriftCount(&report) != 0 {
		t.Fatalf(
			"事实层与治理层计数不符: raw=%d unresolved=%d report=%+v",
			verifyRawDriftCount(&report),
			verifyUnresolvedDriftCount(&report),
			report,
		)
	}
}

func TestVerifyCommandJSONExposesCurationSubset(
	t *testing.T,
) {
	root := buildVerifyCurationRepo(
		t,
		true,
	)
	withVerifyFlags(
		t,
		root,
		true,
	)

	cmd := newVerifyCmd()
	var out bytes.Buffer
	var errOut bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.RunE(
		cmd,
		nil,
	)

	var exitErr *ExitError

	if !errors.As(
		err,
		&exitErr,
	) ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"JSON模式仍有ActionableMissing时应为ExitDrift: %v",
			err,
		)
	}

	var report verifyReport

	if decodeErr := json.Unmarshal(
		out.Bytes(),
		&report,
	); decodeErr != nil {
		t.Fatalf(
			"JSON输出不可解析: %v\n%s",
			decodeErr,
			out.String(),
		)
	}

	if len(report.Result.Missing) != 2 {
		t.Fatalf(
			"原始Missing必须完整保留,实得%v",
			report.Result.Missing,
		)
	}

	if len(report.ActionableMissing) != 1 ||
		report.ActionableMissing[0] != "src/main.go" {
		t.Fatalf(
			"ActionableMissing不符: %v",
			report.ActionableMissing,
		)
	}

	if len(report.CurationExcludedMissing) != 1 ||
		report.CurationExcludedMissing[0] != "docs/x.md" {
		t.Fatalf(
			"JSON策展子集不符: %v",
			report.CurationExcludedMissing,
		)
	}

	if verifyUnresolvedDriftCount(
		&report,
	) != 1 {
		t.Fatalf(
			"未解决治理计数应为1: %+v",
			report,
		)
	}
}
