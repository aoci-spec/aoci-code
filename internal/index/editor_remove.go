// 条目整行删除原语(v2.8 P1,remove-entry/aoci_remove_entry 共用底座)。
// 索引条目: editor_remove.go(待补录,随本批入册)
//
// 定位: 与 editor.go 的 ReplaceEntry 同族的纯文本变换 —— TrimSpace 相等且
// 恰好 1 处才删,0 处/2+ 处均明确报错绝不猜。独立成文件而非并入 editor.go:
// 沿用 editor_append_test.go 分文件先例,避免整文件覆盖既有 600 行级文件。
//
// 删除语义边界(D75 下半句):
//   - 本原语只做文本变换,不判定"该不该删"——孤儿限定(MCP侧)、人工全权
//     (CLI侧)等策展裁决全部归调用方管线;
//   - 删除目标行后统一清除所有纯空目录段；Header、非目录布局Marker以及含
//     非注释正式内容的兼容边界保持不动；
//   - 不落盘,落盘归调用方经 AtomicWrite(与 editor.go 同纪律)。
//
// 换行纪律: 逐文件探测保留 LF/CRLF 与末尾换行有无,与 editor.go 的
// splitPreserve/joinPreserve 同语义 —— 本文件自含实现而不引用包内私有函数,
// 换取文件独立性;两处语义若分叉以 editor.go 为准(混合风格按整文件含 CRLF
// 即 CRLF 的单一风格假设,与 splitPreserve 一致)。
package index

import (
	"errors"
	"fmt"
	"strings"
)

// RemoveEntry 从索引全文中删除与 target 整行 TrimSpace 相等的唯一一行。
// 返回删除后的全文。0 处命中与 2+ 处命中均报错(语义对齐 ReplaceEntry):
// 0 处提示条目可能已被人工修改建议先重取;2+ 处提示重复污染拒绝操作。
func RemoveEntry(text string, target string) (string, error) {
	want := strings.TrimSpace(target)
	if want == "" {
		return "", errors.New("删除目标为空")
	}
	if strings.ContainsAny(want, "\r\n") {
		return "", errors.New("删除目标须为单行条目(含换行符被拒)")
	}

	// 换行风格探测: 整文件含 CRLF 即按 CRLF 重组(单一风格假设,同 splitPreserve)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	hasTrailingNL := strings.HasSuffix(text, "\n")

	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	// Split 在尾换行后会产生一个空尾元素,剥除以免重组时凭空多一行
	if hasTrailingNL && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// 计数并定位(恰好 1 处铁律)
	hit := -1
	count := 0
	for i, ln := range lines {
		if strings.TrimSpace(ln) == want {
			count++
			hit = i
		}
	}
	if count == 0 {
		return "", errors.New("未找到目标条目行: 条目可能已被人工修改,建议先重取当前条目(get_entries)再操作")
	}
	if count > 1 {
		return "", fmt.Errorf("目标条目行出现 %d 处(重复污染),拒绝删除;请先人工修复索引中的重复条目", count)
	}

	// 删除命中行
	out := make([]string, 0, len(lines)-1)
	out = append(out, lines[:hit]...)
	out = append(out, lines[hit+1:]...)

	result := strings.Join(out, eol)
	if hasTrailingNL {
		result += eol
	}
	return PruneEmptySections(result), nil
}

// RemoveEntryForPath removes the unique Entry resolved by repository-relative
// path and verifies its exact preimage line. It avoids cross-section ambiguity
// when two directory sections contain identical Entry text.
func RemoveEntryForPath(text, repoRoot, relPath, oldLine string) (string, error) {
	want := strings.TrimSpace(oldLine)
	if want == "" || strings.ContainsAny(want, "\r\n") {
		return "", errors.New("删除目标须为非空单行条目")
	}
	lines, eol, trailingNL := splitPreserve(text)
	document, _ := Parse(strings.Join(lines, "\n"))
	ResolveRelPaths(document, repoRoot)
	var target *Entry
	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			if entry.RelPath != relPath {
				continue
			}
			if target != nil {
				return "", fmt.Errorf("路径 %s 对应多个条目,拒绝删除", relPath)
			}
			target = entry
		}
	}
	if target == nil {
		return "", fmt.Errorf("路径 %s 未找到目标条目", relPath)
	}
	if strings.TrimSpace(target.FullLine) != want {
		return "", fmt.Errorf("路径 %s 的目标条目已变化", relPath)
	}
	lineIndex := target.LineNo - 1
	if lineIndex < 0 || lineIndex >= len(lines) || strings.TrimSpace(lines[lineIndex]) != want {
		return "", fmt.Errorf("路径 %s 的条目坐标无效", relPath)
	}
	out := make([]string, 0, len(lines)-1)
	out = append(out, lines[:lineIndex]...)
	out = append(out, lines[lineIndex+1:]...)
	return PruneEmptySections(joinPreserve(out, eol, trailingNL)), nil
}

// PruneEmptySections removes directory Sections that contain neither an Entry
// nor independent non-comment formal content. It preserves Header bytes,
// non-directory layout markers, line endings, and the trailing-newline shape.
func PruneEmptySections(text string) string {
	lines, eol, trailingNL := splitPreserve(text)
	document, _ := Parse(strings.Join(lines, "\n"))
	remove := make([]bool, len(lines))
	for sectionIndex, section := range document.Sections {
		if section.AbsPath == "" || len(section.Entries) != 0 {
			continue
		}
		pureEmpty := true
		for _, line := range section.RawLines[1:] {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				pureEmpty = false
				break
			}
		}
		if !pureEmpty {
			continue
		}
		start := section.StartLine - 1
		end := len(lines)
		if sectionIndex+1 < len(document.Sections) {
			end = document.Sections[sectionIndex+1].StartLine - 1
		}
		for lineIndex := start; lineIndex < end && lineIndex < len(remove); lineIndex++ {
			remove[lineIndex] = true
		}
	}
	kept := make([]string, 0, len(lines))
	for lineIndex, line := range lines {
		if !remove[lineIndex] {
			kept = append(kept, line)
		}
	}
	return joinPreserve(kept, eol, trailingNL)
}
