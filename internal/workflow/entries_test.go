// entries draft 编排测试: 复用同包 buildRepo/fakeEndpoint/newTestClient 夹具。
// 覆盖成功链(草稿落盘/redacted快照脱敏/manifest逐文件状态/ledger落账)、
// full档快照含原文、warned带病落盘、失败不中断整批、空头部拒开工、空文件跳过
//   - v2.3 更新模式两防线(oldEntries 透传进 prompt / nil 时 build 无更新痕迹)
//   - v2.3.1 并发与进度两防线(进度回调触发序与终态透传 / 并发下 Statuses 保持
//     targets 原序且回调经互斥序列化后 done 无空洞无重复)。
//
// 索引条目: entries_test.go(workflow包,待补录)
package workflow

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func TestRunEntriesDraftSuccessWithRedaction(t *testing.T) {
	root, cfg, doc := buildRepo(t) // 默认 redacted 档
	reply := "newfile.go[XC5T]: F:未索引演示文件 | R:- | A:- | S:-"
	srv := fakeEndpoint(t, reply, true, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), []string{"newfile.go"}, nil)
	if err != nil {
		t.Fatalf("entries draft 失败: %v", err)
	}
	if res.Drafted != 1 || res.Warned+res.Failed+res.Skipped != 0 {
		t.Fatalf("分状态计数不符: %+v", res)
	}
	// 草稿落盘且内容为条目行
	data, derr := draft.ReadFile(root, res.RunID, "newfile.go.entry.txt")
	if derr != nil {
		t.Fatalf("读草稿失败: %v", derr)
	}
	if !strings.Contains(string(data), "未索引演示文件") {
		t.Fatalf("草稿内容不符: %q", data)
	}
	// redacted 快照: 存在、含指纹行、不含源码独有内容
	snap, serr := draft.ReadFile(root, res.RunID, "newfile.go.prompt.txt")
	if serr != nil {
		t.Fatalf("redacted 档应有逐文件快照: %v", serr)
	}
	if !strings.Contains(string(snap), "源码段已脱敏 sha256:") {
		t.Fatal("redacted 快照应含指纹行")
	}
	if strings.Contains(string(snap), "// 未索引") {
		t.Fatal("redacted 快照不得含源码原文")
	}
	// manifest 逐文件状态与 token
	m, merr := draft.LoadManifest(root, res.RunID)
	if merr != nil {
		t.Fatal(merr)
	}
	if m.Kind != draft.KindEntries || len(m.Entries) != 1 || m.Entries[0].Status != "drafted" {
		t.Fatalf("manifest 不符: %+v", m)
	}
	if len(m.Entries[0].SourceSHA256) != 64 ||
		m.Entries[0].SourceSHA256 != res.Statuses[0].SourceSHA256 {
		t.Fatalf("真实Endpoint草稿必须保存所读源码指纹: %+v", m.Entries[0])
	}
	if m.TokenSource != ledger.TokenSourceExact || m.InputTokens != 100 {
		t.Fatalf("token 口径不符: %+v", m)
	}
	if m.PromptHash != "" {
		t.Fatalf("entries 工序 PromptHash 应留空: %q", m.PromptHash)
	}
	// ledger 落账
	evs, _ := ledger.Recent(root, 10)
	var found bool
	for _, ev := range evs {
		if ev.Op == "entries_draft" && ev.DraftRunID == res.RunID {
			found = true
		}
	}
	if !found {
		t.Fatalf("ledger 未见 entries_draft: %+v", evs)
	}
}

func TestRunEntriesDraftFullSnapshot(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	cfg.AI.PromptSnapshot = "full"
	srv := fakeEndpoint(t, "newfile.go[XC5T]: F:x | R:- | A:- | S:-", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), []string{"newfile.go"}, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	snap, serr := draft.ReadFile(root, res.RunID, "newfile.go.prompt.txt")
	if serr != nil {
		t.Fatalf("full 档应有快照: %v", serr)
	}
	if !strings.Contains(string(snap), "// 未索引") {
		t.Fatal("full 档快照应含源码原文")
	}
}

func TestRunEntriesDraftWarnedStillWritten(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	// 文件名不一致 = 硬拒级违规: 带病落盘记 warned(apply 侧才硬拒,D42)
	srv := fakeEndpoint(t, "wrong.go[XC5T]: F:x | R:- | A:- | S:-", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), []string{"newfile.go"}, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if res.Warned != 1 || res.Drafted != 0 {
		t.Fatalf("应记 warned: %+v", res)
	}
	if _, derr := draft.ReadFile(root, res.RunID, "newfile.go.entry.txt"); derr != nil {
		t.Fatal("warned 草稿也应落盘(带病可见)")
	}
	m, _ := draft.LoadManifest(root, res.RunID)
	if m.Entries[0].Status != "warned" || m.Entries[0].Note == "" {
		t.Fatalf("manifest 应记 warned 与违规说明: %+v", m.Entries)
	}
}

func TestRunEntriesDraftFailureContinues(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	srv := fakeEndpoint(t, "", false, http.StatusInternalServerError, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), []string{"newfile.go", "indexed.go"}, nil)
	if err != nil {
		t.Fatalf("单文件失败不应中断整批: %v", err)
	}
	if res.Failed != 2 {
		t.Fatalf("两目标均应 failed: %+v", res)
	}
	// manifest 照落且 token 口径 estimated(失败无 usage)
	m, merr := draft.LoadManifest(root, res.RunID)
	if merr != nil {
		t.Fatal(merr)
	}
	if len(m.Entries) != 2 || m.TokenSource != ledger.TokenSourceEstimated {
		t.Fatalf("manifest 不符: %+v", m)
	}
}

func TestRunEntriesDraftRefusesEmptyHeader(t *testing.T) {
	root, cfg, _ := buildRepo(t)
	// 构造零头部文档(D28: 字典未立约拒开工)
	emptyDoc, _ := index.Parse("===段/opt/x/===\nf.go[XC1T]: F:x | R:- | A:- | S:-\n")
	srv := fakeEndpoint(t, "x", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	_, err := RunEntriesDraft(context.Background(), root, cfg, emptyDoc, newTestClient(t, srv.URL), []string{"newfile.go"}, nil)
	if err == nil || !strings.Contains(err.Error(), "header bootstrap") {
		t.Fatalf("空头部应拒开工并指引: %v", err)
	}
}

func TestRunEntriesDraftSkipsEmptyFile(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	if werr := writeRepoFile(t, root, "empty.go", ""); werr != nil {
		t.Fatal(werr)
	}
	srv := fakeEndpoint(t, "x", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), []string{"empty.go"}, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if res.Skipped != 1 {
		t.Fatalf("空文件应 skipped: %+v", res)
	}
}

// —— 以下为 v2.3 更新模式防线 ——

// TestRunEntriesDraftUpdateModePassesOldEntry oldEntries 命中目标 → 旧条目
// 与更新纪律进入 prompt(以 full 档快照为观测窗口验证透传链路)。
func TestRunEntriesDraftUpdateModePassesOldEntry(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	cfg.AI.PromptSnapshot = "full" // full 档快照含完整 user 段,作透传观测窗口
	srv := fakeEndpoint(t, "newfile.go[XC5T]: F:x | R:- | A:- | S:-", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	oldEntries := map[string]string{
		"newfile.go": "newfile.go[XC3T]: F:旧职责 | R:- | A:- | S:旧S伤疤须保留判断。",
	}
	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), []string{"newfile.go"}, oldEntries)
	if err != nil {
		t.Fatalf("更新模式 draft 失败: %v", err)
	}
	if res.Drafted != 1 {
		t.Fatalf("应 drafted: %+v", res)
	}
	snap, serr := draft.ReadFile(root, res.RunID, "newfile.go.prompt.txt")
	if serr != nil {
		t.Fatalf("读快照失败: %v", serr)
	}
	s := string(snap)
	for _, anchor := range []string{
		"更新模式纪律",    // system 侧纪律注入(经快照的 system 段观测)
		"现有条目 开始",   // user 侧旧条目区块
		"旧S伤疤须保留判断", // 旧条目逐字在场
	} {
		if !strings.Contains(s, anchor) {
			t.Fatalf("更新模式快照缺少锚点 %q", anchor)
		}
	}
}

// TestRunEntriesDraftNilOldEntriesNoUpdateTrace oldEntries 为 nil → build 模式,
// prompt 无任何更新模式痕迹(引入前行为不变的 workflow 层证明)。
func TestRunEntriesDraftNilOldEntriesNoUpdateTrace(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	cfg.AI.PromptSnapshot = "full"
	srv := fakeEndpoint(t, "newfile.go[XC5T]: F:x | R:- | A:- | S:-", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), []string{"newfile.go"}, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	snap, serr := draft.ReadFile(root, res.RunID, "newfile.go.prompt.txt")
	if serr != nil {
		t.Fatalf("读快照失败: %v", serr)
	}
	s := string(snap)
	for _, forbidden := range []string{"更新模式", "现有条目"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("nil oldEntries 时 prompt 不应含更新模式痕迹: %q", forbidden)
		}
	}
}

// —— 以下为 v2.3.1 并发与进度防线 ——

// progressCall 进度回调观测记录
type progressCall struct {
	done, total  int
	path, status string
}

// TestRunEntriesDraftProgressCallback 串行(默认并发1)下进度回调:
// done 严格 1..N 递增、路径按 targets 原序、终态如实透传(drafted/warned)。
func TestRunEntriesDraftProgressCallback(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	if werr := writeRepoFile(t, root, "second.go", "package s\n"); werr != nil {
		t.Fatal(werr)
	}
	// 固定回复文件名=newfile.go: 第一目标 drafted,第二目标文件名不符记 warned
	srv := fakeEndpoint(t, "newfile.go[XC5T]: F:x | R:- | A:- | S:-", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	targets := []string{"newfile.go", "second.go"}
	var calls []progressCall
	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), targets, nil,
		WithProgress(func(done, total int, path, status string) {
			calls = append(calls, progressCall{done, total, path, status})
		}))
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("回调应触发 2 次: %+v", calls)
	}
	for i, c := range calls {
		if c.done != i+1 || c.total != 2 {
			t.Fatalf("done/total 不符(第%d次): %+v", i+1, c)
		}
		// 串行(默认并发1)下完成顺序即 targets 原序
		if c.path != targets[i] {
			t.Fatalf("串行下回调路径应按原序(第%d次): %+v", i+1, c)
		}
	}
	if calls[0].status != "drafted" || calls[1].status != "warned" {
		t.Fatalf("回调终态透传不符: %+v", calls)
	}
	if res.Drafted != 1 || res.Warned != 1 {
		t.Fatalf("汇总计数不符: %+v", res)
	}
}

// TestRunEntriesDraftConcurrencyPreservesOrder 并发3跑5目标:
// Statuses 恒按 targets 原序(结果槽下标写入的确定性),计数正确,
// 进度回调经互斥序列化后 done 严格 1..N 无空洞无重复(完成顺序可乱,计数不乱)。
// 本用例同时是数据竞争靶(go test -race 下覆盖 worker 池共享状态)。
func TestRunEntriesDraftConcurrencyPreservesOrder(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	targets := []string{"newfile.go"}
	for _, extra := range []string{"c1.go", "c2.go", "c3.go", "c4.go"} {
		if werr := writeRepoFile(t, root, extra, "package c\n"); werr != nil {
			t.Fatal(werr)
		}
		targets = append(targets, extra)
	}
	cfg.AI.MaxConcurrency = 3
	// 固定回复文件名=newfile.go: 仅第一目标 drafted,其余四目标文件名不符 warned
	srv := fakeEndpoint(t, "newfile.go[XC5T]: F:x | R:- | A:- | S:-", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	var calls []progressCall
	res, err := RunEntriesDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL), targets, nil,
		WithProgress(func(done, total int, path, status string) {
			calls = append(calls, progressCall{done, total, path, status})
		}))
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	// Statuses 保序: 逐槽比对 targets
	if len(res.Statuses) != len(targets) {
		t.Fatalf("Statuses 长度不符: %+v", res.Statuses)
	}
	for i, s := range res.Statuses {
		if s.Path != targets[i] {
			t.Fatalf("并发下 Statuses 第 %d 槽应为 %s,得到 %s", i, targets[i], s.Path)
		}
	}
	if res.Drafted != 1 || res.Warned != 4 || res.Failed+res.Skipped != 0 {
		t.Fatalf("计数不符: %+v", res)
	}
	// 进度: 触发 5 次,done 严格 1..5(互斥序列化保证),路径集合=目标集合
	if len(calls) != 5 {
		t.Fatalf("回调应触发 5 次: %+v", calls)
	}
	seen := map[string]bool{}
	for i, c := range calls {
		if c.done != i+1 || c.total != 5 {
			t.Fatalf("并发下 done 应无空洞无重复(第%d次): %+v", i+1, c)
		}
		seen[c.path] = true
	}
	for _, tg := range targets {
		if !seen[tg] {
			t.Fatalf("回调路径集合缺 %s: %+v", tg, calls)
		}
	}
	// manifest 逐文件状态同保序
	m, merr := draft.LoadManifest(root, res.RunID)
	if merr != nil {
		t.Fatal(merr)
	}
	if len(m.Entries) != 5 || m.Entries[0].Path != "newfile.go" {
		t.Fatalf("manifest 保序不符: %+v", m.Entries)
	}
}
