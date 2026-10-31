// Whole-line replacement / directory-section insertion / newline preservation / diff preview
// Index entry: editor.go (tags in the index)
//
// This file is pure text transformation (whole text in, new whole text out) and never writes to disk;
// the caller (an MCP tool or a CLI command) writes through internal/fs.AtomicWrite, without exception.
//
// Platform semantic twins (R6):
//
//	ReplaceEntry ≡ the platform's replaceLineByTrimMatch: locate by TrimSpace equality, replace only when there is exactly 1 hit;
//	  0 hits = the old entry was changed by hand, 2+ hits = duplicate pollution, both return an explicit error and never guess.
//	InsertEntry  ≡ the platform's insertIntoDirSection: locate the target directory section, insert after its last entry line;
//	  a section without entries inserts after the directory header; no section found appends a new directory header plus the entry.
//
// R6 divergence on record (2026-07-09): the append branch on the CLI side was enhanced to be "aware of the
// trailing comment area" (see appendIndexForNewSection); the old "append at the absolute end of the document"
// threw the new section behind the negative-space # explanation area and the #代码索引完毕 closing marker, breaking
// the three-way structure (the root cause of the llm and indexgen sections twice needing a Python detour). The platform's insertIntoDirSection and the Spec still have to adopt this semantics.
//
// Newline discipline: the original newline style (LF/CRLF) is detected and kept per file, so one write-back cannot produce a whole-file false diff.
package index

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// DetectLineEnding detects the text's newline style: any \r\n makes it CRLF, otherwise LF.
func DetectLineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// splitPreserve splits on LF (folding CRLF first) and records whether the original ended with a newline
func splitPreserve(text string) (lines []string, eol string, trailingNL bool) {
	eol = DetectLineEnding(text)
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	trailingNL = strings.HasSuffix(normalized, "\n")
	if trailingNL {
		normalized = strings.TrimSuffix(normalized, "\n")
	}
	return strings.Split(normalized, "\n"), eol, trailingNL
}

// joinPreserve reassembles the whole text in the original newline style and restores the trailing newline
func joinPreserve(lines []string, eol string, trailingNL bool) string {
	out := strings.Join(lines, eol)
	if trailingNL {
		out += eol
	}
	return out
}

// ReplaceEntry replaces one whole line: oldLine is located in the text by TrimSpace equality and replaced only when there is exactly 1 hit.
// It returns the new whole text; 0 or 2+ hits return an error (whose wording lets the agent take its next step directly).
func ReplaceEntry(text, oldLine, newLine string) (string, error) {
	if strings.Contains(newLine, "\n") || strings.Contains(newLine, "\r") {
		return "", errors.New("新条目必须为单行")
	}
	lines, eol, trailingNL := splitPreserve(text)
	want := strings.TrimSpace(oldLine)
	if want == "" {
		return "", errors.New("旧条目为空: 无法定位替换目标")
	}
	// Collect every match position
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

// ErrDirectoryUnspellable reports a directory no section header can spell back to
// the same path, so that a caller can answer with the rule and the way out
// instead of an opaque transform failure.
var ErrDirectoryUnspellable = errors.New("目录名无法在段头中无歧义表示")

// DirectoryUnspellableError names what cannot be spelled: a directory relative to
// the repository, or the root the index records when the root itself is the
// problem and every new section under it would be refused.
type DirectoryUnspellableError struct {
	Directory string
	Root      bool
}

func (e *DirectoryUnspellableError) Error() string {
	return fmt.Sprintf("%s: %q", ErrDirectoryUnspellable.Error(), e.Directory)
}

func (e *DirectoryUnspellableError) Is(target error) bool { return target == ErrDirectoryUnspellable }

// headerSpells reports whether a section header for absDir reads back to absDir.
func headerSpells(absDir string) bool {
	_, _, readBack, ok := matchDirHeader("===" + absDir + "===")
	return ok && normalizeRootPath(readBack) == normalizeRootPath(absDir)
}

// InsertEntry inserts into a directory section: newLine goes into the directory section that owns relPath.
// Placement rules (the platform's insertIntoDirSection semantics plus the CLI-side append enhancement, see the R6 note in the package comment):
//  1. the section has entries → after its last entry line;
//  2. the section has no entries → after the section header line;
//  3. no matching section → reuse the historical repository root shared by the existing directory sections and append a new directory header plus the entry;
//     when the existing sections cannot be parsed or their historical roots disagree it fails closed, and the runtime personal absolute path is never written;
//     the append point is BEFORE the trailing run of # comment / blank lines (the negative-space explanation area), not the absolute end of the document.
//
// repoRoot converts between section absolute paths and relPath.
func InsertEntry(text, relPath, newLine, repoRoot string) (string, error) {
	if strings.Contains(newLine, "\n") || strings.Contains(newLine, "\r") {
		return "", errors.New("新条目必须为单行")
	}
	lines, eol, trailingNL := splitPreserve(text)

	// Borrow the parser to locate the target section and the line number of its last entry (parsing is on LF-folded text, so line numbers align with lines)
	doc, _ := Parse(strings.Join(lines, "\n"))
	ResolveRelPaths(doc, repoRoot)
	sec := FindSectionForPath(doc, repoRoot, relPath)

	if sec != nil {
		insertAfter := sec.StartLine // default: after the section header (line numbers start at 1)
		if n := len(sec.Entries); n > 0 {
			insertAfter = sec.Entries[n-1].LineNo // after the last entry of the section
		}
		// Insert after line insertAfter (1-based) → insert at slice index insertAfter
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

	// No matching section: append a new directory header under the index's existing historical root, so a migrated
	// repository does not inject the current machine's path into the formal index and strip the next clone's mismatched sections of their common prefix.
	headers, planErr := planNewSection(doc, repoRoot, relPath)
	if planErr != nil {
		return "", planErr
	}
	idx := appendIndexForNewSection(lines, doc)
	out := make([]string, 0, len(lines)+2*len(headers)+1)
	out = append(out, lines[:idx]...)
	for _, header := range headers {
		out = append(out, "", header)
	}
	out = append(out, newLine)
	out = append(out, lines[idx:]...)
	return joinPreserve(out, eol, trailingNL), nil
}

// planNewSection decides how a section for relPath's directory is appended to
// doc: the header lines to write, in order, or the refusal. InsertEntry writes
// what it answers and InsertProbe asks it without writing, so a planner and the
// writer cannot disagree about which directories can be recorded.
//
// A new index begins with its root section. When the first Entry lives in a
// directory, the root section is written, empty, ahead of it. That anchor is what
// lets a clone read the index exactly: relocation resolves against the common
// prefix of the sections, which is the root only when some section sits there,
// and an index whose first section holds a cut character and is not the root is,
// byte for byte, what the old writer left under a truncated root.
func planNewSection(doc *Document, repoRoot, relPath string) ([]string, error) {
	root, rootErr := appendSectionRoot(doc, repoRoot)
	if rootErr != nil {
		return nil, rootErr
	}
	relDir := path.Dir(strings.ReplaceAll(relPath, "\\", "/"))
	if relDir == "." {
		relDir = ""
	}

	// A new index begins with its root section under NeutralRepositoryRoot, which
	// spells by construction, so no runtime path, however its segments read, reaches
	// the written text. An existing index continues the root its own headers read
	// back to (appendSectionRoot); one whose root cannot be spelled back is refused
	// as the root rather than written somewhere the next read resolves elsewhere.
	if !headerSpells(root + "/") {
		return nil, &DirectoryUnspellableError{Directory: root, Root: true}
	}
	fresh := firstDirectorySection(doc) == nil
	absDir := root + "/"
	if relDir != "" {
		absDir = root + "/" + relDir + "/"
	}

	// Fail closed on a directory the header grammar cannot spell back (a segment
	// that begins with whitespace, or one that ends with whitespace before a "/"):
	// writing it would record a path the next read resolves somewhere else.
	if !headerSpells(absDir) {
		return nil, &DirectoryUnspellableError{Directory: relDir}
	}
	var headers []string
	if relDir != "" && fresh {
		headers = append(headers, "==="+root+"/===")
	}
	headers = append(headers, "==="+absDir+"===")

	// A header that reads back is necessary, not sufficient: the section also has to
	// resolve to the directory it was written for, against everything the index
	// already holds. The check runs on the section list alone, without re-parsing.
	if !sectionsResolveLast(doc, headers, repoRoot, relDir) {
		if relDir == "" {
			return nil, &DirectoryUnspellableError{Directory: root, Root: true}
		}
		return nil, &DirectoryUnspellableError{Directory: relDir}
	}
	return headers, nil
}

// sectionsResolveLast reports whether, with headers appended to doc's sections
// in order, the last one resolves to relDir against repoRoot.
func sectionsResolveLast(doc *Document, headers []string, repoRoot, relDir string) bool {
	appended := &Document{Sections: append([]*Section{}, doc.Sections...)}
	var last *Section
	for _, header := range headers {
		_, legacyPath, absPath, ok := matchDirHeader(header)
		if !ok {
			return false
		}
		last = &Section{AbsPath: absPath, LegacyAbsPath: legacyPath}
		appended.Sections = append(appended.Sections, last)
	}
	rel, ok := resolveSectionRels(appended, repoRoot)[last]
	return ok && rel == relDir
}

// InsertProbe answers, for one index text parsed once, whether an Entry in a
// given directory could be inserted. A planner uses it to stop before a model
// authors a batch the writer would have to refuse.
type InsertProbe struct {
	doc      *Document
	repoRoot string
}

// NewInsertProbe parses text once for any number of Check calls.
func NewInsertProbe(text, repoRoot string) *InsertProbe {
	lines, _, _ := splitPreserve(text)
	doc, _ := Parse(strings.Join(lines, "\n"))
	return &InsertProbe{doc: doc, repoRoot: repoRoot}
}

// Check reports nil when relPath's directory already has a section or a new one
// could be written for it, and otherwise the error InsertEntry would answer. Only
// the directory is judged, never the file name.
func (p *InsertProbe) Check(relPath string) error {
	if FindSectionForPath(p.doc, p.repoRoot, relPath) != nil {
		return nil
	}
	_, err := planNewSection(p.doc, p.repoRoot, relPath)
	return err
}

// CheckInsertable is InsertProbe for a single path.
func CheckInsertable(text, relPath, repoRoot string) error {
	return NewInsertProbe(text, repoRoot).Check(relPath)
}

// appendSectionRoot derives the single historical repository root from the parseable directory sections. The
// directory sections of an existing index must share one root; a new section only continues that root and never
// writes repoRoot, the runtime location, into the body. Only a zero-section initialization skeleton, with no historical identity to reuse, takes the current root for its first section.
//
// An index written while only the legacy reading existed spells one root two
// ways: in full, by its first section, and truncated, by every section appended
// after it (#58). The reader already resolves that first section through its
// legacy path once the truncated family confirms the old writer, so both
// spellings arrive here as the truncated root and new sections continue it. That
// is deliberate: a relocated clone has no repository root to compare against and
// recognises the old writer by exactly one full-spelled section, so a second one
// would strand it.
func appendSectionRoot(doc *Document, repoRoot string) (string, error) {
	readings := resolveSectionReadings(doc, repoRoot)
	root := ""
	hasDirectorySection := false
	for _, sec := range doc.Sections {
		if sec.AbsPath == "" {
			continue
		}
		hasDirectorySection = true
		reading, ok := readings[sec]
		if !ok {
			return "", fmt.Errorf("目录段历史根无法安全解析: %s", sec.AbsPath)
		}
		relDir := reading.rel
		sectionPath := normalizeRootPath(reading.path(sec))
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
	// A new index records no machine path. Its first section names
	// NeutralRepositoryRoot, so the committed index never publishes the author's
	// directory, and later sections continue under it like any historical root.
	return NeutralRepositoryRoot, nil
}

// NeutralRepositoryRoot is the section root a writer gives a new index. Readers
// need nothing special for it: a root no checkout can sit under always misses
// the runtime root and resolves by relocation, the way a clone made at another
// path resolves the author's root; the released rc22 reader resolves it, and
// its writer appends under it, exactly that way. It cannot be a real directory on Windows, where < and
// > are reserved, nor plausibly one on Unix; a plain name such as /repo could,
// and a checkout under /repo/<dir> that contains <dir>/ would then match some
// sections directly and misread them.
const NeutralRepositoryRoot = "/<repository>"

// appendIndexForNewSection computes the insertion index for "append a new section" (0-based, inserting before that index):
// it skips the trailing run of blank lines and # comment lines backwards from the end, landing before the negative-space explanation area / closing marker.
//
// Two defences:
//  1. A document with zero directory sections (such as an init skeleton, all header # rules) keeps the old behaviour of appending at the end:
//     the "trailing comment area" is then the header itself, and inserting before it would push the new section above the rules area, a worse structure;
//  2. Should the computed landing point cross the last section header line (an extreme such as a section holding only comments), it is allowed to stop
//     after the header line (the header line itself is neither # nor blank, so the backward scan stops there naturally) and never crosses it; if any
//     unexpected shape puts the landing point below the last header line number, it falls back to appending at the end (better the old ailment than a new wound).
func appendIndexForNewSection(lines []string, doc *Document) int {
	if len(doc.Sections) == 0 {
		return len(lines) // defence 1: a zero-section document keeps the old behaviour
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
	// Defence 2: the landing point must not cross the last section header (StartLine starts at 1 and maps to slice index StartLine-1;
	// the smallest legal landing point is after the header line, that is index StartLine)
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
