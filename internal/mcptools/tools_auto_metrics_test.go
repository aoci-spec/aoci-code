// Formal metering reports for the Auto no-change, small-change, and retry paths.
package mcptools

import (
	"strings"
	"testing"
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
