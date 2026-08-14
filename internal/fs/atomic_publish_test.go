package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows 兜底的数据保全铁律(备份语义, 采纳 PR #7): 旧字节从头到尾不被销毁。
// 覆盖失败 → 目标改名为同目录备份 → 重试; 重试成功则删备份, 重试失败则还原备份。

// 重试成功: 目标是新内容, 备份被清理。
func TestPublishAtomicRenameBackupThenRetrySucceeds(t *testing.T) {
	directory := t.TempDir()
	tmpName := filepath.Join(directory, ".aoci-tmp-new")
	targetPath := filepath.Join(directory, "target.txt")
	if err := os.WriteFile(tmpName, []byte("new bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	renameCalls := 0
	rename := func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 1 {
			return errors.New("rename 被文件句柄占用")
		}
		return os.Rename(oldPath, newPath)
	}
	if err := publishAtomicRename(tmpName, targetPath, "windows", rename, os.Remove); err != nil {
		t.Fatalf("备份后重试成功不应报错: %v", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil || string(got) != "new bytes" {
		t.Fatalf("目标应是新内容: %q err=%v", got, err)
	}
	if _, statErr := os.Stat(tmpName + ".bak"); !os.IsNotExist(statErr) {
		t.Fatalf("发布成功后备份应被清理: %v", statErr)
	}
}

// 重试失败: 备份自动还原, 目标保持旧内容完好如初。这正是相对"删目标"语义的
// 核心提升 —— 失败路径不再留下"目标不存在"的降级状态。
func TestPublishAtomicRenameRestoresTargetWhenRetryFails(t *testing.T) {
	directory := t.TempDir()
	tmpName := filepath.Join(directory, ".aoci-tmp-new")
	targetPath := filepath.Join(directory, "target.txt")
	if err := os.WriteFile(tmpName, []byte("new bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	renameCalls := 0
	rename := func(oldPath, newPath string) error {
		renameCalls++
		switch renameCalls {
		case 1: // 首次覆盖失败
			return errors.New("rename 被文件句柄占用")
		case 2: // 目标 → 备份
			return os.Rename(oldPath, newPath)
		case 3: // 重试仍失败
			return errors.New("rename 仍被占用")
		default: // 备份还原
			return os.Rename(oldPath, newPath)
		}
	}
	err := publishAtomicRename(tmpName, targetPath, "windows", rename, os.Remove)
	if err == nil {
		t.Fatal("重试失败必须报错")
	}
	got, readErr := os.ReadFile(targetPath)
	if readErr != nil || string(got) != "old bytes" {
		t.Fatalf("目标必须从备份还原为旧内容: %q err=%v", got, readErr)
	}
	if renameCalls != 4 {
		t.Fatalf("应恰好经历 覆盖/备份/重试/还原 四次 rename, 实际 %d", renameCalls)
	}
}

// 极端情况: 连备份还原都失败 → 新旧两份都保留, 错误回报两个路径供手工处置。
func TestPublishAtomicRenameRetainsBothWhenRestoreFails(t *testing.T) {
	directory := t.TempDir()
	tmpName := filepath.Join(directory, ".aoci-tmp-new")
	targetPath := filepath.Join(directory, "target.txt")
	backupName := tmpName + ".bak"
	if err := os.WriteFile(tmpName, []byte("new bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	renameCalls := 0
	rename := func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 { // 目标 → 备份
			return os.Rename(oldPath, newPath)
		}
		return errors.New("rename 失败")
	}
	err := publishAtomicRename(tmpName, targetPath, "windows", rename, os.Remove)
	if err == nil {
		t.Fatal("还原失败必须报错")
	}
	for path, want := range map[string]string{backupName: "old bytes", tmpName: "new bytes"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s 必须保留内容 %q: got=%q err=%v", path, want, got, readErr)
		}
	}
	if !strings.Contains(err.Error(), backupName) || !strings.Contains(err.Error(), tmpName) {
		t.Fatalf("错误必须回报新旧两个路径: %v", err)
	}
}

// 目标改名为备份这一步本身失败: 原文件仍在原位, 清理临时文件。
func TestPublishAtomicRenameCleansTemporaryWhenTargetSurvives(t *testing.T) {
	directory := t.TempDir()
	tmpName := filepath.Join(directory, ".aoci-tmp-new")
	targetPath := filepath.Join(directory, "target.txt")
	for _, path := range []string{tmpName, targetPath} {
		if err := os.WriteFile(path, []byte("bytes"), 0644); err != nil {
			t.Fatalf("准备 %s 失败: %v", path, err)
		}
	}
	rename := func(string, string) error { return errors.New("rename 失败") }
	if err := publishAtomicRename(tmpName, targetPath, "windows", rename, os.Remove); err == nil {
		t.Fatal("rename 失败必须报错")
	}
	if _, statErr := os.Stat(targetPath); statErr != nil {
		t.Fatalf("目标改名失败时原文件必须留在原位: %v", statErr)
	}
	if _, statErr := os.Stat(tmpName); !os.IsNotExist(statErr) {
		t.Fatalf("临时文件应被清理: %v", statErr)
	}
}

// 非 Windows 平台不进入兜底: 一次 rename 失败即清理临时文件并保留原文件。
func TestPublishAtomicRenameSkipsFallbackOffWindows(t *testing.T) {
	directory := t.TempDir()
	tmpName := filepath.Join(directory, ".aoci-tmp-new")
	targetPath := filepath.Join(directory, "target.txt")
	for _, path := range []string{tmpName, targetPath} {
		if err := os.WriteFile(path, []byte("bytes"), 0644); err != nil {
			t.Fatalf("准备 %s 失败: %v", path, err)
		}
	}
	renameCalls := 0
	rename := func(string, string) error {
		renameCalls++
		return errors.New("rename 失败")
	}
	if err := publishAtomicRename(tmpName, targetPath, "linux", rename, os.Remove); err == nil {
		t.Fatal("rename 失败必须报错")
	}
	if renameCalls != 1 {
		t.Fatalf("非 Windows 不得重试 rename,实际调用 %d 次", renameCalls)
	}
	if _, statErr := os.Stat(targetPath); statErr != nil {
		t.Fatalf("原文件必须原样保留: %v", statErr)
	}
	if _, statErr := os.Stat(tmpName); !os.IsNotExist(statErr) {
		t.Fatalf("临时文件应被清理: %v", statErr)
	}
}
