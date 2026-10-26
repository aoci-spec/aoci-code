// ApplyRemoveEntry 管线与 BuildHeaderText 直测(v2.8 P1/P2)。
// 索引条目: tools_remove_test.go(待补录,随本批入册)
//
// 自建夹具 buildRemoveRepo(t.TempDir)不依赖 tools_test.go 既有夹具符号
// (该文件本批未查看,防 R20 凭记忆引用);覆盖:孤儿删除成功+落盘+账目/
// 活文件 orphanOnly 拒且索引零改动/orphanOnly=false 删活文件成功/
// 未收录条目拒/dryRun 不落盘/头部工具返回头部且不含条目区/
// 带基线仓删除后索引自身指纹必前移(初版曾误传 BaselinePath 给 Load 致
// 前移恒静默跳过而测试全绿 —— 夹具无基线前移分支零覆盖的教训,本用例即防线)。
package mcptools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

func TestVolumeCodeOrphanRemoveIsExplicitGuardedAndResumable(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n")
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	rootBefore := volumeFileText(t, root, "aoci.txt")
	metaBefore := volumeFileText(t, root, "aoci.meta.txt")
	plan, fail := planRemoveEntry(root, "code:main.go", true)
	if fail != nil || !plan.volumeMode || plan.volumeID != cognition.ScopeCode {
		t.Fatalf("Volume orphan plan failed: plan=%#v fail=%+v", plan, fail)
	}
	previousBaselineWrite := writeRemoveBaselineCAS
	writes := 0
	writeRemoveBaselineCAS = func(path string, data []byte, expected string) error {
		writes++
		if writes == 1 {
			return errors.New("test-only Baseline interruption")
		}
		return previousBaselineWrite(path, data, expected)
	}
	t.Cleanup(func() { writeRemoveBaselineCAS = previousBaselineWrite })
	if _, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false); fail == nil {
		t.Fatal("test-only Baseline interruption did not stop explicit remove")
	}
	if strings.Contains(volumeFileText(t, root, "aoci.code.txt"), "main.go[") == false {
		// Expected: the formal Volume postimage is durable and the same request
		// must resume only the Baseline/ledger tail.
	} else {
		t.Fatal("Code Volume did not reach its proven postimage before Baseline interruption")
	}
	writeRemoveBaselineCAS = previousBaselineWrite
	outcome, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false)
	if fail != nil || outcome == nil || !strings.Contains(outcome.RemovedLine, "main.go[") {
		t.Fatalf("same explicit remove did not resume: outcome=%#v fail=%+v", outcome, fail)
	}
	if volumeFileText(t, root, "aoci.txt") != rootBefore || volumeFileText(t, root, "aoci.meta.txt") != metaBefore {
		t.Fatal("explicit orphan remove changed Root or Meta")
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || len(facts.CodeDrift.Orphan) != 0 {
		t.Fatalf("orphan remove did not close governance: facts=%#v err=%v", facts, err)
	}
}

func TestVolumeOwnershipRepairRemovesOnlyRootEntryFromCodeVolume(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD9S]: F:run the fixture | R:aoci.txt | A:main | S:Keep execution deterministic\n"+
		"aoci.txt[CD9S]: F:describe the repository cognition root | R:- | A:- | S:Root ownership remains exclusive\n")
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	rootBefore := volumeFileText(t, root, "aoci.txt")
	sourceBefore := volumeFileText(t, root, "main.go")
	outcome, fail := ApplyRemoveEntry(root, "code:aoci.txt", "agent", true, false)
	if fail != nil || outcome == nil || !outcome.OwnershipRepair || outcome.PreservedOwner != cognition.OwnerRoot ||
		!strings.Contains(outcome.RemovedLine, "aoci.txt[") {
		t.Fatalf("ownership orphan repair failed: outcome=%#v fail=%+v", outcome, fail)
	}
	if volumeFileText(t, root, "aoci.txt") != rootBefore {
		t.Fatal("ownership repair modified the Root file")
	}
	if volumeFileText(t, root, "main.go") != sourceBefore {
		t.Fatal("ownership repair modified source")
	}
	if strings.Contains(volumeFileText(t, root, "aoci.code.txt"), "aoci.txt[") {
		t.Fatal("ownership repair retained the conflicting Code Entry")
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || facts.CodeSourceCount != 1 || facts.CodeEntryCount != 1 {
		t.Fatalf("ownership repair did not align governance: facts=%#v err=%v", facts, err)
	}
}

// 孤儿删除只证明"源文件确实没了", 不看别处的 R 是否还指着它 —— 留下悬空标注
// 是模型的语义领域。真正必须挡住的是 Guard 漂移: 写之前卷变了就零写停下。
// 孤儿删除只证明"源文件确实没了", 不看别处的 R 是否还指着它 —— 留下悬空标注
// 是模型的语义领域。真正必须挡住的是 Guard 漂移: 写之前卷变了就零写停下。
// 孤儿删除只证明"源文件确实没了", 不看别处的 R 是否还指着它 —— 留下悬空标注
// 是模型的语义领域。真正必须挡住的是 Guard 漂移: 写之前卷变了就零写停下。
// 孤儿删除只证明"源文件确实没了", 不看别处的 R 是否还指着它 —— 留下悬空标注
// 是模型的语义领域。真正必须挡住的是 Guard 漂移: 写之前卷变了就零写停下。
func TestVolumeOrphanRemoveIgnoresIncomingRelationButBlocksGuardDrift(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeVolumeTestFile(t, root, "live.go", "package main\n")
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n"+
		"live.go[CD9S]: F:retain the fixture relation | R:code:main.go | A:- | S:-\n")
	cfg, _ := config.LoadReadOnly(root)
	snapshot, _, _ := baseline.Snapshot(root, cfg.WalkOptions())
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	if _, fail := planRemoveEntry(root, "code:main.go", true); fail != nil {
		t.Fatalf("指向该对象的 R 不应阻止孤儿删除: %+v", fail)
	}

	snapshot, _, _ = baseline.Snapshot(root, cfg.WalkOptions())
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	plan, fail := planRemoveEntry(root, "code:main.go", true)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	writeVolumeTestFile(t, root, "aoci.meta.txt", volumeFileText(t, root, "aoci.meta.txt")+"# third-party\n")
	if fail := commitVolumeRemove(root, "agent", plan); fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("Meta guard drift did not block before write: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != codeBefore {
		t.Fatal("guard drift changed the Code Volume")
	}
}

// buildRemoveRepo 造最小仓: 索引含 live.go 与 ghost.go 两条目,
// 磁盘只有 live.go(ghost.go 为孤儿),基线经 scan 语义外的直接缺省(nil 可容忍)。
// buildRemoveRepo 造最小仓: 索引含 live.go 与 ghost.go 两条目,
// 磁盘只有 live.go(ghost.go 为孤儿),基线经 scan 语义外的直接缺省(nil 可容忍)。
// buildRemoveRepo 造最小仓: 索引含 live.go 与 ghost.go 两条目,
// 磁盘只有 live.go(ghost.go 为孤儿),基线经 scan 语义外的直接缺省(nil 可容忍)。
// buildRemoveRepo 造最小仓: 索引含 live.go 与 ghost.go 两条目,
// 磁盘只有 live.go(ghost.go 为孤儿),基线经 scan 语义外的直接缺省(nil 可容忍)。
func buildRemoveRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatalf("建.aoci失败: %v", err)
	}
	idx := "#====测试====\n" +
		"===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[XR9T]: F:索引 | R:- | A:- | S:-\n" +
		"live.go[XR9T]: F:活文件 | R:- | A:- | S:-\n" +
		"ghost.go[XR9T]: F:孤儿 | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(idx), 0o644); err != nil {
		t.Fatalf("写索引失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "live.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("写live.go失败: %v", err)
	}
	return root
}

func readIndex(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatalf("读索引失败: %v", err)
	}
	return string(b)
}

// TestRemoveOrphanSuccess 孤儿条目删除成功: 落盘移除该行,其余保真。
// TestRemoveOrphanSuccess 孤儿条目删除成功: 落盘移除该行,其余保真。
func TestRemoveRecoveryRejectsMissingPreimage(t *testing.T) {
	root := buildRemoveRepo(t)
	current := readIndex(t, root)
	if err := saveRemoveRecovery(root, removeRecovery{
		Version: 1, Rel: "ghost.go",
		RemovedLine:     "ghost.go[XR9T]: F:孤儿 | R:- | A:- | S:-",
		PostIndexSHA256: indexTextHash(current),
	}); err != nil {
		t.Fatal(err)
	}

	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errInternal ||
		!strings.Contains(fail.Msg, "删除意图失败") {
		t.Fatalf("缺少preimage的收据不得授权删除或恢复: out=%+v fail=%+v", out, fail)
	}
	if got := readIndex(t, root); got != current {
		t.Fatal("无效恢复收据不得改变正式索引")
	}
}

// TestHeaderShowReturnsHeaderOnly 头部工具返回头部且不含条目区(source入参形态)。
// TestHeaderShowReturnsHeaderOnly 头部工具返回头部且不含条目区(source入参形态)。
func TestHeaderShowReturnsHeaderOnly(t *testing.T) {
	root := buildRemoveRepo(t)
	header, fail := BuildHeaderText(root, "agent")
	if fail != nil {
		t.Fatalf("头部读取应成功,实得: %+v", fail)
	}
	if !strings.Contains(header, "#====测试====") {
		t.Errorf("头部应含#行,实得: %q", header)
	}
	if strings.Contains(header, "live.go[") || strings.Contains(header, "===代码索引") {
		t.Error("头部输出混入条目区内容(边界破坏)")
	}
}
