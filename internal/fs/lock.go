// Cross-process index write lock: stage D (R36, the first multi-agent concurrency defence) serializing the commit critical section
// Index entry: lock.go (pending; enrolled with this batch)
//
// Position: protects the "read index, change, atomic write, Baseline advance" commit segment so that several
// aoci processes (agents over MCP, a manual CLI, CI) writing concurrently do not overwrite each other. The plan
// segment stays lock-free and purely read (the first commandment, reading the index fresh on every call, stands); the lock wraps only commit and is held for sub-second spans.
//
// Design decision (why not flock/LockFileEx):
// POSIX flock and Windows LockFileEx either pull in golang.org/x/sys or mean two platform code paths. Here the
// prepared owner is written atomically and in full first, then published as the canonical lock file with
// os.Link's O_EXCL semantics; identity and lock visibility are one filesystem atomic action, standard library only on both platforms.
//
// Stale-lock takeover: a crashed holder leaves an orphan lock. For an ordinary file lock, only when the mtime is
// past the threshold and the operating system confirms the owner process has exited does a waiter complete the
// takeover with a hard-link tombstone and an inode re-check. The directory lock used where hard links are
// unavailable has no portable directory rename CAS, so stale recovery must fail closed and never moves a canonical directory an earlier Stat may have seen replaced.
//
// Token against wrong deletion: the canonical lock file carries its creator's random token; Release reads the
// file and checks the token first, and a mismatch (this holder was taken over, the lock belongs to someone
// else) refuses the delete, so a holder resuming after a hang cannot delete the new holder's lock.
//
// Concurrency boundary (a lesson from the tests): this lock establishes process-level mutual exclusion, not a
// happens-before edge the Go race detector can see; bare cross-goroutine memory access under the lock is
// still reported by -race, as it should be. In-process shared memory belongs to sync/atomic; this lock
// handles cross-process file mutual exclusion only, and the two layers do not mix.
//
// Default lock location: <root>/.aoci/lock. The .aoci directory name is knowledge the fs layer already holds
// (walk.go excludes it unconditionally), so reusing it adds no dependency; the rule that fs does not import config stands.
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
	"time"
)

// ErrLockTimeout is the sentinel for a lock wait that timed out (callers recognize it with errors.Is and map it to write_conflict)
var ErrLockTimeout = errors.New("获取索引写锁超时")

const (
	// DefaultLockTimeout is the total time to wait for the lock; past it the wait gives up with an error, never waiting forever
	DefaultLockTimeout = 10 * time.Second

	// DefaultLockStale is the staleness threshold: a lock file whose mtime is older than this reads as a crashed holder
	DefaultLockStale = 60 * time.Second

	// lockBackoffStart is the first back-off wait (each retry doubles the sleep from here)
	lockBackoffStart = 25 * time.Millisecond

	// lockBackoffMax caps the back-off wait
	lockBackoffMax = 500 * time.Millisecond

	directoryInitializationStale = 5 * time.Second
)

// lockContent is the lock file's JSON: diagnostics plus the token against wrong deletion
type lockContent struct {
	// PID is the holder's process id (diagnostic, never a criterion)
	PID int `json:"pid"`
	// Token is the random token, the only criterion Release checks identity by
	Token string `json:"token"`
	// CreatedAt is the creation time in RFC3339 (diagnostic; staleness uses mtime, not this field)
	CreatedAt string `json:"created_at"`
	// ProcessIdentity binds the operating system's process start identity so PID reuse is not mistaken for the original owner being alive.
	ProcessIdentity string `json:"process_identity,omitempty"`
}

// Lock is a held lock handle. Release it after use (defer is recommended); a forgotten release is backed by stale takeover.
type Lock struct {
	// path is the lock file's absolute path
	path string
	// token is this holder's random token
	token string
}

// lockProcessIdentity lets tests simulate an exited, a live, or a reused PID deterministically. Production
// implementations come from the platform files; when it cannot be confirmed, known=false must be returned and the caller takes the no-takeover direction.
var lockProcessIdentity = processIdentity
var writePreparedLockOwner = AtomicWrite
var afterLockOwnerRead = func() {}

// statOpenedLockOwner is the fstat behind observeLockOwner; a variable only so
// a test can make it answer the way 9p does for an unlinked file.
var statOpenedLockOwner = func(file *os.File) (os.FileInfo, error) { return file.Stat() }
var publishPreparedLockOwner = os.Link

// AcquireIndexLock acquires the repository index write lock with the default parameters.
// Success returns the lock handle; a timed-out wait returns an error wrapping ErrLockTimeout; other IO failures are returned as they are.
func AcquireIndexLock(root string) (*Lock, error) {
	return acquireLockWith(filepath.Join(root, ".aoci", "lock"), DefaultLockTimeout, DefaultLockStale)
}

// acquireLockWith is the parameterized form (tests inject a short timeout or staleness threshold; production always enters through AcquireIndexLock)
func acquireLockWith(lockPath string, timeout, stale time.Duration) (*Lock, error) {
	// The lock's directory must exist (.aoci may not be initialized yet; MkdirAll is idempotent and harmless)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("创建锁目录失败: %w", err)
	}

	token := newLockToken()
	deadline := time.Now().Add(timeout)
	backoff := lockBackoffStart

	for {
		// Exclusive creation: O_EXCL fails when the file exists, the atomic cornerstone of cross-process mutual exclusion
		created, err := tryCreateLock(lockPath, token)
		if err != nil {
			return nil, err
		}
		if created {
			return &Lock{path: lockPath, token: token}, nil
		}

		// The lock is taken: check for staleness
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
			// Just released normally: retry at once without spending back-off
			continue
		}

		// Timeout judgement
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w(%s): %s 正被其他进程持有,稍后重试",
				ErrLockTimeout, timeout, lockPath)
		}

		// Back-off wait (with 0 to 25 ms of random jitter so several waiters do not wake together)
		jitter := time.Duration(rand.Int63n(int64(25 * time.Millisecond)))
		time.Sleep(backoff + jitter)
		backoff *= 2
		if backoff > lockBackoffMax {
			backoff = lockBackoffMax
		}
	}
}

// observeLockOwner reads the owner and FileInfo from an open descriptor and confirms before returning that
// the canonical name still points at the same inode, so owner-death evidence and the quarantine target never mix generations.
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
	info, err := statOpenedLockOwner(ownerFile)
	if err != nil {
		_ = ownerFile.Close()
		// A 9p mount (WSL DrvFs) answers ENOENT to fstat on a file unlinked
		// after it was opened: the owner released between open and stat.
		// That is the same release the name checks below would report, not
		// a failure to read the owner (#116 follow-up).
		if os.IsNotExist(err) || lockOwnerObservationTransient(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("读取锁owner身份失败: %w", err)
	}
	data, err := io.ReadAll(ownerFile)
	if err != nil {
		_ = ownerFile.Close()
		if os.IsNotExist(err) || lockOwnerObservationTransient(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("读取锁owner内容失败: %w", err)
	}
	// Windows does not allow deleting a lock file another observer still holds open. Once the identity and
	// body are read the handle must be closed before the name re-check that may race with the owner's Release.
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

// tryCreateLock prepares the complete identity first, then publishes the canonical lock atomically with a hard link.
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
	// Write the complete owner to an invisible unique prepared file first, then publish it atomically as the
	// canonical lockPath with hard-link O_EXCL semantics. A process crashing in any preparation step leaves only
	// a non-blocking prepared file; no other process ever observes a formal lock without, or with half of, an identity.
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
		// The canonical hard link is published; a failed prepared-file cleanup does not affect the lock's identity.
		return true, nil
	}
	return true, nil
}

// publishPreparedDirectoryLock publishes a complete owner on filesystems without hard links: Mkdir takes the
// place atomically, then the fsynced prepared owner moves into the private directory. An initializer exiting
// between the two steps leaves only an empty directory reclaimable by mtime, never a half owner.
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

// quarantineStaleLock creates a persistent hard-link tombstone for the observed lock and deletes the canonical
// entry only after re-checking that the inode still equals the observed one. A second waiter cannot overwrite the same tombstone.
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
		// Neither POSIX rename nor Windows MoveFile can express the atomic condition "only while the directory is
		// still this inode/token". Even a same-generation re-check can be followed by a normal release and a new
		// generation before the Rename, so the canonical entry stays and the failure is explicit: TOCTOU must not pose as CAS.
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
			// The previous recoverer may have exited after publishing the tombstone and before deleting the canonical
			// entry. Only while all three still point at the same observed inode does the successor finish the
			// deletion; once a new owner has replaced the canonical entry it is never touched.
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

// sameDirectoryLockGeneration re-checks the generation without side effects and authorizes no directory move.
// A directory inode can be reused quickly, so the owner token is bound as well; any unstable evidence reads as a different generation.
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
		// An empty directory still initializing has no verifiable owner generation and cannot be taken over.
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

// Release releases the lock. Idempotent: a lock file that no longer exists (cleaned up after a stale takeover) returns nil.
// A file that exists with a mismatched token means this holder was taken over and the lock belongs to a new
// holder: the delete is refused with an error (the caller only logs a warning and never retries the delete).
// Normal production takeover happens only after the operating system confirmed the owner exited; the token check still defends against a hand-replaced directory or a damaged identity.
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
	// A matching token, or damaged content (damage is treated as a leftover and deleted), both delete
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

// newLockToken generates a random token (pid + nanoseconds + a random number, so two processes on one machine at one instant do not collide)
func newLockToken() string {
	return fmt.Sprintf("%d-%d-%d", os.Getpid(), time.Now().UnixNano(), rand.Int63())
}
