// Score换行宽容生产路径测试。
package indexgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
)

func buildScoreLineEndingRepo(
	t *testing.T,
) (
	string,
	*config.Config,
	*index.Document,
) {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		"===配置索引" +
		rootSlash +
		"/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n" +
		"x.go[XRT9T]: F:测试文件 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(root, "aoci.txt"),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "x.go"),
		[]byte("package x\n\nvar Value = 1\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
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

	document, parseWarnings := index.Parse(indexText)
	if len(parseWarnings) != 0 {
		t.Fatalf(
			"测试索引不应有解析警告: %+v",
			parseWarnings,
		)
	}

	index.ResolveRelPaths(document, root)

	return root, cfg, document
}

func TestScoreLineEndingToleranceAndStrictMode(
	t *testing.T,
) {
	root, cfg, document :=
		buildScoreLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(root, "x.go"),
		[]byte("package x\r\n\r\nvar Value = 1\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	tolerantScore, err := BuildScore(
		root,
		cfg,
		document,
	)
	if err != nil {
		t.Fatal(err)
	}

	freshness := scoreDimByName(
		t,
		tolerantScore,
		"freshness",
	)

	if tolerantScore.Drift.Stale != 0 ||
		tolerantScore.Drift.LineEndingOnly != 1 ||
		freshness.Bad != 0 {
		t.Fatalf(
			"默认宽容Score口径不符: drift=%+v freshness=%+v",
			tolerantScore.Drift,
			freshness,
		)
	}

	cfg.LineEndingTolerance = false

	strictScore, err := BuildScore(
		root,
		cfg,
		document,
	)
	if err != nil {
		t.Fatal(err)
	}

	strictFreshness := scoreDimByName(
		t,
		strictScore,
		"freshness",
	)

	if strictScore.Drift.Stale != 1 ||
		strictScore.Drift.LineEndingOnly != 0 ||
		strictFreshness.Bad != 1 {
		t.Fatalf(
			"严格Score口径不符: drift=%+v freshness=%+v",
			strictScore.Drift,
			strictFreshness,
		)
	}
}

func TestScoreLineEndingToleranceDoesNotHideRealChange(
	t *testing.T,
) {
	root, cfg, document :=
		buildScoreLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(root, "x.go"),
		[]byte("package x\r\n\r\nvar Value = 2\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	scoreValue, err := BuildScore(
		root,
		cfg,
		document,
	)
	if err != nil {
		t.Fatal(err)
	}

	freshness := scoreDimByName(
		t,
		scoreValue,
		"freshness",
	)

	if scoreValue.Drift.Stale != 1 ||
		scoreValue.Drift.LineEndingOnly != 0 ||
		freshness.Bad != 1 {
		t.Fatalf(
			"真实变化不得被宽容吞掉: drift=%+v freshness=%+v",
			scoreValue.Drift,
			freshness,
		)
	}
}
