// 消失的 observe 源必须被报为移除, 哪怕 Git 还跟踪着它。
//
// 真实经历: 删掉一个被跟踪的 observe 测试文件后, scope acknowledge 永远无法成功。
// scope status 按 snapshot 判定, 如实报 observed_removed; Build 按 roles 判定, 而
// 一个"已删除但仍被跟踪"的文件仍会被 Safe Inventory 评估为 unsafe_filesystem_object
// 的 Exclude 项 —— 它留在 roles 里, 于是读起来像"被策略重新分类", 不像"消失"。
// 两份清单因此永远对不上, acknowledge 以 observed_evidence_review_required 拒绝,
// 而错误既不说多了什么也不说少了什么。操作者手里没有能清除它的动作。
//
// roles 判据本身是对的: 它把"被策略重新分类"(仍在 roles, 由计划复核)与"消失"
// (不在 roles)分开。缺的只是让消失路径不再伪装成前者。
package scopechange

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// trackAndVanish 让夹具进入"被 Git 跟踪、Baseline 记为 observe、但工作区已删除"
// 的状态 —— 这三条同时成立才是那个盲点。
func trackAndVanish(t *testing.T, root, rel string) {
	t.Helper()
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	fingerprint, recorded := state.Files[rel]
	if !recorded {
		t.Fatalf("fixture precondition: %s must be in the Baseline", rel)
	}
	fingerprint.Role = machinecontract.ScopeRoleObserve
	state.Files[rel] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", root, "add", "-A")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

func vanishedFixtureFacts(t *testing.T, root string) (*managedscope.Evaluation, map[string]baseline.Fingerprint, map[string]string, *baseline.Baseline, *config.Config) {
	t.Helper()
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	active, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
		WalkOptions: cfg.WalkOptions(), CurationExclude: cfg.CurationExclude})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	return evaluation, snapshot, evaluationRoles(evaluation), active, cfg
}

func TestVanishedButTrackedObserveSourceIsReportedRemoved(t *testing.T) {
	root, _ := buildChangeFixture(t)
	trackAndVanish(t, root, "main_test.go")
	evaluation, snapshot, roles, active, cfg := vanishedFixtureFacts(t, root)

	// 前提: 它确实还在评估里, 而不是干脆消失 —— 否则本用例测不到那个盲点。
	if _, governed := roles["main_test.go"]; !governed {
		t.Fatal("fixture precondition: a tracked-but-deleted path must still be evaluated, " +
			"otherwise the blind spot this pins cannot occur")
	}
	vanished := vanishedPaths(evaluation)
	if !vanished["main_test.go"] {
		t.Fatalf("a governed path the inventory cannot find must be named vanished: %v", vanished)
	}
	changes := observedEvidenceChanges(active, snapshot, roles, vanished, true, cfg.LineEndingTolerance)
	found := false
	for _, path := range changes {
		if path == "main_test.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a vanished observe source must be reported removed, got %v", changes)
	}
}

// 真正要钉的性质不是这一个症状, 而是两套计算不得分歧: acknowledge 用 status 的
// 清单提交复核, Build 用自己的清单校验, 只要两者不同就永远匹配不上。
func TestBuildAndStatusAgreeOnObserveRemovals(t *testing.T) {
	root, _ := buildChangeFixture(t)
	trackAndVanish(t, root, "main_test.go")
	evaluation, snapshot, roles, active, cfg := vanishedFixtureFacts(t, root)

	raw, err := os.ReadFile(filepath.Join(root, cfg.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	document, _ := index.Parse(string(raw))
	index.ResolveRelPaths(document, root)
	drift := baseline.DetectManagedScope(root, document, active, snapshot,
		afs.WalkOptions{HighRiskOptIn: cfg.SafeInventoryHighRiskOptIn}, cfg.LineEndingTolerance)
	if len(drift.ObservedRemoved) == 0 {
		t.Fatal("fixture precondition: status must see a removal for this comparison to mean anything")
	}

	changes := map[string]bool{}
	for _, path := range observedEvidenceChanges(active, snapshot, roles, vanishedPaths(evaluation), true, cfg.LineEndingTolerance) {
		changes[path] = true
	}
	for _, path := range drift.ObservedRemoved {
		if !changes[path] {
			t.Fatalf("status reports %q removed but the Scope Change plan does not; the review set "+
				"submitted by scope acknowledge can then never match and the repository is stuck",
				path)
		}
	}
}
