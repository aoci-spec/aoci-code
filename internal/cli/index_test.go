// header apply 闸门链路测试: 结构硬拒两规(索引零改动) + 成功链路五断言
// (头部替换/条目区保真/备份产生/基线前移/ledger落账)
// + loadIndexForCLI 的 RelPath 填充防线(entries diff 假新增缺陷防再犯,2026-07-10)
// 索引条目: index_test.go(待补录)
//
// 测试方式: 直接调 newHeaderApplyCmd().RunE,经 flagRepo 全局覆盖定根
// (root.go 的 resolveRepoRoot 以 --repo 覆盖为最高优先,仅要求目录存在);
// 共享全局 flagRepo 故本文件用例不并行。
// safety 禁区词命中用例暂缺(词表内容不在本测试认知内,不臆造样本)—— 挂账。
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

// buildApplyRepo 造最小真实仓库: 索引(两行头部+一段一条目) + 指定内容的 header 草稿
func buildApplyRepo(t *testing.T, draftText string) (root, runID string) {
	t.Helper()
	root = t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	idx := "#旧头部第一行\n#旧头部第二行\n===段" + rootSlash + "/===\nf.go[XC5T]: F:x | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(idx), 0644); err != nil {
		t.Fatal(err)
	}
	runID = "20260709T120000Z"
	if err := draft.WriteFile(root, runID, draft.HeaderFileName, []byte(draftText)); err != nil {
		t.Fatal(err)
	}
	return root, runID
}

// runApply 以 flagRepo 覆盖定根执行 apply,返回 RunE 原始错误
func runApply(t *testing.T, root, runID string) error {
	t.Helper()
	old := flagRepo
	flagRepo = root
	t.Cleanup(func() { flagRepo = old })

	cmd := newHeaderApplyCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd.RunE(cmd, []string{runID})
}

// readIndex 读回索引全文
func readIndex(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHeaderApplySuccessChain(t *testing.T) {
	root, runID := buildApplyRepo(t, "#新头部甲\n#新头部乙\n")
	if err := runApply(t, root, runID); err != nil {
		t.Fatalf("合规草稿 apply 失败: %v", err)
	}

	// 断言一: 头部被整体替换
	got := readIndex(t, root)
	if !strings.HasPrefix(got, "#新头部甲\n#新头部乙\n===段") {
		t.Fatalf("头部未按草稿替换: %q", got)
	}
	// 断言二: 条目区字节级保真
	if !strings.HasSuffix(got, "f.go[XC5T]: F:x | R:- | A:- | S:-\n") {
		t.Fatalf("条目区被改动: %q", got)
	}
	if strings.Contains(got, "旧头部") {
		t.Fatal("旧头部残留")
	}
	// 断言三: 时间戳备份产生(BackupThenWrite 语义)
	backups, err := filepath.Glob(filepath.Join(root, "aoci.txt.backup.*"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("apply 应产生 aoci.txt.backup.时间戳 备份: %v %v", backups, err)
	}
	// 断言四: 基线前移(baseline.json 存在且含索引路径键)
	blData, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatalf("apply 后应有基线文件(IndexPath 自身单文件前移): %v", err)
	}
	if !strings.Contains(string(blData), "aoci.txt") {
		t.Fatalf("基线应含 aoci.txt 指纹: %s", blData)
	}
	// 断言五: ledger 落 header_apply 账且关联 draft_run_id
	evs, _ := ledger.Recent(root, 10)
	var found bool
	for _, ev := range evs {
		if ev.Op == "header_apply" && ev.DraftRunID == runID && ev.Source == ledger.SourceHuman {
			found = true
		}
	}
	if !found {
		t.Fatalf("ledger 未见 header_apply(draft_run_id=%s)落账: %+v", runID, evs)
	}
}

func TestHeaderApplyCompletedRetryIsZeroWrite(t *testing.T) {
	root, runID := buildApplyRepo(t, "#新头部甲\n#新头部乙\n")
	if err := draft.SaveManifest(root, &draft.Manifest{
		RunID: runID, Kind: draft.KindHeader,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runApply(t, root, runID); err != nil {
		t.Fatal(err)
	}
	backupsBefore, err := filepath.Glob(filepath.Join(root, "aoci.txt.backup.*"))
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, _ := ledger.Recent(root, 100)
	if err := runApply(t, root, runID); err != nil {
		t.Fatalf("已完成Header重复调用应幂等成功: %v", err)
	}
	backupsAfter, _ := filepath.Glob(filepath.Join(root, "aoci.txt.backup.*"))
	eventsAfter, _ := ledger.Recent(root, 100)
	if len(backupsAfter) != len(backupsBefore) || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("重复调用不得创建备份或审计: backups=%d/%d events=%d/%d",
			len(backupsBefore), len(backupsAfter), len(eventsBefore), len(eventsAfter))
	}
}

func TestHeaderApplyIdenticalContentStillCompletesGovernance(t *testing.T) {
	root, runID := buildApplyRepo(t, "#旧头部第一行\n#旧头部第二行\n")
	if err := draft.SaveManifest(root, &draft.Manifest{
		RunID: runID, Kind: draft.KindHeader,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runApply(t, root, runID); err != nil {
		t.Fatalf("相同Header仍应完成Baseline和Application: %v", err)
	}
	manifest, err := draft.LoadManifest(root, runID)
	if err != nil || manifest.AppliedAt == "" {
		t.Fatalf("相同内容不得留下未应用Run: err=%v manifest=%+v", err, manifest)
	}
	state, exists, loadErr := baseline.Load(root)
	want, hashErr := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if loadErr != nil || hashErr != nil || !exists || state.Files["aoci.txt"] != want {
		t.Fatalf("相同内容也必须前移索引Baseline: exists=%v load=%v hash=%v state=%+v want=%+v",
			exists, loadErr, hashErr, state, want)
	}
	backups, _ := filepath.Glob(filepath.Join(root, "aoci.txt.backup.*"))
	if len(backups) != 0 {
		t.Fatalf("相同内容治理收尾不得制造无意义备份: %v", backups)
	}
}

func TestHeaderApplyRejectsCorruptBaselineBeforeIndexWrite(t *testing.T) {
	root, runID := buildApplyRepo(t, "#新头部甲\n#新头部乙\n")
	before := readIndex(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runApply(t, root, runID)
	var exitErr *ExitError
	if err == nil || !errors.As(err, &exitErr) || exitErr.Code != ExitInternal {
		t.Fatalf("损坏Baseline必须在Header写前停止: %v", err)
	}
	if readIndex(t, root) != before {
		t.Fatal("Baseline预检失败不得修改正式索引")
	}
}

func TestHeaderApplyBaselineFailureIsRecoverableError(t *testing.T) {
	root, runID := buildApplyRepo(t, "#新头部甲\n#新头部乙\n")
	previousSave := saveHeaderBaseline
	saveHeaderBaseline = func(string, *baseline.Baseline) error {
		return errors.New("injected baseline failure")
	}
	t.Cleanup(func() { saveHeaderBaseline = previousSave })

	err := runApply(t, root, runID)
	var exitErr *ExitError
	if err == nil || !errors.As(err, &exitErr) || exitErr.Code != ExitInternal ||
		!strings.Contains(err.Error(), "header已写入") {
		t.Fatalf("写后Baseline失败必须返回可恢复非零终态: %v", err)
	}
	if !strings.HasPrefix(readIndex(t, root), "#新头部甲\n#新头部乙\n") {
		t.Fatal("错误终态必须如实保留已经完成的Header写入")
	}
	events, _ := ledger.Recent(root, 20)
	for _, event := range events {
		if event.Op == "header_apply" && event.Result == ledger.ResultOK {
			t.Fatalf("Baseline失败不得留下Header成功审计: %+v", events)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	batch, batchFail := mcptools.ApplyUpdateEntriesAtomic(root, []mcptools.AtomicUpdateItem{{
		Path: "f.go", NewEntry: "f.go[XC5T]: F:新职责 | R:- | A:- | S:-",
		SourceSHA256: fingerprint.SHA256,
	}}, ledger.SourceAgent, false)
	if batch != nil || batchFail == nil || batchFail.Code != "write_conflict" {
		t.Fatalf("未完成Header事务必须阻止Entries越过恢复postimage: outcome=%+v fail=%+v", batch, batchFail)
	}
	saveHeaderBaseline = previousSave
	if retryErr := runApply(t, root, runID); retryErr != nil {
		t.Fatalf("同一Header run应先完成恢复: %v", retryErr)
	}
	batch, batchFail = mcptools.ApplyUpdateEntriesAtomic(root, []mcptools.AtomicUpdateItem{{
		Path: "f.go", NewEntry: "f.go[XC5T]: F:新职责 | R:- | A:- | S:-",
		SourceSHA256: fingerprint.SHA256,
	}}, ledger.SourceAgent, false)
	if batchFail != nil || batch == nil || !batch.BaselineComplete {
		t.Fatalf("Header恢复完成后Entries应正常治理: outcome=%+v fail=%+v", batch, batchFail)
	}
}

func TestHeaderApplyKeepsRecoveryWhenCASWritesThenReturnsError(t *testing.T) {
	root, runID := buildApplyRepo(t, "#新头部甲\n#新头部乙\n")
	previousWrite := writeHeaderIndex
	writes := 0
	writeHeaderIndex = func(path string, data []byte, expected string) error {
		writes++
		if err := previousWrite(path, data, expected); err != nil {
			return err
		}
		return errors.New("injected postimage cleanup failure")
	}
	t.Cleanup(func() { writeHeaderIndex = previousWrite })

	firstErr := runApply(t, root, runID)
	if firstErr == nil || !strings.Contains(firstErr.Error(), "header已写入") {
		t.Fatalf("CAS已写入后的错误必须形成可恢复停点: %v", firstErr)
	}
	intentPath, err := headerRecoveryPath(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(intentPath); err != nil {
		t.Fatalf("postimage已写入时必须保留Header恢复意图: %v", err)
	}

	writeHeaderIndex = previousWrite
	if retryErr := runApply(t, root, runID); retryErr != nil {
		t.Fatalf("同一run重试应只补齐治理事务: %v", retryErr)
	}
	if writes != 1 {
		t.Fatalf("恢复不得重复写Header: writes=%d", writes)
	}
	if _, err := os.Stat(intentPath); !os.IsNotExist(err) {
		t.Fatalf("恢复完成后Header意图必须清理: %v", err)
	}
}

func TestHeaderApplyDoesNotBaselineUnexpectedPostimage(t *testing.T) {
	root, runID := buildApplyRepo(t, "#新头部甲\n#新头部乙\n")
	previousWrite := writeHeaderIndex
	writeHeaderIndex = func(path string, data []byte, expected string) error {
		if err := previousWrite(path, data, expected); err != nil {
			return err
		}
		return os.WriteFile(path, append(data, []byte("#external\n")...), 0o644)
	}
	t.Cleanup(func() { writeHeaderIndex = previousWrite })

	err := runApply(t, root, runID)
	var exitErr *ExitError
	if err == nil || !errors.As(err, &exitErr) || exitErr.Code != ExitInternal ||
		!strings.Contains(err.Error(), "postimage") {
		t.Fatalf("意外Header postimage必须返回非零终态: %v", err)
	}
	state, exists, loadErr := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if loadErr != nil || hashErr != nil || (exists && state.Files["aoci.txt"] == current) {
		t.Fatalf("意外Header postimage不得被Baseline洗白: exists=%v load=%v hash=%v state=%+v current=%+v", exists, loadErr, hashErr, state, current)
	}
}

func TestHeaderApplyManifestFailureIsNotReportedAsSuccess(t *testing.T) {
	root, runID := buildApplyRepo(t, "#新头部甲\n#新头部乙\n")
	if err := draft.SaveManifest(root, &draft.Manifest{RunID: runID, Kind: draft.KindHeader}); err != nil {
		t.Fatal(err)
	}
	previousMark := markHeaderApplied
	markHeaderApplied = func(string, string) error {
		return errors.New("injected manifest failure")
	}
	t.Cleanup(func() { markHeaderApplied = previousMark })

	err := runApply(t, root, runID)
	var exitErr *ExitError
	if err == nil || !errors.As(err, &exitErr) || exitErr.Code != ExitInternal ||
		!strings.Contains(err.Error(), "Application审计未完成") {
		t.Fatalf("Application审计失败必须返回非零终态: %v", err)
	}
	events, _ := ledger.Recent(root, 20)
	for _, event := range events {
		if event.Op == "header_apply" && event.Result == ledger.ResultOK {
			t.Fatalf("Manifest失败不得留下Header成功审计: %+v", events)
		}
	}
	markHeaderApplied = previousMark
	if retryErr := runApply(t, root, runID); retryErr != nil {
		t.Fatalf("同一run重试应补齐Application审计: %v", retryErr)
	}
}

func TestHeaderApplyRejectsSectionLine(t *testing.T) {
	root, runID := buildApplyRepo(t, "#合法行\n===偷渡段/opt/evil/===\n")
	before := readIndex(t, root)

	err := runApply(t, root, runID)
	if err == nil {
		t.Fatal("含 === 行的草稿应被 apply 硬拒")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitInvalid {
		t.Fatalf("应为 ExitError{ExitInvalid},得到: %v", err)
	}
	// 被拒时索引零改动、零备份产生
	if readIndex(t, root) != before {
		t.Fatal("被拒的 apply 不得改动索引")
	}
	if backups, _ := filepath.Glob(filepath.Join(root, "aoci.txt.backup.*")); len(backups) != 0 {
		t.Fatalf("被拒的 apply 不应产生备份: %v", backups)
	}
}

func TestHeaderApplyRejectsBareLine(t *testing.T) {
	// #行规则在 apply 闸的接线证明: 散文/条目形态行均被拦
	for _, bad := range []string{
		"#合法行\n模型输出的解释性散文\n",
		"#合法行\ng.go[XC1T]: F:偷渡条目 | R:- | A:- | S:-\n",
	} {
		root, runID := buildApplyRepo(t, bad)
		before := readIndex(t, root)
		err := runApply(t, root, runID)
		if err == nil {
			t.Fatalf("非#非空行草稿应被硬拒: %q", bad)
		}
		var ee *ExitError
		if !errors.As(err, &ee) || ee.Code != ExitInvalid {
			t.Fatalf("应为 ExitError{ExitInvalid},得到: %v", err)
		}
		if readIndex(t, root) != before {
			t.Fatal("被拒的 apply 不得改动索引")
		}
	}
}

func TestHeaderApplyMissingDraft(t *testing.T) {
	root, _ := buildApplyRepo(t, "#有草稿但用错run\n")
	err := runApply(t, root, "20260709T235959Z") // 形态合法但不存在的 run
	if err == nil {
		t.Fatal("草稿缺失应报错")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitConfig {
		t.Fatalf("应为 ExitError{ExitConfig},得到: %v", err)
	}
}

// TestLoadIndexForCLI_ResolvesRelPaths loadIndexForCLI 必须填充条目 RelPath
// (entries diff 假新增缺陷防再犯,2026-07-10)。
//
// 事故背景: 旧版 loadIndexForCLI 只调 index.Parse 未调 ResolveRelPaths,
// 条目 RelPath 全空,致 entries diff 中 index.FindEntry(按 RelPath 精确匹配)
// 对已有条目恒 miss、把"替换"谎报为"新增"—— diff 是 draft-first 流程中
// 人工裁决的唯一依据,假 diff 让人在错误信息上做确认。apply 侧不受影响
// (走 mcptools.loadRepoCtx,该处已含 ResolveRelPaths),故此缺陷仅测试
// diff 所依赖的调用面即可锁死。
//
// 断言: 用 buildApplyRepo 的真实索引(根段一条目 f.go),loadIndexForCLI
// 返回的 doc 经 FindEntry("f.go")必须命中且 FullLine 正确 —— 再有人重写
// loadIndexForCLI 漏掉 ResolveRelPaths,本用例即红。
func TestLoadIndexForCLI_ResolvesRelPaths(t *testing.T) {
	root, _ := buildApplyRepo(t, "#不使用草稿\n")

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load 失败: %v", err)
	}
	cmd := newHeaderDiffCmd() // 任取一个 cobra 命令载体,仅为提供 ErrOrStderr
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	doc, idxPath, err := loadIndexForCLI(cmd, root, cfg)
	if err != nil {
		t.Fatalf("loadIndexForCLI 失败: %v", err)
	}
	if idxPath != filepath.Join(root, "aoci.txt") {
		t.Errorf("索引路径不符: %q", idxPath)
	}

	hit := index.FindEntry(doc, "f.go")
	if hit == nil {
		t.Fatal("FindEntry(f.go) 应命中 —— RelPath 未被填充(ResolveRelPaths 被漏掉,假新增缺陷复发)")
	}
	if !strings.Contains(hit.FullLine, "f.go[XC5T]") {
		t.Errorf("命中条目 FullLine 不符: %q", hit.FullLine)
	}
}
