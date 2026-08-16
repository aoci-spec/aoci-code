// aoci init使用的.aoci仓库内Git资产边界。
//
// 设计原则:
//
//   - .aoci中的未知资产默认视为本机运行时产物并忽略；
//   - 只有明确列入白名单的正式认知与治理资产进入Git；
//   - .gitignore自身必须可跟踪；
//   - 已有用户文件绝不覆盖；
//   - 新建文件必须通过fs.AtomicWrite，避免半写。
package cli

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/textassets"
)

// aociRuntimeGitignoreContent定义.aoci目录的默认拒绝、正式资产白名单。
//
// aoci.txt位于仓库根，不由本文件管理。
// reports.jsonl、ledger.jsonl、drafts、verify_history、lock、hooks、tmp、
// 备份文件及未来未知文件都会被首行*默认忽略。
// ensureAOCIRuntimeGitignore确保.aoci/.gitignore首次初始化时就位。
//
// 已存在时无条件跳过，防止覆盖维护者自定义策略。
func ensureAOCIRuntimeGitignore(
	root string,
) (
	string,
	error,
) {
	targetPath := filepath.Join(
		root,
		config.DirName,
		".gitignore",
	)

	if _, err := os.Stat(targetPath); err == nil {
		return cliMessage("init.gitignore_exists"), nil
	} else if !errors.Is(
		err,
		os.ErrNotExist,
	) {
		return "", errors.New(cliMessage("init.gitignore_stat_error", err))
	}

	if err := os.MkdirAll(
		filepath.Dir(targetPath),
		0o755,
	); err != nil {
		return "", errors.New(cliMessage("init.gitignore_mkdir_error", err))
	}
	content, err := textassets.Load(textassets.ActiveLocale(), textassets.TemplateAOCIGitignore)
	if err != nil {
		return "", err
	}

	if err := fs.AtomicWrite(
		targetPath,
		[]byte(content),
	); err != nil {
		return "", errors.New(cliMessage("init.gitignore_create_error", err))
	}

	return cliMessage("init.gitignore_created"), nil
}

// hostConfigIgnoreMarker标记init自己管理的根.gitignore区块。维护者写的其他行
// 永不被读取或改写,再次init只在同一标记下补新路径。
const hostConfigIgnoreMarker = "# AOCI agent host integration (machine-bound; do not commit)"

// ensureHostConfigGitignore把init刚写出的宿主配置加进仓库根.gitignore。
//
// 为什么必须在首次scan之前发生: 这些文件带本机绝对路径,提交后别人的init只按
// key存在与否判定,会幂等早退、静默空转。而Managed Scope的角色在首次scan时就
// 定下,此后--force不能推进,摘除要走覆盖缩减的Scope Change审批 —— 所以错过这
// 个时机的代价是一次人工批准,不是改一行配置。
//
// 边界: 只追加本次真实存在的路径;已被Git跟踪的路径一律跳过,因为那代表维护者
// 已经决定提交它,不由init替其改主意;根.gitignore已有等价条目时不重复写。
func ensureHostConfigGitignore(
	root string,
	candidatePaths []string,
) (
	string,
	error,
) {
	present := make([]string, 0, len(candidatePaths))
	for _, relativePath := range candidatePaths {
		if _, err := os.Lstat(filepath.Join(root, relativePath)); err != nil {
			continue
		}
		present = append(present, relativePath)
	}
	if len(present) == 0 {
		return "", nil
	}

	tracked, err := trackedRepositoryPaths(root)
	if err != nil {
		// Git事实不可得时不猜测: 宁可不写, 也不替维护者改变提交意图。
		return "", nil
	}

	targetPath := filepath.Join(root, ".gitignore")
	existing, readErr := os.ReadFile(targetPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return "", errors.New(cliMessage("init.host_gitignore_read_error", readErr))
	}
	current := string(existing)

	added := make([]string, 0, len(present))
	for _, relativePath := range present {
		if tracked[relativePath] {
			continue
		}
		if gitignoreAlreadyCovers(current, relativePath) {
			continue
		}
		added = append(added, relativePath)
		current = appendGitignoreLine(current, relativePath)
	}
	if len(added) == 0 {
		return "", nil
	}

	if err := fs.AtomicWrite(targetPath, []byte(current)); err != nil {
		return "", errors.New(cliMessage("init.host_gitignore_write_error", err))
	}
	return cliMessage("init.host_gitignore_updated", strings.Join(added, ", ")), nil
}

// trackedRepositoryPaths读取当前被Git跟踪的路径集合。
func trackedRepositoryPaths(root string) (map[string]bool, error) {
	command := fs.UntrustedRepositoryGitCommand(
		root, "-c", "core.quotepath=false", "ls-files", "-z", "--cached",
	)
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	tracked := map[string]bool{}
	for _, entry := range strings.Split(string(output), "\x00") {
		if entry != "" {
			tracked[entry] = true
		}
	}
	return tracked, nil
}

// gitignoreAlreadyCovers判断现有内容是否已经写过这个路径。
//
// 这是保守的逐行相等比较, 不解释gitignore的模式语义: 宁可漏判导致一次重复
// 条目, 也不误判成已覆盖而让机器绑定配置进入Managed Scope。
func gitignoreAlreadyCovers(content, relativePath string) bool {
	directoryForm := relativePath + "/"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "/")
		switch trimmed {
		case relativePath, directoryForm:
			return true
		}
		if directory := path.Dir(relativePath); directory != "." {
			if trimmed == directory || trimmed == directory+"/" {
				return true
			}
		}
	}
	return false
}

// appendGitignoreLine把一个路径追加到init标记区块内。
func appendGitignoreLine(content, relativePath string) string {
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if !strings.Contains(content, hostConfigIgnoreMarker) {
		if content != "" {
			content += "\n"
		}
		content += hostConfigIgnoreMarker + "\n"
		return content + relativePath + "\n"
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) != hostConfigIgnoreMarker {
			continue
		}
		insertAt := index + 1
		for insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) != "" {
			insertAt++
		}
		rebuilt := append([]string{}, lines[:insertAt]...)
		rebuilt = append(rebuilt, relativePath)
		rebuilt = append(rebuilt, lines[insertAt:]...)
		return strings.Join(rebuilt, "\n") + "\n"
	}
	return content + relativePath + "\n"
}
