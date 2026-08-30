package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// 首次范围阻塞必须点名触发它的路径,并给出可执行的下一步 —— 单独一行机器码
// 无法定位,而这个阻塞在 init 之前没有别的命令能复现。
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

// 点名数量有上限,超出部分以计数收尾,避免把整棵目录树刷到终端。
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

// 另外两类触发各自给出自己的出路,不能都退化成同一句话。
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
