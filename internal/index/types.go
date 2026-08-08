// 索引文档结构体与标签解析
// 索引条目: types.go[ITY9S]
//
// 核心约定:
//   - Entry.FullLine 保留原始整行,是替换/比对的 canonical;
//     F/R/Api/S 等拆解字段仅供检索与展示,绝不用于重渲染回写
//     (重渲染会丢失原文空格/标点,造成假 diff,污染"diff 即变更记录"卖点)。
//   - 未知行/注释/空行原样保留在 Section.RawLines,编辑只动目标条目行。
//   - ParseTags 双形态兼容: 含点号按点分切段,否则按平台紧凑规则逐字符;
//     解析失败 TagsParsed 置空不报错(容忍手写非标标签,检索降级为全文匹配)。
//   - 逗号分隔形态判非标(操作者裁决 2026-07-13,三选一定案: 非标降级/硬拒/
//     容错解析中取非标降级): [I,CL,C8,EL] 形态是 httpx-rerun 实弹中起草模型
//     在字典闸假绿期自创的协议外形态 —— 旧启发式对其切出语义垃圾但非空的
//     map(B 维被切成 ",CL,C"),落在"合法"与"非标"之间的第三态,dict 闸报警
//     但 P-15 不可解析警告不触发。定案理由: 硬拒堵死存量仓一切回写(违反
//     警告放行哲学);容错解析等于给诞生于闸门假绿期的畸形形态发合法牌照且
//     三方解析面被迫扩大;判非标则空 map 自动归入 P-15 警告链(点名标签+
//     明示双闸跳过,跳过可见),该路径已被真机证明可驱动 agent 自纠。
//     协议口径: 合法形态仅紧凑连写与点分两种,逗号(半角/全角)出现即非标。
package index

import "strings"

// Document 一份完整 AOCI 索引文本的解析结果
type Document struct {
	// HeaderLines 首个目录段之前的全部原始行(头部规范/标签字典等),原样保留
	HeaderLines []string
	// Sections 全部区段(含目录段与非目录段),按出现顺序排列
	Sections []*Section
	// RawText 解析时的原始全文(已统一折为 LF),供编辑器整体操作
	RawText string
}

// Section 一个 ===...=== 界定的区段
type Section struct {
	// Name 段头中路径之外的描述文字(可为空)
	Name string
	// AbsPath 段头中提取的绝对路径(以 / 开头);非目录段为空字符串
	AbsPath string
	// HeaderLine 段头原始整行
	HeaderLine string
	// Entries 本段内成功解析的条目;仅 AbsPath 非空的目录段收集条目
	Entries []*Entry
	// RawLines 本段全部原始行(含段头/注释/空行/条目行),原样保留供整体回写参照
	RawLines []string
	// StartLine 段头在全文中的行号(从 1 起)
	StartLine int
}

// Entry 一条索引条目(单行)
type Entry struct {
	// RelPath 相对仓库根的路径(正斜杠);由 ResolveRelPaths 按 repoRoot 换算填充,
	// 段路径不在仓库根之下时为空字符串
	RelPath string
	// Filename 条目行 [ 之前的文件名部分(可含目录前缀,如 scripts/db_backup.sh,
	// 也可为目录条目,如 verify_history/)
	Filename string
	// TagsRaw 方括号内原始标签字符串
	TagsRaw string
	// TagsParsed 标签五维解析结果,键为 A/B/C/D/E;解析失败为空 map
	TagsParsed map[string]string
	// F/R/Api/S 四要素拆解(仅供检索展示;S1/S2/S3 变体合并进 S)
	F   string
	R   string
	Api string
	S   string
	// FullLine 原始整行(canonical,替换比对以此为准)
	FullLine string
	// LineNo 在全文中的行号(从 1 起)
	LineNo int
}

// Warning 解析过程中的非致命问题(不丢行,只记录)
type Warning struct {
	LineNo int    // 行号,从 1 起
	Msg    string // 问题描述
}

// ParseTags 标签解析,双形态兼容。
// 点分形态(含 "."): [Index.Types.9.S] 4 段 = A.B.C.E;[FS.Paths.9.Xp.S] 5 段 = A.B.C.D.E;
// 其余段数视为非标,返回空 map。
// 紧凑形态(不含 "."): 按平台启发式 —— 首字符=A 层级,至首个数字前=B 模块,
// 该数字=C 重要度,末字符=E 规模,数字与末字符之间=D 特征(可空);
// 无数字视为非标,返回空 map。示例 WA9JM → A=W,B=A,C=9,D=J,E=M。
//
// 逗号形态判非标(操作者裁决,见文件头注释): 标签内出现半角或全角逗号
// (如 [I,CL,C8,EL])直接返回空 map —— 不进任何形态的启发式切分,防止切出
// 语义垃圾的非空 map 使条目落入"合法"与"非标"之间的第三态;空 map 令
// P-15 不可解析警告触发,dict/escale 双闸的跳过对写入者可见。
//
// 字典纪律(启发式的前提约束): 紧凑形态的 B 模块与 D 特征符号禁用数字 ——
// "首个数字即 C"是切分锚点,B 含数字(如 V2)会把该数字误切为重要度。
// 本约束由标签字典的制定者遵守(工具不校验),Spec 层同步声明。
func ParseTags(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}
	}
	// 逗号形态判非标(半角与全角;裁决出处见文件头注释)
	if strings.ContainsAny(raw, ",，") {
		return map[string]string{}
	}
	if strings.Contains(raw, ".") {
		return parseDotTags(raw)
	}
	return parseCompactTags(raw)
}

// parseDotTags 点分形态解析
func parseDotTags(raw string) map[string]string {
	segs := strings.Split(raw, ".")
	out := map[string]string{}
	switch len(segs) {
	case 4: // A.B.C.E
		out["A"], out["B"], out["C"], out["E"] = segs[0], segs[1], segs[2], segs[3]
	case 5: // A.B.C.D.E
		out["A"], out["B"], out["C"], out["D"], out["E"] = segs[0], segs[1], segs[2], segs[3], segs[4]
	default:
		return map[string]string{} // 非标段数,降级为无标签
	}
	// 任一段为空同样视为非标
	for _, v := range out {
		if strings.TrimSpace(v) == "" {
			return map[string]string{}
		}
	}
	return out
}

// parseCompactTags 平台紧凑形态启发式解析(元协议兼容:CLI 能读平台旧索引)
func parseCompactTags(raw string) map[string]string {
	runes := []rune(raw)
	if len(runes) < 3 {
		return map[string]string{}
	}
	// 定位首个数字(C 重要度)
	digitIdx := -1
	for i, r := range runes {
		if r >= '0' && r <= '9' {
			digitIdx = i
			break
		}
	}
	// 无数字/数字在首位(缺 A 与 B)/数字在末位(缺 E)均视为非标
	if digitIdx <= 0 || digitIdx >= len(runes)-1 {
		return map[string]string{}
	}
	out := map[string]string{}
	out["A"] = string(runes[0])
	if digitIdx > 1 {
		out["B"] = string(runes[1:digitIdx])
	} else {
		out["B"] = "" // 首字符后紧跟数字: 平台标签至少 1 位 B,此处容忍为空
	}
	out["C"] = string(runes[digitIdx])
	out["E"] = string(runes[len(runes)-1])
	if digitIdx+1 < len(runes)-1 {
		out["D"] = string(runes[digitIdx+1 : len(runes)-1])
	}
	if out["B"] == "" {
		return map[string]string{}
	}
	return out
}
