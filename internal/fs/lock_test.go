// Table-driven and concurrency tests for lock.go
// Index entry: lock_test.go (pending; enrolled with this batch)
//
// Coverage: acquire/release round trip, contention timeout (ErrLockTimeout recognizable), stale-lock takeover,
// the token against wrong deletion (Release refuses someone else's lock), idempotent release, in-process mutual exclusion.
// Every case builds its repository in t.TempDir without a real filesystem dependency; short timeouts and staleness
// thresholds are injected through the internal acquireLockWith, keeping the production defaults (10s/60s would slow the tests) off the test surface.
//
// Instrument lesson of the mutual-exclusion case (the first version's data race, on record): a file lock establishes
// no happens-before edge the race detector can see, and bare memory access under the lock is still reported by -race,
// so the first version's bare int counter being shot down by -race was an instrument error, not a lock defect. The right
// instrument is atomic.Int32 critical-section overlap detection: reading a value other than 1 after Add(1) on entry
// proves the exclusion failed; atomic operations are legal to the detector, and the overlap assertion proves the exclusion semantics directly.
package fs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lockTestPath is the lock file path inside a test repository (helpers carry the lock prefix against same-package name clashes, the mkEScaleEntry lesson)
func lockTestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".aoci", "lock")
}

// TestLockAcquireRelease is the basic round trip: the lock file exists after acquire and is gone after release
func TestLockAcquireRelease(t *testing.T) {
	p := lockTestPath(t)
	l, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("获取锁失败: %v", err)
	}
	if _, serr := os.Stat(p); serr != nil {
		t.Fatalf("持有期间锁文件应存在: %v", serr)
	}
	// The lock file should carry this holder's pid and a non-empty token (a spot check of the diagnostic fields)
	data, rerr := os.ReadFile(lockOwnerPath(p))
	if rerr != nil {
		t.Fatalf("读取锁文件失败: %v", rerr)
	}
	var c lockContent
	if jerr := json.Unmarshal(data, &c); jerr != nil || c.PID != os.Getpid() || c.Token == "" {
		t.Fatalf("锁文件内容异常: %s (err=%v)", data, jerr)
	}
	if relErr := l.Release(); relErr != nil {
		t.Fatalf("释放失败: %v", relErr)
	}
	if _, serr := os.Stat(p); !os.IsNotExist(serr) {
		t.Fatalf("释放后锁文件应消失, stat err=%v", serr)
	}
}

// TestLockContention: while held, a second acquirer fails on a short timeout, and the error is recognizable with errors.Is
func TestLockContention(t *testing.T) {
	p := lockTestPath(t)
	l1, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("首个获取失败: %v", err)
	}
	defer l1.Release()

	_, err2 := acquireLockWith(p, 200*time.Millisecond, time.Minute)
	if err2 == nil {
		t.Fatalf("争用场景第二个获取者应超时失败")
	}
	if !errors.Is(err2, ErrLockTimeout) {
		t.Fatalf("超时错误应可经 errors.Is(ErrLockTimeout) 识别, got: %v", err2)
	}
}

// TestLockStalePreempt: an orphan lock whose mtime aged past the threshold is removed and the acquire succeeds
func TestLockStalePreempt(t *testing.T) {
	p := lockTestPath(t)
	previousIdentity := lockProcessIdentity
	lockProcessIdentity = func(int) (string, bool, bool) { return "", false, true }
	t.Cleanup(func() { lockProcessIdentity = previousIdentity })
	// Build the orphan lock by hand (simulating a crashed holder): create the directory, then turn its mtime back
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"pid":1,"token":"dead","created_at":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}

	l, err := acquireLockWith(p, time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("陈旧锁应被抢占, 获取失败: %v", err)
	}
	defer l.Release()
	// After the takeover the lock file carries the new holder's token, not dead
	data, rerr := os.ReadFile(lockOwnerPath(p))
	if rerr != nil {
		t.Fatalf("读取锁文件失败: %v", rerr)
	}
	var c lockContent
	if json.Unmarshal(data, &c) != nil || c.Token == "dead" {
		t.Fatalf("抢占后锁文件应属新持有者: %s", data)
	}
}

func TestStaleLockOwnedByLiveProcessCannotBePreempted(t *testing.T) {
	p := lockTestPath(t)
	previousIdentity := lockProcessIdentity
	lockProcessIdentity = func(int) (string, bool, bool) { return "same", true, true }
	t.Cleanup(func() { lockProcessIdentity = previousIdentity })
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"pid":42,"token":"paused","created_at":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}

	_, err := acquireLockWith(p, 100*time.Millisecond, time.Millisecond)
	if err == nil || !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("活进程的陈旧mtime不得触发抢占: %v", err)
	}
	data, readErr := os.ReadFile(lockOwnerPath(p))
	if readErr != nil || !strings.Contains(string(data), `"token":"paused"`) {
		t.Fatalf("活持有者身份不得被替换: err=%v data=%s", readErr, data)
	}
	os.RemoveAll(p)
}

func TestStaleLockWithReusedPIDCanBePreempted(t *testing.T) {
	p := lockTestPath(t)
	previousIdentity := lockProcessIdentity
	lockProcessIdentity = func(int) (string, bool, bool) { return "new-process", true, true }
	t.Cleanup(func() { lockProcessIdentity = previousIdentity })
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	owner := lockContent{PID: 42, Token: "old", ProcessIdentity: "old-process"}
	data, _ := json.Marshal(owner)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLockWith(p, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("PID已复用时旧锁应可恢复: %v", err)
	}
	defer lock.Release()
}

func TestOwnerWriteFailureNeverPublishesCanonicalLock(t *testing.T) {
	p := lockTestPath(t)
	previousWrite := writePreparedLockOwner
	writePreparedLockOwner = func(path string, data []byte) error {
		if err := os.WriteFile(path, data[:len(data)/2], 0o644); err != nil {
			return err
		}
		return errors.New("injected owner write failure")
	}
	t.Cleanup(func() { writePreparedLockOwner = previousWrite })
	created, err := tryCreateLock(p, "partial")
	if err == nil || created {
		t.Fatalf("owner写失败必须拒绝持锁: created=%v err=%v", created, err)
	}
	if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
		t.Fatalf("部分owner不得发布canonical lock: %v", statErr)
	}
}

func TestLockFallsBackToDirectoryWhenHardLinkUnavailable(t *testing.T) {
	p := lockTestPath(t)
	previousPublish := publishPreparedLockOwner
	publishPreparedLockOwner = func(string, string) error {
		return errors.New("injected hard-link unsupported")
	}
	t.Cleanup(func() { publishPreparedLockOwner = previousPublish })
	lock, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("目录锁降级获取失败: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		t.Fatalf("hard-link不可用时应发布目录锁: err=%v info=%v", err, info)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("目录锁释放失败: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("目录锁释放后canonical应消失: %v", err)
	}
}

func TestLateStaleWaiterCannotQuarantineReplacementLock(t *testing.T) {
	p := lockTestPath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if created, err := tryCreateLock(p, "old-token"); err != nil || !created {
		t.Fatalf("创建旧锁失败: created=%v err=%v", created, err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	observedOwner, err := os.ReadFile(lockOwnerPath(p))
	if err != nil {
		t.Fatal(err)
	}
	moved, err := quarantineStaleLock(p, observed, observedOwner)
	if err != nil || !moved {
		t.Fatalf("首个等待者应隔离旧锁: moved=%v err=%v", moved, err)
	}
	if created, err := tryCreateLock(p, "new-token"); err != nil || !created {
		t.Fatalf("创建替代锁失败: created=%v err=%v", created, err)
	}

	moved, err = quarantineStaleLock(p, observed, observedOwner)
	if err != nil || moved {
		t.Fatalf("晚到等待者不得移动替代锁: moved=%v err=%v", moved, err)
	}
	data, err := os.ReadFile(lockOwnerPath(p))
	var content lockContent
	if err != nil || json.Unmarshal(data, &content) != nil || content.Token != "new-token" {
		t.Fatalf("替代锁身份被陈旧等待者破坏: err=%v data=%s", err, data)
	}
	os.RemoveAll(p)
}

// TestDirectoryLateStaleWaiterCannotQuarantineReplacementLock covers the real directory-lock generation race
// where hard links are unavailable: while a waiter holds observation evidence of the old generation, the old owner
// releases and a new owner takes the same canonical name; the late waiter must not move or delete the new generation's directory lock.
func TestDirectoryLateStaleWaiterCannotQuarantineReplacementLock(t *testing.T) {
	p := lockTestPath(t)
	previousPublish := publishPreparedLockOwner
	publishPreparedLockOwner = func(string, string) error {
		return errors.New("injected hard-link unsupported")
	}
	t.Cleanup(func() { publishPreparedLockOwner = previousPublish })

	oldLock, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("创建旧代目录锁失败: %v", err)
	}
	observedOwner, observed, stable, err := observeLockOwner(p)
	if err != nil || !stable || observed == nil || !observed.IsDir() {
		t.Fatalf("观察旧代目录锁失败: stable=%v observed=%v err=%v", stable, observed, err)
	}

	startLateWaiter := make(chan struct{})
	result := make(chan struct {
		moved bool
		err   error
	}, 1)
	go func() {
		<-startLateWaiter
		moved, quarantineErr := quarantineStaleLock(p, observed, observedOwner)
		result <- struct {
			moved bool
			err   error
		}{moved: moved, err: quarantineErr}
	}()

	if err := oldLock.Release(); err != nil {
		t.Fatalf("释放旧代目录锁失败: %v", err)
	}
	newLock, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("创建新代目录锁失败: %v", err)
	}
	close(startLateWaiter)
	late := <-result
	if late.err != nil || late.moved {
		t.Fatalf("晚到等待者不得隔离新代目录锁: moved=%v err=%v", late.moved, late.err)
	}
	data, err := os.ReadFile(lockOwnerPath(p))
	var content lockContent
	if err != nil || json.Unmarshal(data, &content) != nil || content.Token != newLock.token {
		t.Fatalf("新代目录锁身份被旧等待者破坏: err=%v data=%s", err, data)
	}
	if err := newLock.Release(); err != nil {
		t.Fatalf("释放新代目录锁失败: %v", err)
	}
}

func TestStaleDirectoryLockRecoveryFailsClosed(t *testing.T) {
	p := lockTestPath(t)
	previousPublish := publishPreparedLockOwner
	publishPreparedLockOwner = func(string, string) error {
		return errors.New("injected hard-link unsupported")
	}
	t.Cleanup(func() { publishPreparedLockOwner = previousPublish })

	lock, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("创建目录锁失败: %v", err)
	}
	owner, observed, stable, err := observeLockOwner(p)
	if err != nil || !stable || observed == nil || !observed.IsDir() {
		t.Fatalf("观察目录锁失败: stable=%v observed=%v err=%v", stable, observed, err)
	}
	moved, err := quarantineStaleLock(p, observed, owner)
	if moved || !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("无法原子证明目录代次时必须fail-closed: moved=%v err=%v", moved, err)
	}
	data, readErr := os.ReadFile(lockOwnerPath(p))
	var content lockContent
	if readErr != nil || json.Unmarshal(data, &content) != nil || content.Token != lock.token {
		t.Fatalf("fail-closed不得破坏目录锁: err=%v data=%s", readErr, data)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("原owner仍须可正常释放目录锁: %v", err)
	}
}

func TestStaleLockQuarantineCrashCanBeCompleted(t *testing.T) {
	p := lockTestPath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if created, err := tryCreateLock(p, "dead-token"); err != nil || !created {
		t.Fatalf("创建旧锁失败: created=%v err=%v", created, err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tombstone := staleLockTombstonePath(p, observed, owner)
	if err := os.Link(p, tombstone); err != nil {
		t.Fatalf("模拟恢复者发布tombstone失败: %v", err)
	}

	moved, err := quarantineStaleLock(p, observed, owner)
	if err != nil || !moved {
		t.Fatalf("后继恢复者应接力完成canonical删除: moved=%v err=%v", moved, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("canonical陈旧锁应被删除: %v", err)
	}
	tombstoneInfo, err := os.Stat(tombstone)
	if err != nil || !os.SameFile(observed, tombstoneInfo) {
		t.Fatalf("持久tombstone必须保留旧owner证据: err=%v", err)
	}
}

func TestLockOwnerEvidenceCannotCrossReplacementInode(t *testing.T) {
	p := lockTestPath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"pid":1,"token":"dead"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	replacement := p + ".replacement"
	if err := os.WriteFile(replacement, []byte(`{"pid":2,"token":"live"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	previousHook := afterLockOwnerRead
	afterLockOwnerRead = func() {
		if err := os.Rename(replacement, p); err != nil {
			t.Fatalf("替换canonical锁失败: %v", err)
		}
	}
	t.Cleanup(func() { afterLockOwnerRead = previousHook })

	owner, info, stable, err := observeLockOwner(p)
	if err != nil || stable || info != nil || owner != nil {
		t.Fatalf("跨inode的owner与stat证据必须作废: stable=%v info=%v owner=%s err=%v",
			stable, info, owner, err)
	}
	data, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(data), `"token":"live"`) {
		t.Fatalf("替代锁必须保持完整: err=%v data=%s", err, data)
	}
}

// TestLockReleaseForeign, the token against wrong deletion: once the lock was taken over (its content replaced by someone else's token) Release refuses the delete
func TestLockReleaseForeign(t *testing.T) {
	p := lockTestPath(t)
	l, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the takeover: overwrite with someone else's lock content
	foreign := `{"pid":99999,"token":"someone-else","created_at":""}`
	if werr := os.WriteFile(lockOwnerPath(p), []byte(foreign), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if relErr := l.Release(); relErr == nil {
		t.Fatalf("token 不匹配时 Release 应拒绝删除他人锁")
	}
	// The other holder's lock file must be left as it was
	data, rerr := os.ReadFile(lockOwnerPath(p))
	if rerr != nil {
		t.Fatalf("读取锁文件失败: %v", rerr)
	}
	if string(data) != foreign {
		t.Fatalf("他人锁被破坏: %s", data)
	}
	os.RemoveAll(p)
}

// TestLockReleaseIdempotent: Release returns nil when the lock file is already gone (cleaned up by a takeover)
func TestLockReleaseIdempotent(t *testing.T) {
	p := lockTestPath(t)
	l, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if rerr := os.RemoveAll(p); rerr != nil {
		t.Fatal(rerr)
	}
	if relErr := l.Release(); relErr != nil {
		t.Fatalf("锁文件已消失时 Release 应幂等返回 nil, got: %v", relErr)
	}
}

// TestLockMutualExclusion, in-process concurrent mutual exclusion through atomic critical-section overlap detection (the instrument decision is in the file header).
// O_EXCL applies to in-process goroutines just the same, a fair proxy for the cross-process exclusion semantics.
func TestLockMutualExclusion(t *testing.T) {
	p := lockTestPath(t)
	const workers, rounds = 8, 5

	// inCrit: holders inside the critical section at once, always 1; total: completed critical-section rounds
	var inCrit atomic.Int32
	var total atomic.Int32
	var overlap atomic.Bool

	// The proposition is that the exclusion holds and every round completes; the waiters' patience on a slow machine
	// is not the proposition. The acquire loop backs off to 500ms and promises no fairness, so under eight
	// contenders one worker may lose every wake-up in a row and exceed 10s on a loaded Windows runner
	// (full-confidence 2026-09-11: 32 of 40 rounds completed, no overlap). The timeout stays far below the
	// staleness threshold, so a lock left by a broken Release still fails by timeout rather than being judged stale, quarantined, and stolen.
	const acquireTimeout, staleHorizon = 2 * time.Minute, 5 * time.Minute

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				l, err := acquireLockWith(p, acquireTimeout, staleHorizon)
				if err != nil {
					t.Errorf("并发获取锁失败: %v", err)
					return
				}
				// Entering the critical section: anyone already inside means the exclusion has failed
				if cur := inCrit.Add(1); cur != 1 {
					overlap.Store(true)
				}
				time.Sleep(time.Millisecond) // widen the overlap observation window
				inCrit.Add(-1)
				total.Add(1)
				if relErr := l.Release(); relErr != nil {
					t.Errorf("并发释放失败: %v", relErr)
					return
				}
			}
		}()
	}
	wg.Wait()
	if overlap.Load() {
		t.Fatalf("互斥失效: 观测到多个持有者同时处于临界区")
	}
	if got := total.Load(); got != workers*rounds {
		t.Fatalf("完成轮次不足: got=%d, want %d(存在获取或释放失败)", got, workers*rounds)
	}
}
