// 跨平台目录遍历
// 索引条目: walk.go[FS.Walk.8.Xp.S]
//
// 纪律:
//   - exclude_dirs 用 fs.SkipDir 在 walk 层剪枝而非遍历后过滤(node_modules 几万文件级性能差异);
//   - 不跟随符号链接(防环与越界);
//   - 内置无条件排除仅两个: .aoci(防自吞)与 .git(VCS 内部目录零索引价值且含二进制 pack,
//     2026-07-10 httpx 实弹缺陷: 新仓 init 后默认配置为空,.git 全量扫入 inventory/基线),
//     二者不依赖调用方配置、用户不可关闭;
//   - node_modules/vendor 等常见业务排除不在本层硬编码 —— 它们经 config.DefaultConfig
//     注入配置层(用户可见可改),本层只收 WalkOptions 注入的最终结果;
//   - 输出按 rel_path 排序,保证下游 JSON 稳定可 diff。
//
// 依赖说明: 本包不 import internal/config(避免 fs←config←fs 环),
// 排除规则经 WalkOptions 由调用方注入;config 包负责把 config.json 映射为本结构。
package fs

import "strings"

// WalkOptions 遍历排除规则
type WalkOptions struct {
	// ExcludeDirs 目录名排除(按目录基名匹配,如 node_modules)
	ExcludeDirs []string
	// ExcludeFiles 文件排除模式,语义对齐平台 matchExcludePattern:
	// *.bak 后缀 / backup_* 前缀 / *.backup.* 包含 / 无 * 则基名或相对路径精确
	ExcludeFiles []string
	// HighRiskOptIn contains exact repository-relative sensitive-file paths.
	// Runtime, generated, AOCI, VCS, and unsafe filesystem boundaries cannot be
	// opted in through this mechanism.
	HighRiskOptIn []string
	// IncludeIgnoredCandidates keeps Git-ignored regular-file names available
	// to Managed Scope evaluation. The Safe Inventory hard boundary still
	// excludes sensitive, runtime, generated, and unsafe objects before any
	// content read. Legacy callers leave this false and retain rc17 behavior.
	IncludeIgnoredCandidates bool
}

// WalkRepo 遍历仓库,返回排序后的相对路径列表(正斜杠,仅普通文件)。
func WalkRepo(root string, opt WalkOptions) ([]string, error) {
	inventory, err := BuildSafeInventory(root, opt)
	if err != nil {
		return nil, err
	}
	return append([]string{}, inventory.ManagedCandidates...), nil
}

// MatchExcludePattern 文件排除匹配,语义对齐平台 matchExcludePattern:
// 模式含 * 时支持 前缀*(prefix*)/后缀(*.ext)/双端包含(*.backup.*)/前缀+后缀(backup_*.sql);
// 无 * 时按基名或完整相对路径精确匹配。
func MatchExcludePattern(relPath string, patterns []string) bool {
	base := relPath
	if i := strings.LastIndex(relPath, "/"); i >= 0 {
		base = relPath[i+1:]
	}
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if !strings.Contains(pat, "*") {
			if pat == base || pat == relPath {
				return true
			}
			continue
		}
		switch {
		case strings.HasPrefix(pat, "*") && strings.HasSuffix(pat, "*"):
			// *xx* 包含
			mid := strings.Trim(pat, "*")
			if mid != "" && strings.Contains(base, mid) {
				return true
			}
		case strings.HasPrefix(pat, "*"):
			// *.ext 后缀
			if strings.HasSuffix(base, strings.TrimPrefix(pat, "*")) {
				return true
			}
		case strings.HasSuffix(pat, "*"):
			// prefix* 前缀
			if strings.HasPrefix(base, strings.TrimSuffix(pat, "*")) {
				return true
			}
		default:
			// 中置 *: prefix*suffix(如 backup_*.sql)
			i := strings.Index(pat, "*")
			pre, suf := pat[:i], pat[i+1:]
			if strings.HasPrefix(base, pre) && strings.HasSuffix(base, suf) {
				return true
			}
		}
	}
	return false
}
