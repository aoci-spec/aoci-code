// 仓库根定位与 .aoci 路径解析
// 索引条目: paths.go[Cfg.Paths.8.Xp.S]
//
// 定根顺序: --repo 显式覆盖 > 向上找 .git(git 根才是 canonical)> 向上找 .aoci。
// 两轮独立向上扫描: 先整轮找 .git,未果再整轮找 .aoci ——
// 保证"子目录里有 .aoci、上层有 .git"时以 git 根为准,不被就近的 .aoci 截胡。
// 索引内 rel_path 永远正斜杠;Windows 盘符只存在于本地访问层。
package config

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNoRepoRoot 未定位到仓库根(调用方据此映射退出码 3 = 配置错误)
var ErrNoRepoRoot = errors.New("未找到仓库根: 当前目录及各级父目录均无 .git 或 .aoci;可用 --repo 显式指定,或在项目根执行 aoci init")

// FindRepoRoot 定位仓库根。
// override 非空(--repo)时直接使用(须为存在的目录);否则自 start 起两轮向上扫描。
func FindRepoRoot(start, override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			return "", errors.New("--repo 指定的路径不存在或不是目录: " + override)
		}
		return abs, nil
	}

	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}

	// 第一轮: 找 .git(canonical)
	if root, ok := ascendFind(abs, ".git"); ok {
		return root, nil
	}

	// 第二轮: 找 .aoci(无 git 的裸目录场景)
	if root, ok := ascendFind(abs, ".aoci"); ok {
		return root, nil
	}

	return "", ErrNoRepoRoot
}

// ascendFind 自 dir 逐级向上查找含 marker 子项的目录。
func ascendFind(dir, marker string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Paths 仓库内各运行时产物的绝对路径集合。
type Paths struct {
	Root             string // 仓库根
	AOCIDir          string // .aoci/
	IndexPath        string // 索引文件(可被 config.index_path 覆盖)
	BaselinePath     string // 基线指纹
	ConfigPath       string // 团队配置
	CurationPath     string // 文件级语义策展资产
	ReportsPath      string // report 待办
	LedgerPath       string // 遥测台账
	VerifyHistoryDir string // verify 快照目录
}

// AOCIPaths 按仓库根与索引相对路径构造路径集合。
// indexRel 传空串时用历史默认 .aoci/index.txt。
func AOCIPaths(root, indexRel string) Paths {
	if indexRel == "" {
		indexRel = ".aoci/index.txt"
	}

	dir := filepath.Join(root, ".aoci")

	return Paths{
		Root:             root,
		AOCIDir:          dir,
		IndexPath:        filepath.Join(root, filepath.FromSlash(indexRel)),
		BaselinePath:     filepath.Join(dir, "baseline.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
		CurationPath:     filepath.Join(dir, "curation.json"),
		ReportsPath:      filepath.Join(dir, "reports.jsonl"),
		LedgerPath:       filepath.Join(dir, "ledger.jsonl"),
		VerifyHistoryDir: filepath.Join(dir, "verify_history"),
	}
}
