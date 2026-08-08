// 跨进程索引写锁 —— 阶段 D(R36 多 agent 并发防线一): 串行化 commit 临界区
// 索引条目: lock.go(待补录,随本批入册)
//
// 定位: 保护"读索引→改→原子写→基线前移"的 commit 段,使多个 aoci 进程
// (多 agent 经 MCP / 人工 CLI / CI)并发回写时互不覆盖。plan 段保持无锁纯读
// (每次调用现读索引的第一戒律不变),锁只包 commit,持有时间为亚秒级。
//
// 方案裁决(为何不用 flock/LockFileEx):
// POSIX flock 与 Windows LockFileEx 要么引入golang.org/x/sys,要么写双份平台
// 代码。这里先完整原子写prepared owner，再以os.Link的O_EXCL语义发布canonical
// 锁文件；身份与锁可见性是同一个文件系统原子动作，两平台均只用标准库。
//
// 陈旧锁抢占: 持有者崩溃会留下孤儿锁。普通文件锁只有mtime超过阈值且owner
// 进程已被操作系统确认退出时，等待者才以hard-link tombstone和inode复核完成
// 抢占。hard-link不可用时的目录锁缺少可移植的目录Rename CAS，陈旧恢复必须
// fail-closed，绝不凭先前Stat移动可能已换代的canonical目录。
//
// token 防误删: canonical锁文件含创建者随机token,Release先读文件核对token,
// 不匹配(自己已被抢占,锁归他人)即拒绝删除 —— 防止挂起后恢复的旧持有者
// 删掉新持有者的锁。
//
// 并发语义边界(测试教训入档): 本锁建立的是进程级互斥,不建立 Go race
// detector 可见的 happens-before 边 —— 锁保护下的跨 goroutine 裸内存读写
// 仍会被 -race 如实标记。进程内共享内存的同步归 sync/atomic,本锁只管
// 跨进程文件互斥,两层职责不混。
//
// 默认锁位置: <root>/.aoci/lock。.aoci 目录名是 fs 层已有内置知识(walk.go
// 无条件排除),此处沿用不构成新依赖;fs 不 import config 的方向纪律不变。
package fs

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrLockTimeout 获取锁超时的哨兵错误(调用方可 errors.Is 识别后映射 write_conflict)
var ErrLockTimeout = errors.New("获取索引写锁超时")

const (
	// DefaultLockTimeout 等待锁的总超时 —— 超过即放弃并报错,绝不无限等待
	DefaultLockTimeout = 10 * time.Second

	// DefaultLockStale 陈旧判定阈值 —— 锁文件 mtime 距今超过此值视为持有者已崩溃
	DefaultLockStale = 60 * time.Second

	// lockBackoffStart 退避等待起点(重试 sleep 从此值倍增)
	lockBackoffStart = 25 * time.Millisecond

	// lockBackoffMax 退避等待上限
	lockBackoffMax = 500 * time.Millisecond

	directoryInitializationStale = 5 * time.Second
)

// lockContent 锁文件 JSON 内容 —— 诊断信息 + token 防误删
type lockContent struct {
	// PID 持有者进程号(诊断用,不作判据)
	PID int `json:"pid"`
	// Token 随机 token,Release 核对身份的唯一判据
	Token string `json:"token"`
	// CreatedAt 创建时刻 RFC3339(诊断用;陈旧判定用 mtime 不用本字段)
	CreatedAt string `json:"created_at"`
	// ProcessIdentity绑定操作系统进程启动身份，防PID复用被误判为原owner存活。
	ProcessIdentity string `json:"process_identity,omitempty"`
}

// Lock 已持有的锁句柄。用后必须 Release(建议 defer),忘记释放由陈旧抢占兜底。
type Lock struct {
	// path 锁文件绝对路径
	path string
	// token 本持有者的随机 token
	token string
}

// lockProcessIdentity允许测试确定性模拟已退出、仍存活或PID复用。生产实现由平台
// 文件提供；无法确认时必须返回known=false，调用方采取不抢占的安全方向。
var lockProcessIdentity = processIdentity
var writePreparedLockOwner = AtomicWrite
var afterLockOwnerRead = func() {}
var publishPreparedLockOwner = os.Link

// AcquireIndexLock 获取仓库索引写锁(默认参数)。
// 成功返回锁句柄;等待超时返回包裹 ErrLockTimeout 的错误;其他 IO 故障原样返回。
func AcquireIndexLock(root string) (*Lock, error) {
	return acquireLockWith(filepath.Join(root, ".aoci", "lock"), DefaultLockTimeout, DefaultLockStale)
}

// AcquireManifestLock串行化同一Draft Run的Manifest读改写审计。
func AcquireManifestLock(root, runID string) (*Lock, error) {
	if strings.TrimSpace(runID) == "" || filepath.Base(runID) != runID ||
		strings.ContainsAny(runID, `/\\`) {
		return nil, fmt.Errorf("manifest锁run_id非法: %q", runID)
	}
	return acquireLockWith(
		filepath.Join(root, ".aoci", "drafts", runID, "manifest.lock"),
		DefaultLockTimeout,
		DefaultLockStale,
	)
}

// AcquireEntriesRunLock把同一Entries run的Check→Diff→Apply→Application串成
// 一次治理租约；Manifest自身的细粒度锁仍可在租约内保护各次原子审计写入。
func AcquireEntriesRunLock(root, runID string) (*Lock, error) {
	if strings.TrimSpace(runID) == "" || filepath.Base(runID) != runID ||
		strings.ContainsAny(runID, `/\`) {
		return nil, fmt.Errorf("entries run锁run_id非法: %q", runID)
	}
	return acquireLockWith(
		filepath.Join(root, ".aoci", "drafts", runID, "entries-finalize.lock"),
		DefaultLockTimeout,
		DefaultLockStale,
	)
}

// acquireLockWith 带参版本(测试注入短超时/短陈旧阈值;生产入口恒走 AcquireIndexLock)
func acquireLockWith(lockPath string, timeout, stale time.Duration) (*Lock, error) {
	// 锁文件所在目录须存在(.aoci 可能尚未 init —— MkdirAll 幂等无害)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("创建锁目录失败: %w", err)
	}

	token := newLockToken()
	deadline := time.Now().Add(timeout)
	backoff := lockBackoffStart

	for {
		// 独占创建: O_EXCL 保证已存在即失败,是跨进程互斥的原子基石
		created, err := tryCreateLock(lockPath, token)
		if err != nil {
			return nil, err
		}
		if created {
			return &Lock{path: lockPath, token: token}, nil
		}

		// 锁已被占用: 检查陈旧
		observedOwner, st, stable, observeErr := observeLockOwner(lockPath)
		if observeErr != nil {
			return nil, observeErr
		}
		if stable {
			age := time.Since(st.ModTime())
			ownerDead := staleOwnerIsDead(observedOwner)
			if st.IsDir() && len(observedOwner) == 0 && age > directoryInitializationStale {
				ownerDead = true
			}
			if age > stale && ownerDead {
				moved, moveErr := quarantineStaleLock(lockPath, st, observedOwner)
				if moveErr != nil {
					return nil, moveErr
				}
				if moved {
					continue
				}
			}
		} else {
			// 刚被正常释放: 立即重试,不消耗退避
			continue
		}

		// 超时判定
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w(%s): %s 正被其他进程持有,稍后重试",
				ErrLockTimeout, timeout, lockPath)
		}

		// 退避等待(附 0~25ms 随机抖动防多等待者同步唤醒)
		jitter := time.Duration(rand.Int63n(int64(25 * time.Millisecond)))
		time.Sleep(backoff + jitter)
		backoff *= 2
		if backoff > lockBackoffMax {
			backoff = lockBackoffMax
		}
	}
}

// observeLockOwner从已打开文件描述符读取owner与FileInfo，并在返回前确认
// canonical名称仍指向同一inode。owner死亡证据与隔离对象因此不可跨代混用。
func observeLockOwner(lockPath string) ([]byte, os.FileInfo, bool, error) {
	canonicalInfo, err := os.Stat(lockPath)
	if os.IsNotExist(err) {
		return nil, nil, false, nil
	}
	if lockOwnerObservationTransient(err) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("读取锁canonical身份失败: %w", err)
	}
	ownerFile, err := openLockOwnerForObservation(lockOwnerPath(lockPath))
	if os.IsNotExist(err) && canonicalInfo.IsDir() {
		return nil, canonicalInfo, true, nil
	}
	if os.IsNotExist(err) {
		return nil, nil, false, nil
	}
	if lockOwnerObservationTransient(err) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("打开锁owner失败: %w", err)
	}
	info, err := ownerFile.Stat()
	if err != nil {
		_ = ownerFile.Close()
		return nil, nil, false, fmt.Errorf("读取锁owner身份失败: %w", err)
	}
	data, err := io.ReadAll(ownerFile)
	if err != nil {
		_ = ownerFile.Close()
		return nil, nil, false, fmt.Errorf("读取锁owner内容失败: %w", err)
	}
	// Windows不允许删除仍被其他观察者打开的锁文件。身份和正文读完后必须先
	// 关闭句柄，再进入可能与owner Release并发的名称复核阶段。
	if closeErr := ownerFile.Close(); closeErr != nil {
		return nil, nil, false, fmt.Errorf("关闭锁owner句柄失败: %w", closeErr)
	}
	afterLockOwnerRead()
	current, err := os.Stat(lockPath)
	if os.IsNotExist(err) {
		return nil, nil, false, nil
	}
	if lockOwnerObservationTransient(err) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("复核锁owner名称失败: %w", err)
	}
	if !os.SameFile(canonicalInfo, current) {
		return nil, nil, false, nil
	}
	if !canonicalInfo.IsDir() && !os.SameFile(info, current) {
		return nil, nil, false, nil
	}
	return data, canonicalInfo, true, nil
}

func staleOwnerIsDead(ownerData []byte) bool {
	var owner lockContent
	if json.Unmarshal(ownerData, &owner) != nil || owner.PID <= 0 {
		return false
	}
	identity, alive, known := lockProcessIdentity(owner.PID)
	if !known {
		return false
	}
	if !alive {
		return true
	}
	return owner.ProcessIdentity != "" && identity != "" &&
		owner.ProcessIdentity != identity
}

// tryCreateLock先准备完整身份，再以hard-link原子发布canonical锁。
func tryCreateLock(lockPath, token string) (bool, error) {
	processIdentityValue, processAliveValue, processKnown := lockProcessIdentity(os.Getpid())
	if !processKnown || !processAliveValue {
		processIdentityValue = ""
	}
	data, merr := json.Marshal(lockContent{
		PID:             os.Getpid(),
		Token:           token,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		ProcessIdentity: processIdentityValue,
	})
	if merr != nil {
		return false, fmt.Errorf("序列化锁内容失败: %w", merr)
	}
	// 先把完整owner写到不可见的唯一准备文件，再用hard-link的O_EXCL语义
	// 原子发布为canonical lockPath。进程在任一准备步骤崩溃只会留下不阻塞
	// 的prepared文件，其他进程绝不会观察到无身份或半身份的正式锁。
	prepared := lockPath + ".prepared." + token
	if werr := writePreparedLockOwner(prepared, data); werr != nil {
		_ = os.Remove(prepared)
		return false, fmt.Errorf("写入锁文件失败: %w", werr)
	}
	linkErr := publishPreparedLockOwner(prepared, lockPath)
	if linkErr != nil && !os.IsExist(linkErr) && !os.IsNotExist(linkErr) {
		return publishPreparedDirectoryLock(lockPath, prepared)
	}
	removeErr := os.Remove(prepared)
	if linkErr != nil {
		if os.IsExist(linkErr) {
			return false, nil
		}
		return false, fmt.Errorf("原子发布锁身份失败: %w", linkErr)
	}
	if removeErr != nil && !os.IsNotExist(removeErr) {
		// canonical hard-link已成功发布；prepared清理失败不影响锁身份完整性。
		return true, nil
	}
	return true, nil
}

// publishPreparedDirectoryLock为不支持hard-link的文件系统提供完整owner发布：
// Mkdir原子占位后把已fsync的prepared owner移动进私有目录。初始化者若在两步
// 之间退出，只留下可按mtime回收的空目录，不会暴露半份owner。
func publishPreparedDirectoryLock(lockPath, prepared string) (bool, error) {
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		_ = os.Remove(prepared)
		if os.IsExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("发布目录锁失败: %w", err)
	}
	ownerPath := filepath.Join(lockPath, "owner.json")
	if err := os.Rename(prepared, ownerPath); err != nil {
		_ = os.Remove(prepared)
		_ = os.Remove(lockPath)
		return false, fmt.Errorf("发布目录锁owner失败: %w", err)
	}
	return true, nil
}

func lockOwnerPath(lockPath string) string {
	if info, err := os.Stat(lockPath); err == nil && info.IsDir() {
		return filepath.Join(lockPath, "owner.json")
	}
	return lockPath
}

// quarantineStaleLock为已观察锁建立持久hard-link tombstone，核对inode仍
// 等于观察对象后才删除canonical入口。第二个等待者无法覆盖同一tombstone。
func quarantineStaleLock(
	lockPath string,
	observed os.FileInfo,
	observedOwner []byte,
) (bool, error) {
	if observed.IsDir() {
		sameGeneration, err := sameDirectoryLockGeneration(lockPath, observed, observedOwner)
		if err != nil {
			return false, err
		}
		if !sameGeneration {
			return false, nil
		}
		// POSIX rename和Windows MoveFile都不能表达"仅当目录仍是该inode/token"
		// 的原子条件。即使此刻复核同代，复核后到Rename前仍可被正常释放并换代；
		// 因此保留canonical并明确失败，不能用TOCTOU冒充CAS。
		return false, fmt.Errorf(
			"%w: 陈旧目录锁缺少可移植的原子代次CAS,拒绝抢占: %s",
			ErrLockTimeout,
			lockPath,
		)
	}
	tombstone := staleLockTombstonePath(lockPath, observed, observedOwner)
	if err := os.Link(lockPath, tombstone); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		if os.IsExist(err) {
			// 上一恢复者可能在发布tombstone后、删除canonical前退出。只有三者
			// 仍指向同一已观察inode时，后继恢复者才接力完成删除；canonical已
			// 被新owner替换时绝不触碰。
			tombstoneInfo, tombstoneErr := os.Stat(tombstone)
			currentInfo, currentErr := os.Stat(lockPath)
			if tombstoneErr != nil || currentErr != nil ||
				!os.SameFile(observed, tombstoneInfo) || !os.SameFile(observed, currentInfo) {
				return false, nil
			}
			if removeErr := os.Remove(lockPath); removeErr != nil {
				if os.IsNotExist(removeErr) {
					return false, nil
				}
				return false, fmt.Errorf("接力移除陈旧锁入口失败: %w", removeErr)
			}
			return true, nil
		}
		return false, fmt.Errorf("隔离陈旧锁失败: %w", err)
	}
	tombstoneInfo, tombstoneErr := os.Stat(tombstone)
	currentInfo, currentErr := os.Stat(lockPath)
	if tombstoneErr != nil || currentErr != nil ||
		!os.SameFile(observed, tombstoneInfo) || !os.SameFile(observed, currentInfo) {
		_ = os.Remove(tombstone)
		return false, nil
	}
	if err := os.Remove(lockPath); err != nil {
		_ = os.Remove(tombstone)
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("移除陈旧锁入口失败: %w", err)
	}
	return true, nil
}

// sameDirectoryLockGeneration只做无副作用代次复核，不授权目录移动。目录inode
// 可能被快速复用，所以必须同时绑定owner token；任一证据不稳定都按不同代处理。
func sameDirectoryLockGeneration(
	lockPath string,
	observed os.FileInfo,
	observedOwner []byte,
) (bool, error) {
	current, err := os.Stat(lockPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if lockOwnerObservationTransient(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("复核目录锁身份失败: %w", err)
	}
	if !current.IsDir() || !os.SameFile(observed, current) {
		return false, nil
	}
	var oldContent lockContent
	if json.Unmarshal(observedOwner, &oldContent) != nil || oldContent.Token == "" {
		// 初始化中空目录没有可验证owner代次，不能抢占。
		return len(observedOwner) == 0, nil
	}
	ownerFile, err := openLockOwnerForObservation(lockOwnerPath(lockPath))
	if os.IsNotExist(err) {
		return false, nil
	}
	if lockOwnerObservationTransient(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("复核目录锁owner失败: %w", err)
	}
	currentOwner, readErr := io.ReadAll(ownerFile)
	closeErr := ownerFile.Close()
	if readErr != nil {
		return false, fmt.Errorf("读取目录锁owner失败: %w", readErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("关闭目录锁owner句柄失败: %w", closeErr)
	}
	var currentContent lockContent
	if json.Unmarshal(currentOwner, &currentContent) != nil ||
		currentContent.Token == "" || currentContent.Token != oldContent.Token {
		return false, nil
	}
	after, err := os.Stat(lockPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if lockOwnerObservationTransient(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("再次复核目录锁身份失败: %w", err)
	}
	return after.IsDir() && os.SameFile(current, after), nil
}

func staleLockTombstonePath(lockPath string, observed os.FileInfo, observedOwner []byte) string {
	identityInput := append(append([]byte{}, observedOwner...), []byte(
		fmt.Sprintf("\x00%d", observed.ModTime().UnixNano()),
	)...)
	identity := fmt.Sprintf("%x", sha256.Sum256(identityInput))
	return lockPath + ".stale." + identity
}

// Release 释放锁。幂等:锁文件已不存在(被陈旧抢占后清理)返回 nil。
// 文件存在但 token 不匹配 = 自己已被抢占、锁归新持有者 —— 拒绝删除并返回错误
// (调用方仅记警告,绝不重试删除)。正常生产抢占只发生在操作系统已经确认
// owner进程退出后；token核对继续防御目录被人工替换或身份损坏的异常场景。
func (l *Lock) Release() error {
	data, err := os.ReadFile(lockOwnerPath(l.path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取锁文件失败: %w", err)
	}
	var c lockContent
	if jerr := json.Unmarshal(data, &c); jerr == nil && c.Token != l.token {
		return fmt.Errorf("锁已被其他进程抢占持有(token 不匹配),拒绝删除他人锁: %s", l.path)
	}
	// token 匹配或内容损坏(损坏视为遗留物照删)均执行删除
	if info, statErr := os.Stat(l.path); statErr == nil && info.IsDir() {
		if rerr := os.Remove(lockOwnerPath(l.path)); rerr != nil && !os.IsNotExist(rerr) {
			return fmt.Errorf("删除目录锁owner失败: %w", rerr)
		}
	}
	if rerr := os.Remove(l.path); rerr != nil && !os.IsNotExist(rerr) {
		return fmt.Errorf("删除锁文件失败: %w", rerr)
	}
	return nil
}

// newLockToken 生成随机 token(pid + 纳秒 + 随机数,同机同刻双进程也不撞)
func newLockToken() string {
	return fmt.Sprintf("%d-%d-%d", os.Getpid(), time.Now().UnixNano(), rand.Int63())
}
