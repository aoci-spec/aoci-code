// indexgen 包 score 评分层直接测试。
//
// 覆盖面:
//   - 九维度齐全性与固定顺序(format/coverage/freshness/squota/dict/token/
//     agent_ready/escale/tagparse,新维度只允许尾部追加);
//   - 各维 Bad 判定正确性(format硬拒/coverage差集/freshness四态含Orphan/
//     squota配额/dict字典外符号/escale档位错配/tagparse标签不可解析),
//     违规样本为 rel 路径口径;
//   - dict 与 escale 在头部阈值不可用时 Total=0 表不可判定,绝不误报;
//   - escale 对不在盘条目跳过不计错,.aoci运行时资产专项见
//     score_escale_runtime_test.go;
//   - tagparse 复用 ValidateEntryLineWith 的 Warning,不与 format/dict/escale串扰;
//   - D59 铁律机器化: 默认截断与 limit<=0 全量共用同一判定循环;
//   - 纯读零副作用: 评分前后索引文件字节不变,且不创建Baseline。
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

// scoreDimByName 按名取维,消费方绝不依赖下标。
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

// scoreContains 判断样本清单是否包含目标字符串。
func scoreContains(list []string, want string) bool {
	for _, value := range list {
		if value == want {
			return true
		}
	}
	return false
}

// scoreTestHeader 返回含完整字典与E规模数字阈值的测试头部。
func scoreTestHeader() string {
	return "#====测试索引====\n" +
		"#===索引规范===\n" +
		"#A层级: X-测试 C-CLI A运行时产物\n" +
		"#B模块: RT根 IN初始化 LG台账\n" +
		"#C重要度: 9核心 3辅助\n" +
		"#E规模: L大>400 M中200-400 S小100-200 T微<100\n" +
		"#===索引规范完毕===\n"
}

// buildScoreViolationRepo 构造各评分维度相互隔离的违规夹具。
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

// TestScoreNineDimensionsFixedOrder 九维齐全且顺序固定。
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

// TestScoreViolationJudgments 验证各违规只进入所属评分维度。
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

// TestScoreNotJudgeableDims 不可判定维度必须Total=0且不误报。
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

// TestScoreLimitFullSameJudgment 默认截断与全量必须共用同一判据。
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

// TestScoreTagParseLimitFullSameJudgment 锁定tagparse样本截断。
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

// TestScoreReadOnlyNoSideEffects 评分不得修改索引或创建Baseline。
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
