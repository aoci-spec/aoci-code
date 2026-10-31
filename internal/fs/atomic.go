// Atomic write utilities: the one channel through which the whole program writes to disk
// Index entry: atomic.go[FAT9AT]
//
// Flow: write a temporary file in the target's directory, fsync, close, rename over the target.
// Design note: rename is atomic only when source and target share a filesystem, and the target's own directory
// is the only hard guarantee: the hooks installer writes files outside the repository such as ~/.claude, where a fixed tmp directory would fail with EXDEV across filesystems.
// The temporary name carries the .aoci-tmp- prefix so it can be recognized and cleaned up.
//
// Windows semantics: Go's os.Rename uses MOVEFILE_REPLACE_EXISTING on Windows and normally overwrites an
// existing target; when the overwrite fails, the fallback renames the target to a same-directory backup and
// retries, restoring the backup if the retry fails, so the old bytes are destroyed at no step (PR #7); it is never the default path.
//
// Failure semantics: any failing step cleans up the temporary file and leaves the original as it was, so a
// process killed mid-write leaves no half-written fact source.
// Durability trade-off (recorded): the parent directory is not fsynced; in an extreme power loss the rename may
// revert to the old file, but both states are complete (consistency is intact), which buys a simple write path, a standard trade-off.
package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrAtomicCASConflict means the target changed after planning and before the final replacement.
var ErrAtomicCASConflict = errors.New("原子写入preimage CAS冲突")

// ErrAtomicCASRecovery means the last CAS was interrupted by an external concurrent change and left a preserved copy that needs a human decision.
var ErrAtomicCASRecovery = errors.New("原子写入CAS恢复需要裁决")

// ErrAtomicCASUnavailable means the platform or filesystem offers no atomic path exchange.
var ErrAtomicCASUnavailable = errors.New("原子写入CAS不可用")

// ErrAtomicCreateConflict means an absent-target transaction lost its CAS
// race. The existing target is never replaced or removed.
var ErrAtomicCreateConflict = errors.New("原子创建preimage CAS冲突")

// ErrAtomicCreateUnavailable means the platform cannot publish a prepared
// file atomically with no-replace semantics.
var ErrAtomicCreateUnavailable = errors.New("原子创建CAS不可用")

type atomicCASIntent struct {
	ExpectedSHA256 string `json:"expected_sha256"`
	NewSHA256      string `json:"new_sha256"`
}

var beforeAtomicCASExchange = func(string) {}
var exchangeAtomicPathsPlatform = exchangeAtomicPaths
var beforeAtomicCreatePublish = func(string) {}
var publishAtomicCreatePlatform = publishAtomicCreate
var beforeAtomicMovePublish = func(string, string) {}

// AtomicWrite writes data atomically to targetPath (absolute or relative; the directory must exist).
// Permissions: a new file is 0644; an existing target keeps its current permissions.
func AtomicWrite(targetPath string, data []byte) error {
	return atomicWrite(targetPath, data)
}

// AtomicWriteMode publishes with exactly the given file mode, also when it
// replaces an existing file, so a private backup never inherits the generic
// new-file mode or a prior backup's broader permissions.
func AtomicWriteMode(targetPath string, data []byte, mode os.FileMode) error {
	return atomicWriteWithPerm(targetPath, data, &mode)
}

// AtomicCreateCAS publishes complete bytes only when targetPath is absent at
// the atomic publication instant. It never falls back to an overwrite rename.
func AtomicCreateCAS(targetPath string, data []byte) error {
	return AtomicCreateCASMode(targetPath, data, 0o644)
}

// AtomicCreateCASMode is AtomicCreateCAS with an explicit regular-file mode.
func AtomicCreateCASMode(targetPath string, data []byte, mode os.FileMode) error {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("解析原子创建目标路径失败: %w", err)
	}
	if err := validateAtomicCreateTarget(absTarget); err != nil {
		return err
	}
	lock, err := acquireLockWith(atomicCASLockPath(absTarget), DefaultLockTimeout, 0)
	if err != nil {
		return fmt.Errorf("获取原子创建互斥锁失败: %w", err)
	}
	defer lock.Release()
	if _, err := os.Lstat(absTarget); err == nil {
		return fmt.Errorf("%w: %s", ErrAtomicCreateConflict, absTarget)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查原子创建目标失败: %w", err)
	}

	directory := filepath.Dir(absTarget)
	temporary, err := os.CreateTemp(directory, ".aoci-create-*")
	if err != nil {
		return fmt.Errorf("准备原子创建临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("写入原子创建临时文件失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("flush原子创建临时文件失败: %w", err)
	}
	if err := temporary.Chmod(mode.Perm()); err != nil {
		cleanup()
		return fmt.Errorf("设置原子创建文件权限失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("关闭原子创建临时文件失败: %w", err)
	}

	beforeAtomicCreatePublish(absTarget)
	if err := publishAtomicCreatePlatform(temporaryPath, absTarget); err != nil {
		_ = os.Remove(temporaryPath)
		if _, statErr := os.Lstat(absTarget); statErr == nil {
			return fmt.Errorf("%w: %s", ErrAtomicCreateConflict, absTarget)
		}
		return fmt.Errorf("%w: %v", ErrAtomicCreateUnavailable, err)
	}
	want := sha256.Sum256(data)
	got, err := atomicFileSHA256(absTarget)
	if err != nil || got != hex.EncodeToString(want[:]) {
		return fmt.Errorf("%w: published bytes could not be verified", ErrAtomicCASRecovery)
	}
	return nil
}

// AtomicMoveCAS moves the exact expected regular-file bytes to an absent
// recovery-owned path. Publication uses the same native no-replace primitive
// as AtomicCreateCAS. If a non-cooperating writer wins between inspection and
// publication, the captured bytes are verified after the move and restored
// without replacement when possible; no captured bytes are deleted.
func AtomicMoveCAS(sourcePath, recoveryPath, expectedSHA256 string) error {
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	absRecovery, err := filepath.Abs(recoveryPath)
	if err != nil {
		return err
	}
	if err := validateAtomicCreateTarget(absRecovery); err != nil {
		if !errors.Is(err, ErrAtomicCreateConflict) {
			return err
		}
		return fmt.Errorf("%w: recovery target exists", ErrAtomicCreateConflict)
	}
	lock, err := acquireLockWith(atomicCASLockPath(absSource), DefaultLockTimeout, 0)
	if err != nil {
		return err
	}
	defer lock.Release()
	info, err := os.Lstat(absSource)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: source is not the expected regular file", ErrAtomicCASConflict)
	}
	current, err := atomicFileSHA256(absSource)
	if err != nil || current != expectedSHA256 {
		return fmt.Errorf("%w: source digest drift", ErrAtomicCASConflict)
	}
	beforeAtomicMovePublish(absSource, absRecovery)
	if err := publishAtomicCreatePlatform(absSource, absRecovery); err != nil {
		return fmt.Errorf("%w: %v", ErrAtomicCreateUnavailable, err)
	}
	captured, err := atomicFileSHA256(absRecovery)
	if err == nil && captured == expectedSHA256 {
		return nil
	}
	// The moved bytes are third-party state. Preserve them and attempt to put
	// them back only if the canonical path remains absent.
	if _, statErr := os.Lstat(absSource); os.IsNotExist(statErr) {
		if restoreErr := publishAtomicCreatePlatform(absRecovery, absSource); restoreErr == nil {
			return fmt.Errorf("%w: source changed at move boundary and was restored", ErrAtomicCASConflict)
		}
	}
	return fmt.Errorf("%w: captured third-party bytes retained at %s", ErrAtomicCASRecovery, absRecovery)
}

// ValidateCreateTarget reports whether AtomicCreateCASMode would accept
// absTarget, without creating or touching anything.
//
// It exists so a caller can refuse a bad output path *before* asking a human to
// confirm something. Discovering the path is unusable only after the phrase has
// been typed wastes a confirmation that cannot be reused. The atomic create
// still re-checks at publication, so this is an early answer, not the guarantee.
func ValidateCreateTarget(absTarget string) error {
	return validateAtomicCreateTarget(absTarget)
}

func validateAtomicCreateTarget(absTarget string) error {
	parent := filepath.Dir(absTarget)
	volume := filepath.VolumeName(parent)
	remainder := strings.TrimPrefix(parent, volume)
	current := volume + string(filepath.Separator)
	for _, component := range strings.FieldsFunc(remainder, func(r rune) bool { return r == '/' || r == '\\' }) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("原子创建父路径不可用: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("原子创建父路径不安全: %s", current)
		}
	}
	if info, err := os.Lstat(absTarget); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%w: target has unsafe type", ErrAtomicCreateConflict)
		}
		return fmt.Errorf("%w: target exists", ErrAtomicCreateConflict)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查原子创建目标失败: %w", err)
	}
	return nil
}

// AtomicWriteCAS holds the cross-process target lock first, then captures the real preimage at the instant of
// replacement through an atomic path exchange. When the captured digest differs from expectedSHA256 it swaps back
// atomically and returns ErrAtomicCASConflict; the recovery intent lets a process that exits before or after the swap converge deterministically on the next call, and an external version that cannot be proven is always preserved.
func AtomicWriteCAS(targetPath string, data []byte, expectedSHA256 string) error {
	if strings.TrimSpace(expectedSHA256) == "" {
		return fmt.Errorf("%w: expected_sha256为空", ErrAtomicCASConflict)
	}
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("解析CAS目标路径失败: %w", err)
	}
	lockPath := atomicCASLockPath(absTarget)
	lock, err := acquireLockWith(lockPath, DefaultLockTimeout, 0)
	if err != nil {
		return fmt.Errorf("获取CAS互斥锁失败: %w", err)
	}
	defer lock.Release()

	intentPath := absTarget + ".aoci-cas.intent"
	swapPath := absTarget + ".aoci-cas.swap"
	if err := recoverAtomicCAS(absTarget, swapPath, intentPath); err != nil {
		return err
	}
	currentSHA, err := atomicFileSHA256(absTarget)
	if err != nil || currentSHA != expectedSHA256 {
		return fmt.Errorf("%w: %s", ErrAtomicCASConflict, absTarget)
	}
	targetInfo, err := os.Stat(absTarget)
	if err != nil {
		return fmt.Errorf("读取CAS目标权限失败: %w", err)
	}
	targetPerm := targetInfo.Mode().Perm()
	if err := atomicWriteWithPerm(swapPath, data, &targetPerm); err != nil {
		return fmt.Errorf("准备CAS交换文件失败: %w", err)
	}
	newDigest := sha256.Sum256(data)
	intent := atomicCASIntent{
		ExpectedSHA256: expectedSHA256,
		NewSHA256:      hex.EncodeToString(newDigest[:]),
	}
	intentData, err := json.Marshal(intent)
	if err != nil {
		_ = os.Remove(swapPath)
		return fmt.Errorf("序列化CAS恢复意图失败: %w", err)
	}
	if err := AtomicWrite(intentPath, intentData); err != nil {
		_ = os.Remove(swapPath)
		return fmt.Errorf("写入CAS恢复意图失败: %w", err)
	}

	beforeAtomicCASExchange(absTarget)
	if err := exchangeAtomicPathsRecoverable(absTarget, swapPath); err != nil {
		recoveryErr := recoverAtomicCAS(absTarget, swapPath, intentPath)
		currentSHA, _ := atomicFileSHA256(absTarget)
		if recoveryErr == nil && currentSHA == intent.NewSHA256 {
			return nil
		}
		if recoveryErr != nil {
			return recoveryErr
		}
		return fmt.Errorf("%w: 原子交换目标失败: %v", ErrAtomicCASUnavailable, err)
	}
	capturedSHA, err := atomicFileSHA256(swapPath)
	if err != nil {
		return fmt.Errorf("%w: 读取CAS捕获preimage失败: %v", ErrAtomicCASRecovery, err)
	}
	if capturedSHA == expectedSHA256 {
		if err := cleanupAtomicCAS(swapPath, intentPath); err != nil {
			return fmt.Errorf("CAS已写入但清理恢复资产失败: %w", err)
		}
		return nil
	}
	if err := rollbackAtomicCAS(absTarget, swapPath, intentPath, intent.NewSHA256); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrAtomicCASConflict, absTarget)
}

func atomicCASLockPath(targetPath string) string {
	digest := sha256.Sum256([]byte(targetPath))
	return filepath.Join(os.TempDir(), "aoci-cas-locks", hex.EncodeToString(digest[:])+".lock")
}

func atomicFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func exchangeAtomicPathsRecoverable(first, second string) error {
	// Without an atomic exchange there is no rename sequence that both captures the preimage at the instant of
	// replacement and never overwrites a non-cooperating writer. Better an explicit "unavailable" than mistaking a persisted intent for concurrent atomicity.
	return exchangeAtomicPathsPlatform(first, second)
}

// recoverAtomicCAS converges the deterministic state a process left by exiting before or after the atomic
// exchange. Two external versions whose ownership cannot be proven are never deleted automatically; the caller stops on the real conflict.
func recoverAtomicCAS(targetPath, swapPath, intentPath string) error {
	intentData, err := os.ReadFile(intentPath)
	if os.IsNotExist(err) {
		if _, swapErr := os.Lstat(swapPath); swapErr == nil {
			return fmt.Errorf("%w: 发现无恢复意图的交换资产 %s", ErrAtomicCASRecovery, swapPath)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: 读取恢复意图失败: %v", ErrAtomicCASRecovery, err)
	}
	var intent atomicCASIntent
	if err := json.Unmarshal(intentData, &intent); err != nil {
		return fmt.Errorf("%w: 恢复意图损坏: %v", ErrAtomicCASRecovery, err)
	}
	if err := normalizeAtomicExchangeArtifacts(swapPath); err != nil {
		return fmt.Errorf("%w: 归一化交换资产失败: %v", ErrAtomicCASRecovery, err)
	}
	targetSHA, targetErr := atomicFileSHA256(targetPath)
	swapSHA, swapErr := atomicFileSHA256(swapPath)
	if os.IsNotExist(swapErr) {
		// The final cleanup after a successful swap can only delete swap before intent; the canonical target is still complete.
		if targetErr == nil {
			return removeAtomicCASIntent(intentPath)
		}
		return fmt.Errorf("%w: canonical与交换资产同时缺失", ErrAtomicCASRecovery)
	}
	if targetErr != nil || swapErr != nil {
		return fmt.Errorf("%w: target=%v swap=%v", ErrAtomicCASRecovery, targetErr, swapErr)
	}
	switch {
	case swapSHA == intent.NewSHA256:
		// The swap has not happened, or the conflict rollback is already complete.
		return cleanupAtomicCAS(swapPath, intentPath)
	case swapSHA == intent.ExpectedSHA256:
		// The swap captured the expected preimage and the formal write is complete; an external update of target
		// after that is a new fact written afterwards and must not be overwritten with the old version.
		return cleanupAtomicCAS(swapPath, intentPath)
	case targetSHA == intent.NewSHA256:
		// The swap captured an unplanned version and the process exited before the rollback.
		return rollbackAtomicCAS(targetPath, swapPath, intentPath, intent.NewSHA256)
	default:
		return fmt.Errorf("%w: target与swap均含计划外版本，保留现场", ErrAtomicCASRecovery)
	}
}

func rollbackAtomicCAS(targetPath, swapPath, intentPath, newSHA string) error {
	if err := exchangeAtomicPathsRecoverable(targetPath, swapPath); err != nil {
		return fmt.Errorf("%w: 回滚捕获preimage失败: %v", ErrAtomicCASRecovery, err)
	}
	displacedSHA, err := atomicFileSHA256(swapPath)
	if err != nil {
		return fmt.Errorf("%w: 核对回滚置换版本失败: %v", ErrAtomicCASRecovery, err)
	}
	if displacedSHA == newSHA {
		return cleanupAtomicCAS(swapPath, intentPath)
	}

	// Another external version entered canonical during the rollback: swap again to restore that newest external
	// version and quarantine the first captured version persistently, never silently overwriting any external write.
	if err := exchangeAtomicPathsRecoverable(targetPath, swapPath); err != nil {
		return fmt.Errorf("%w: 恢复并发外部版本失败: %v", ErrAtomicCASRecovery, err)
	}
	capturedSHA, err := atomicFileSHA256(swapPath)
	if err != nil {
		return fmt.Errorf("%w: 读取隔离版本失败: %v", ErrAtomicCASRecovery, err)
	}
	quarantinePath := targetPath + ".aoci-cas-conflict." + capturedSHA[:12] + "." +
		fmt.Sprint(time.Now().UnixNano())
	if err := os.Rename(swapPath, quarantinePath); err != nil {
		return fmt.Errorf("%w: 隔离冲突版本失败: %v", ErrAtomicCASRecovery, err)
	}
	if err := removeAtomicCASIntent(intentPath); err != nil {
		return fmt.Errorf("%w: 冲突版本已隔离到%s但意图清理失败: %v",
			ErrAtomicCASRecovery, quarantinePath, err)
	}
	return fmt.Errorf("%w: %w: 并发外部版本已恢复，捕获版本保存在%s",
		ErrAtomicCASConflict, ErrAtomicCASRecovery, quarantinePath)
}

func cleanupAtomicCAS(swapPath, intentPath string) error {
	if err := os.Remove(swapPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeAtomicCASIntent(intentPath)
}

func removeAtomicCASIntent(intentPath string) error {
	if err := os.Remove(intentPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func atomicWrite(targetPath string, data []byte) error {
	return atomicWriteWithPerm(targetPath, data, nil)
}

func atomicWriteWithPerm(targetPath string, data []byte, requestedPerm *os.FileMode) error {
	dir := filepath.Dir(targetPath)

	// Remember an existing target's permissions so the rename keeps them
	perm := os.FileMode(0644)
	if requestedPerm != nil {
		perm = requestedPerm.Perm()
	} else if st, err := os.Stat(targetPath); err == nil {
		perm = st.Mode().Perm()
	}

	// Create the temporary file in the target's directory (one filesystem keeps the rename atomic)
	tmp, err := os.CreateTemp(dir, ".aoci-tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// Every failure path cleans up the temporary file
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	// fsync puts the data on disk before the rename (after a power loss, a visible rename means complete content)
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("fsync 失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("设置权限失败: %w", err)
	}
	// rename first (the normal path on POSIX and Windows overwrites atomically)
	return publishAtomicRename(tmpName, targetPath, runtime.GOOS, os.Rename, os.Remove)
}

// publishAtomicRename finishes the publication: a normal rename, and only on Windows one non-atomic
// fallback after the rename fails.
//
// Data-preservation rule (PR #7's backup semantics replacing the earlier "delete the target and retry"): the old
// bytes are never destroyed: the target is renamed to a same-directory backup first, the rename is retried, and a
// failed retry restores the backup so the target is intact; only when even the restore fails are both the old and
// the new file kept, with both paths reported in the error for the operator to handle by hand. Unlike deletion,
// renaming leaves the old bytes whatever step a crash hits. rename/remove are injected so the Windows branch is testable off Windows.
func publishAtomicRename(tmpName, targetPath, goos string,
	rename func(string, string) error, remove func(string) error) error {
	err := rename(tmpName, targetPath)
	if err == nil {
		return nil
	}
	if goos == "windows" {
		backupName := tmpName + ".bak"
		if bakErr := rename(targetPath, backupName); bakErr == nil {
			if retryErr := rename(tmpName, targetPath); retryErr == nil {
				remove(backupName)
				return nil
			}
			if restoreErr := rename(backupName, targetPath); restoreErr != nil {
				return fmt.Errorf("rename 落盘失败且备份还原失败,旧内容在 %s,新内容在 %s(请手工处置): %w",
					backupName, tmpName, err)
			}
			remove(tmpName)
			return fmt.Errorf("rename 落盘失败(目标已从备份还原): %w", err)
		}
	}
	remove(tmpName)
	return fmt.Errorf("rename 落盘失败: %w", err)
}
