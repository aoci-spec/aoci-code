// Entries Check的R关系轻量Warning测试。
package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestEntriesCheckCoreWarnsMissingRelationWithoutRejecting(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:R关系异常候选 | " +
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
		result.Review.Warned != 1 ||
		result.Review.Rejected != 0 ||
		len(result.Items) != 1 {
		t.Fatalf(
			"R关系Warning摘要不符: %+v",
			result,
		)
	}

	item := result.Items[0]
	if item.Outcome != "warned" ||
		len(item.Errors) != 0 ||
		len(item.Warnings) != 1 ||
		item.Warnings[0].Code != "relation" ||
		!strings.Contains(
			item.Warnings[0].Message,
			"R目标不存在",
		) {
		t.Fatalf(
			"R关系Warning分类不符: %+v",
			item,
		)
	}

	if !strings.Contains(
		output.String(),
		"[relation]",
	) ||
		!strings.Contains(
			output.String(),
			"R目标不存在",
		) {
		t.Fatalf(
			"人读输出缺少relation Warning: %s",
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
		manifest.Reviews[0].Warned != 1 ||
		manifest.Reviews[0].Rejected != 0 {
		t.Fatalf(
			"R关系Warning审阅记录不符: %+v",
			manifest.Reviews,
		)
	}
}
