// Formal metering reports for the Auto no-change, small-change, and retry paths.
package mcptools

import (
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
	sample, truncation := boundApplyAlignmentFindings(details)
	if !slices.Equal(sample, details[:limit]) || truncation == nil {
		t.Fatalf("the sample must be an unchanged prefix with truncation counts: %v, %+v", sample, truncation)
	}
	if truncation.Limit != limit || truncation.Total != len(details) || len(truncation.ByKind) != 4 ||
		truncation.ByKind["missing"] != limit || truncation.ByKind["observed_changed"] != 1 ||
		truncation.ByKind["evidence_unavailable"] != 1 || truncation.ByKind["baseline_unavailable"] != 1 {
		t.Fatalf("the breakdown must include all omitted kinds: %+v", truncation)
	}
}
