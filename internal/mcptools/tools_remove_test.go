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
	"github.com/aoci-spec/aoci-code/internal/ledger"
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

func TestVolumeOrphanRemoveRejectsStillValidRelationAndGuardDrift(t *testing.T) {
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
	if _, fail := planRemoveEntry(root, "code:main.go", true); fail == nil || fail.Msg != "remove_orphan_relation_still_valid" {
		t.Fatalf("still-valid R relation did not block removal: %+v", fail)
	}

	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n"+
		"live.go[CD9S]: F:retain the fixture | R:- | A:- | S:-\n")
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

// seedRemoveBaseline 为夹具仓建立基线(aoci.txt与live.go两键),
// API形态取自亲见样板(index_header.go: NewBaseline/HashFile/UpdateOne/Save(root,bl))。
func seedRemoveBaseline(t *testing.T, root string) {
	t.Helper()
	bl := baseline.NewBaseline(nil)
	for _, rel := range []string{"aoci.txt", "live.go"} {
		fp, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("夹具指纹计算失败(%s): %v", rel, err)
		}
		baseline.UpdateOne(bl, rel, fp)
	}
	if err := baseline.Save(root, bl); err != nil {
		t.Fatalf("夹具基线落盘失败: %v", err)
	}
}

// TestRemoveOrphanSuccess 孤儿条目删除成功: 落盘移除该行,其余保真。
func TestRemoveOrphanSuccess(t *testing.T) {
	root := buildRemoveRepo(t)
	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if fail != nil {
		t.Fatalf("孤儿删除应成功,实得: %+v", fail)
	}
	if !strings.Contains(out.RemovedLine, "ghost.go") {
		t.Errorf("回显被删行应含ghost.go,实得: %s", out.RemovedLine)
	}
	after := readIndex(t, root)
	if strings.Contains(after, "ghost.go") {
		t.Error("落盘后索引仍含ghost.go条目")
	}
	if !strings.Contains(after, "live.go") || !strings.Contains(after, "aoci.txt[") {
		t.Error("无关条目被误删")
	}
}

func TestRemoveReportsBaselineFailureAfterIndexWrite(t *testing.T) {
	root := buildRemoveRepo(t)
	previous := saveRemoveBaseline
	saveRemoveBaseline = func(string, *baseline.Baseline) error {
		return errors.New("injected baseline failure")
	}
	t.Cleanup(func() { saveRemoveBaseline = previous })

	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errInternal ||
		!strings.Contains(fail.Msg, "已删除") || !strings.Contains(fail.Msg, "Baseline失败") {
		t.Fatalf("索引写后Baseline失败必须返回真实停点: out=%+v fail=%+v", out, fail)
	}
	indexData, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil || strings.Contains(string(indexData), "ghost.go[") {
		t.Fatalf("停点必须保留已完成的索引删除: err=%v index=%s", err, indexData)
	}
	events, _ := ledger.Recent(root, 20)
	found := false
	for _, event := range events {
		if event.Op == "remove_entry" && event.Result == ledger.ResultError && event.AppliedCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("Baseline失败不得留下OK删除审计: %+v", events)
	}
	saveRemoveBaseline = previous
	recovered, retryFail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if retryFail != nil || recovered == nil {
		t.Fatalf("重复同一删除请求应从恢复意图补齐Baseline: out=%+v fail=%+v", recovered, retryFail)
	}
	state, exists, loadErr := baseline.Load(root)
	want, hashErr := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if loadErr != nil || hashErr != nil || !exists || state.Files["aoci.txt"] != want {
		t.Fatalf("恢复后Baseline必须绑定删除postimage: exists=%v load=%v hash=%v state=%+v want=%+v", exists, loadErr, hashErr, state, want)
	}
}

func TestRemoveRejectsCorruptBaselineBeforeIndexWrite(t *testing.T) {
	root := buildRemoveRepo(t)
	before := readIndex(t, root)
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if fail == nil || fail.Code != errInternal || !strings.Contains(fail.Msg, "尚未执行") {
		t.Fatalf("损坏Baseline必须在删除写前停止: %+v", fail)
	}
	if readIndex(t, root) != before {
		t.Fatal("Baseline预检失败不得删除索引条目")
	}
}

func TestRemoveDoesNotBaselineUnexpectedIndexPostimage(t *testing.T) {
	root := buildRemoveRepo(t)
	seedRemoveBaseline(t, root)
	previousWrite := writeRemoveIndex
	writeRemoveIndex = func(path string, data []byte, expected string) error {
		if err := previousWrite(path, data, expected); err != nil {
			return err
		}
		return os.WriteFile(path, append(data, []byte("#external\n")...), 0o644)
	}
	t.Cleanup(func() { writeRemoveIndex = previousWrite })
	_, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if fail == nil || fail.Code != errWriteConflict || !strings.Contains(fail.Msg, "postimage") {
		t.Fatalf("外部postimage不得被Baseline接受: %+v", fail)
	}
	state, exists, err := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if err != nil || hashErr != nil || !exists || state.Files["aoci.txt"] == current {
		t.Fatalf("外部索引变化必须保持Stale可见: exists=%v load=%v hash=%v state=%+v current=%+v", exists, err, hashErr, state, current)
	}
}

func TestRemoveCASConflictPreservesIntentWhenExternalVersionWins(t *testing.T) {
	root := buildRemoveRepo(t)
	previousWrite := writeRemoveIndex
	external := []byte("external replacement\n")
	writeRemoveIndex = func(path string, data []byte, expected string) error {
		if err := os.WriteFile(path, external, 0o644); err != nil {
			return err
		}
		return previousWrite(path, data, expected)
	}
	t.Cleanup(func() { writeRemoveIndex = previousWrite })

	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("替换边界CAS冲突必须零删除: out=%+v fail=%+v", out, fail)
	}
	if current, err := os.ReadFile(filepath.Join(root, "aoci.txt")); err != nil || string(current) != string(external) {
		t.Fatalf("外部索引必须保留: err=%v current=%q", err, current)
	}
	if _, err := os.Stat(removeRecoveryPath(root, "ghost.go")); err != nil {
		t.Fatalf("canonical已变成计划外版本时必须保留事务证据: %v", err)
	}
}

func TestRemoveOrphanCheckRejectsUnknownFilesystemState(t *testing.T) {
	root := buildRemoveRepo(t)
	previousLstat := lstatRemoveTarget
	lstatRemoveTarget = func(string) (os.FileInfo, error) { return nil, os.ErrPermission }
	t.Cleanup(func() { lstatRemoveTarget = previousLstat })
	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errInternal ||
		!strings.Contains(fail.Msg, "无法确认") {
		t.Fatalf("权限或I/O错误不得当作孤儿: out=%+v fail=%+v", out, fail)
	}
}

func TestRemoveOrphanCheckRejectsDanglingSymlink(t *testing.T) {
	root := buildRemoveRepo(t)
	if err := os.Symlink("missing-target", filepath.Join(root, "ghost.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errBadArgs {
		t.Fatalf("断链符号链接仍是存在的仓库路径，不得当作孤儿: out=%+v fail=%+v", out, fail)
	}
}

func TestRemoveRechecksOrphanInsideWriteLock(t *testing.T) {
	root := buildRemoveRepo(t)
	plan, fail := planRemoveEntry(root, "ghost.go", true)
	if fail != nil {
		t.Fatalf("初始孤儿计划失败: %+v", fail)
	}
	if err := os.WriteFile(filepath.Join(root, "ghost.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFail := commitRemove(root, ledger.SourceAgent, plan)
	if commitFail == nil || commitFail.Code != errBadArgs {
		t.Fatalf("计划后恢复的活文件必须在锁内阻断删除: %+v", commitFail)
	}
	if !strings.Contains(readIndex(t, root), "ghost.go[") {
		t.Fatal("锁内孤儿复核失败必须保持索引条目")
	}
}

func TestRemoveKeepsIntentWhenCASWritesThenReturnsError(t *testing.T) {
	root := buildRemoveRepo(t)
	previousWrite := writeRemoveIndex
	writes := 0
	writeRemoveIndex = func(path string, data []byte, expected string) error {
		writes++
		if err := previousWrite(path, data, expected); err != nil {
			return err
		}
		return errors.New("injected postimage cleanup failure")
	}
	t.Cleanup(func() { writeRemoveIndex = previousWrite })

	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errInternal ||
		!strings.Contains(fail.Msg, "postimage已写入") {
		t.Fatalf("CAS已写入后的错误必须形成可恢复停点: out=%+v fail=%+v", out, fail)
	}
	intentPath := removeRecoveryPath(root, "ghost.go")
	if _, err := os.Stat(intentPath); err != nil {
		t.Fatalf("postimage已写入时必须保留删除意图: %v", err)
	}

	writeRemoveIndex = previousWrite
	recovered, retryFail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if retryFail != nil || recovered == nil {
		t.Fatalf("重复请求应零删除补齐Baseline: out=%+v fail=%+v", recovered, retryFail)
	}
	if writes != 1 {
		t.Fatalf("恢复不得重复执行删除写入: writes=%d", writes)
	}
	if _, err := os.Stat(intentPath); !os.IsNotExist(err) {
		t.Fatalf("恢复完成后删除意图必须清理: %v", err)
	}
}

func TestRemoveCleanupFailureIsExplicitAndRetryable(t *testing.T) {
	root := buildRemoveRepo(t)
	previousRemove := removeRecoveryFile
	removeRecoveryFile = func(string) error {
		return errors.New("injected cleanup failure")
	}
	t.Cleanup(func() { removeRecoveryFile = previousRemove })

	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errInternal ||
		!strings.Contains(fail.Msg, "清理恢复意图失败") {
		t.Fatalf("恢复意图清理失败不得报告成功: out=%+v fail=%+v", out, fail)
	}
	if strings.Contains(readIndex(t, root), "ghost.go[") {
		t.Fatal("错误终态必须保留已完成的索引删除")
	}
	removeRecoveryFile = previousRemove
	recovered, retryFail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if retryFail != nil || recovered == nil {
		t.Fatalf("同一删除请求应完成残留意图清理: out=%+v fail=%+v", recovered, retryFail)
	}
	if _, err := os.Stat(removeRecoveryPath(root, "ghost.go")); !os.IsNotExist(err) {
		t.Fatalf("重试后恢复意图必须清理: %v", err)
	}
}

func TestCompletedRemoveIntentDoesNotBlockLaterIndexVersion(t *testing.T) {
	root := buildRemoveRepo(t)
	previousRemove := removeRecoveryFile
	removeRecoveryFile = func(string) error { return errors.New("injected cleanup failure") }
	t.Cleanup(func() { removeRecoveryFile = previousRemove })
	if _, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false); fail == nil {
		t.Fatal("夹具必须停在已完成但清理失败状态")
	}
	path := filepath.Join(root, "aoci.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("#later valid governance change\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	removeRecoveryFile = previousRemove
	_, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if fail == nil || fail.Code != errBadArgs || strings.Contains(fail.Msg, "偏离") {
		t.Fatalf("已完成的过期意图不得永久阻断后续索引版本: %+v", fail)
	}
	if _, err := os.Stat(removeRecoveryPath(root, "ghost.go")); !os.IsNotExist(err) {
		t.Fatalf("过期完成意图必须被清理: %v", err)
	}
}

func TestRemovePreviewNeverCleansCompletedRecoveryIntent(t *testing.T) {
	root := buildRemoveRepo(t)
	previousRemove := removeRecoveryFile
	removeRecoveryFile = func(string) error { return errors.New("injected cleanup failure") }
	if _, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false); fail == nil {
		t.Fatal("夹具必须停在删除完成但意图清理失败状态")
	}
	removeRecoveryFile = previousRemove
	t.Cleanup(func() { removeRecoveryFile = previousRemove })

	indexPath := filepath.Join(root, "aoci.txt")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	reappeared := string(data) + "ghost.go[XR9T]: F:重新出现 | R:- | A:- | S:-\n"
	if err := os.WriteFile(indexPath, []byte(reappeared), 0o644); err != nil {
		t.Fatal(err)
	}
	intentPath := removeRecoveryPath(root, "ghost.go")
	before, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	_, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, true)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("重新出现的条目必须保持冲突: %+v", fail)
	}
	after, err := os.ReadFile(intentPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("preview必须保持恢复意图字节不变: err=%v before=%q after=%q", err, before, after)
	}
}

func TestIncompleteRemoveIntentBlocksDeletionWhenEntryReappears(t *testing.T) {
	root := buildRemoveRepo(t)
	indexPath := filepath.Join(root, "aoci.txt")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	withoutGhost := strings.Replace(string(before), "ghost.go[XR9T]: F:孤儿 | R:- | A:- | S:-\n", "", 1)
	intent := removeRecovery{
		Version: 1, Rel: "ghost.go",
		RemovedLine:     "ghost.go[XR9T]: F:孤儿 | R:- | A:- | S:-",
		PreIndexSHA256:  indexTextHash(string(before)),
		PostIndexSHA256: indexTextHash(withoutGhost),
	}
	if err := saveRemoveRecovery(root, intent); err != nil {
		t.Fatal(err)
	}

	// 模拟首次删除后外部治理重新加入了同一路径的新语义条目。
	reappeared := strings.Replace(string(before), "F:孤儿", "F:外部恢复的新语义", 1)
	if err := os.WriteFile(indexPath, []byte(reappeared), 0o644); err != nil {
		t.Fatal(err)
	}
	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if out != nil || fail == nil || fail.Code != errWriteConflict ||
		!strings.Contains(fail.Msg, "重新出现") {
		t.Fatalf("未完成重试不得再次删除重现条目: out=%+v fail=%+v", out, fail)
	}
	if got := readIndex(t, root); got != reappeared {
		t.Fatal("冲突停点必须保留外部恢复的新条目")
	}
}

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

func TestIncompleteRemoveIntentResumesFromOriginalPreimage(t *testing.T) {
	root := buildRemoveRepo(t)
	indexPath := filepath.Join(root, "aoci.txt")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	withoutGhost := strings.Replace(
		string(before), "ghost.go[XR9T]: F:孤儿 | R:- | A:- | S:-\n", "", 1,
	)
	if err := saveRemoveRecovery(root, removeRecovery{
		Version: 1, Rel: "ghost.go",
		RemovedLine:     "ghost.go[XR9T]: F:孤儿 | R:- | A:- | S:-",
		PreIndexSHA256:  indexTextHash(string(before)),
		PostIndexSHA256: indexTextHash(withoutGhost),
	}); err != nil {
		t.Fatal(err)
	}

	out, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false)
	if fail != nil || out == nil {
		t.Fatalf("意图写入后、CAS前中断应继续同一删除事务: out=%+v fail=%+v", out, fail)
	}
	if got := readIndex(t, root); got != withoutGhost {
		t.Fatalf("恢复删除postimage不符:\n%s", got)
	}
	if _, err := os.Stat(removeRecoveryPath(root, "ghost.go")); !os.IsNotExist(err) {
		t.Fatalf("恢复完成后删除意图必须清理: %v", err)
	}
}

// TestRemoveAdvancesIndexBaseline 带基线仓删除后,索引自身指纹必须前移到删除后状态
// (D51防线;初版误传BaselinePath时本断言必红)。
func TestRemoveAdvancesIndexBaseline(t *testing.T) {
	root := buildRemoveRepo(t)
	seedRemoveBaseline(t, root)

	if _, fail := ApplyRemoveEntry(root, "ghost.go", "agent", true, false); fail != nil {
		t.Fatalf("孤儿删除应成功,实得: %+v", fail)
	}

	wantFp, err := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatalf("删除后指纹计算失败: %v", err)
	}
	bl, exists, err := baseline.Load(root)
	if err != nil || !exists || bl == nil {
		t.Fatalf("基线应存在且可读,实得 exists=%v err=%v", exists, err)
	}
	got, ok := bl.Files["aoci.txt"]
	if !ok {
		t.Fatal("基线中缺aoci.txt键")
	}
	if got.SHA256 != wantFp.SHA256 {
		t.Errorf("索引自身指纹未前移: 基线=%s 磁盘=%s(每次删除将自造假Stale)", got.SHA256, wantFp.SHA256)
	}
}

// TestRemoveLiveRejectedWhenOrphanOnly orphanOnly=true 时活文件条目拒且索引零改动。
func TestRemoveLiveRejectedWhenOrphanOnly(t *testing.T) {
	root := buildRemoveRepo(t)
	before := readIndex(t, root)
	_, fail := ApplyRemoveEntry(root, "live.go", "agent", true, false)
	if fail == nil {
		t.Fatal("活文件在orphanOnly下应被拒")
	}
	if !strings.Contains(fail.Msg, "仍存在于磁盘") || !strings.Contains(fail.Hint, "aoci_report") {
		t.Errorf("拒绝文案应含护栏说明与report引导,实得: %+v", fail)
	}
	if readIndex(t, root) != before {
		t.Error("被拒后索引发生改动(零改动铁律破坏)")
	}
}

// TestRemoveLiveAllowedForHuman orphanOnly=false(CLI人工全权)删活文件成功。
func TestRemoveLiveAllowedForHuman(t *testing.T) {
	root := buildRemoveRepo(t)
	_, fail := ApplyRemoveEntry(root, "live.go", "human", false, false)
	if fail != nil {
		t.Fatalf("人工全权删除活文件应成功,实得: %+v", fail)
	}
	if strings.Contains(readIndex(t, root), "live.go[") {
		t.Error("live.go条目应已删除")
	}
}

// TestRemoveUnknownRejected 未收录条目拒。
func TestRemoveUnknownRejected(t *testing.T) {
	root := buildRemoveRepo(t)
	_, fail := ApplyRemoveEntry(root, "nope.go", "agent", true, false)
	if fail == nil {
		t.Fatal("未收录条目应被拒")
	}
	if !strings.Contains(fail.Msg, "不存在") {
		t.Errorf("拒绝文案应说明条目不存在,实得: %+v", fail)
	}
}

// TestRemoveDryRunNoWrite dryRun 走完plan返回结果但索引零改动。
func TestRemoveDryRunNoWrite(t *testing.T) {
	root := buildRemoveRepo(t)
	before := readIndex(t, root)
	out, fail := ApplyRemoveEntry(root, "ghost.go", "human", false, true)
	if fail != nil {
		t.Fatalf("dryRun应成功返回,实得: %+v", fail)
	}
	if !out.DryRun {
		t.Error("DryRun标记应为true")
	}
	if readIndex(t, root) != before {
		t.Error("dryRun落盘了(零副作用铁律破坏)")
	}
}

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
