// Direct tests of the indexgen package score layer.
//
// Coverage:
//   - completeness and fixed order of the nine dimensions (format/coverage/freshness/squota/dict/token/
//     agent_ready/escale/tagparse; a new dimension may only be appended at the end);
//   - correctness of each dimension's Bad judgement (format hard refusal / coverage difference / freshness four states including Orphan /
//     squota quota / dict symbol outside the dictionary / escale band mismatch / tagparse unparseable tag),
//     with violation samples in the rel path reading;
//   - dict and escale report Total=0 as undecidable when the header thresholds are unavailable, never misreporting;
//   - escale skips entries not on disk without counting errors, and the .aoci runtime assets have their own test in
//     score_escale_runtime_test.go;
//   - tagparse reuses ValidateEntryLineWith's Warning without crosstalk with format/dict/escale;
//   - the D59 iron rule mechanized: the default truncation and limit<=0 full output share one judgement loop;
//   - pure read with zero side effects: the index file bytes are unchanged before and after scoring, and no Baseline is created.
package indexgen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// scoreDimByName takes a dimension by name; consumers never depend on the index.
func scoreDimByName(t *testing.T, sc *Score, name string) Dimension {
	t.Helper()
	for _, d := range sc.Dimensions {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("评分结果缺维度 %s,实有: %+v", name, sc.Dimensions)
	return Dimension{}
}

// scoreContains reports whether the sample list holds the target string.
func scoreContains(list []string, want string) bool {
	for _, value := range list {
		if value == want {
			return true
		}
	}
	return false
}

// scoreTestHeader returns a test header holding the complete dictionary and numeric E-scale thresholds.
func scoreTestHeader() string {
	return "#====测试索引====\n" +
		"#===索引规范===\n" +
		"#A层级: X-测试 C-CLI A运行时产物\n" +
		"#B模块: RT根 IN初始化 LG台账\n" +
		"#C重要度: 9核心 3辅助\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		"#===索引规范完毕===\n"
}

// buildScoreViolationRepo builds a violation fixture in which the score dimensions are isolated from each other.
func buildScoreViolationRepo(
	t *testing.T,
) (string, *config.Config, *index.Document) {
	t.Helper()

	longS := strings.Repeat("坑", 60)
	files := map[string]string{
		"main.go":    "package main\n",
		"bad.go":     "package main\n",
		"dictbad.go": "package main\n",
		"quota.go":   "package main\n",
		"escale.go":  "package main\n",
		"tagbad.go":  "package main\n",
	}

	return buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return scoreTestHeader() +
			"===配置索引" + root + "/===\n" +
			"aoci.txt[XRT9T]: F:索引本体 | R:- | A:- | S:测试\n" +
			"main.go[XRT9T]: F:入口 | R:- | A:- | S:测试\n" +
			"bad.go[XRT9T]: F:仅有F段\n" +
			"dictbad.go[ZRT9T]: F:字典外A符号 | R:- | A:- | S:测试\n" +
			"quota.go[XRT3T]: F:超配额 | R:- | A:- | S:" + longS + "\n" +
			"escale.go[XRT9L]: F:档位错配 | R:- | A:- | S:测试\n" +
			"tagbad.go[UAU8]: F:标签缺位 | R:- | A:- | S:测试\n" +
			"ghost.go[XRT9L]: F:索引先行不在盘 | R:- | A:- | S:测试\n"
	})
}

// TestScoreNineDimensionsFixedOrder: the nine dimensions are complete and the order is fixed.
func TestScoreNineDimensionsFixedOrder(t *testing.T) {
	root, cfg, doc := buildScoreViolationRepo(t)
	sc, err := BuildScore(root, cfg, doc)
	if err != nil {
		t.Fatalf("BuildScore失败: %v", err)
	}

	want := []string{
		"format",
		"coverage",
		"freshness",
		"squota",
		"dict",
		"token",
		"agent_ready",
		"escale",
		"tagparse",
	}
	if len(sc.Dimensions) != len(want) {
		t.Fatalf("维度数应为%d,实得%d", len(want), len(sc.Dimensions))
	}
	for position, name := range want {
		if sc.Dimensions[position].Name != name {
			t.Errorf(
				"维度[%d]应为%s,实得%s",
				position,
				name,
				sc.Dimensions[position].Name,
			)
		}
	}
}

// TestScoreViolationJudgments verifies that each violation enters only its own score dimension.
func TestScoreViolationJudgments(t *testing.T) {
	root, cfg, doc := buildScoreViolationRepo(t)
	sc, err := BuildScore(root, cfg, doc)
	if err != nil {
		t.Fatalf("BuildScore失败: %v", err)
	}

	if sc.EntryCount != 8 {
		t.Errorf("EntryCount应为8,实得%d", sc.EntryCount)
	}
	if sc.DiskCount != 7 {
		t.Errorf("DiskCount应为7,实得%d", sc.DiskCount)
	}

	dimension := scoreDimByName(t, sc, "format")
	if dimension.Total != 8 ||
		dimension.Bad != 1 ||
		len(dimension.Samples) != 1 ||
		dimension.Samples[0] != "bad.go" {
		t.Errorf("format判定不符: %+v", dimension)
	}

	dimension = scoreDimByName(t, sc, "coverage")
	if dimension.Total != 7 || dimension.Bad != 0 {
		t.Errorf("coverage判定不符: %+v", dimension)
	}

	dimension = scoreDimByName(t, sc, "freshness")
	if dimension.Total != 7 || dimension.Bad != 8 {
		t.Errorf("freshness判定不符: %+v", dimension)
	}

	dimension = scoreDimByName(t, sc, "squota")
	if dimension.Bad != 1 ||
		len(dimension.Samples) != 1 ||
		dimension.Samples[0] != "quota.go" {
		t.Errorf("squota判定不符: %+v", dimension)
	}

	dimension = scoreDimByName(t, sc, "dict")
	if dimension.Total != 8 ||
		dimension.Bad != 1 ||
		len(dimension.Samples) != 1 ||
		dimension.Samples[0] != "dictbad.go" {
		t.Errorf("dict判定不符: %+v", dimension)
	}
	if scoreContains(dimension.Samples, "tagbad.go") {
		t.Error("不可解析标签不得被dict伪判")
	}

	dimension = scoreDimByName(t, sc, "escale")
	if dimension.Total != 8 ||
		dimension.Bad != 1 ||
		len(dimension.Samples) != 1 ||
		dimension.Samples[0] != "escale.go" {
		t.Errorf("escale判定不符: %+v", dimension)
	}
	if scoreContains(dimension.Samples, "ghost.go") ||
		scoreContains(dimension.Samples, "tagbad.go") {
		t.Errorf("escale不得计入无实体或不可解析标签: %+v", dimension.Samples)
	}

	dimension = scoreDimByName(t, sc, "tagparse")
	if dimension.Total != 8 ||
		dimension.Bad != 1 ||
		len(dimension.Samples) != 1 ||
		dimension.Samples[0] != "tagbad.go" {
		t.Errorf("tagparse判定不符: %+v", dimension)
	}
}

// TestScoreNotJudgeableDims: an undecidable dimension must have Total=0 and not misreport.
func TestScoreNotJudgeableDims(t *testing.T) {
	files := map[string]string{"main.go": "package main\n"}
	root, cfg, doc := buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return "#====测试索引====\n" +
			"===配置索引" + root + "/===\n" +
			"aoci.txt[QRT9Q]: F:索引 | R:- | A:- | S:测试\n" +
			"main.go[QRT9Q]: F:入口 | R:- | A:- | S:测试\n"
	})

	sc, err := BuildScore(root, cfg, doc)
	if err != nil {
		t.Fatalf("BuildScore失败: %v", err)
	}

	dimension := scoreDimByName(t, sc, "dict")
	if dimension.Total != 0 || dimension.Bad != 0 {
		t.Errorf("dict不可判定口径不符: %+v", dimension)
	}

	dimension = scoreDimByName(t, sc, "escale")
	if dimension.Total != 0 || dimension.Bad != 0 {
		t.Errorf("escale不可判定口径不符: %+v", dimension)
	}

	dimension = scoreDimByName(t, sc, "tagparse")
	if dimension.Total != 2 || dimension.Bad != 0 {
		t.Errorf("tagparse合法标签口径不符: %+v", dimension)
	}
}

// TestScoreLimitFullSameJudgment: the default truncation and the full output must share one criterion.
func TestScoreLimitFullSameJudgment(t *testing.T) {
	longS := strings.Repeat("坑", 60)
	files := map[string]string{}
	entryLines := ""

	for position := 1; position <= 7; position++ {
		name := fmt.Sprintf("q%d.go", position)
		files[name] = "package main\n"
		entryLines += name +
			"[XRT3T]: F:超配额 | R:- | A:- | S:" +
			longS +
			"\n"
	}

	root, cfg, doc := buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return scoreTestHeader() +
			"===配置索引" + root + "/===\n" +
			"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:测试\n" +
			entryLines
	})

	defaultScore, err := BuildScore(root, cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	fullScore, err := BuildScoreWithLimit(root, cfg, doc, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(defaultScore.Dimensions) != len(fullScore.Dimensions) {
		t.Fatal("两口径维度数不等")
	}
	for position := range defaultScore.Dimensions {
		defaultDimension := defaultScore.Dimensions[position]
		fullDimension := fullScore.Dimensions[position]
		if defaultDimension.Name != fullDimension.Name ||
			defaultDimension.Bad != fullDimension.Bad ||
			defaultDimension.Total != fullDimension.Total {
			t.Errorf(
				"维度[%d]两口径判定不一致: %+v / %+v",
				position,
				defaultDimension,
				fullDimension,
			)
		}
	}
}

// TestScoreTagParseLimitFullSameJudgment locks the tagparse sample truncation.
func TestScoreTagParseLimitFullSameJudgment(t *testing.T) {
	files := map[string]string{}
	entryLines := ""

	for position := 1; position <= 7; position++ {
		name := fmt.Sprintf("t%d.go", position)
		files[name] = "package main\n"
		entryLines += name +
			"[UAU8]: F:标签缺位 | R:- | A:- | S:测试\n"
	}

	root, cfg, doc := buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return scoreTestHeader() +
			"===配置索引" + root + "/===\n" +
			"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:测试\n" +
			entryLines
	})

	defaultScore, err := BuildScore(root, cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	fullScore, err := BuildScoreWithLimit(root, cfg, doc, 0)
	if err != nil {
		t.Fatal(err)
	}

	defaultDimension := scoreDimByName(t, defaultScore, "tagparse")
	fullDimension := scoreDimByName(t, fullScore, "tagparse")

	if defaultDimension.Bad != 7 || fullDimension.Bad != 7 {
		t.Fatalf(
			"tagparse Bad应均为7: %d/%d",
			defaultDimension.Bad,
			fullDimension.Bad,
		)
	}
	if len(defaultDimension.Samples) != scoreSampleLimit ||
		len(fullDimension.Samples) != 7 {
		t.Fatalf(
			"tagparse样本截断不符: %d/%d",
			len(defaultDimension.Samples),
			len(fullDimension.Samples),
		)
	}
}

// TestScoreReadOnlyNoSideEffects: scoring must not change the index or create a Baseline.
func TestScoreReadOnlyNoSideEffects(t *testing.T) {
	files := map[string]string{"main.go": "package main\n"}
	root, cfg, doc := buildTestRepo(t, files, func(root string) string {
		root = filepath.ToSlash(root)
		return scoreTestHeader() +
			"===配置索引" + root + "/===\n" +
			"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:测试\n" +
			"main.go[XRT9T]: F:入口 | R:- | A:- | S:测试\n"
	})

	indexPath := filepath.Join(root, "aoci.txt")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := BuildScore(root, cfg, doc); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("评分前后索引文件字节发生变化")
	}

	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	if _, err := os.Stat(baselinePath); !os.IsNotExist(err) {
		t.Errorf("评分不得创建基线文件: %v", err)
	}
}
