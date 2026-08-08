// 索引文本解析器 —— 平台 parseCodeIndex 的语义孪生
// 索引条目: parser.go[IPS9RM]
//
// 契约(R6): 三正则(目录头/分隔符/条目行)与平台 consistencyDirRe/SepRe/EntryRe
// 语义对齐;任一侧改动必须三方同步(平台/CLI/Spec)并先加差分测试用例。
// Windows 扩展(真机教训): 段头路径捕获组允许可选盘符前缀 X:/ ——
// Windows 仓库根为 C:/repo 形态不以 / 开头,原正则无法匹配致骨架零目录段;
// 该扩展对 POSIX 路径行为零变化,平台与 Python 参考件同步挂账(R6)。
//
// 路径可移植性(2026-07-10 httpx 实弹协议级缺陷,同日两轮修法): 段头绝对路径
// 是创建时快照,仓库迁移后全部段路径失配,旧实现令全体条目 RelPath 为空。
// 第一轮修法为"全部失配才按最长公共前缀重定位";同日 Codex 真机暴露其自我
// 瓦解形态 —— 迁移仓库内 agent 插入新段(段头为当前根路径)即构成混合命中,
// 存量段全体放弃重定位再度失明,而真实混合根几乎唯一成因就是"迁移后插新段"。
// 现行规则(精化): 逐段解析 —— 每段先试直接前缀命中,命中即用;全部失配的
// 段落自成集合,最长公共目录前缀只在该集合内计算并重定位;失配段集合无公共
// 前缀时保守留空,绝不猜。ResolveRelPaths 与 FindSectionForPath 共享
// resolveSectionRels 单一实现(消隐性孪生,重定位若只改一处必现失配)。
//
// 解析规则:
//   - UTF-8 BOM(\ufeff)在解析前剥离(P-18 宽进): Windows 编辑器常给文件加 BOM,
//     首行若为 ===...=== 会因 BOM 前缀令两正则零匹配 —— 首行段头的索引整段
//     沦为头部、条目全灭;本仓 spec 约定 UTF-8 无 BOM,读侧对外部仓宽容;
//   - # 开头为注释行: 跳过提取但原样保留;
//   - ===...=== 行: 若行内含首个路径 token(可选盘符 + / 开头,遇空白或左括号即止)
//     则为目录段头,否则为分隔符 —— 分隔符清空当前目录上下文;
//   - 条目行判据: 首个 [ 之前有非空文件名,且 ] 后紧跟冒号;
//   - 条目仅在目录段(AbsPath 非空)内收集;
//   - CRLF 输入统一折为 LF 解析(逐行剥离尾部 \r);原文换行风格由编辑器负责探测保留;
//   - 解析失败收 Warning 不丢行,行号从 1 起。
package index

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// 三正则 —— 平台 parseCodeIndex 坐标系的语义孪生(R6 三方同步契约)
var (
	// consistencyDirRe 目录段头: === + 可选描述 + 绝对路径(可选盘符 X: + /,遇空白/左括号/等号即止) + 任意尾注 + ===
	// 示例: ===配置索引/opt/aoci-code/===
	//       ===C:/aoci-test/===(Windows 形态)
	consistencyDirRe = regexp.MustCompile(`^===(.*?)((?:[A-Za-z]:)?/[^\s=(（]+)(.*)===\s*$`)
	// consistencySepRe 分隔符: 3 个及以上等号包裹的任意行(未命中目录头时生效),清空当前目录上下文
	consistencySepRe = regexp.MustCompile(`^={3,}.*={3,}\s*$`)
	// consistencyEntryRe 条目行: 文件名(不含 [ 且不以空白开头) + [标签] + 冒号 + 余下内容
	consistencyEntryRe = regexp.MustCompile(`^([^\[\s][^\[]*?)\[([^\]]*)\]:\s?(.*)$`)
)

// stripBOM 剥离文本首部的 UTF-8 BOM(P-18 单点实现)。
// 仅剥文首一枚 \ufeff —— 正文中出现的 BOM(拼接产物)属内容问题不在此层处置;
// 消费点: Parse 入口 / ExtractHeader / ReplaceHeader(读侧宽进,写侧治愈)。
func stripBOM(text string) string {
	return strings.TrimPrefix(text, "\ufeff")
}

// normalizeRootPath 仓库根/段路径归一: 反斜杠转正斜杠 + 去尾斜杠 + Windows 盘符大写
// (段头写 c:/repo 而 root 传 C:\repo 时仍须互认;仅归一盘符字母,不动其余段大小写)
func normalizeRootPath(p string) string {
	s := strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if len(s) >= 2 && s[1] == ':' && s[0] >= 'a' && s[0] <= 'z' {
		s = string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

// Parse 解析索引全文。
// 返回 Document(全部行无损保留)与非致命 Warning 列表;仅在输入为空时返回空文档零警告。
// 文首 UTF-8 BOM 在解析前剥离(P-18),RawText 不携带 BOM。
func Parse(text string) (*Document, []Warning) {
	// BOM 剥离(P-18)后 CRLF 统一折为 LF 解析;原文风格保留归编辑器职责
	normalized := strings.ReplaceAll(stripBOM(text), "\r\n", "\n")

	doc := &Document{RawText: normalized}
	var warnings []Warning

	// current 为当前收集区段;进入首个 ===...=== 行之前的内容归 HeaderLines
	var current *Section
	inHeader := true

	// seen 用于同文档内 rel 级重复条目检测(键: 段绝对路径 + 文件名)
	seen := map[string]int{}

	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		lineNo := i + 1

		// 判定 ===...=== 行(目录头或分隔符)
		if consistencySepRe.MatchString(line) || consistencyDirRe.MatchString(line) {
			inHeader = false
			if m := consistencyDirRe.FindStringSubmatch(line); m != nil {
				// 目录段头: 开启新目录段
				absPath := strings.TrimSpace(m[2])
				current = &Section{
					Name:       strings.TrimSpace(m[1]),
					AbsPath:    absPath,
					HeaderLine: line,
					Entries:    []*Entry{},
					RawLines:   []string{line},
					StartLine:  lineNo,
				}
			} else {
				// 分隔符: 清空目录上下文,开启无路径区段(内容原样保留)
				current = &Section{
					Name:       strings.Trim(line, "= \t"),
					AbsPath:    "",
					HeaderLine: line,
					Entries:    []*Entry{},
					RawLines:   []string{line},
					StartLine:  lineNo,
				}
			}
			doc.Sections = append(doc.Sections, current)
			continue
		}

		// 首个区段之前的行归 HeaderLines
		if inHeader {
			doc.HeaderLines = append(doc.HeaderLines, line)
			continue
		}
		// 区段内的普通行原样保留
		if current != nil {
			current.RawLines = append(current.RawLines, line)
		}

		// 注释行与空行: 只保留不提取
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// 条目行提取: 仅在目录段内收集
		if current == nil || current.AbsPath == "" {
			continue
		}
		m := consistencyEntryRe.FindStringSubmatch(line)
		if m == nil {
			// 形似条目但不匹配(如含 [ 却缺 ]: 结构)时给出警告,便于人工排查
			if strings.Contains(line, "[") && strings.Contains(line, "]:") {
				warnings = append(warnings, Warning{LineNo: lineNo, Msg: "疑似条目行但未通过条目正则: " + truncateForWarn(line)})
			}
			continue
		}
		filename := strings.TrimSpace(m[1])
		tagsRaw := m[2]
		rest := m[3]

		entry := &Entry{
			Filename:   filename,
			TagsRaw:    tagsRaw,
			TagsParsed: ParseTags(tagsRaw),
			FullLine:   line,
			LineNo:     lineNo,
		}
		entry.F, entry.R, entry.Api, entry.S = splitFRAS(rest)

		// 同段同名重复检测(重复污染会使整行替换的"恰好 1 处"约束失效,提前预警)
		key := current.AbsPath + "\x00" + filename
		if prev, ok := seen[key]; ok {
			warnings = append(warnings, Warning{LineNo: lineNo, Msg: "重复条目: 与第 " + strconv.Itoa(prev) + " 行同段同名(" + filename + ")"})
		} else {
			seen[key] = lineNo
		}

		current.Entries = append(current.Entries, entry)
	}
	return doc, warnings
}

// ParseEntryLine parses one canonical Repository Cognition Object line.
//
// It intentionally performs no semantic generation and no path interpretation:
// callers supply an already-authored line and receive only its deterministic
// F/R/A/S decomposition. The legacy document parser and Volumes v1 object
// loader therefore share the exact same lexical grammar.
func ParseEntryLine(line string, lineNo int) (*Entry, bool) {
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	m := consistencyEntryRe.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	entry := &Entry{
		Filename:   strings.TrimSpace(m[1]),
		TagsRaw:    m[2],
		TagsParsed: ParseTags(m[2]),
		FullLine:   line,
		LineNo:     lineNo,
	}
	entry.F, entry.R, entry.Api, entry.S = splitFRAS(m[3])
	return entry, true
}

// splitFRAS 拆解条目正文的 F/R/A/S 四要素。
// 以 " | " 分段,按前缀识别 F:/R:/A:/S:(含 S1:/S2:/S3: 变体合并进 S);
// 未识别前缀的分段忽略(FullLine 已保留全部原文,拆解字段仅供检索展示)。
func splitFRAS(rest string) (f, r, api, s string) {
	segs := strings.Split(rest, " | ")
	var sParts []string
	for _, seg := range segs {
		seg = strings.TrimSpace(seg)
		switch {
		case strings.HasPrefix(seg, "F:"):
			f = strings.TrimSpace(seg[2:])
		case strings.HasPrefix(seg, "R:"):
			r = strings.TrimSpace(seg[2:])
		case strings.HasPrefix(seg, "A:"):
			api = strings.TrimSpace(seg[2:])
		case strings.HasPrefix(seg, "S:"):
			sParts = append(sParts, strings.TrimSpace(seg[2:]))
		case regexpSVariant.MatchString(seg):
			// S1:/S2:/S3: 变体(平台索引真实存在),合并进 S
			idx := strings.Index(seg, ":")
			sParts = append(sParts, strings.TrimSpace(seg[idx+1:]))
		}
	}
	s = strings.Join(sParts, " | ")
	return
}

// regexpSVariant 匹配 S 后跟数字的变体前缀,如 "S1:" "S2:"
var regexpSVariant = regexp.MustCompile(`^S\d+:`)

// resolveSectionRels 计算全部目录段相对当前仓库根的相对目录(段解析单一实现)。
// 返回 map[段指针]relDir;不在映射中的段表示无法解析(条目不参与 rel 匹配)。
//
// 逐段解析(2026-07-10 二轮精化):
//  1. 每段先试直接前缀比对 —— 段路径等于根记 "",在根下记剥前缀,命中即用;
//  2. 全部失配的段落自成集合: 取该集合内全体段路径的最长公共目录前缀视为
//     "创建时根",各段剥离该前缀后即迁移不变的相对目录;
//  3. 失配段集合无公共前缀时保守留空,绝不猜。
//
// 初版"存在任一命中段即放弃全部重定位"已废止 —— 迁移仓库内 agent 插入新段
// (段头为当前根路径)即触发混合命中,存量段全体失明;而真实混合根几乎唯一
// 成因就是"迁移后插新段",逐段规则下新旧两界各自成立。
func resolveSectionRels(doc *Document, repoRoot string) map[*Section]string {
	root := normalizeRootPath(repoRoot)
	rels := map[*Section]string{}
	var missed []*Section

	// 第一遍: 逐段直接前缀比对,失配段收集入子集
	for _, sec := range doc.Sections {
		if sec.AbsPath == "" {
			continue
		}
		secDir := normalizeRootPath(sec.AbsPath)
		switch {
		case secDir == root:
			rels[sec] = ""
		case strings.HasPrefix(secDir, root+"/"):
			rels[sec] = strings.TrimPrefix(secDir, root+"/")
		default:
			missed = append(missed, sec)
		}
	}
	if len(missed) == 0 {
		return rels
	}

	// 第二遍: 失配段子集内计算最长公共目录前缀并重定位
	prefix := commonDirPrefix(missed)
	if prefix == "" {
		return rels // 子集无公共前缀,保守放弃(失配段维持无解)
	}
	for _, sec := range missed {
		secDir := normalizeRootPath(sec.AbsPath)
		switch {
		case secDir == prefix:
			rels[sec] = ""
		case strings.HasPrefix(secDir, prefix+"/"):
			rels[sec] = strings.TrimPrefix(secDir, prefix+"/")
		}
	}
	return rels
}

// commonDirPrefix 计算目录段路径(归一后)的最长公共目录前缀。
// 按 "/" 分段逐层比较 —— 绝不逐字符(防 /opt/httpx 与 /opt/httpx2 错切成 /opt/httpx)。
// Windows 盘符不同(C: 与 D:)时首段即不等,公共前缀为空。
// 全部路径完全相同时(单段索引即此形态)公共前缀为路径本身,剥离后为根段。
func commonDirPrefix(sections []*Section) string {
	if len(sections) == 0 {
		return ""
	}
	parts := strings.Split(normalizeRootPath(sections[0].AbsPath), "/")
	for _, sec := range sections[1:] {
		cur := strings.Split(normalizeRootPath(sec.AbsPath), "/")
		n := len(parts)
		if len(cur) < n {
			n = len(cur)
		}
		i := 0
		for i < n && parts[i] == cur[i] {
			i++
		}
		parts = parts[:i]
		if len(parts) == 0 {
			return ""
		}
	}
	prefix := strings.Join(parts, "/")
	// 纯根("/"切分后为["",""]→拼回空串或"/")不构成有意义的创建时根
	if prefix == "" || prefix == "/" {
		return ""
	}
	return prefix
}

// ResolveRelPaths 按仓库根换算全部条目的 RelPath(正斜杠)。
// 段解析(含迁移仓库的重定位)统一走 resolveSectionRels;无法解析的段其条目
// RelPath 保持空串(不参与路径匹配)。
func ResolveRelPaths(doc *Document, repoRoot string) {
	rels := resolveSectionRels(doc, repoRoot)
	for _, sec := range doc.Sections {
		relDir, ok := rels[sec]
		if !ok {
			continue // 段无法解析(失配子集无公共前缀),条目不参与 rel 匹配
		}
		for _, e := range sec.Entries {
			// 文件名自身可含目录前缀(如 scripts/check-public-text.sh)或为目录条目(尾斜杠)
			name := strings.TrimSpace(e.Filename)
			joined := path.Join(relDir, strings.TrimSuffix(name, "/"))
			if strings.HasSuffix(name, "/") {
				joined += "/"
			}
			e.RelPath = joined
		}
	}
}

// FindSectionForPath 定位 relPath 所属目录段 —— 精确目录匹配(平台 insertIntoDirSection 同语义)。
// 只在段相对目录与 relPath 的父目录完全相等时命中;多段同目录时取文档顺序首个。
// 精确匹配是刻意收紧: 此前的前缀匹配使根段(secRel 为空)成为通配符,吞并一切新目录插入
// (冒烟场景4暴露),且形如"internal/ 与 testdata/"的多路径段头会经前缀误配邻近目录。
// 段解析与 ResolveRelPaths 共享 resolveSectionRels(消隐性孪生): 迁移后的仓库
// 内插入定位与 rel 解析必然同判,不会出现"verify 认得段而插入定位失配"。
// 找不到返回 nil,由调用方走"文末追加新段头"分支。
func FindSectionForPath(doc *Document, repoRoot, relPath string) *Section {
	relDir := path.Dir(strings.ReplaceAll(relPath, "\\", "/"))
	if relDir == "." {
		relDir = ""
	}
	rels := resolveSectionRels(doc, repoRoot)
	for _, sec := range doc.Sections {
		secRel, ok := rels[sec]
		if !ok {
			continue
		}
		if secRel == relDir {
			return sec
		}
	}
	return nil
}

// FindEntry 按仓库相对路径精确查找条目。
// 须先调用 ResolveRelPaths;只认 rel_path 精确匹配 —— 仅文件名匹配只可用于提示,
// 绝不用于自动回写(同名文件误改防线)。找不到返回 nil。
func FindEntry(doc *Document, relPath string) *Entry {
	want := strings.ReplaceAll(relPath, "\\", "/")
	for _, sec := range doc.Sections {
		for _, e := range sec.Entries {
			if e.RelPath != "" && e.RelPath == want {
				return e
			}
		}
	}
	return nil
}

// FindEntriesByFilename 按文件名(末段)查找,仅供提示与消歧展示,禁止用于自动回写
func FindEntriesByFilename(doc *Document, filename string) []*Entry {
	var out []*Entry
	for _, sec := range doc.Sections {
		for _, e := range sec.Entries {
			if strings.TrimSuffix(e.Filename, "/") == strings.TrimSuffix(filename, "/") {
				out = append(out, e)
			}
		}
	}
	return out
}

// truncateForWarn 警告文案截断,防超长行刷屏(按 rune 截断防切碎多字节字符)
func truncateForWarn(line string) string {
	runes := []rune(line)
	if len(runes) <= 60 {
		return line
	}
	return string(runes[:60]) + "…"
}
