// R52 run 一致性防线测试(guardImplicitApply/lastReviewedRunID)
// 索引条目待补: index_header_r52_test.go
//
// 夹具纪律(R43): 被测分支的前置状态必须真实存在 —— ledger 审阅事件经真实
// 写入端 ledger.Append 产生,绝不手写 JSONL 行副本;t.TempDir 造仓隔离。
// 独立成文缘由: 既有 index_test.go 承载 header apply 闸门链路,本文件只测
// R52 判据函数本体,不与其夹具耦合(辅助符号带 r52 前缀防同包撞名)。
package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// r52SeedReview 经真实写入端落一条审阅事件(R43: 正例取材写入端实物)
func r52SeedReview(t *testing.T, root, op, runID string) {
	t.Helper()
	ledger.Append(root, true, ledger.Event{
		Op: op, Source: ledger.SourceHuman, PathsCount: 1, DraftRunID: runID,
	})
}

// TestR52ExplicitAlwaysPasses 显式给 run_id 视为人已裁决: 即使台账审的是
// 别的 run 也直接放行(零警告零错误)。
func TestR52ExplicitAlwaysPasses(t *testing.T) {
	root := t.TempDir()
	r52SeedReview(t, root, "header_diff", "runA")
	warn, err := guardImplicitApply(root, "runB", true, "aoci index header apply", "header_diff")
	if err != nil || warn != "" {
		t.Fatalf("显式 run_id 应无条件放行: warn=%q err=%v", warn, err)
	}
}

// TestR52NoReviewRecordWarnsOnly 无审阅记录(空台账): 仅警告不阻断(保守放行,
// ledger 关闭仓的工作流不被破坏)。
func TestR52NoReviewRecordWarnsOnly(t *testing.T) {
	root := t.TempDir()
	warn, err := guardImplicitApply(root, "runA", false, "aoci index header apply", "header_diff")
	if err != nil {
		t.Fatalf("无审阅记录应放行: %v", err)
	}
	if warn == "" {
		t.Fatal("无审阅记录应给出警告文本(静默放行即防线不可见)")
	}
}

// TestR52MatchPasses 审过的 run 与将应用的 run 一致: 零警告零错误。
func TestR52MatchPasses(t *testing.T) {
	root := t.TempDir()
	r52SeedReview(t, root, "header_diff", "runA")
	warn, err := guardImplicitApply(root, "runA", false, "aoci index header apply", "header_diff")
	if err != nil || warn != "" {
		t.Fatalf("run 一致应无声放行: warn=%q err=%v", warn, err)
	}
}

// TestR52MismatchRejects 实弹形态判决用例: diff 审 runA → 二次 draft 产 runB
// → 隐式 apply 取到 runB —— 必须硬拒且文案含两个 run 与显式指定指引。
func TestR52MismatchRejects(t *testing.T) {
	root := t.TempDir()
	r52SeedReview(t, root, "header_diff", "runA")
	_, err := guardImplicitApply(root, "runB", false, "aoci index header apply", "header_diff")
	if err == nil {
		t.Fatal("审阅对象漂移必须硬拒(静默应用未审草稿即人审失效)")
	}
	for _, want := range []string{"runA", "runB", "R52", "aoci index header apply"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("拒绝文案应含 %q: %s", want, err.Error())
		}
	}
}

// TestR52MultiOpsAndRecency entries 侧双判据: check 也是审阅;多事件取最近。
func TestR52MultiOpsAndRecency(t *testing.T) {
	root := t.TempDir()
	r52SeedReview(t, root, "entries_diff", "runOld")
	r52SeedReview(t, root, "entries_check", "runNew")
	// 最近审阅是 runNew(check 事件): runNew 隐式应用放行
	if warn, err := guardImplicitApply(root, "runNew", false,
		"aoci index entries apply", "entries_diff", "entries_check"); err != nil || warn != "" {
		t.Fatalf("最近审阅(check)一致应放行: warn=%q err=%v", warn, err)
	}
	// runOld 已不是最近审阅: 拒
	if _, err := guardImplicitApply(root, "runOld", false,
		"aoci index entries apply", "entries_diff", "entries_check"); err == nil {
		t.Fatal("非最近审阅的 run 隐式应用应被拒(取最近语义)")
	}
	// 无关 op(header_diff)不算 entries 侧审阅
	root2 := t.TempDir()
	r52SeedReview(t, root2, "header_diff", "runX")
	warn, err := guardImplicitApply(root2, "runX", false,
		"aoci index entries apply", "entries_diff", "entries_check")
	if err != nil {
		t.Fatalf("跨族审阅不计,应走无记录警告放行: %v", err)
	}
	if warn == "" {
		t.Fatal("跨族审阅不计,应给出无记录警告")
	}
}
