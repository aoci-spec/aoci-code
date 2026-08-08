// 单条与原子批量共享R关系Warning测试。
package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestApplyUpdateEntryRelationWarningDoesNotBlock(
	t *testing.T,
) {
	root := buildRepo(t)

	outcome, fail := ApplyUpdateEntry(
		root,
		"src/a.go",
		"a.go[X.Y.5.T]: F:单条R警告仍写入 | "+
			"R:src/missing.go | A:- | S:-",
		ledger.SourceAgent,
		false,
	)
	if fail != nil {
		t.Fatalf(
			"单条R异常只应Warning，不得阻断写入: %+v",
			fail,
		)
	}
	if outcome == nil ||
		len(outcome.Warnings) == 0 {
		t.Fatalf(
			"单条写入应返回R Warning: %+v",
			outcome,
		)
	}

	foundWarning := false
	for _, warning := range outcome.Warnings {
		if strings.Contains(
			warning,
			"R目标不存在",
		) {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf(
			"单条结果缺少R目标不存在Warning: %+v",
			outcome.Warnings,
		)
	}

	indexText := readBatchIndex(
		t,
		root,
	)
	if !strings.Contains(
		indexText,
		"F:单条R警告仍写入",
	) {
		t.Fatalf(
			"单条R Warning不应阻断正式写入:\n%s",
			indexText,
		)
	}
}

func TestAtomicBatchRelationWarningStillApplies(
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
				NewEntry: "b.go[X.Y.5.T]: F:Auto批量R警告仍写入 | " +
					"R:src/missing.go | A:- | S:-",
			},
		},
		ledger.SourceCLIAI,
		false,
	)
	if fail != nil {
		t.Fatalf(
			"Auto批量R异常只应Warning，不得阻断Apply: %+v",
			fail,
		)
	}
	if outcome == nil ||
		len(outcome.Items) != 1 ||
		len(outcome.Items[0].Warnings) == 0 {
		t.Fatalf(
			"Auto批量结果缺少R Warning: %+v",
			outcome,
		)
	}

	foundWarning := false
	for _, warning := range outcome.Items[0].Warnings {
		if strings.Contains(
			warning,
			"R目标不存在",
		) {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf(
			"Auto批量结果缺少R目标不存在Warning: %+v",
			outcome.Items[0].Warnings,
		)
	}

	indexText := readBatchIndex(
		t,
		root,
	)
	if !strings.Contains(
		indexText,
		"F:Auto批量R警告仍写入",
	) {
		t.Fatalf(
			"Auto批量R Warning不应阻断正式Apply:\n%s",
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
			"带R Warning的Auto Apply仍应记录成功事件: %+v",
			events,
		)
	}
}
