// Integration tests of Score's E-scale exclusion of AOCI governance assets.
package indexgen

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestScoreEScaleSkipsAOCIGovernanceAssets locks that Score consumes the shared path criterion.
//
// The line count of .aoci/ledger.jsonl means the length of governance history, and the line count of aoci.txt the size of the index;
// neither stands for ordinary source complexity. Ordinary static source must still take the same E-band machine check.
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
