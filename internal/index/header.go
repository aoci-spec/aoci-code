// 索引头部区原语: 提取 / 结构校验 / 整体替换 / 行级 diff
// 索引条目: header.go[IHD8S]
//
// 头部区定义(与 parser.go 的 Parse 完全同源): 全文中首个 ===...=== 行
// (目录段头或分隔符,由 consistencyDirRe / consistencySepRe 判定)之前的
// 全部行。Parse 把这些行收进 Document.HeaderLines —— 本文件的边界判定
// 直接复用同一对正则,保证"头部"在解析侧与编辑侧永远指同一段文本。
//
// 本文件为纯文本变换(输入全文,输出新全文),不做任何落盘 ——
// 落盘由调用方经 internal/fs.AtomicWrite 完成(与 editor.go 同纪律)。
//
// 设计约束:
//   - BOM 口径与 Parse 同源(P-18): ExtractHeader/ReplaceHeader 处理前剥离
//     文首 UTF-8 BOM —— 首行为 ===...=== 且带 BOM 时边界判定会把段头误归
//     头部,ReplaceHeader 会借"改头"之名吞掉首个目录段;读侧宽进,
//     ReplaceHeader 落回不再携带 BOM(spec 约定 UTF-8 无 BOM,写侧顺带治愈,
//     一次性 diff 且调用方备份在场);
//   - ReplaceHeader 只动头部区,条目区(首个 === 行起至文末)一字节不动;
//   - 新头部两条结构硬规(ValidateHeaderText,与 prompt 层对模型的承诺对齐):
//     其一,禁含 ===...=== 形态行 —— 该形态会前移头部/条目边界,等于借
//     "改头部"之名改变文档结构;
//     其二,非空行必须以 # 开头 —— 三分法中头部为说明性内容,该规则同时
//     自动拦截条目形态行与模型输出的解释性散文(均不以#开头);
//     两条规则只约束【新头部】,读取含裸头部行的历史索引不受影响(spec
//     兼容形态"无#头部行不采集"仍成立,旧文件头部原样提取原样保留);
//   - 换行纪律与 editor.go 一致: 逐文件探测并保留原换行风格(LF/CRLF)
//     与末尾换行有无,防一次回写造成全文件假 diff;新头部无论以 LF 还是
//     CRLF 提供,落回时统一按目标文件原风格重组;
//   - 内容合理性(禁区词/字典质量)不在本层 —— 结构原语保持"笨",
//     纪律校验归上层(safety 闸门与 apply 命令)。
package index

import (
	"fmt"
	"strings"
)

// headerBoundary 返回头部/条目区的边界下标(0 起):
// 首个命中目录段头或分隔符正则的行的下标;全文无 === 行时返回 len(lines)。
// 该判定与 Parse 的 inHeader 翻转条件逐字符同源(共用两条包内正则)。
func headerBoundary(lines []string) int {
	for i, line := range lines {
		if consistencySepRe.MatchString(line) || consistencyDirRe.MatchString(line) {
			return i
		}
	}
	return len(lines)
}

// ExtractHeader 提取头部区文本。
// 返回值 header 以 LF 连接(供展示/diff/草稿对照,不承载原文件换行风格);
// hasSections=false 表示全文没有任何 === 行(如极早期骨架),此时头部即全文。
// 注意: 提取不做任何校验 —— 历史索引的裸头部行原样提取(读旧写新原则)。
// 文首 BOM 在处理前剥离(P-18,与 Parse 同口径 —— 否则带 BOM 的首行段头
// 会被边界判定误归头部)。
func ExtractHeader(text string) (header string, hasSections bool) {
	lines, _, _ := splitPreserve(stripBOM(text))
	b := headerBoundary(lines)
	return strings.Join(lines[:b], "\n"), b < len(lines)
}

// ValidateHeaderText 校验候选头部文本的结构合法性,供 ReplaceHeader 与
// 上层 apply/draft 预检共用。两条硬性结构约束(见包注释):
//  1. 禁含 ===...=== 形态的行(改变头部/条目区边界);
//  2. 非空行必须以 # 开头(三分法头部纪律,同时拦截条目行与散文行)。
//
// 返回首个违规行的行号(1 起)与说明;完全合法返回 (0, "")。
// 空头部在结构上合法(是否允许清空由调用方裁决),本函数放行。
func ValidateHeaderText(newHeader string) (int, string) {
	if newHeader == "" {
		return 0, ""
	}
	normalized := strings.ReplaceAll(newHeader, "\r\n", "\n")
	for i, line := range strings.Split(normalized, "\n") {
		if consistencySepRe.MatchString(line) || consistencyDirRe.MatchString(line) {
			return i + 1, "头部中不得出现 ===...=== 形态的行(会改变头部/条目区边界): " + truncateForWarn(line)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return i + 1, "头部非空行必须以#开头(三分法:头部为说明性内容,条目只属目录段): " + truncateForWarn(line)
		}
	}
	return 0, ""
}

// ReplaceHeader 以 newHeader 整体替换 text 的头部区。
// 条目区(首个 === 行起至文末)一字节不动;原文换行风格与末尾换行有无保留。
// newHeader 可为 LF 或 CRLF 风格(内部统一折 LF 再按原文风格重组);
// newHeader 为空串时头部被清空(结构合法,内容合理性由调用方把关)。
// 文首 BOM 在处理前剥离(P-18): 带 BOM 时边界判定失准会吞掉首个目录段;
// 剥离后落回的全文不再携带 BOM —— 写侧顺带治愈(spec 约定 UTF-8 无 BOM),
// 属一次性 diff,调用方管线备份在场。
func ReplaceHeader(text, newHeader string) (string, error) {
	if ln, msg := ValidateHeaderText(newHeader); ln > 0 {
		return "", fmt.Errorf("新头部第 %d 行非法: %s", ln, msg)
	}
	lines, eol, trailingNL := splitPreserve(stripBOM(text))
	b := headerBoundary(lines)

	// 新头部按 LF 归一拆行;末尾换行剥掉,交 joinPreserve 按原文统一还原
	nh := strings.ReplaceAll(newHeader, "\r\n", "\n")
	nh = strings.TrimSuffix(nh, "\n")
	var newLines []string
	if nh != "" {
		newLines = strings.Split(nh, "\n")
	}

	out := make([]string, 0, len(newLines)+len(lines)-b)
	out = append(out, newLines...)
	out = append(out, lines[b:]...)
	return joinPreserve(out, eol, trailingNL), nil
}
