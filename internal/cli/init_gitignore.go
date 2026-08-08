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
	"path/filepath"

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
