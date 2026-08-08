// 团队静态路径排除匹配语义。
//
// 本文件只处理config.json中的curation_exclude路径选择器，它是维护者预先
// 声明的团队级负空间策略，不等同于正式文件级语义策展资产：
//
//   - config.curation_exclude：静态路径本体或目录子树选择器；
//   - .aoci/curation.json：由internal/curation维护、绑定source_sha256的
//     文件级include/exclude决策。
//
// 两层共同参与Missing分类时，静态路径策略优先于文件级决策。
//
// 匹配语义最初位于mcptools包内，随着maintain、build、verify和正式Missing
// 分类共同消费，迁入config包成为唯一事实源，防止跨包复制匹配逻辑。
//
// v1.1语义：
//   - 每一项都是仓库相对路径选择器，尾斜杠可写可不写；
//   - 命中路径本体或其目录子树，按目录边界匹配；
//   - 支持单文件精确命中；
//   - 反斜杠、重复斜杠与“./”统一归一；
//   - 绝对路径、仓外逃逸路径与空白项无效；
//   - 暂不支持通配符，避免与fs.MatchExcludePattern混合为未裁决语义。
//
// 当前消费边界：
//   - Missing分类：优先转为CurationExcludedMissing；
//   - maintain：只过滤Missing派发面，已有条目的Stale仍按条目优先处理；
//   - build --missing：过滤自动起草队列，显式--paths保留人工覆盖权；
//   - verify：保留原始Missing事实，同时展示静态路径排除来源。
package config

import (
	"path"
	"strings"
)

// normalizeCurationPath 把静态路径选择器或待匹配路径归一为仓库相对路径。
// 返回空串表示输入无效或不应参与匹配。
func normalizeCurationPath(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if s == "" {
		return ""
	}

	// path.Clean统一重复斜杠、尾斜杠和“./”。
	clean := path.Clean(s)
	if clean == "." ||
		path.IsAbs(clean) ||
		clean == ".." ||
		strings.HasPrefix(clean, "../") {
		return ""
	}

	return strings.TrimSuffix(clean, "/")
}

// CurationExcluded 判断rel是否命中config.curation_exclude静态路径策略。
//
// 该策略表示“不进入自动补录”，不是“文件不存在”的事实改写；verify仍保留
// 原始Missing。正式文件级include/exclude决策由internal/curation处理。
func (c *Config) CurationExcluded(rel string) bool {
	if c == nil || len(c.CurationExclude) == 0 {
		return false
	}

	rel = normalizeCurationPath(rel)
	if rel == "" {
		return false
	}

	for _, raw := range c.CurationExclude {
		ex := normalizeCurationPath(raw)
		if ex == "" {
			continue
		}
		if rel == ex ||
			strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}

	return false
}

// CountCurationExcluded 统计路径清单中命中静态路径策略的条数。
// 需要完整路径证据的调用方应直接遍历CurationExcluded收集。
func (c *Config) CountCurationExcluded(rels []string) int {
	count := 0

	for _, rel := range rels {
		if c.CurationExcluded(rel) {
			count++
		}
	}

	return count
}
