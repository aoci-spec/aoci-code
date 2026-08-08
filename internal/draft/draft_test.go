// 草稿区管理测试: run_id 形态与冲突 / 路径安全 / manifest 往返 / 最新查询
// 索引条目: draft_test.go(待补录)
package draft

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNewRunFormatAndCollision(t *testing.T) {
	root := t.TempDir()
	id, err := NewRun(root)
	if err != nil {
		t.Fatalf("NewRun 失败: %v", err)
	}
	if !runIDRe.MatchString(id) {
		t.Fatalf("run_id 形态非法: %q", id)
	}
	if strings.Contains(id, ":") {
		t.Fatalf("run_id 不得含冒号(Windows 兼容): %q", id)
	}
	// 预建同名目录制造同秒冲突,NewRun 须避让为 -2 后缀
	dir, _ := RunDir(root, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	id2, err := NewRun(root)
	if err != nil {
		t.Fatalf("冲突避让失败: %v", err)
	}
	if id2 == id {
		t.Fatalf("同秒冲突未避让: %q", id2)
	}
	if !runIDRe.MatchString(id2) {
		t.Fatalf("避让后 run_id 形态非法: %q", id2)
	}
}

func TestNewRunAtomicallyReservesConcurrentIDs(t *testing.T) {
	root := t.TempDir()
	const workers = 32
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			id, err := NewRun(root)
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		}()
	}
	close(start)
	wait.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("并发分配run_id失败: %v", err)
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("并发NewRun返回重复身份: %s", id)
		}
		seen[id] = true
		dir, err := RunDir(root, id)
		if err != nil {
			t.Fatal(err)
		}
		if stat, err := os.Stat(dir); err != nil || !stat.IsDir() {
			t.Fatalf("返回前必须原子保留run目录(%s): stat=%v err=%v", id, stat, err)
		}
	}
	if len(seen) != workers {
		t.Fatalf("并发分配数量不足: got=%d want=%d", len(seen), workers)
	}
}

func TestRunDirRejectsEscape(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"../../etc", "20260709T120000Z/../x", "", "abc", "20260709T120000Z-"} {
		if _, err := RunDir(root, bad); err == nil {
			t.Fatalf("非法 run_id 应被拒绝: %q", bad)
		}
	}
}

func TestWriteFileNameSafety(t *testing.T) {
	root := t.TempDir()
	id, _ := NewRun(root)
	for _, bad := range []string{"", "a/b.txt", "a\\b.txt", "..", "x..y"} {
		if err := WriteFile(root, id, bad, []byte("x")); err == nil {
			t.Fatalf("非法草稿文件名应被拒绝: %q", bad)
		}
	}
}

func TestWriteReadRoundtrip(t *testing.T) {
	root := t.TempDir()
	id, _ := NewRun(root)
	content := []byte("#草稿头部第一行\n#第二行\n")
	if err := WriteFile(root, id, HeaderFileName, content); err != nil {
		t.Fatalf("写草稿失败: %v", err)
	}
	got, err := ReadFile(root, id, HeaderFileName)
	if err != nil {
		t.Fatalf("读草稿失败: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("往返内容不符: %q", got)
	}
	// 草稿必须落在 .aoci/drafts/<run_id>/ 之下
	if _, err := os.Stat(filepath.Join(root, ".aoci", "drafts", id, HeaderFileName)); err != nil {
		t.Fatalf("草稿落点不符: %v", err)
	}
}

func TestManifestRoundtripAndValidation(t *testing.T) {
	root := t.TempDir()
	id, _ := NewRun(root)
	m := &Manifest{
		RunID: id, Kind: KindHeader,
		Model: "test-model", Provider: "openai-compatible",
		EndpointHash: "cd4a9f297a66ee3b",
		InputTokens:  120, OutputTokens: 480, TokenSource: "exact",
		Files: []string{HeaderFileName},
	}
	if err := SaveManifest(root, m); err != nil {
		t.Fatalf("落盘 manifest 失败: %v", err)
	}
	if m.CreatedAt == "" {
		t.Fatal("CreatedAt 应被自动填充")
	}
	got, err := LoadManifest(root, id)
	if err != nil {
		t.Fatalf("读取 manifest 失败: %v", err)
	}
	if got.RunID != id || got.Kind != KindHeader || got.TokenSource != "exact" || got.EndpointHash != "cd4a9f297a66ee3b" {
		t.Fatalf("manifest 往返不符: %+v", got)
	}
	// kind 白名单
	if err := SaveManifest(root, &Manifest{RunID: id, Kind: "bogus"}); err == nil {
		t.Fatal("非法 kind 应被拒绝")
	}
	// run_id 形态
	if err := SaveManifest(root, &Manifest{RunID: "../x", Kind: KindHeader}); err == nil {
		t.Fatal("非法 run_id 应被拒绝")
	}
}

func TestListAndLatest(t *testing.T) {
	root := t.TempDir()
	// 草稿区不存在: 空清单零错误 + LatestRunID 返回 ErrNoDraft
	ids, err := ListRunIDs(root)
	if err != nil || len(ids) != 0 {
		t.Fatalf("空草稿区应返回空清单零错误: %v %v", ids, err)
	}
	if _, err := LatestRunID(root, ""); !errors.Is(err, ErrNoDraft) {
		t.Fatalf("空草稿区应返回 ErrNoDraft,得到: %v", err)
	}

	// 手工构造两个时间上有序的 run: 早=entries,晚=header
	early, late := "20260709T010101Z", "20260709T020202Z"
	if err := SaveManifest(root, &Manifest{RunID: early, Kind: KindEntries}); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(root, &Manifest{RunID: late, Kind: KindHeader}); err != nil {
		t.Fatal(err)
	}
	// 干扰项: 非法名目录不入清单
	if err := os.MkdirAll(filepath.Join(DraftsDir(root), "not-a-run"), 0755); err != nil {
		t.Fatal(err)
	}

	ids, err = ListRunIDs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != early || ids[1] != late {
		t.Fatalf("清单排序不符: %v", ids)
	}

	got, err := LatestRunID(root, "")
	if err != nil || got != late {
		t.Fatalf("最新 run 不符: %q %v", got, err)
	}
	got, err = LatestRunID(root, KindEntries)
	if err != nil || got != early {
		t.Fatalf("按 kind 过滤不符: %q %v", got, err)
	}
	if _, err := LatestRunID(root, "bogus"); !errors.Is(err, ErrNoDraft) {
		t.Fatalf("无匹配 kind 应返回 ErrNoDraft,得到: %v", err)
	}
}

func TestLatestPendingRunScansPastNewerCompletedRuns(t *testing.T) {
	root := t.TempDir()
	older := "20260709T030303Z"
	newer := "20260709T040404Z"
	if err := SaveManifest(root, &Manifest{RunID: older, Kind: KindEntries}); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(root, &Manifest{
		RunID: newer, Kind: KindEntries, AppliedAt: "2026-07-09T04:04:04Z",
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := LatestPendingRun(root, KindEntries)
	if err != nil || pending != older {
		t.Fatalf("an older unresolved run must not be hidden by a newer completed run: run=%q err=%v", pending, err)
	}
}

func TestLatestPendingRunSkipsSafeZeroWriteRejections(t *testing.T) {
	root := t.TempDir()
	checkRejected := "20260709T050505Z"
	applyRejected := "20260709T060606Z"
	if err := SaveManifest(root, &Manifest{
		RunID: checkRejected, Kind: KindEntries,
		Reviews: []ReviewRecord{{
			At: "2026-07-09T05:05:05Z", Action: ReviewActionCheck,
			DraftHash: strings.Repeat("a", 64), PathsCount: 2, Rejected: 1,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(root, &Manifest{
		RunID: applyRejected, Kind: KindEntries,
		Applications: []ApplicationRecord{{
			At: "2026-07-09T06:06:06Z", DraftHash: strings.Repeat("b", 64),
			PathsCount: 2, Rejected: 2, RejectKinds: "conflict",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := LatestPendingRun(root, KindEntries)
	if err != nil || pending != "" {
		t.Fatalf("safe zero-write rejections must not block a fresh Guide: run=%q err=%v", pending, err)
	}

	unsafe := "20260709T070707Z"
	if err := SaveManifest(root, &Manifest{
		RunID: unsafe, Kind: KindEntries,
		Applications: []ApplicationRecord{{
			At: "2026-07-09T07:07:07Z", DraftHash: strings.Repeat("c", 64),
			PathsCount: 2, Applied: 2, Rejected: 2, RejectKinds: "baseline_incomplete",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	pending, err = LatestPendingRun(root, KindEntries)
	if err != nil || pending != unsafe {
		t.Fatalf("post-write failure must remain pending: run=%q err=%v", pending, err)
	}
}

func TestTerminalRunResolutionRejectsMultipleTerminalClaims(t *testing.T) {
	record := RunResolutionRecord{
		At: "2026-07-09T04:04:04Z", Status: RunResolutionRecovered,
		FailureKinds: "baseline_incomplete", TransactionID: strings.Repeat("a", 64),
		PreIndexSHA256: strings.Repeat("b", 64), PostIndexSHA256: strings.Repeat("c", 64),
		CurrentIndexSHA256:     strings.Repeat("c", 64),
		CurrentBaselineSHA256:  strings.Repeat("d", 64),
		RepositorySHA256:       strings.Repeat("e", 64),
		ArchivedRecoveryAsset:  ".aoci/transactions/history/entries-" + strings.Repeat("a", 64) + ".json",
		ArchivedRecoverySHA256: strings.Repeat("f", 64),
	}
	if _, ok := TerminalRunResolution(&Manifest{
		Resolutions: []RunResolutionRecord{record, record},
	}); ok {
		t.Fatal("multiple terminal claims must fail closed")
	}
}
