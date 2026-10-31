package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// The first scope block must name the paths that triggered it and give an actionable next step; a lone machine-code line
// cannot be located, and no other command can reproduce this block before init.
func TestInitialScopeBlockedDetailNamesTrackedSafetyExclusions(t *testing.T) {
	// The counter that selects this arm is AutoBlockerCount: the paths worth
	// naming are the ones whose content an explicit opt-in will read, not every
	// path a rule excluded.
	evaluation := &managedscope.Evaluation{
		RequiredHumanReview: 2,
		SafeInventory:       fs.SafeInventorySummary{AutoBlockerCount: 2, RequiredHumanReview: 2},
		Exclude: []managedscope.PathEvaluation{
			{Path: "vendor/lib.js", GitStatus: "ignored", SafetyStatus: fs.SafetyIgnored},
			{Path: "data/audit_dump.log", GitStatus: "tracked", SafetyStatus: "runtime"},
			{Path: ".env.production", GitStatus: "tracked", SafetyStatus: fs.SafetySensitive},
		},
	}
	detail := initialScopeBlockedDetail(evaluation, "production", 0)
	for _, want := range []string{"managed_scope_auto_authorization_blocked", "data/audit_dump.log", ".env.production"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("诊断缺少 %q: %s", want, detail)
		}
	}
	if strings.Contains(detail, "vendor/lib.js") {
		t.Fatalf("仅被 gitignore 排除的路径不该被点名: %s", detail)
	}
}

// The number of named paths is capped, and the rest ends with a count, so a whole directory tree is not flushed to the terminal.
func TestInitialScopeBlockedDetailCapsNamedPaths(t *testing.T) {
	excluded := make([]managedscope.PathEvaluation, 0, 9)
	for _, path := range []string{"a.log", "b.log", "c.log", "d.log", "e.log", "f.log", "g.log", "h.log", "i.log"} {
		excluded = append(excluded, managedscope.PathEvaluation{Path: path, GitStatus: "tracked", SafetyStatus: "runtime"})
	}
	detail := initialScopeBlockedDetail(&managedscope.Evaluation{RequiredHumanReview: 9,
		SafeInventory: fs.SafeInventorySummary{AutoBlockerCount: 9, RequiredHumanReview: 9},
		Exclude:       excluded}, "production", 0)
	if strings.Contains(detail, "f.log") {
		t.Fatalf("超过上限的路径不应逐条列出: %s", detail)
	}
	if !strings.Contains(detail, "(+4)") {
		t.Fatalf("诊断应报告未列出的剩余数量: %s", detail)
	}
}

// The other two kinds of trigger each give their own way out, and must not all degrade into the same sentence.
func TestInitialScopeBlockedDetailSeparatesOtherCauses(t *testing.T) {
	highRisk := initialScopeBlockedDetail(&managedscope.Evaluation{}, "production", 3)
	if !strings.Contains(highRisk, "3") || strings.Contains(highRisk, "index role") {
		t.Fatalf("高风险接纳应有独立诊断: %s", highRisk)
	}
	noIndex := initialScopeBlockedDetail(&managedscope.Evaluation{
		SafeInventory: fs.SafeInventorySummary{FinalManagedCandidates: 7}}, "custom", 0)
	if !strings.Contains(noIndex, "custom") || !strings.Contains(noIndex, "7") {
		t.Fatalf("无 index 角色应点名配置与候选数量: %s", noIndex)
	}
}
