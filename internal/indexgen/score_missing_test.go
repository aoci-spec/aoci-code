// Score对Missing三分、规范字段兼容、Pending子集及freshness治理口径的测试。
package indexgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

func TestScoreMissingThreeWayClassification(
	t *testing.T,
) {
	files := map[string]string{
		"docs/x.md":   "# doc\n",
		"empty.txt":   "",
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

	if score.Drift.Missing != 3 ||
		score.Drift.RawMissing != 3 ||
		score.Drift.ActionableMissing != 1 ||
		score.Drift.CurationExcludedMissing != 1 ||
		score.Drift.SkippedMissing != 1 ||
		score.Drift.PendingCuration != 1 ||
		score.Drift.PendingCurationMissing != 1 {
		t.Fatalf(
			"Missing三分、规范字段或Pending子集不符:%+v",
			score.Drift,
		)
	}

	if score.Drift.Missing !=
		score.Drift.RawMissing {
		t.Fatalf(
			"历史missing必须继续等于规范raw_missing:%+v",
			score.Drift,
		)
	}

	if score.Drift.PendingCuration !=
		score.Drift.PendingCurationMissing {
		t.Fatalf(
			"历史pending_curation必须继续等于规范pending_curation_missing:%+v",
			score.Drift,
		)
	}

	if score.Drift.ActionableMissing+
		score.Drift.CurationExcludedMissing+
		score.Drift.SkippedMissing !=
		score.Drift.RawMissing {
		t.Fatalf(
			"Missing三分守恒失败:%+v",
			score.Drift,
		)
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
			"coverage只应统计ActionableMissing:%+v",
			coverage,
		)
	}

	freshness := scoreDimByName(
		t,
		score,
		"freshness",
	)

	if freshness.Bad != 2 {
		t.Fatalf(
			"freshness应统计Actionable与Pending两条未解决路径:%+v",
			freshness,
		)
	}

	if !scoreContains(
		freshness.Samples,
		"src/main.go",
	) ||
		!scoreContains(
			freshness.Samples,
			"empty.txt",
		) ||
		scoreContains(
			freshness.Samples,
			"docs/x.md",
		) {
		t.Fatalf(
			"freshness样本应包含Actionable和Pending但排除已exclude路径:%+v",
			freshness,
		)
	}

	if !strings.Contains(
		freshness.Note,
		"ActionableMissing+PendingCurationMissing+Orphan+Stale+Unbaselined",
	) {
		t.Fatalf(
			"freshness说明必须使用完整规范术语PendingCurationMissing:%q",
			freshness.Note,
		)
	}

	if strings.Contains(
		freshness.Note,
		"ActionableMissing+PendingCuration+Orphan",
	) {
		t.Fatalf(
			"freshness说明不得退回旧缩写PendingCuration:%q",
			freshness.Note,
		)
	}
}
