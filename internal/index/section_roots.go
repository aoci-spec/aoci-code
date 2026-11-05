package index

import (
	"fmt"
	"strings"
)

// RelocateSectionRoots rewrites every directory section header of a Code
// Volume so that it records its directory under NeutralRepositoryRoot: the
// header's descriptive prefix and the directory are kept, only the root the
// index was created under is replaced. Sections are resolved exactly as every
// reader resolves them (resolveSectionRels), so the rewritten index names the
// same repository-relative directories as before; a section no reader can
// place fails the whole rewrite, and the result is read back before it is
// returned so that it resolves to those directories again. The new text and
// the number of headers that changed are returned; zero means the text is
// already neutral and is returned unchanged.
func RelocateSectionRoots(text, repoRoot string) (string, int, error) {
	lines, eol, trailingNL := splitPreserve(text)
	doc, _ := Parse(strings.Join(lines, "\n"))
	rels := resolveSectionRels(doc, repoRoot)
	expected := map[int]string{}
	changed := 0
	for _, sec := range doc.Sections {
		if sec.AbsPath == "" {
			continue
		}
		rel, ok := rels[sec]
		if !ok {
			return "", 0, fmt.Errorf("section root unresolved: %s", sec.AbsPath)
		}
		rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
		if sec.StartLine < 1 || sec.StartLine > len(lines) {
			return "", 0, fmt.Errorf("section header line out of range: %d", sec.StartLine)
		}
		expected[sec.StartLine] = rel
		line := lines[sec.StartLine-1]
		match := consistencyDirRe.FindStringSubmatchIndex(line)
		if match == nil {
			return "", 0, fmt.Errorf("section header unreadable at line %d", sec.StartLine)
		}
		dir := NeutralRepositoryRoot + "/"
		if rel != "" {
			dir += rel + "/"
		}
		// Everything before the path token is the header's own description
		// and is kept byte for byte; the path and whatever followed it go.
		header := line[:match[4]] + dir + "==="
		if header != line {
			lines[sec.StartLine-1] = header
			changed++
		}
	}
	if changed == 0 {
		return text, 0, nil
	}
	after, _ := Parse(strings.Join(lines, "\n"))
	afterRels := resolveSectionRels(after, repoRoot)
	for _, sec := range after.Sections {
		if sec.AbsPath == "" {
			continue
		}
		rel, ok := afterRels[sec]
		if !ok || strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/") != expected[sec.StartLine] {
			return "", 0, fmt.Errorf("section root relocation does not read back: %s", sec.AbsPath)
		}
	}
	return joinPreserve(lines, eol, trailingNL), changed, nil
}

// EnsureSectionRootNote inserts note into a Meta Volume unless a line of it
// already names NeutralRepositoryRoot. The note follows the canonical Code
// identity example when the Meta has one, otherwise precedes the first tag
// dictionary, otherwise closes the file; line endings are kept. It reports
// whether the Meta changed.
func EnsureSectionRootNote(meta []byte, note string) ([]byte, bool) {
	text := string(meta)
	if strings.Contains(text, NeutralRepositoryRoot) {
		return meta, false
	}
	lines, eol, trailingNL := splitPreserve(text)
	at := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#Code canonical identity example:") {
			at = i + 1
			break
		}
	}
	if at < 0 {
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#[Tag dictionary:") {
				at = i
				break
			}
		}
	}
	if at < 0 {
		at = len(lines)
		trailingNL = true
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, note)
	out = append(out, lines[at:]...)
	return []byte(joinPreserve(out, eol, trailingNL)), true
}
