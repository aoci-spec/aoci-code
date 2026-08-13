// 原子写工具 —— 全仓一切落盘的唯一通道
// 索引条目: atomic.go[FAT9AT]
//
// 流程: 目标同目录写临时文件 → fsync → close → rename 覆盖。
// 设计说明: rename 的原子性要求源与目标同文件系统,目标同目录是唯一硬保证 ——
// hooks 安装器要写 ~/.claude 等仓外文件,固定 tmp 目录在跨文件系统场景会 EXDEV 失败。
// 临时文件名带 .aoci-tmp- 前缀便于识别清理。
//
// Windows fallback semantics: the normal rename can replace an existing target.
// If it fails, move the target to a same-directory backup before retrying, then
// restore it when the retry fails. If restoration also fails, retain both the
// prepared temp file and the backup for recovery.
//
// Failure semantics: preparation failures remove the temp file and preserve the
// original target. The exceptional Windows rollback failure follows the recovery
// preservation rule above.
// 持久性取舍(记录): 不对父目录做 fsync —— 极端掉电场景 rename 可能回退到旧文件,
// 但两个状态均完整(一致性无损),换取写路径简洁,属标准取舍。
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

// ErrAtomicCASConflict表示目标在计划后、最终替换前已经改变。
var ErrAtomicCASConflict = errors.New("原子写入preimage CAS冲突")

// ErrAtomicCASRecovery表示上次CAS在外部并发修改中断后留下了需要人工裁决的保全副本。
var ErrAtomicCASRecovery = errors.New("原子写入CAS恢复需要裁决")

// ErrAtomicCASUnavailable表示当前平台或文件系统不提供原子路径交换能力。
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

// AtomicWrite 原子写入 data 到 targetPath(绝对或相对均可,目录须已存在)。
// 权限: 新文件 0644;目标已存在时沿用其现有权限。
func AtomicWrite(targetPath string, data []byte) error {
	return atomicWrite(targetPath, data)
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

// AtomicWriteCAS先持有跨进程目标锁，再用原子路径交换捕获替换瞬间的真实preimage。
// 捕获摘要不等于expectedSHA256时原子换回并返回ErrAtomicCASConflict；恢复意图
// 使进程在交换前后退出也能由下一调用确定性收敛，无法证明的外部版本一律保全。
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
	// 不具备原子交换时不存在既能捕获替换瞬间preimage、又不覆盖非协作方
	// 写入的rename序列。这里宁可明确不可用，也绝不把持久意图误当成并发原子性。
	return exchangeAtomicPathsPlatform(first, second)
}

// recoverAtomicCAS收敛进程在原子交换前后退出留下的确定性状态。无法证明归属的
// 双外部版本绝不自动删除，交给调用方按真实冲突停止。
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
		// 交换成功后的最后清理只可能先删swap再删intent；canonical目标仍完整。
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
		// 交换尚未发生，或冲突回滚已经完成。
		return cleanupAtomicCAS(swapPath, intentPath)
	case swapSHA == intent.ExpectedSHA256:
		// 交换捕获了预期preimage，正式写入已经完成；target之后即使被外部更新，
		// 也属于写入后的新事实，不得用旧版本覆盖。
		return cleanupAtomicCAS(swapPath, intentPath)
	case targetSHA == intent.NewSHA256:
		// 交换捕获的是计划外版本且进程在回滚前退出。
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

	// 回滚期间又有外部版本进入canonical：再次交换恢复该最新外部版本，并把
	// 最初捕获版本持久隔离，绝不静默覆盖任一外部写入。
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

	// 目标已存在时记录其权限,rename 后沿用
	perm := os.FileMode(0644)
	if requestedPerm != nil {
		perm = requestedPerm.Perm()
	} else if st, err := os.Stat(targetPath); err == nil {
		perm = st.Mode().Perm()
	}

	// 目标同目录建临时文件(同文件系统保证 rename 原子)
	tmp, err := os.CreateTemp(dir, ".aoci-tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// Preparation failures clean up the temp file. The publication fallback may
	// deliberately retain recovery files after an unsuccessful rollback.
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	// fsync 保证数据落盘后再 rename(掉电场景下 rename 可见即内容完整)
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
	// Prefer rename; on Windows this describes only the normal overwrite path,
	// because Go does not guarantee atomic rename semantics on non-Unix systems.
	if err := os.Rename(tmpName, targetPath); err != nil {
		// On Windows, preserve the target before retrying an overwrite that failed
		// because of filesystem behavior or an open handle. This last-resort path
		// is not atomic.
		if runtime.GOOS == "windows" {
			return windowsRenameFallback(tmpName, targetPath)
		}
		os.Remove(tmpName)
		return fmt.Errorf("rename 落盘失败: %w", err)
	}
	return nil
}

// windowsRenameFallback derives a same-directory backup from the random temp
// name and restores the old target when the retry fails.
func windowsRenameFallback(tmpName, targetPath string) error {
	return windowsRenameFallbackWithRename(tmpName, targetPath, os.Rename)
}

func windowsRenameFallbackWithRename(tmpName, targetPath string, rename func(string, string) error) error {
	backupName := tmpName + ".bak"
	if err := rename(targetPath, backupName); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := rename(tmpName, targetPath); err != nil {
		if restoreErr := rename(backupName, targetPath); restoreErr != nil {
			return restoreErr
		}
		os.Remove(tmpName)
		return err
	}
	os.Remove(backupName)
	return nil
}
