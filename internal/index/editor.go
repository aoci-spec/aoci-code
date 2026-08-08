// 整行替换 / 目录段插入 / 换行保留 / diff 预览
// 索引条目: editor.go(标签见索引)
//
// 本文件为纯文本变换(输入全文,输出新全文),不做任何落盘 ——
// 落盘由调用方(MCP 工具/CLI 命令)经 internal/fs.AtomicWrite 完成,无例外。
//
// 平台语义孪生(R6):
//
//	ReplaceEntry ≡ 平台 replaceLineByTrimMatch —— TrimSpace 相等定位,恰好 1 处才替换;
//	  0 处 = 旧条目已被人工改,2+ 处 = 重复污染,均返回明确 error 绝不猜。
//	InsertEntry  ≡ 平台 insertIntoDirSection —— 定位目标目录段,插到段内最后一个条目行之后;
//	  段内无条目插目录头后;找不到段则追加新目录头+条目。
//
// R6 分歧挂账(2026-07-09): 追加分支 CLI 侧已增强为"文末注释区感知"(见
// appendIndexForNewSection)—— 原"文档绝对末尾追加"会把新段甩到负空间 #说明区
// 与 #代码索引完毕 闭合标记之后,破坏三分法结构(llm 段/indexgen 段两次被迫
// Python 绕行的根因)。平台 insertIntoDirSection 与 Spec 待同步此语义。
//
// 换行纪律: 逐文件探测并保留原换行风格(LF/CRLF),防一次回写造成全文件假 diff。
package index

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// DetectLineEnding 探测文本换行风格。含任一 \r\n 即判为 CRLF,否则 LF。
func DetectLineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// splitPreserve 按 LF 拆行(输入先折 CRLF),并记录原文是否以换行结尾
func splitPreserve(text string) (lines []string, eol string, trailingNL bool) {
	eol = DetectLineEnding(text)
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	trailingNL = strings.HasSuffix(normalized, "\n")
	if trailingNL {
		normalized = strings.TrimSuffix(normalized, "\n")
	}
	return strings.Split(normalized, "\n"), eol, trailingNL
}

// joinPreserve 以原换行风格重组全文,并还原末尾换行有无
func joinPreserve(lines []string, eol string, trailingNL bool) string {
	out := strings.Join(lines, eol)
	if trailingNL {
		out += eol
	}
	return out
}

// ReplaceEntry 整行替换: 在全文中以 TrimSpace 相等定位 oldLine,恰好 1 处才替换。
// 返回替换后的新全文;0 处或 2+ 处返回 error(错误文案供 agent 直接采取下一步动作)。
func ReplaceEntry(text, oldLine, newLine string) (string, error) {
	if strings.Contains(newLine, "\n") || strings.Contains(newLine, "\r") {
		return "", errors.New("新条目必须为单行")
	}
	lines, eol, trailingNL := splitPreserve(text)
	want := strings.TrimSpace(oldLine)
	if want == "" {
		return "", errors.New("旧条目为空: 无法定位替换目标")
	}
	// 收集全部匹配位置
	var hits []int
	for i, l := range lines {
		if strings.TrimSpace(l) == want {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return "", errors.New("未找到旧条目(0 处匹配): 该条目可能已被人工修改,请先 aoci_get_entries 重取最新条目再试")
	case 1:
		lines[hits[0]] = newLine
		return joinPreserve(lines, eol, trailingNL), nil
	default:
		return "", fmt.Errorf("旧条目出现 %d 处(重复污染): 拒绝自动替换,请先人工清理重复条目", len(hits))
	}
}

// ReplaceEntryForPath resolves the formal Entry by repository-relative path
// before replacing its exact old line. This preserves the original optimistic
// content check while allowing two different paths to have byte-identical Entry
// text in different directory sections.
func ReplaceEntryForPath(text, repoRoot, relPath, oldLine, newLine string) (string, error) {
	if strings.Contains(newLine, "\n") || strings.Contains(newLine, "\r") {
		return "", errors.New("新条目必须为单行")
	}
	lines, eol, trailingNL := splitPreserve(text)
	document, _ := Parse(strings.Join(lines, "\n"))
	ResolveRelPaths(document, repoRoot)
	wantOld := strings.TrimSpace(oldLine)
	matches := []*Entry{}
	allOldEntries := []*Entry{}
	seenOldPaths := map[string]struct{}{}
	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			if strings.TrimSpace(entry.FullLine) == wantOld {
				allOldEntries = append(allOldEntries, entry)
				if entry.RelPath == "" {
					return "", fmt.Errorf("旧条目正文存在无法解析路径的副本: 拒绝替换%s", relPath)
				}
				if _, duplicatePath := seenOldPaths[entry.RelPath]; duplicatePath {
					return "", fmt.Errorf("旧条目在路径%s重复: 拒绝替换", entry.RelPath)
				}
				seenOldPaths[entry.RelPath] = struct{}{}
			}
			if entry.RelPath == relPath {
				matches = append(matches, entry)
			}
		}
	}
	rawOldCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == wantOld {
			rawOldCount++
		}
	}
	if rawOldCount != len(allOldEntries) {
		return "", fmt.Errorf("旧条目正文有%d处但仅%d处属于可解析正式Entry: 拒绝替换", rawOldCount, len(allOldEntries))
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("路径%s对应 %d 条正式Entry: 拒绝猜测替换", relPath, len(matches))
	}
	target := matches[0]
	if strings.TrimSpace(target.FullLine) != wantOld {
		return "", fmt.Errorf("路径%s的旧条目已变化: 拒绝应用陈旧候选", relPath)
	}
	position := target.LineNo - 1
	if position < 0 || position >= len(lines) || strings.TrimSpace(lines[position]) != wantOld {
		return "", fmt.Errorf("路径%s的Entry行号与正文不一致: 拒绝替换", relPath)
	}
	lines[position] = newLine
	return joinPreserve(lines, eol, trailingNL), nil
}

// InsertEntry 目录段插入: 将 newLine 插入 relPath 所属目录段。
// 定位规则(平台 insertIntoDirSection 同语义 + CLI 侧追加增强,见包注释 R6 挂账):
//  1. 段内有条目 → 插到最后一个条目行之后;
//  2. 段内无条目 → 插到段头行之后;
//  3. 找不到匹配段 → 复用既有目录段一致的历史仓库根，追加新目录头+条目;
//     既有段无法解析或历史根不一致时fail-closed，禁止写入运行时个人绝对路径;
//     追加点为文末连续 #注释/空行 区(负空间说明区)【之前】,而非文档绝对末尾。
//
// repoRoot 用于段绝对路径与 relPath 的换算。
func InsertEntry(text, relPath, newLine, repoRoot string) (string, error) {
	if strings.Contains(newLine, "\n") || strings.Contains(newLine, "\r") {
		return "", errors.New("新条目必须为单行")
	}
	lines, eol, trailingNL := splitPreserve(text)

	// 借解析器定位目标段与段内最后条目的行号(解析基于 LF 归一文本,行号与 lines 对齐)
	doc, _ := Parse(strings.Join(lines, "\n"))
	ResolveRelPaths(doc, repoRoot)
	sec := FindSectionForPath(doc, repoRoot, relPath)

	if sec != nil {
		insertAfter := sec.StartLine // 默认插段头之后(行号从 1 起)
		if n := len(sec.Entries); n > 0 {
			insertAfter = sec.Entries[n-1].LineNo // 段内最后一个条目之后
		}
		// 在 insertAfter 行(1 起)之后插入 → 切片下标 insertAfter 处插入
		idx := insertAfter
		if idx > len(lines) {
			idx = len(lines)
		}
		out := make([]string, 0, len(lines)+1)
		out = append(out, lines[:idx]...)
		out = append(out, newLine)
		out = append(out, lines[idx:]...)
		return joinPreserve(out, eol, trailingNL), nil
	}

	// 无匹配段: 复用索引既有历史根追加新目录头，避免迁移仓把当前机器路径
	// 注入正式索引并令下一次克隆的失配段集合失去公共前缀。
	root, rootErr := appendSectionRoot(doc, repoRoot)
	if rootErr != nil {
		return "", rootErr
	}
	relDir := path.Dir(strings.ReplaceAll(relPath, "\\", "/"))
	var absDir string
	if relDir == "." || relDir == "" {
		absDir = root + "/"
	} else {
		absDir = root + "/" + relDir + "/"
	}

	idx := appendIndexForNewSection(lines, doc)
	out := make([]string, 0, len(lines)+3)
	out = append(out, lines[:idx]...)
	out = append(out, "", "==="+absDir+"===", newLine)
	out = append(out, lines[idx:]...)
	return joinPreserve(out, eol, trailingNL), nil
}

// appendSectionRoot从当前可解析目录段反推唯一历史仓库根。已有索引的目录段
// 必须共享同一根；新段只延续该根，不把repoRoot这个运行时位置写入正文。
// 零目录段的初始化骨架没有历史身份可复用，才使用当前根建立第一段。
func appendSectionRoot(doc *Document, repoRoot string) (string, error) {
	rels := resolveSectionRels(doc, repoRoot)
	root := ""
	hasDirectorySection := false
	for _, sec := range doc.Sections {
		if sec.AbsPath == "" {
			continue
		}
		hasDirectorySection = true
		relDir, ok := rels[sec]
		if !ok {
			return "", fmt.Errorf("目录段历史根无法安全解析: %s", sec.AbsPath)
		}
		sectionPath := normalizeRootPath(sec.AbsPath)
		candidate := sectionPath
		if relDir != "" {
			suffix := "/" + strings.Trim(strings.ReplaceAll(relDir, "\\", "/"), "/")
			if !strings.HasSuffix(sectionPath, suffix) {
				return "", fmt.Errorf("目录段历史根无法从相对目录反推: %s", sec.AbsPath)
			}
			candidate = strings.TrimSuffix(sectionPath, suffix)
		}
		if candidate == "" {
			return "", fmt.Errorf("目录段历史根为空: %s", sec.AbsPath)
		}
		if root == "" {
			root = candidate
			continue
		}
		if normalizeRootPath(root) != normalizeRootPath(candidate) {
			return "", fmt.Errorf("目录段历史根不一致: %s 与 %s", root, candidate)
		}
	}
	if hasDirectorySection {
		return root, nil
	}
	root = normalizeRootPath(repoRoot)
	if root == "" {
		return "", errors.New("运行时仓库根为空,无法建立首个目录段")
	}
	return root, nil
}

// appendIndexForNewSection 计算"追加新段"的插入下标(0 起,插在该下标之前的位置):
// 从文末向前跳过连续的空行与 # 注释行,落点即负空间说明区/闭合标记之前。
//
// 两道防御:
//  1. 文档零目录段(如 init 骨架,全文皆头部 # 规范区)→ 保持旧行为文末追加:
//     此时"文末注释区"就是头部本身,插到它之前会把新段顶到规范区之上,结构更糟;
//  2. 计算落点若越过最后一个段头行(极端如段内全是注释)→ 允许:落点停在段头行
//     之后(段头行本身非 # 非空,向前扫描自然止步),不会越过;若因任何未预期
//     形态使落点小于最后段头行号,回退文末追加(宁可旧病不可新伤)。
func appendIndexForNewSection(lines []string, doc *Document) int {
	if len(doc.Sections) == 0 {
		return len(lines) // 防御1: 零段文档保持旧行为
	}
	idx := len(lines)
	for idx > 0 {
		t := strings.TrimSpace(lines[idx-1])
		if t == "" || strings.HasPrefix(t, "#") {
			idx--
			continue
		}
		break
	}
	// 防御2: 落点不得越过最后一个段头(StartLine 从 1 起,对应切片下标 StartLine-1;
	// 落点最小合法值为段头行之后,即下标 StartLine)
	lastHeader := doc.Sections[len(doc.Sections)-1].StartLine
	if idx < lastHeader {
		return len(lines)
	}
	return idx
}

// RenderEntryDiff renders a line-level preview (- old / + new) without doing a
// semantic diff. Callers may provide one Locale-owned note for the create case;
// the deterministic index package does not own user-visible natural language.
func RenderEntryDiff(oldLine, newLine string, createNote ...string) string {
	var b strings.Builder
	if strings.TrimSpace(oldLine) == "" {
		b.WriteString("+ " + newLine + "\n")
		if len(createNote) > 0 && strings.TrimSpace(createNote[0]) != "" {
			b.WriteString("(" + createNote[0] + ")\n")
		}
		return b.String()
	}
	b.WriteString("- " + oldLine + "\n")
	b.WriteString("+ " + newLine + "\n")
	return b.String()
}
