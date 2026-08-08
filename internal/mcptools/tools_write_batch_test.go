// D69-2A 原子批量回写测试。
package mcptools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func writeBatchSource(
	t *testing.T,
	root,
	rel string,
) {
	t.Helper()

	absolutePath := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)
	if err := os.MkdirAll(
		filepath.Dir(absolutePath),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		absolutePath,
		[]byte("package demo\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func readBatchIndex(t *testing.T, root string) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(root, ".aoci", "index.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func buildSelfIndexedBatchRepo(
	t *testing.T,
) (string, AtomicUpdateItem, string) {
	t.Helper()
	root := buildRepo(t)
	indexPath := filepath.Join(root, ".aoci", "index.txt")
	current, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	oldEntry := "index.txt[X.Y.5.T]: F:旧索引职责 | R:- | A:- | S:-"
	withSelf := strings.Replace(
		string(current),
		"====完====",
		"===段 "+filepath.ToSlash(filepath.Join(root, ".aoci"))+"/===\n"+
			oldEntry+"\n====完====",
		1,
	)
	if withSelf == string(current) {
		t.Fatal("未能构造索引自条目")
	}
	if err := os.WriteFile(indexPath, []byte(withSelf), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state == nil {
		t.Fatalf("读取自条目夹具Baseline失败: exists=%v err=%v", exists, err)
	}
	baseline.UpdateOne(state, ".aoci/index.txt", fingerprint)
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	return root, AtomicUpdateItem{
		Path:         ".aoci/index.txt",
		NewEntry:     "index.txt[X.Y.5.T]: F:新索引职责 | R:- | A:- | S:-",
		SourceSHA256: fingerprint.SHA256,
	}, fingerprint.SHA256
}

func TestAtomicBatchSuccessReplaceAndInsert(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")

	outcome, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path: "src/a.go",
				NewEntry: "a.go[X.Y.5.T]: F:批量替换甲 | " +
					"R:- | A:- | S:整批原子",
			},
			{
				Path: "src/b.go",
				NewEntry: "b.go[X.Y.5.T]: F:批量新增乙 | " +
					"R:- | A:- | S:整批原子",
			},
		},
		ledger.SourceCLIAI,
		false,
	)
	if fail != nil {
		t.Fatalf("原子批量应成功: %+v", fail)
	}
	if outcome == nil || len(outcome.Items) != 2 {
		t.Fatalf("批量结果不符: %+v", outcome)
	}

	indexText := readBatchIndex(t, root)
	for _, anchor := range []string{
		"F:批量替换甲",
		"F:批量新增乙",
	} {
		if !strings.Contains(indexText, anchor) {
			t.Fatalf("批量写入缺少 %q:\n%s", anchor, indexText)
		}
	}

	baselineState, exists, err := baseline.Load(root)
	if err != nil || !exists || baselineState == nil {
		t.Fatalf(
			"批量应用后基线应存在: exists=%v err=%v",
			exists,
			err,
		)
	}
	for _, rel := range []string{
		"src/a.go",
		"src/b.go",
		".aoci/index.txt",
	} {
		if _, ok := baselineState.Files[rel]; !ok {
			t.Fatalf("批量基线缺 %s: %+v", rel, baselineState.Files)
		}
	}

	events, _ := ledger.Recent(root, 20)
	found := false
	for _, event := range events {
		if event.Op == "update_entries_batch" &&
			event.PathsCount == 2 &&
			event.AppliedCount == 2 &&
			event.Source == ledger.SourceCLIAI {
			found = true
		}
	}
	if !found {
		t.Fatalf("未见原子批量 ledger 事件: %+v", events)
	}
}

func TestSelfIndexedAtomicBatchUsesProvenPostimageForRecovery(t *testing.T) {
	root, item, preimage := buildSelfIndexedBatchRepo(t)
	items := []AtomicUpdateItem{item}
	previousWrite := writeAtomicIndex
	previousSave := saveAtomicBaseline
	writes := 0
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		writes++
		return previousWrite(path, data, expected)
	}
	saveAtomicBaseline = func(string, *baseline.Baseline) error {
		return errors.New("injected baseline failure")
	}
	t.Cleanup(func() {
		writeAtomicIndex = previousWrite
		saveAtomicBaseline = previousSave
	})

	first, fail := ApplyUpdateEntriesAtomicBound(
		root, items, ledger.SourceAgent, false, preimage,
	)
	if fail != nil || first == nil || first.BaselineComplete ||
		first.AppliedCount != 1 {
		t.Fatalf("索引自条目Baseline故障必须保留正式postimage: fail=%+v outcome=%+v", fail, first)
	}
	postimage, err := baseline.HashFile(filepath.Join(root, ".aoci", "index.txt"))
	if err != nil || postimage.SHA256 == preimage {
		t.Fatalf("索引自条目未产生确定postimage: fingerprint=%+v err=%v", postimage, err)
	}

	saveAtomicBaseline = previousSave
	recovered, recoveryFail := ApplyUpdateEntriesAtomicBound(
		root, items, ledger.SourceAgent, false, preimage,
	)
	if recoveryFail != nil || recovered == nil || !recovered.BaselineComplete ||
		!recovered.AlreadyApplied || recovered.AppliedCount != 0 ||
		recovered.RecoveredCount != 1 {
		t.Fatalf("收据证明的索引自条目必须零写入恢复: fail=%+v outcome=%+v", recoveryFail, recovered)
	}
	if writes != 1 {
		t.Fatalf("索引自条目恢复不得重复正式写入: writes=%d", writes)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state == nil ||
		state.Files[".aoci/index.txt"].SHA256 != postimage.SHA256 {
		t.Fatalf("恢复后Baseline必须绑定正式postimage: exists=%v err=%v state=%+v", exists, err, state)
	}

	if err := CompleteUpdateEntriesAtomicRecovery(root, items); err != nil {
		t.Fatal(err)
	}
	unproven, unprovenFail := ApplyUpdateEntriesAtomicBound(
		root, items, ledger.SourceAgent, false, preimage,
	)
	if unproven != nil || unprovenFail == nil ||
		unprovenFail.Code != errWriteConflict ||
		!strings.Contains(unprovenFail.Msg, "源码指纹CAS冲突") {
		t.Fatalf("无事务证明的旧索引自绑定必须拒绝: outcome=%+v fail=%+v", unproven, unprovenFail)
	}
	if writes != 1 {
		t.Fatalf("拒绝旧自绑定不得写正式索引: writes=%d", writes)
	}
}

func TestHostAgentBatchRejectsGenerationPreimageDrift(t *testing.T) {
	root := buildRepo(t)
	expected := indexTextHash(readBatchIndex(t, root))
	indexPath := filepath.Join(root, ".aoci", "index.txt")
	concurrent := readBatchIndex(t, root) + "# concurrent governance change\n"
	if err := os.WriteFile(indexPath, []byte(concurrent), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, fail := ApplyUpdateEntriesAtomicBound(
		root,
		[]AtomicUpdateItem{{
			Path:     "src/a.go",
			NewEntry: "a.go[X.Y.5.T]: F:过期候选 | R:- | A:- | S:-",
		}},
		ledger.SourceAgent,
		false,
		expected,
	)
	if outcome != nil || fail == nil || fail.Code != errWriteConflict ||
		!strings.Contains(fail.Msg, "Generation Plan") {
		t.Fatalf("过期Host-Agent preimage必须整批拒绝: outcome=%+v fail=%+v", outcome, fail)
	}
	if current := readBatchIndex(t, root); current != concurrent {
		t.Fatal("Generation Plan冲突不得把旧候选重基到并发索引")
	}
}

func TestHostAgentBatchRejectsNoOpFromLaterVersionWithoutReceipt(t *testing.T) {
	root := buildRepo(t)
	items := []AtomicUpdateItem{{
		Path: "src/a.go", NewEntry: "a.go[X.Y.5.T]: F:已存在候选 | R:- | A:- | S:-",
	}}
	expected := indexTextHash(readBatchIndex(t, root))
	first, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceCLIAI, false)
	if fail != nil || first == nil || !first.BaselineComplete {
		t.Fatalf("夹具首次普通批次应完成并清理恢复收据: fail=%+v outcome=%+v", fail, first)
	}
	indexPath := filepath.Join(root, ".aoci", "index.txt")
	concurrent := readBatchIndex(t, root) + "# unrelated later governance change\n"
	if err := os.WriteFile(indexPath, []byte(concurrent), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, boundFail := ApplyUpdateEntriesAtomicBound(
		root, items, ledger.SourceAgent, false, expected,
	)
	if outcome != nil || boundFail == nil || boundFail.Code != errWriteConflict ||
		!strings.Contains(boundFail.Msg, "Generation Plan") {
		t.Fatalf("无本批收据的后续no-op版本必须拒绝: outcome=%+v fail=%+v", outcome, boundFail)
	}
	if current := readBatchIndex(t, root); current != concurrent {
		t.Fatal("拒绝Host误恢复时必须保留后续索引版本")
	}
}

func TestHostAgentBatchRecoversWhenCASWritesThenReturnsError(t *testing.T) {
	root := buildRepo(t)
	fingerprint, err := baseline.HashFile(filepath.Join(root, "src", "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	items := []AtomicUpdateItem{{
		Path: "src/a.go", NewEntry: "a.go[X.Y.5.T]: F:CAS半完成 | R:- | A:- | S:-",
		SourceSHA256: fingerprint.SHA256,
	}}
	expected := indexTextHash(readBatchIndex(t, root))
	previousWrite := writeAtomicIndex
	writes := 0
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		writes++
		if err := previousWrite(path, data, expected); err != nil {
			return err
		}
		return errors.New("injected postimage cleanup failure")
	}
	t.Cleanup(func() { writeAtomicIndex = previousWrite })

	first, fail := ApplyUpdateEntriesAtomicBound(root, items, ledger.SourceAgent, false, expected)
	if fail != nil || first == nil || first.BaselineComplete || first.AppliedCount != 1 ||
		!strings.Contains(first.BaselineNote, "postimage已写入") {
		t.Fatalf("CAS已写入后的错误必须保留真实半完成状态: fail=%+v outcome=%+v", fail, first)
	}

	writeAtomicIndex = previousWrite
	recovered, retryFail := ApplyUpdateEntriesAtomicBound(root, items, ledger.SourceAgent, false, expected)
	if retryFail != nil || recovered == nil || !recovered.BaselineComplete ||
		!recovered.AlreadyApplied || recovered.AppliedCount != 0 {
		t.Fatalf("同一Host批次应零写入恢复: fail=%+v outcome=%+v", retryFail, recovered)
	}
	if writes != 1 {
		t.Fatalf("恢复不得重复执行Entries索引写入: writes=%d", writes)
	}
	if err := CompleteUpdateEntriesAtomicRecovery(root, items); err != nil {
		t.Fatalf("Application审计后应可清理批次收据: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "entries-*.json")); err != nil || len(matches) != 0 {
		t.Fatalf("治理事务收口后不得残留批次收据: err=%v matches=%v", err, matches)
	}
}

func TestAtomicBatchRecoveryRejectsMissingPreimage(t *testing.T) {
	root := buildRepo(t)
	items := []AtomicUpdateItem{{
		Path: "src/a.go", NewEntry: "a.go[X.Y.5.T]: F:非法恢复 | R:- | A:- | S:-",
	}}
	normalized, err := normalizeAtomicRecoveryItems(items)
	if err != nil {
		t.Fatal(err)
	}
	key := atomicBatchKey(normalized)
	if err := saveAtomicBatchRecovery(root, atomicBatchRecovery{
		Version: 1, BatchKey: key,
		PostIndexSHA256: indexTextHash(readBatchIndex(t, root)),
	}); err != nil {
		t.Fatal(err)
	}

	pending, pendingErr := UpdateEntriesAtomicRecoveryPending(root, items)
	if pending || pendingErr == nil || !strings.Contains(pendingErr.Error(), "entries_batch_recovery_receipt_invalid") {
		t.Fatalf("缺少preimage的批次收据不得成为恢复证据: pending=%v err=%v", pending, pendingErr)
	}
}

func TestAtomicBatchMissingTargetCannotReportBaselineComplete(t *testing.T) {
	root := buildRepo(t)
	outcome, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{{
			Path:     "src/missing.go",
			NewEntry: "missing.go[X.Y.5.T]: F:旧批次缺目标 | R:- | A:- | S:-",
		}},
		ledger.SourceCLIAI,
		false,
	)
	if fail != nil || outcome == nil || outcome.BaselineComplete || outcome.AppliedCount != 1 {
		t.Fatalf("目标指纹失败必须报告索引已写但Baseline不完整: fail=%+v outcome=%+v", fail, outcome)
	}
	if !strings.Contains(outcome.BaselineNote, "部分前移") {
		t.Fatalf("Baseline部分完成说明不符: %+v", outcome)
	}
}

func TestAtomicBatchValidationFailureWritesNothing(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")
	writeBatchSource(t, root, "src/c.go")

	indexBefore := readBatchIndex(t, root)
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineBefore, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}

	_, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path: "src/b.go",
				NewEntry: "b.go[X.Y.5.T]: F:本可成功 | " +
					"R:- | A:- | S:-",
			},
			{
				Path: "src/c.go",
				NewEntry: "wrong.go[X.Y.5.T]: F:文件名错误 | " +
					"R:- | A:- | S:-",
			},
		},
		ledger.SourceCLIAI,
		false,
	)
	if fail == nil {
		t.Fatal("批次含非法条目时应整批拒绝")
	}
	if fail.Code != errBadArgs {
		t.Fatalf("应为 bad_args,得到 %+v", fail)
	}

	if readBatchIndex(t, root) != indexBefore {
		t.Fatal("规划失败后正式索引必须零变化")
	}
	baselineAfter, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(baselineAfter) != string(baselineBefore) {
		t.Fatal("规划失败后基线必须零变化")
	}
}

func TestAtomicBatchRejectsDuplicatePath(t *testing.T) {
	root := buildRepo(t)

	_, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path:     "src/a.go",
				NewEntry: "a.go[X.Y.5.T]: F:一 | R:- | A:- | S:-",
			},
			{
				Path:     "src/a.go",
				NewEntry: "a.go[X.Y.5.T]: F:二 | R:- | A:- | S:-",
			},
		},
		ledger.SourceCLIAI,
		false,
	)
	if fail == nil || fail.Code != errBadArgs {
		t.Fatalf("重复路径应 bad_args: %+v", fail)
	}
	if !strings.Contains(fail.Msg, "重复路径") {
		t.Fatalf("拒绝文案应点明重复路径: %+v", fail)
	}
}

func TestAtomicBatchCASConflictWritesNothing(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")

	plan, fail := planUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path: "src/a.go",
				NewEntry: "a.go[X.Y.5.T]: F:计划内替换 | " +
					"R:- | A:- | S:-",
			},
			{
				Path: "src/b.go",
				NewEntry: "b.go[X.Y.5.T]: F:计划内新增 | " +
					"R:- | A:- | S:-",
			},
		},
	)
	if fail != nil {
		t.Fatalf("批量规划应成功: %+v", fail)
	}

	indexPath := filepath.Join(root, ".aoci", "index.txt")
	current, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := string(current) + "#外部通道修改\n"
	if err := os.WriteFile(
		indexPath,
		[]byte(tampered),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, _, commitFail := commitAtomicBatch(
		root,
		ledger.SourceCLIAI,
		plan,
		false,
	)
	if commitFail == nil ||
		commitFail.Code != errWriteConflict {
		t.Fatalf("CAS 冲突应整批拒绝: %+v", commitFail)
	}

	if readBatchIndex(t, root) != tampered {
		t.Fatal("CAS 拒绝后索引必须保持外部修改后的原样")
	}
}

func TestAtomicBatchSourceDriftAfterIndexWriteCannotCompleteBaseline(t *testing.T) {
	root := buildRepo(t)
	fingerprint, err := baseline.HashFile(filepath.Join(root, "src", "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	previousWrite := writeAtomicIndex
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		if writeErr := previousWrite(path, data, expected); writeErr != nil {
			return writeErr
		}
		return os.WriteFile(
			filepath.Join(root, "src", "a.go"),
			[]byte("package sample\n\nvar Concurrent = true\n"),
			0o644,
		)
	}
	t.Cleanup(func() { writeAtomicIndex = previousWrite })

	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path:         "src/a.go",
		NewEntry:     "a.go[X.Y.5.T]: F:并发绑定 | R:- | A:- | S:-",
		SourceSHA256: fingerprint.SHA256,
	}}, ledger.SourceCLIAI, false)
	if fail != nil || outcome == nil || outcome.BaselineComplete || outcome.AppliedCount != 1 {
		t.Fatalf("索引写后源码漂移必须保留已写事实并停止: fail=%+v outcome=%+v", fail, outcome)
	}
	if !strings.Contains(outcome.BaselineNote, "CAS边界发生漂移") {
		t.Fatalf("停止原因必须指出源码竞态: %+v", outcome)
	}
	state, exists, loadErr := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, "src", "a.go"))
	if loadErr != nil || hashErr != nil || !exists || state.Files["src/a.go"] == current {
		t.Fatalf("并发源码不得被Baseline洗白: exists=%v load=%v hash=%v baseline=%+v current=%+v", exists, loadErr, hashErr, state, current)
	}
}

func TestAtomicBatchUnexpectedIndexPostimageCannotCompleteBaseline(t *testing.T) {
	root := buildRepo(t)
	previousWrite := writeAtomicIndex
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		if err := previousWrite(path, data, expected); err != nil {
			return err
		}
		return os.WriteFile(path, append(data, []byte("#external\n")...), 0o644)
	}
	t.Cleanup(func() { writeAtomicIndex = previousWrite })

	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "src/a.go", NewEntry: "a.go[X.Y.5.T]: F:索引竞态 | R:- | A:- | S:-",
	}}, ledger.SourceCLIAI, false)
	if fail != nil || outcome == nil || outcome.BaselineComplete ||
		!strings.Contains(outcome.BaselineNote, "postimage") {
		t.Fatalf("意外索引postimage必须停止且Baseline不完成: fail=%+v outcome=%+v", fail, outcome)
	}
	state, exists, loadErr := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, ".aoci", "index.txt"))
	if loadErr != nil || hashErr != nil || !exists || state.Files[".aoci/index.txt"] == current {
		t.Fatalf("外部索引postimage不得被Baseline洗白: exists=%v load=%v hash=%v state=%+v current=%+v", exists, loadErr, hashErr, state, current)
	}
}

func TestAtomicBatchReplacementBoundaryCASPreservesExternalEdit(t *testing.T) {
	root := buildRepo(t)
	indexPath := filepath.Join(root, ".aoci", "index.txt")
	previousWrite := writeAtomicIndex
	external := []byte("external editor replacement\n")
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		if err := os.WriteFile(path, external, 0o644); err != nil {
			return err
		}
		return previousWrite(path, data, expected)
	}
	t.Cleanup(func() { writeAtomicIndex = previousWrite })

	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "src/a.go", NewEntry: "a.go[X.Y.5.T]: F:替换边界 | R:- | A:- | S:-",
	}}, ledger.SourceCLIAI, false)
	if outcome != nil || fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("替换紧邻边界CAS冲突必须零应用: outcome=%+v fail=%+v", outcome, fail)
	}
	current, err := os.ReadFile(indexPath)
	if err != nil || string(current) != string(external) {
		t.Fatalf("外部编辑必须原样保留: err=%v current=%q", err, current)
	}
}

func TestAtomicBatchIndexDriftDuringBaselineSaveCannotSucceed(t *testing.T) {
	root := buildRepo(t)
	previousSave := saveAtomicBaseline
	saveAtomicBaseline = func(root string, state *baseline.Baseline) error {
		if err := previousSave(root, state); err != nil {
			return err
		}
		path := filepath.Join(root, ".aoci", "index.txt")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(data, []byte("#external\n")...), 0o644)
	}
	t.Cleanup(func() { saveAtomicBaseline = previousSave })

	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "src/a.go", NewEntry: "a.go[X.Y.5.T]: F:保存期竞态 | R:- | A:- | S:-",
	}}, ledger.SourceCLIAI, false)
	if fail != nil || outcome == nil || outcome.BaselineComplete ||
		!strings.Contains(outcome.BaselineNote, "postimage") {
		t.Fatalf("Baseline保存期索引漂移必须降级: fail=%+v outcome=%+v", fail, outcome)
	}
	state, exists, loadErr := baseline.Load(root)
	current, hashErr := baseline.HashFile(filepath.Join(root, ".aoci", "index.txt"))
	if loadErr != nil || hashErr != nil || !exists || state.Files[".aoci/index.txt"] == current {
		t.Fatalf("保存期外部变化必须保持Stale: exists=%v load=%v hash=%v state=%+v current=%+v", exists, loadErr, hashErr, state, current)
	}
}

func TestAtomicBatchReplaySourceDriftDuringBaselineSaveCannotSucceed(t *testing.T) {
	root := buildRepo(t)
	fingerprint, err := baseline.HashFile(filepath.Join(root, "src", "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	item := AtomicUpdateItem{
		Path:         "src/a.go",
		NewEntry:     "a.go[X.Y.5.T]: F:重放绑定 | R:- | A:- | S:-",
		SourceSHA256: fingerprint.SHA256,
	}
	first, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceCLIAI, false)
	if fail != nil || first == nil || !first.BaselineComplete {
		t.Fatalf("首次应用应成功: fail=%+v outcome=%+v", fail, first)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("读取首次Baseline失败: exists=%v err=%v", exists, err)
	}
	delete(state.Files, "src/a.go")
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}

	previousSave := saveAtomicBaseline
	saveAtomicBaseline = func(root string, state *baseline.Baseline) error {
		if saveErr := previousSave(root, state); saveErr != nil {
			return saveErr
		}
		return os.WriteFile(
			filepath.Join(root, "src", "a.go"),
			[]byte("package sample\n\nvar ChangedDuringReplay = true\n"),
			0o644,
		)
	}
	t.Cleanup(func() { saveAtomicBaseline = previousSave })

	replayed, replayFail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceCLIAI, false)
	if replayFail != nil || replayed == nil || replayed.BaselineComplete ||
		!replayed.AlreadyApplied || replayed.AppliedCount != 0 {
		t.Fatalf("重放保存期源码漂移必须停止且索引零重写: fail=%+v outcome=%+v", replayFail, replayed)
	}
	if !strings.Contains(replayed.BaselineNote, "源码发生漂移") {
		t.Fatalf("重放停止原因不符: %+v", replayed)
	}
}

func TestAtomicBatchDryRunWritesNothing(t *testing.T) {
	root := buildRepo(t)
	writeBatchSource(t, root, "src/b.go")

	before := readBatchIndex(t, root)

	outcome, fail := ApplyUpdateEntriesAtomic(
		root,
		[]AtomicUpdateItem{
			{
				Path: "src/b.go",
				NewEntry: "b.go[X.Y.5.T]: F:只预览 | " +
					"R:- | A:- | S:-",
			},
		},
		ledger.SourceCLIAI,
		true,
	)
	if fail != nil {
		t.Fatalf("dry-run 应成功: %+v", fail)
	}
	if outcome == nil || !outcome.DryRun {
		t.Fatalf("dry-run 标记缺失: %+v", outcome)
	}
	if readBatchIndex(t, root) != before {
		t.Fatal("dry-run 不得修改正式索引")
	}
}
