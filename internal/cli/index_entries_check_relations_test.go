// Entries Check的R形式Warning测试。
//
// 指向不存在目标的R不再产生任何提示: 机器不核对R指向谁。仍会提示的只有这一行
// 本身读不通的情况(空片段、占位符混用等), 而且永远只是Warning。
package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestEntriesCheckCoreAcceptsRelationToMissingTargetWithoutWarning(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:R指向尚未创作的对象 | " +
				"R:./missing.go | A:- | S:-",
		},
	)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	result, err := runEntriesCheckCore(
		root,
		runID,
		cfg,
		loadEntriesCheckCoreDoc(t, root),
		&output,
		ledger.SourceCLIAI,
	)
	if err != nil {
		t.Fatalf(
			"R关系Warning不得形成core错误: %v\n%s",
			err,
			output.String(),
		)
	}

	if result == nil ||
		result.Review.Passed != 1 ||
		result.Review.Warned != 0 ||
		result.Review.Rejected != 0 ||
		len(result.Items) != 1 {
		t.Fatalf(
			"指向不存在目标的R不应产生Warning: %+v",
			result,
		)
	}

	item := result.Items[0]
	if item.Outcome != "passed" ||
		len(item.Errors) != 0 ||
		len(item.Warnings) != 0 {
		t.Fatalf(
			"机器不应评判R指向: %+v",
			item,
		)
	}

	if strings.Contains(
		output.String(),
		"[relation]",
	) {
		t.Fatalf(
			"人读输出不应出现关系评判: %s",
			output.String(),
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].Passed != 1 ||
		manifest.Reviews[0].Warned != 0 ||
		manifest.Reviews[0].Rejected != 0 {
		t.Fatalf(
			"审阅记录不符: %+v",
			manifest.Reviews,
		)
	}
}
