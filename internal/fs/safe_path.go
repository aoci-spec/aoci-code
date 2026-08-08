// 相对路径安全校验 —— agent 输入的第一道防线
// 索引条目: safe_path.go[FS.Paths.9.S]
//
// 威胁模型: MCP 的 path 参数来自 agent,而 agent 的输入可能被仓库内容
// (README/代码注释里的注入指令)污染 —— ../ 逃逸或绝对路径可使回写落到仓库外。
// 平台 agent_tools.go 的 validatePathInWorkDir 伤疤在此移植。
//
// 纪律: 索引查询/回写/hook 的全部 path 入口先过 NormalizeRelPath,无例外。
package fs

import (
	"errors"
	"path"
	"strings"
)

// NormalizeRelPath 归一化并校验仓库相对路径。
// 规则: 反斜杠统一转正斜杠 → path.Clean → 拒绝空/绝对路径/盘符/.. 逃逸/清理后为空。
// 返回干净的正斜杠相对路径;目录路径的尾斜杠予以保留。
func NormalizeRelPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("路径为空")
	}
	p := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")

	// 拒绝绝对路径(POSIX 根/Windows 盘符/UNC)
	if strings.HasPrefix(p, "/") {
		return "", errors.New("拒绝绝对路径: 只接受仓库相对路径")
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", errors.New("拒绝 Windows 盘符路径: 只接受仓库相对路径")
	}
	if strings.HasPrefix(p, "//") {
		return "", errors.New("拒绝 UNC 路径")
	}

	// 记录尾斜杠(目录条目语义),Clean 会剥掉它
	trailingSlash := strings.HasSuffix(p, "/")

	clean := path.Clean(p)
	if clean == "." || clean == "" {
		return "", errors.New("路径清理后为空: 需要指向具体文件或目录")
	}
	// .. 逃逸: Clean 后仍以 .. 开头即越界
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("拒绝 .. 路径逃逸: 路径必须落在仓库根之下")
	}

	if trailingSlash {
		clean += "/"
	}
	return clean, nil
}
