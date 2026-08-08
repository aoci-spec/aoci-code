// 失败路径落账(R60-F.9-A1)判决测试 —— 兼作 ledger v3 字段往返验证。
//
// 判决:
//  1. 校验拒绝(bad_args)→ ledger 含 result=rejected + fail_code=bad_args;
//  2. CAS 冲突(write_conflict)→ ledger 含 result=conflict;
//  3. dry-run 拒绝 → 零落账(纯读纪律);
//  4. 成功事件 result=ok(与失败事件可区分)。
package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// lastEventByOp 取最近一条指定 op 的事件。
func lastEventByOp(t *testing.T, root, op string) *ledger.Event {
	t.Helper()
	events, _ := ledger.Recent(root, 0)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Op == op {
			return &events[i]
		}
	}
	return nil
}

// TestUpdateEntryFailLedger 单条回写失败落账四判决。
func TestUpdateEntryFailLedger(t *testing.T) {
	root := buildRepo(t)

	// 判决1: 校验拒绝(文件名不符 → bad_args)落账 rejected
	_, fail := ApplyUpdateEntry(root, "src/a.go",
		"wrong.go[X.Y.5.T]: F:- | R:- | A:- | S:-", "agent", false)
	if fail == nil || fail.Code != errBadArgs {
		t.Fatalf("前置: 文件名不符应 bad_args: %+v", fail)
	}
	ev := lastEventByOp(t, root, "update_entry")
	if ev == nil {
		t.Fatal("拒绝路径必须落账(A1 核心判决)")
	}
	if ev.Result != ledger.ResultRejected || ev.FailCode != errBadArgs {
		t.Fatalf("拒绝事件应 result=rejected fail_code=bad_args: %+v", ev)
	}

	// 判决2: 重复条目冲突(write_conflict)落账 conflict
	idxPath := filepath.Join(root, ".aoci", "index.txt")
	data, _ := os.ReadFile(idxPath)
	dupLine := "a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:改前必读\n"
	os.WriteFile(idxPath, append(data, []byte(dupLine)...), 0644)
	_, fail = ApplyUpdateEntry(root, "src/a.go",
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:再改", "agent", false)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("前置: 重复条目应 write_conflict: %+v", fail)
	}
	ev = lastEventByOp(t, root, "update_entry")
	if ev == nil || ev.Result != ledger.ResultConflict {
		t.Fatalf("冲突事件应 result=conflict: %+v", ev)
	}
	os.WriteFile(idxPath, data, 0644)

	// 判决3: dry-run 拒绝零落账(纯读纪律)
	before, _ := ledger.Recent(root, 0)
	_, fail = ApplyUpdateEntry(root, "src/a.go",
		"wrong.go[X.Y.5.T]: F:- | R:- | A:- | S:-", "human", true)
	if fail == nil {
		t.Fatal("前置: dry-run 同样应被拒")
	}
	after, _ := ledger.Recent(root, 0)
	if len(after) != len(before) {
		t.Fatalf("dry-run 拒绝不得落账: before=%d after=%d",
			len(before), len(after))
	}

	// 判决4: 成功事件 result=ok
	_, fail = ApplyUpdateEntry(root, "src/a.go",
		"a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:成功态", "agent", false)
	if fail != nil {
		t.Fatalf("前置: 成功回写异常: %+v", fail)
	}
	ev = lastEventByOp(t, root, "update_entry")
	if ev == nil || ev.Result != ledger.ResultOK || ev.FailCode != "" {
		t.Fatalf("成功事件应 result=ok 且无 fail_code: %+v", ev)
	}
}

// TestAtomicBatchFailLedger 批量管线失败落账判决。
func TestAtomicBatchFailLedger(t *testing.T) {
	root := buildRepo(t)

	// 批内任一条校验拒绝 → 整批落账 rejected
	items := []AtomicUpdateItem{
		{Path: "src/a.go",
			NewEntry: "a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:合规"},
		{Path: "src/b.go",
			NewEntry: "mismatch.go[X.Y.5.T]: F:- | R:- | A:- | S:-"},
	}
	_, fail := ApplyUpdateEntriesAtomic(root, items, "agent", false)
	if fail == nil {
		t.Fatal("前置: 批内文件名不符应整批拒绝")
	}
	ev := lastEventByOp(t, root, "update_entries_batch")
	if ev == nil {
		t.Fatal("批量拒绝必须落账")
	}
	if ev.Result != ledger.ResultRejected {
		t.Fatalf("批量拒绝事件应 result=rejected: %+v", ev)
	}
}
