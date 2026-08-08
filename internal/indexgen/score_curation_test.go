// score对策展Missing的原始事实层与治理质量层分型测试。
package indexgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestScoreCurationSeparatesRawAndActionable(
	t *testing.T,
) {
	files := map[string]string{
		"docs/x.md":   "# doc\n",
		"src/main.go": "package main\n",
	}

	root, cfg, doc := buildTestRepo(
		t,
		files,
		func(root string) string {
			root = filepath.ToSlash(root)
			return "#测试索引\n" +
				"#A层级: X测试\n" +
				"#B模块: RT根\n" +
				"#C重要度: 9核心\n" +
				"#E规模: T微<100\n" +
				"===配置索引" +
				root +
				"/===\n" +
				"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"
		},
	)

	cfg.CurationExclude = []string{
		"docs",
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

	score, err := BuildScore(
		root,
		cfg,
		doc,
	)
	if err != nil {
		t.Fatal(err)
	}

	coverage := scoreDimByName(
		t,
		score,
		"coverage",
	)

	if coverage.Bad != 1 ||
		len(coverage.Samples) != 1 ||
		coverage.Samples[0] != "src/main.go" {
		t.Fatalf(
			"coverage应只保留非策展待补录项,实得%+v",
			coverage,
		)
	}

	freshness := scoreDimByName(
		t,
		score,
		"freshness",
	)

	if freshness.Bad != 1 ||
		len(freshness.Samples) != 1 ||
		freshness.Samples[0] != "src/main.go" {
		t.Fatalf(
			"freshness应只统计未解决Actionable路径,实得%+v",
			freshness,
		)
	}

	if score.Drift.Missing != 2 {
		t.Fatalf(
			"原始Missing应为2,实得%+v",
			score.Drift,
		)
	}

	if score.Drift.CurationExcludedMissing != 1 {
		t.Fatalf(
			"策展Missing应为1,实得%+v",
			score.Drift,
		)
	}

	if score.Drift.ActionableMissing != 1 {
		t.Fatalf(
			"可执行Missing应为1,实得%+v",
			score.Drift,
		)
	}

	if score.Drift.Stale != 0 ||
		score.Drift.Orphan != 0 ||
		score.Drift.Unbaselined != 0 {
		t.Fatalf(
			"不应混入其他漂移:%+v",
			score.Drift,
		)
	}
}

func TestScoreOnlyCurationExcludedFreshnessClean(
	t *testing.T,
) {
	files := map[string]string{
		"docs/x.md": "# doc\n",
	}

	root, cfg, doc := buildTestRepo(
		t,
		files,
		func(root string) string {
			root = filepath.ToSlash(root)
			return "#测试索引\n" +
				"#A层级: X测试\n" +
				"#B模块: RT根\n" +
				"#C重要度: 9核心\n" +
				"#E规模: T微<100\n" +
				"===配置索引" +
				root +
				"/===\n" +
				"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n"
		},
	)

	cfg.CurationExclude = []string{
		"docs",
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

	score, err := BuildScore(
		root,
		cfg,
		doc,
	)
	if err != nil {
		t.Fatal(err)
	}

	coverage := scoreDimByName(
		t,
		score,
		"coverage",
	)
	freshness := scoreDimByName(
		t,
		score,
		"freshness",
	)

	if coverage.Bad != 0 ||
		freshness.Bad != 0 {
		t.Fatalf(
			"只有已exclude的原始Missing时coverage和freshness都应归零: coverage=%+v freshness=%+v",
			coverage,
			freshness,
		)
	}

	if score.Drift.Missing != 1 ||
		score.Drift.CurationExcludedMissing != 1 ||
		score.Drift.ActionableMissing != 0 {
		t.Fatalf(
			"原始事实与治理解释必须继续可见:%+v",
			score.Drift,
		)
	}

	if !strings.Contains(
		coverage.Note,
		"策展排除1项不计待补录",
	) {
		t.Fatalf(
			"coverage说明应明确排除治理结果:%s",
			coverage.Note,
		)
	}

	if !strings.Contains(
		freshness.Note,
		"已解释排除",
	) {
		t.Fatalf(
			"freshness说明应使用排除治理措辞:%s",
			freshness.Note,
		)
	}

	if strings.Contains(
		coverage.Note+"\n"+freshness.Note,
		"负空间",
	) {
		t.Fatalf(
			"Score机器说明不得继续使用负空间术语: coverage=%s freshness=%s",
			coverage.Note,
			freshness.Note,
		)
	}
}

func TestBuildDriftSummaryPartitionInvariant(
	t *testing.T,
) {
	cfg := &config.Config{
		CurationExclude: []string{
			"docs",
		},
	}

	detected := &baseline.DetectResult{
		Missing: []string{
			"docs/a.md",
			"docs/b.md",
			"src/main.go",
		},
		Orphan: []string{
			"ghost.go",
		},
		Stale: []string{
			"old.go",
		},
		Unbaselined: []string{
			"new.go",
		},
	}

	summary := buildDriftSummary(
		cfg,
		detected,
	)

	if summary.ActionableMissing+
		summary.CurationExcludedMissing != summary.Missing {
		t.Fatalf(
			"Missing分型守恒被破坏:%+v",
			summary,
		)
	}

	if summary.ActionableMissing != 1 ||
		summary.CurationExcludedMissing != 2 {
		t.Fatalf(
			"Missing分型不符:%+v",
			summary,
		)
	}
}
