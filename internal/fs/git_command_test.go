package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 仓库自带 .git/config 的 core.fsmonitor 会让 git 在遍历工作树时执行任意程序。
// 加固后的调用必须完成同样的枚举,且不触发该程序。
func TestUntrustedRepositoryGitCommandBlocksRepositoryControlledExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("测试脚本使用 POSIX shebang")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("测试环境无git可执行文件")
	}
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "repository-controlled-execution")
	script := filepath.Join(t.TempDir(), "fsmonitor.sh")
	if err := os.WriteFile(script,
		[]byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0700); err != nil {
		t.Fatalf("写入探针脚本失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("bytes\n"), 0644); err != nil {
		t.Fatalf("写入仓库文件失败: %v", err)
	}
	for _, args := range [][]string{
		{"init", "-q", root},
		{"-C", root, "config", "user.email", "fixture@test.invalid"},
		{"-C", root, "config", "user.name", "fixture"},
		{"-C", root, "add", "-A"},
		{"-C", root, "commit", "-q", "-m", "fixture"},
		{"-C", root, "config", "core.fsmonitor", script},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("准备仓库失败 %v: %v %s", args, err, output)
		}
	}

	// 先证明探针本身有效: 未加固的等价调用会执行仓库指定的程序。
	unhardened := exec.Command("git", "-C", root, "-c", "core.quotepath=false",
		"ls-files", "-z", "--cached")
	unhardened.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	_, _ = unhardened.Output()
	if _, err := os.Stat(marker); err != nil {
		t.Skipf("当前 git 未经由 core.fsmonitor 执行程序,探针无效: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("清理探针标记失败: %v", err)
	}

	output, err := UntrustedRepositoryGitCommand(root, "-c", "core.quotepath=false",
		"ls-files", "-z", "--cached").Output()
	if err != nil {
		t.Fatalf("加固后的枚举必须仍然成功: %v", err)
	}
	if !strings.Contains(string(output), "tracked.txt") {
		t.Fatalf("加固后的枚举应返回受管文件,实际: %q", string(output))
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("加固调用不得执行仓库配置指定的程序: %v", statErr)
	}
}
