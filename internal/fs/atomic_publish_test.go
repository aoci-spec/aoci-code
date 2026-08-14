package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows 兜底路径的数据保全铁律: 目标已删且二次 rename 失败时,新内容只剩
// 临时文件一份,必须原样保留并在错误里回报其路径。
func TestPublishAtomicRenameKeepsTemporaryAfterTargetRemoval(t *testing.T) {
	directory := t.TempDir()
	tmpName := filepath.Join(directory, ".aoci-tmp-new")
	targetPath := filepath.Join(directory, "target.txt")
	if err := os.WriteFile(tmpName, []byte("new bytes"), 0644); err != nil {
		t.Fatalf("准备临时文件失败: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("old bytes"), 0644); err != nil {
		t.Fatalf("准备目标文件失败: %v", err)
	}

	renameCalls := 0
	rename := func(string, string) error {
		renameCalls++
		return errors.New("rename 被文件句柄占用")
	}
	err := publishAtomicRename(tmpName, targetPath, "windows", rename, os.Remove)
	if err == nil {
		t.Fatal("两次 rename 均失败时必须报错")
	}
	if renameCalls != 2 {
		t.Fatalf("Windows 兜底应恰好重试一次 rename,实际调用 %d 次", renameCalls)
	}
	if _, statErr := os.Stat(tmpName); statErr != nil {
		t.Fatalf("临时文件必须保留(新内容的唯一副本),实际: %v", statErr)
	}
	if !strings.Contains(err.Error(), tmpName) {
		t.Fatalf("错误必须回报临时文件路径以便取回内容: %v", err)
	}
	if _, statErr := os.Stat(targetPath); !os.IsNotExist(statErr) {
		t.Fatalf("兜底已删除目标,状态应为不存在: %v", statErr)
	}
}

// 目标删除本身失败时保持原语义: 原文件仍在,清理临时文件。
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
	remove := func(string) error { return errors.New("目标删除失败") }
	if err := publishAtomicRename(tmpName, targetPath, "windows", rename, remove); err == nil {
		t.Fatal("rename 失败必须报错")
	}
	if _, statErr := os.Stat(targetPath); statErr != nil {
		t.Fatalf("目标删除失败时原文件必须保留: %v", statErr)
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
