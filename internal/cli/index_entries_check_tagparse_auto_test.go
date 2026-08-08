// 自动生成来源的标签不可解析分层测试。
//
// 存量人工维护继续允许非标标签以Warning进入审阅；Host-Agent与Endpoint新生成
// 候选没有存量兼容负担，必须在正式Apply前转为tagparse拒绝并进入自动修复。
package cli

import (
	"bytes"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestEntriesCheckGeneratedSourceRejectsUnparseableTag(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[REFM]: F:缺少重要度 | R:- | A:- | S:-",
		},
	)

	cfg, err := config.Load(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runEntriesCheckCore(
		root,
		runID,
		cfg,
		loadEntriesCheckCoreDoc(t, root),
		&bytes.Buffer{},
		ledger.SourceAgent,
	)
	if err != nil {
		t.Fatalf(
			"正常tagparse拒绝应由结果表达: %v",
			err,
		)
	}

	if result.Review.Passed != 0 ||
		result.Review.Warned != 0 ||
		result.Review.Rejected != 1 ||
		len(result.Items) != 1 ||
		result.Items[0].Outcome != "rejected" ||
		len(result.Items[0].Errors) != 1 ||
		result.Items[0].Errors[0].Code != "tagparse" {
		t.Fatalf(
			"自动生成tagparse分层不符: %+v",
			result,
		)
	}

	events, corrupt := ledger.Recent(
		root,
		20,
	)
	if corrupt != 0 {
		t.Fatalf(
			"Ledger不应损坏: %d",
			corrupt,
		)
	}

	found := false
	for _, event := range events {
		if event.Op != "entries_check" ||
			event.DraftRunID != runID {
			continue
		}

		found = true

		if event.Result !=
			ledger.ResultRepairRequired ||
			event.PathsCount != 1 ||
			event.PassedCount != 0 ||
			event.WarnedCount != 0 ||
			event.RejectedCount != 1 ||
			event.SkippedCount != 0 ||
			event.WarningsCount != 0 {
			t.Fatalf(
				"自动生成拒绝Ledger事实不符: %+v",
				event,
			)
		}
	}

	if !found {
		t.Fatalf(
			"未找到entries_check Ledger事件: %+v",
			events,
		)
	}
}

func TestEntriesCheckHumanKeepsUnparseableTagWarning(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[REFM]: F:存量非标标签 | R:- | A:- | S:-",
		},
	)

	cfg, err := config.Load(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runEntriesCheckCore(
		root,
		runID,
		cfg,
		loadEntriesCheckCoreDoc(t, root),
		&bytes.Buffer{},
		ledger.SourceHuman,
	)
	if err != nil {
		t.Fatalf(
			"人工来源非标标签应保持Warning: %v",
			err,
		)
	}

	if result.Review.Passed != 1 ||
		result.Review.Warned != 1 ||
		result.Review.Rejected != 0 ||
		len(result.Items) != 1 ||
		result.Items[0].Outcome != "warned" ||
		len(result.Items[0].Warnings) == 0 {
		t.Fatalf(
			"人工存量兼容分层不符: %+v",
			result,
		)
	}
}
