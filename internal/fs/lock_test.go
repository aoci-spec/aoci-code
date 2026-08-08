// lock.go 的表驱动与并发测试
// 索引条目: lock_test.go(待补录,随本批入册)
//
// 覆盖面: 获取/释放往返、争用超时(ErrLockTimeout 可识别)、陈旧锁抢占、
// token 防误删(Release 拒删他人锁)、释放幂等、进程内并发互斥。
// 全用例 t.TempDir 造仓不依赖真实文件系统;短超时/短陈旧阈值经内部函数
// acquireLockWith 注入,生产默认参数不进测试面(10s/60s 会拖慢测试)。
//
// 互斥用例的观测仪器教训(初版数据竞争入档): 文件锁不建立 race detector
// 可见的 happens-before 边,锁保护下的裸内存读写仍被 -race 如实标记 ——
// 初版用裸 int 计数器被 -race 击落属仪器错误而非锁缺陷。正确仪器 =
// atomic.Int32 临界区重叠探测: 进临界区 Add(1) 后读到值 ≠1 即证互斥失效,
// 原子操作对 detector 合法,重叠断言直接证明互斥语义本身。
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

// lockTestPath 测试仓内锁文件路径(辅助函数带 lock 前缀防同包撞名 —— mkEScaleEntry 教训同族)
func lockTestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".aoci", "lock")
}

// TestLockAcquireRelease 基本往返: 获取后锁文件存在,释放后消失
func TestLockAcquireRelease(t *testing.T) {
	p := lockTestPath(t)
	l, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("获取锁失败: %v", err)
	}
	if _, serr := os.Stat(p); serr != nil {
		t.Fatalf("持有期间锁文件应存在: %v", serr)
	}
	// 锁文件内容应含本持有者 pid 与非空 token(诊断字段抽查)
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

// TestLockContention 争用: 已被持有时第二个获取者短超时失败,且错误可经 errors.Is 识别
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

// TestLockStalePreempt 陈旧抢占: mtime 老化超过阈值的孤儿锁被删除后成功获取
func TestLockStalePreempt(t *testing.T) {
	p := lockTestPath(t)
	previousIdentity := lockProcessIdentity
	lockProcessIdentity = func(int) (string, bool, bool) { return "", false, true }
	t.Cleanup(func() { lockProcessIdentity = previousIdentity })
	// 手工制造孤儿锁(模拟持有者崩溃): 建目录后把 mtime 拨回过去
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
	// 抢占后锁文件应是新持有者的 token 而非 dead
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

// TestDirectoryLateStaleWaiterCannotQuarantineReplacementLock覆盖hard-link
// 不可用时的真实目录锁代次竞态：等待者持有旧代观察证据期间，旧owner释放且
// 新owner取得同名canonical；晚到等待者不得移动或删除新代目录锁。
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

// TestLockReleaseForeign token 防误删: 锁被抢占(内容换成他人 token)后 Release 拒绝删除
func TestLockReleaseForeign(t *testing.T) {
	p := lockTestPath(t)
	l, err := acquireLockWith(p, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟被抢占: 覆盖为他人的锁内容
	foreign := `{"pid":99999,"token":"someone-else","created_at":""}`
	if werr := os.WriteFile(lockOwnerPath(p), []byte(foreign), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if relErr := l.Release(); relErr == nil {
		t.Fatalf("token 不匹配时 Release 应拒绝删除他人锁")
	}
	// 他人锁文件必须原样保留
	data, rerr := os.ReadFile(lockOwnerPath(p))
	if rerr != nil {
		t.Fatalf("读取锁文件失败: %v", rerr)
	}
	if string(data) != foreign {
		t.Fatalf("他人锁被破坏: %s", data)
	}
	os.RemoveAll(p)
}

// TestLockReleaseIdempotent 释放幂等: 锁文件已消失(被抢占清理)时 Release 返回 nil
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

// TestLockMutualExclusion 进程内并发互斥: atomic 临界区重叠探测(仪器裁决见文件头注释)。
// O_EXCL 对进程内 goroutine 同样生效,是跨进程互斥语义的合理代理。
func TestLockMutualExclusion(t *testing.T) {
	p := lockTestPath(t)
	const workers, rounds = 8, 5

	// inCrit: 同时处于临界区的持有者数,恒应为 1;total: 完成的临界区轮次
	var inCrit atomic.Int32
	var total atomic.Int32
	var overlap atomic.Bool

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				l, err := acquireLockWith(p, 10*time.Second, time.Minute)
				if err != nil {
					t.Errorf("并发获取锁失败: %v", err)
					return
				}
				// 进临界区: 若已有他人在内,互斥即已失效
				if cur := inCrit.Add(1); cur != 1 {
					overlap.Store(true)
				}
				time.Sleep(time.Millisecond) // 拉大重叠观测窗口
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
