// 单条与原子批量共享的R形式约定测试。
//
// 指向不存在目标的R既不阻断写入, 也不再产生Warning: 机器不核对R指向谁。
package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestApplyUpdateEntryRelationToMissingTargetIsSilentAndApplies(
	t *testing.T,
) {
	root := buildRepo(t)

	outcome, fail := ApplyUpdateEntry(
		root,
		"src/a.go",
		"a.go[X.Y.5.T]: F:单条R指向尚未创作的对象 | "+
			"R:src/missing.go | A:- | S:-",
		ledger.SourceAgent,
		false,
	)
	if fail != nil {
		t.Fatalf(
			"R指向不应阻断写入: %+v",
			fail,
		)
	}
	if outcome == nil {
		t.Fatal("单条写入未返回结果")
	}
	for _, warning := range outcome.Warnings {
		if strings.Contains(warning, "R目标") {
			t.Fatalf(
				"机器不应评判R指向: %+v",
				outcome.Warnings,
			)
		}
	}

	indexText := readBatchIndex(
		t,
		root,
	)
	if !strings.Contains(
		indexText,
		"R:src/missing.go",
	) {
		t.Fatalf(
			"模型写下的关系没有被原样保留:\n%s",
			indexText,
		)
	}
}

func TestAtomicBatchRelationToMissingTargetIsSilentAndApplies(
	t *testing.T,
) {
	root := buildRepo(t)
	writeBatchSource(
		t,
		root,
		"src/b.go",
	)

	outcome, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path: "src/b.go",
				NewEntry: "b.go[X.Y.5.T]: F:Auto批量R指向尚未创作的对象 | " +
					"R:src/missing.go | A:- | S:-",
			},
		},
		ledger.SourceCLIAI,
		false,
	)
	if fail != nil {
		t.Fatalf(
			"R指向不应阻断Apply: %+v",
			fail,
		)
	}
	if outcome == nil || len(outcome.Items) != 1 {
		t.Fatalf("Auto批量未返回结果: %+v", outcome)
	}
	for _, warning := range outcome.Items[0].Warnings {
		if strings.Contains(warning, "R目标") {
			t.Fatalf(
				"机器不应评判R指向: %+v",
				outcome.Items[0].Warnings,
			)
		}
	}

	indexText := readBatchIndex(
		t,
		root,
	)
	if !strings.Contains(
		indexText,
		"R:src/missing.go",
	) {
		t.Fatalf(
			"模型写下的关系没有被原样保留:\n%s",
			indexText,
		)
	}

	events, _ := ledger.Recent(
		root,
		20,
	)
	foundSuccess := false
	for _, event := range events {
		if event.Op == "update_entries_batch" &&
			event.Result == ledger.ResultOK &&
			event.Source == ledger.SourceCLIAI &&
			event.PathsCount == 1 &&
			event.AppliedCount == 1 {
			foundSuccess = true
		}
	}
	if !foundSuccess {
		t.Fatalf(
			"Auto Apply仍应记录成功事件: %+v",
			events,
		)
	}
}
