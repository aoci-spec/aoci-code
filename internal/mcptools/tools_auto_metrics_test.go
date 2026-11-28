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
	if _, exists := result["transport_budget_exceeded"]; exists {
		t.Fatalf("a reply that fits within budget after emptying findings must omit transport_budget_exceeded: %v", result["transport_budget_exceeded"])
	}
}

func TestRenderBoundedApplyResultMarksTransportBudgetExceededWhenFixedFieldsOverrun(t *testing.T) {
	longPaths := make([]string, 16)
	for i := range longPaths {
		longPaths[i] = "docs/" + strings.Repeat("a", 200) + "/note.md"
	}
	review := &batchObserveReview{
		Status:            "acknowledged",
		ReviewDigest:      strings.Repeat("a", 64),
		ReviewedBy:        "reviewer",
		Acknowledged:      len(longPaths),
		AcknowledgedPaths: longPaths,
		TransactionID:     "tx-observe-1",
	}

	t.Run("unaligned_with_findings_preserves_stats_and_committed_facts", func(t *testing.T) {
		base := autoResult{
			Version:             1,
			Status:              autoStatusApplied,
			Aligned:             false,
			Attempted:           1,
			Applied:             1,
			Remaining:           4,
			FormalWritesStarted: true,
			NextAction:          "call_maintain_next",
			ObserveReview:       review,
		}
		details := []string{
			"missing: pkg/a.go",
			"missing: pkg/b.go",
			"missing: pkg/c.go",
			"stale: pkg/d.go",
		}
		const smallBudget = 2048
		raw := renderBoundedApplyResult(base, details, smallBudget)
		if len(raw) <= smallBudget {
			t.Fatalf("fixture must exceed budget even with empty sample: len=%d budget=%d", len(raw), smallBudget)
		}
		got := decodeMaintainJSON(t, raw)
		if got["transport_budget_exceeded"] != true {
			t.Fatalf("over-budget reply with empty sample must set transport_budget_exceeded=true: %v", got["transport_budget_exceeded"])
		}
		findings, isList := got["findings"].([]any)
		if !isList || len(findings) != 0 || got["finding_count"] != float64(4) {
			t.Fatalf("findings sample and total count mismatch: findings=%v count=%v", got["findings"], got["finding_count"])
		}
		meta, _ := got["findings_truncated"].(map[string]any)
		kinds, _ := meta["by_kind"].(map[string]any)
		if meta["limit"] != float64(machinecontract.MaintainTransportListLimit) || meta["byte_limit"] != float64(smallBudget) ||
			meta["total"] != float64(4) || kinds["missing"] != float64(3) || kinds["stale"] != float64(1) {
			t.Fatalf("findings_truncated must keep complete counts: %+v", meta)
		}
		if got["attempted"] != float64(1) || got["applied"] != float64(1) || got["remaining"] != float64(4) ||
			got["aligned"] != false || got["formal_writes_started"] != true || got["next_action"] != "call_maintain_next" {
			t.Fatalf("committed facts and control fields must stay unchanged: %v", got)
		}
		gotReview, _ := got["observe_review"].(map[string]any)
		gotPaths, _ := gotReview["acknowledged_paths"].([]any)
		if len(gotPaths) != len(longPaths) || gotReview["acknowledged"] != float64(len(longPaths)) {
			t.Fatalf("confirmed observe_review facts must not be truncated: len=%d", len(gotPaths))
		}
	})

	t.Run("aligned_empty_findings_preserves_terminal_proof", func(t *testing.T) {
		base := autoResult{
			Version:             1,
			Status:              autoStatusApplied,
			Aligned:             true,
			Attempted:           1,
			Applied:             1,
			Remaining:           0,
			FormalWritesStarted: true,
			NextAction:          "done_aligned",
			TerminalProof:       &TerminalProof{Aligned: true, StructureValid: true, CheckPassed: true},
			ObserveReview:       review,
		}
		const smallBudget = 2048
		raw := renderBoundedApplyResult(base, nil, smallBudget)
		if len(raw) <= smallBudget {
			t.Fatalf("aligned fixture must exceed budget: len=%d budget=%d", len(raw), smallBudget)
		}
		got := decodeMaintainJSON(t, raw)
		if got["transport_budget_exceeded"] != true {
			t.Fatalf("over-budget aligned reply must set transport_budget_exceeded=true: %v", got["transport_budget_exceeded"])
		}
		if _, hasTruncated := got["findings_truncated"]; hasTruncated {
			t.Fatalf("empty findings must not emit findings_truncated: %v", got["findings_truncated"])
		}
		proof, _ := got["terminal_proof"].(map[string]any)
		if got["aligned"] != true || got["remaining"] != float64(0) || got["next_action"] != "done_aligned" ||
			proof == nil || proof["aligned"] != true {
			t.Fatalf("aligned terminal_proof and control fields must stay intact: %v", got)
		}
		gotReview, _ := got["observe_review"].(map[string]any)
		gotPaths, _ := gotReview["acknowledged_paths"].([]any)
		if len(gotPaths) != len(longPaths) {
			t.Fatalf("confirmed observe_review paths must stay complete: len=%d", len(gotPaths))
		}
	})

	t.Run("within_budget_omits_marker", func(t *testing.T) {
		base := autoResult{
			Version:    1,
			Status:     autoStatusApplied,
			Aligned:    false,
			Attempted:  1,
			Applied:    1,
			Remaining:  1,
			NextAction: "call_maintain_next",
		}
		raw := renderBoundedApplyResult(base, []string{"missing: main.go"}, machinecontract.MaintainTransportBudgetBytesDefault)
		got := decodeMaintainJSON(t, raw)
		if _, exists := got["transport_budget_exceeded"]; exists {
			t.Fatalf("within-budget reply must omit transport_budget_exceeded: %v", got["transport_budget_exceeded"])
		}
	})
}
