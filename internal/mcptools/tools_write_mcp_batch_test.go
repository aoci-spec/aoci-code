// MCP原生批量回写、紧凑终态与重复Apply防重测试。
package mcptools

import (
	"encoding/json"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func assertAutoMachineCounts(
	t *testing.T,
	raw string,
	attempted,
	applied,
	duplicateApplies,
	semanticFiles,
	formatOnlyFiles int,
) {
	t.Helper()
	var document struct {
		Attempted *int `json:"attempted"`
		Applied   *int `json:"applied"`
		Remaining *int `json:"remaining"`
		Metrics   struct {
			DuplicateApplies *int `json:"duplicate_applies"`
			SemanticFiles    *int `json:"semantic_files"`
			FormatOnlyFiles  *int `json:"format_only_files"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatalf("机器终态必须是单一合法JSON对象: %v\n%s", err, raw)
	}
	if document.Attempted == nil || *document.Attempted != attempted ||
		document.Applied == nil || *document.Applied != applied ||
		document.Remaining == nil || *document.Remaining != 0 ||
		document.Metrics.DuplicateApplies == nil || *document.Metrics.DuplicateApplies != duplicateApplies ||
		document.Metrics.SemanticFiles == nil || *document.Metrics.SemanticFiles != semanticFiles ||
		document.Metrics.FormatOnlyFiles == nil || *document.Metrics.FormatOnlyFiles != formatOnlyFiles {
		t.Fatalf("机器终态关键计数缺失、类型错误或数值不符: %+v\n%s", document, raw)
	}
}

func successfulBatchApplications(t *testing.T, root string) int {
	t.Helper()
	events, corrupt := ledger.Recent(root, 100)
	if corrupt != 0 {
		t.Fatalf("Ledger不应存在损坏行: %d", corrupt)
	}
	count := 0
	for _, event := range events {
		if event.Op == "update_entries_batch" &&
			event.Result == ledger.ResultOK &&
			event.AppliedCount > 0 {
			count++
		}
	}
	return count
}

func TestMCPBatchRequiresMaintainSourceBinding(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	indexBefore := readBatchIndex(t, root)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path:     "src/b.go",
		NewEntry: "b.go[X.Y.5.T]: F:缺绑定 | R:- | A:- | S:-",
	}}))
	if result.Status != autoStatusRepairRequired || result.Aligned || len(result.Findings) == 0 {
		t.Fatalf("无源码指纹的MCP批次必须零写入拒绝: %+v", result)
	}
	if readBatchIndex(t, root) != indexBefore {
		t.Fatal("缺少source_sha256不得写入索引")
	}
}
