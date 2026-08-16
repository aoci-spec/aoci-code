// Git 忽略状态探测 —— 判定"某路径是否被 Git 的忽略权威隐藏"的唯一通道
// 索引条目: git_ignore.go[CG7T]
//
// 为什么需要它: Safe Inventory 的文件清单取自 Git,所以被忽略的文件根本不会
// 进入 Baseline。当被忽略的是形式认知资产本身时,后果不是少了一个文件,而是
// 整个 Volume 无法入账、Guide 全线阻塞 —— 而失败现场距离原因(某处一行忽略
// 规则)已经很远。这里把"是否被忽略、被哪条规则忽略"变成可直接回答的事实。
package fs

import (
	"bytes"
	"os/exec"
	"strings"
)

// PathIgnoredByGit 回答 rel 是否被 Git 的忽略权威隐藏,并给出命中的规则来源。
//
// 一次调用同时覆盖 .gitignore、.git/info/exclude 与 core.excludesFile 三个来源,
// 这正是必须问 Git 而不能自己解析 .gitignore 的原因。
//
// 语义上的两个要点:
//   - 已被跟踪的文件永远回 false。git check-ignore 对已跟踪路径不报忽略,这与
//     AOCI 的需要一致: 被跟踪就一定进得了清单,忽略规则对它无效。
//   - 不传 --no-index。加上它会把已跟踪文件也判成忽略,把免疫的情况误报成故障。
//
// 无法取得 Git 事实时回 (false, "")：宁可不报,也不因为环境问题诬告一条规则。
func PathIgnoredByGit(root, rel string) (ignored bool, matchedRule string) {
	command := UntrustedRepositoryGitCommand(root,
		"-c", "core.quotepath=false", "check-ignore", "-v", "--", rel)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = nil
	if err := command.Run(); err != nil {
		// 退出码 1 是"未被忽略"的正常回答,不是失败。
		var exitError *exec.ExitError
		if !asExitError(err, &exitError) || exitError.ExitCode() != 1 {
			return false, ""
		}
		return false, ""
	}
	line := strings.TrimSpace(stdout.String())
	if line == "" {
		return false, ""
	}
	// -v 输出形如 "<source>:<line>:<pattern>\t<path>"。规则来源对操作者的价值
	// 高于模式本身: 它直接指出该去改哪个文件。
	if tab := strings.IndexByte(line, '\t'); tab > 0 {
		line = line[:tab]
	}
	return true, line
}

func asExitError(err error, target **exec.ExitError) bool {
	value, ok := err.(*exec.ExitError)
	if ok {
		*target = value
	}
	return ok
}
