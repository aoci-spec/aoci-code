// Formal metering reports for the Auto no-change, small-change, and retry paths.
package mcptools

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestRenderAutoResultKeepsCriticalZeroCounts(t *testing.T) {
	raw := renderAutoResult(autoResult{
		Version:    1,
		Status:     autoStatusApplied,
		Aligned:    true,
		Receipt:    cognitionReceipt{},
		Metrics:    autoMetrics{},
		NextAction: autoApplyNextAction(false, true, 0, 0),
	})
	assertAutoMachineCounts(t, raw, 0, 0, 0, 0, 0)
	for _, field := range []string{
		`"attempted":0`,
		`"applied":0`,
		`"remaining":0`,
		`"semantic_files":0`,
		`"format_only_files":0`,
		`"duplicate_applies":0`,
	} {
		if !strings.Contains(raw, field) {
			t.Fatalf("关键零值字段不得从机器终态省略: field=%s raw=%s", field, raw)
		}
	}
}

func TestBoundApplyAlignmentFindingsCountsKindsOutsideSample(t *testing.T) {
	limit := machinecontract.MaintainTransportListLimit
	details := make([]string, limit, limit+3)
	for i := range details {
		details[i] = "missing: pkg/missing.go"
	}
	details = append(details, "observed_changed: data/observed.md",
		"evidence_unavailable: source:primary", "baseline_unavailable")
	budget := machinecontract.MaintainTransportBudgetBytesDefault
	var wire struct {
		Findings  []string                 `json:"findings"`
		Truncated *applyFindingsTruncation `json:"findings_truncated"`
	}
	if err := json.Unmarshal([]byte(renderBoundedApplyResult(autoResult{}, details, budget)), &wire); err != nil {
		t.Fatal(err)
	}
	sample, truncation := wire.Findings, wire.Truncated
	if !slices.Equal(sample, details[:limit]) || truncation == nil {
		t.Fatalf("the sample must be an unchanged prefix with truncation counts: %v, %+v", sample, truncation)
	}
	if truncation.Limit != limit || truncation.ByteLimit != budget || truncation.Total != len(details) || len(truncation.ByKind) != 4 ||
		truncation.ByKind["missing"] != limit || truncation.ByKind["observed_changed"] != 1 ||
		truncation.ByKind["evidence_unavailable"] != 1 || truncation.ByKind["baseline_unavailable"] != 1 {
		t.Fatalf("the breakdown must include all omitted kinds: %+v", truncation)
	}
}

func TestApplyFindingsBudgetAllowsAnEmptyOrderedPrefix(t *testing.T) {
	// JSON expands each ampersand to six bytes, so the first diagnostic is
	// too large on the wire even though its unescaped path would fit.
	details := []string{"stale: " + strings.Repeat("&", 3300), "missing: main.go"}
	budget := machinecontract.MaintainTransportBudgetBytesMin
	raw := renderBoundedApplyResult(autoResult{}, details, budget)
	result := decodeMaintainJSON(t, raw)
	meta, _ := result["findings_truncated"].(map[string]any)
	kinds, _ := meta["by_kind"].(map[string]any)
	findings, isList := result["findings"].([]any)
	if len(raw) > budget || result["finding_count"] != float64(2) || !isList || len(findings) != 0 ||
		meta["limit"] != float64(machinecontract.MaintainTransportListLimit) || meta["byte_limit"] != float64(budget) ||
		meta["total"] != float64(2) || len(kinds) != 2 || kinds["stale"] != float64(1) || kinds["missing"] != float64(1) {
		t.Fatalf("the escaped first finding must cut the sample, including the shorter second item: bytes=%d count=%v truncated=%v",
			len(raw), result["finding_count"], meta)
	}
}
