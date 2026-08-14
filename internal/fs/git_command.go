// 不受信仓库的 git 调用构造器 —— 全仓读取目标仓库 Git 事实的唯一通道
// 索引条目: git_command.go[CG8T]
//
// 威胁: 被扫描的仓库自带 .git/config,其中 core.fsmonitor 会让 git 在遍历
// 工作树(ls-files 等)时执行任意程序 —— 即在 AOCI 进程内拿到命令执行。
// 已在 git 2.53.0 实测复现: 未加固时一次 ls-files 就触发了配置指定的程序。
// 命令行 -c 的优先级高于仓库配置文件,因此每次调用都显式关闭仓库可控的
// 程序执行入口,再附加调用方参数。
//
// 边界: 这里只中和"仓库自带配置"这一来源;调用方环境变量属操作者自身信任域,
// 不在此处覆盖。GIT_OPTIONAL_LOCKS=0 保持只读调用不写 .git 索引锁。
package fs

import (
	"os"
	"os/exec"
)

// UntrustedRepositoryGitCommand 构造一条针对 root 仓库的只读 git 调用。
// args 直接跟在安全参数之后,调用方可以继续追加自己的 -c 选项和子命令。
func UntrustedRepositoryGitCommand(root string, args ...string) *exec.Cmd {
	hardened := []string{
		"-C", root,
		// 仓库配置可指定的程序执行入口,逐项关闭。
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.pager=",
	}
	command := exec.Command("git", append(hardened, args...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return command
}
