// Score对AOCI治理资产的E规模排除集成测试。
package indexgen

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestScoreEScaleSkipsAOCIGovernanceAssets锁定Score消费共享路径判据。
//
// .aoci/ledger.jsonl行数表示治理历史长度，aoci.txt行数表示索引条目规模；
// 两者都不代表普通源码复杂度。普通静态源码仍必须接受同一E档位机器检查。
func TestScoreEScaleSkipsAOCIGovernanceAssets(
	t *testing.T,
) {
	longFile := strings.Repeat(
		"x\n",
		401,
	)

	files := map[string]string{
		".aoci/ledger.jsonl": strings.Repeat(
			"{}\n",
			401,
		),
		"src/big.go": longFile,
	}

	root, cfg, doc := buildTestRepo(
		t,
		files,
		func(root string) string {
			root = filepath.ToSlash(root)
			return scoreTestHeader() +
				strings.Repeat(
					"#索引规模填充\n",
					401,
				) +
				"===配置索引" +
				root +
				"/===\n" +
				"aoci.txt[XRT9M]: F:索引本体 | R:- | A:- | S:测试\n" +
				"===治理资产" +
				root +
				"/.aoci/===\n" +
				"ledger.jsonl[ALG3M]: F:台账 | R:- | A:- | S:测试\n" +
				"===源码" +
				root +
				"/src/===\n" +
				"big.go[XRT9M]: F:静态源码 | R:- | A:- | S:测试\n"
		},
	)

	score, err := BuildScore(
		root,
		cfg,
		doc,
	)
	if err != nil {
		t.Fatal(err)
	}

	dimension := scoreDimByName(
		t,
		score,
		"escale",
	)

	if dimension.Bad != 1 ||
		len(dimension.Samples) != 1 ||
		dimension.Samples[0] != "src/big.go" {
		t.Fatalf(
			"只有普通静态源码应产生E档位告警: %+v",
			dimension,
		)
	}

	for _, excluded := range []string{
		"aoci.txt",
		".aoci/ledger.jsonl",
	} {
		if scoreContains(
			dimension.Samples,
			excluded,
		) {
			t.Fatalf(
				"AOCI治理资产不得进入E档位样本%s: %+v",
				excluded,
				dimension.Samples,
			)
		}
	}
}
