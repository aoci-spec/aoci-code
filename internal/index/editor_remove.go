// Whole-line entry removal primitive (v2.8 P1, the shared base of remove-entry and aoci_remove_entry).
// Index entry: editor_remove.go (to be recorded, entering the index with this batch)
//
// Position: a pure text transformation of the same family as ReplaceEntry in editor.go: TrimSpace equality and
// exactly 1 hit to delete, with 0 and 2+ hits both reported explicitly and never guessed. It is its own file rather than part of editor.go,
// following the editor_append_test.go split-file precedent, to avoid overwriting an existing 600-line file as a whole.
//
// Removal semantics boundary (the second half of D75):
//   - this primitive only transforms text and never decides "whether to delete"; curation decisions such as the orphan
//     restriction (MCP side) and full human authority (CLI side) all belong to the caller's pipeline;
//   - after the target line is removed, every purely empty directory section is cleared; the Header, non-directory
//     layout Markers, and compatibility boundaries holding non-comment formal content stay untouched;
//   - no disk write; the caller writes through AtomicWrite (the same discipline as editor.go).
//
// Newline discipline: LF/CRLF and the trailing newline are detected and kept per file, with the same semantics as
// splitPreserve/joinPreserve in editor.go; this file carries its own implementation rather than calling the package's private
// functions, buying file independence; should the two diverge, editor.go wins (a mixed style follows the single-style assumption
// that a file holding any CRLF is CRLF, matching splitPreserve).
package index

import (
	"errors"
	"fmt"
	"strings"
)

// RemoveEntry removes the single line whose TrimSpace equals target from the whole index text.
// It returns the text after removal. 0 hits and 2+ hits both return an error (semantics aligned with ReplaceEntry):
// 0 hits says the entry may have been changed by hand and suggests re-fetching first; 2+ hits reports duplicate pollution and refuses.
func RemoveEntry(text string, target string) (string, error) {
	want := strings.TrimSpace(target)
	if want == "" {
		return "", errors.New("删除目标为空")
	}
	if strings.ContainsAny(want, "\r\n") {
		return "", errors.New("删除目标须为单行条目(含换行符被拒)")
	}

	// Newline style detection: a file holding any CRLF is reassembled with CRLF (the single-style assumption, as in splitPreserve)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	hasTrailingNL := strings.HasSuffix(text, "\n")

	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	// Split yields an empty trailing element after a trailing newline; strip it so reassembly does not add a line from nowhere
	if hasTrailingNL && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Count and locate (the exactly-1-hit rule)
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

	// Remove the hit line
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
//
// The root section is the exception while any other directory section remains:
// it anchors the coordinates of the whole index. Relocation resolves against the
// common prefix of the sections, which is the root only while a section sits
// there; without it a clone, and under a root no header can spell the origin
// too, resolves every remaining Entry somewhere else and offers live Entries as
// orphans. An empty root section costs one header line.
func PruneEmptySections(text string) string {
	lines, eol, trailingNL := splitPreserve(text)
	document, _ := Parse(strings.Join(lines, "\n"))
	remove := make([]bool, len(lines))
	for sectionIndex, section := range document.Sections {
		if section.AbsPath == "" || len(section.Entries) != 0 {
			continue
		}
		if anchorsOtherSections(document, section) {
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

// anchorsOtherSections reports whether section is the root of the index's
// coordinates: some other directory section exists, and every one of them lies
// strictly below this section's path. For the first directory section the path
// may be the legacy reading of its header, which is how the old writer's one
// full-spelled section anchors the family it left under the truncated root. It
// is judged from the text alone, like everything else about section roots.
func anchorsOtherSections(document *Document, section *Section) bool {
	bases := []string{normalizeRootPath(section.AbsPath)}
	if section == firstDirectorySection(document) {
		if legacy := normalizeRootPath(section.LegacyAbsPath); legacy != "" && legacy != bases[0] {
			bases = append(bases, legacy)
		}
	}
	for _, base := range bases {
		others, anchored := 0, true
		for _, other := range document.Sections {
			if other == section || other.AbsPath == "" {
				continue
			}
			others++
			if rel, ok := relUnder(normalizeRootPath(other.AbsPath), base); !ok || rel == "" {
				anchored = false
				break
			}
		}
		if anchored && others > 0 {
			return true
		}
	}
	return false
}
