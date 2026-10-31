// Index text parser: the semantic twin of the platform's parseCodeIndex
// Index entry: parser.go[IPS9RM]
//
// Contract (R6): the three regexes (directory header, separator, entry line) align semantically with the
// platform's consistencyDirRe/SepRe/EntryRe; a change on either side must be synchronized three ways
// (platform, CLI, Spec) with a differential test case added first.
// Windows extension (a real-machine lesson): the section header path capture allows an optional drive prefix X:/.
// A Windows repository root of the form C:/repo does not start with /, so the original regex could not match
// it and the skeleton had zero directory sections; the extension changes nothing for POSIX paths and is on the platform and Python reference ledger (R6).
//
// Path portability (the 2026-07-10 httpx live protocol-level defect, fixed in two rounds the same day): the
// absolute section-header paths are a snapshot taken at creation, so after a repository move every section path
// misses and the old implementation left every Entry's RelPath empty. The first fix relocated by longest common
// prefix only when every section missed; the same day a real Codex run exposed its self-defeating shape: an agent
// inserting a new section (headed by the current root) inside a moved repository created a mixed hit, every
// existing section gave up relocation and went blind again, while "a new section after a move" is almost the only
// real cause of mixed roots. Current rule (refined): resolve section by section; each section first tries a direct
// prefix hit and uses it when it hits; the sections that all miss form their own set, and the longest common
// directory prefix is computed and relocated within that set only; when the missing set has no common prefix,
// leave it empty rather than guess. ResolveRelPaths and FindSectionForPath share the single resolveSectionRels
// implementation (a hidden twin: relocation changed in one place only would miss). When the complete historical
// root lies below the current root (a nested worktree returned to the main work tree), relocate by the historical
// root first; otherwise the direct prefix would take the whole worktree path as every Entry's relative prefix.
//
// Parsing rules:
//   - a UTF-8 BOM (\ufeff) is stripped before parsing (P-18 lenient read): Windows editors often add one, and a
//     first line of ===...=== would then match neither regex, turning the first section into header and losing
//     every entry; this repository's spec says UTF-8 without BOM, and the read side is lenient with foreign repositories;
//   - a line starting with # is a comment: skipped for extraction, kept as it is;
//   - a ===...=== line is a directory section header when it holds a first path token (optional drive, then /,
//     ending at whitespace or an opening parenthesis), otherwise a separator, which clears the current directory context;
//   - an entry line has a non-empty filename before the first [ and a colon right after the ];
//   - entries are collected only inside a directory section (AbsPath non-empty);
//   - CRLF input is folded to LF for parsing (a trailing \r is stripped per line); the editor detects and keeps the original line-ending style;
//   - a parse failure records a Warning and drops no line; line numbers start at 1.
package index

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// The three regexes: the semantic twin of the platform parseCodeIndex coordinate system (the R6 three-way sync contract)
var (
	// consistencyDirRe, the directory section header: === + optional description + absolute path (optional drive X: + /, ending at whitespace, an opening parenthesis, or =) + any trailing note + ===
	// Examples: ===配置索引/opt/aoci-code/===
	//           ===C:/aoci-test/=== (the Windows form)
	consistencyDirRe = regexp.MustCompile(`^===(.*?)((?:[A-Za-z]:)?/[^\s=(（]+)(.*)===\s*$`)
	// consistencySepRe, the separator: any line wrapped in three or more equals signs (applies when no directory header matched), clearing the current directory context
	consistencySepRe = regexp.MustCompile(`^={3,}.*={3,}\s*$`)
	// consistencyEntryRe, the entry line: filename (no [, not starting with whitespace) + [tags] + colon + the rest
	consistencyEntryRe = regexp.MustCompile(`^([^\[\s][^\[]*?)\[([^\]]*)\]:\s?(.*)$`)
)

// Extended readings (#58, #60). The three regexes above stay the first reading,
// so every line that parses today parses the same way. They could not spell two
// kinds of valid repository path: a directory whose name holds whitespace, "=",
// "(" or "（" (the path stopped there, and the writer then reused that truncated
// path as the root of every later section), and a file whose name holds "["
// (Next.js, Nuxt and SvelteKit dynamic routes such as [...id].jsx), which could
// not be written at all and failed its whole atomic batch for good.
var (
	// extendedEntryRe applies only when the legacy reading fails. The tag is then
	// the bracket group immediately followed by ": F:", and holds no bracket.
	extendedEntryRe = regexp.MustCompile(`^(\S.*?)\[([^\[\]]*)\]:\s?(F:.*)$`)
	// secondPathStartRe recognises a human multi-path or annotated header whose
	// remainder starts another absolute path; such a header keeps the legacy reading.
	secondPathStartRe = regexp.MustCompile(`\s(?:[A-Za-z]:)?/`)
)

// matchEntryLine is the one entry-line lexer: legacy first, extended second.
// A match whose name trims to nothing is not an Entry: RE2's \s is ASCII
// while TrimSpace is Unicode, so a line beginning with a vertical tab or a
// no-break space before "[" lexes as an Entry whose file name is empty and
// whose RelPath would collapse to its section directory.
func matchEntryLine(line string) []string {
	m := consistencyEntryRe.FindStringSubmatch(line)
	if m == nil {
		m = extendedEntryRe.FindStringSubmatch(line)
	}
	if m != nil && strings.TrimSpace(m[1]) == "" {
		return nil
	}
	return m
}

// EntryTagSpan locates the tag of an Entry line for every consumer that needs it
// without a full parse (dictionary, E-scale, S-quota, file-name normalization,
// migration byte ranges). It reads the line the way matchEntryLine does, so a
// bracketed file name is never mistaken for the tag. A line neither reading
// accepts keeps the historical loose anchor, the first "[" and the first "]:"
// after it, so nothing that was decidable before becomes undecidable.
// nameEnd is the offset of the "[" that opens the tag.
func EntryTagSpan(line string) (nameEnd, tagStart, tagEnd int, ok bool) {
	for _, re := range []*regexp.Regexp{consistencyEntryRe, extendedEntryRe} {
		if m := re.FindStringSubmatchIndex(line); m != nil {
			return m[4] - 1, m[4], m[5], true
		}
	}
	open := strings.Index(line, "[")
	if open < 0 {
		return 0, 0, 0, false
	}
	closing := strings.Index(line[open:], "]:")
	if closing < 0 {
		return 0, 0, 0, false
	}
	return open, open + 1, open + closing, true
}

// matchDirHeader is the one directory-header lexer. A machine-written header is
// `===<description><absolute directory>/===` with nothing after the directory, so
// when the legacy reading leaves a remainder that itself ends the header with
// "/", that remainder is the rest of the path the legacy character class cut
// off. A remainder that reads as a note (whitespace right after a complete
// directory) or that starts another absolute path keeps the legacy reading.
func matchDirHeader(line string) (name, legacyPath, absPath string, ok bool) {
	m := consistencyDirRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", "", false
	}
	legacyPath = strings.TrimSpace(m[2])
	absPath = legacyPath
	rest := m[3]
	noteLike := strings.HasSuffix(m[2], "/") && rest != "" && (rest[0] == ' ' || rest[0] == '\t')
	if strings.HasSuffix(rest, "/") && !noteLike && !secondPathStartRe.MatchString(rest) {
		absPath = m[2] + rest
	}
	return strings.TrimSpace(m[1]), legacyPath, absPath, true
}

// isLegacyCut reports whether the legacy path character class stops at r.
func isLegacyCut(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\f', '\r', '=', '(', '（':
		return true
	}
	return false
}

// isLegacyTruncationOf reports whether short is what the legacy reading makes of
// full: a prefix of it that ends where a cut character begins a path segment or
// interrupts one.
func isLegacyTruncationOf(short, full string) bool {
	if short == "" || short == full || !strings.HasPrefix(full, short) {
		return false
	}
	rest := strings.TrimPrefix(full[len(short):], "/")
	for _, r := range rest {
		return isLegacyCut(r)
	}
	return false
}

// stripBOM strips the UTF-8 BOM at the start of the text (the single P-18 implementation).
// Only a leading \ufeff is stripped; a BOM inside the body (a concatenation artefact) is a content problem outside this layer.
// Consumers: the Parse entry, ExtractHeader, and ReplaceHeader (lenient on read, healing on write).
func stripBOM(text string) string {
	// Strip every leading BOM: two tools adding one each leave both at the start, and stripping one would leave
	// RawText starting with the second, which another parse would strip, so Parse(RawText) would no longer be a
	// fixed point (FuzzParse seed d03d5667d745b3ab). A BOM inside the body is still outside this layer.
	return strings.TrimLeft(text, "\ufeff")
}

// normalizeRootPath normalizes a repository root or section path: backslashes to forward slashes, trailing slash removed, Windows drive letter upper-cased
// (a header written as c:/repo must still recognize a root passed as C:\repo; only the drive letter is normalized, the rest of the path keeps its case)
func normalizeRootPath(p string) string {
	s := strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if len(s) >= 2 && s[1] == ':' && s[0] >= 'a' && s[0] <= 'z' {
		s = string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

// Parse parses the complete index text.
// It returns the Document (every line preserved) and a list of non-fatal Warnings; only empty input returns an empty document with no warning.
// A leading UTF-8 BOM is stripped before parsing (P-18); RawText carries no BOM.
func Parse(text string) (*Document, []Warning) {
	// After the BOM strip (P-18), CRLF is folded to LF for parsing; keeping the original style is the editor's job
	normalized := strings.ReplaceAll(stripBOM(text), "\r\n", "\n")

	doc := &Document{RawText: normalized}
	var warnings []Warning

	// current is the section being collected; content before the first ===...=== line belongs to HeaderLines
	var current *Section
	inHeader := true

	// seen detects duplicate entries at rel level within one document (key: section absolute path + filename)
	seen := map[string]int{}
	// seenInFirst holds the names of the first directory section when its header has
	// two readings; acrossReadings the names a later section with the same header
	// repeats, which are duplicates only if both are read as one directory.
	seenInFirst := map[string]int{}
	var acrossReadings []Warning

	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		lineNo := i + 1

		// Judge a ===...=== line (directory header or separator)
		if consistencySepRe.MatchString(line) || consistencyDirRe.MatchString(line) {
			inHeader = false
			if name, legacyPath, absPath, ok := matchDirHeader(line); ok {
				// Directory section header: open a new directory section
				current = &Section{
					Name:          name,
					AbsPath:       absPath,
					LegacyAbsPath: legacyPath,
					HeaderLine:    line,
					Entries:       []*Entry{},
					RawLines:      []string{line},
					StartLine:     lineNo,
				}
			} else {
				// Separator: clear the directory context and open a pathless section (content kept as it is)
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

		// Lines before the first section belong to HeaderLines
		if inHeader {
			doc.HeaderLines = append(doc.HeaderLines, line)
			continue
		}
		// Ordinary lines inside a section are kept as they are
		if current != nil {
			current.RawLines = append(current.RawLines, line)
		}

		// Comment and blank lines: kept, not extracted
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Entry extraction: collected only inside a directory section
		if current == nil || current.AbsPath == "" {
			continue
		}
		m := matchEntryLine(line)
		if m == nil {
			// A line shaped like an entry that does not match (a [ without the ]: structure, say) gets a warning for a human to inspect
			if strings.Contains(line, "[") && strings.Contains(line, "]:") {
				warnings = append(warnings, newWarning(lineNo, "疑似条目行但未通过条目正则: "+truncateForWarn(line)))
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

		// Duplicate detection within one section (a duplicate would defeat the "exactly one place" constraint of whole-line replacement, so warn early).
		// The first directory section is its own key space when its header has two
		// readings: it may resolve at the truncated root while a later section with the
		// very same header resolves at the directory the header names (the old writer's
		// first section next to the section a repair adds for that directory). The same
		// file name in both is then two different paths, and a warning here would refuse
		// every write to that index for good. Whether it is read that way is a function
		// of the text alone, so it is decided below, once every section is known.
		key := current.AbsPath + "\x00" + filename
		twoReadings := current == firstDirectorySection(doc) && current.LegacyAbsPath != current.AbsPath
		switch {
		case twoReadings:
			if prev, ok := seenInFirst[key]; ok {
				warnings = append(warnings, newWarning(lineNo, "重复条目: 与第 "+strconv.Itoa(prev)+" 行同段同名("+filename+")"))
			} else {
				seenInFirst[key] = lineNo
			}
		default:
			if prev, ok := seen[key]; ok {
				warnings = append(warnings, newWarning(lineNo, "重复条目: 与第 "+strconv.Itoa(prev)+" 行同段同名("+filename+")"))
			} else {
				seen[key] = lineNo
				if prev, ok := seenInFirst[key]; ok {
					acrossReadings = append(acrossReadings, newWarning(lineNo, "重复条目: 与第 "+strconv.Itoa(prev)+" 行同段同名("+filename+")"))
				}
			}
		}

		current.Entries = append(current.Entries, entry)
	}

	// A name shared by the first section and a later one with the very same header is
	// two paths only when the first section is read through its legacy path, which
	// is exactly when the text-only old-writer test holds. Otherwise both sections
	// name one directory and the Entries are one path twice: a true duplicate, which
	// the single-file layout has no other place to report.
	if len(acrossReadings) > 0 {
		var directories []*Section
		for _, sec := range doc.Sections {
			if sec.AbsPath != "" {
				directories = append(directories, sec)
			}
		}
		if _, oldWriter := oldWriterReadings(directories, firstDirectorySection(doc)); !oldWriter {
			warnings = append(warnings, acrossReadings...)
		}
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
	m := matchEntryLine(line)
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

// splitFRAS splits an entry body into its F/R/A/S parts.
// It splits on " | " and recognizes the F:/R:/A:/S: prefixes (the S1:/S2:/S3: variants merge into S);
// a segment with an unrecognized prefix is ignored (FullLine keeps the complete original; the split fields serve search and display only).
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
			// The S1:/S2:/S3: variants (present in real platform indexes) merge into S
			idx := strings.Index(seg, ":")
			sParts = append(sParts, strings.TrimSpace(seg[idx+1:]))
		}
	}
	s = strings.Join(sParts, " | ")
	return
}

// regexpSVariant matches the variant prefix of S followed by a digit, such as "S1:" or "S2:"
var regexpSVariant = regexp.MustCompile(`^S\d+:`)

// resolveSectionRels computes every directory section's directory relative to the current repository root (the single section-resolution implementation).
// It returns map[section pointer]relDir; a section absent from the map could not be resolved (its entries take no part in rel matching).
//
// Section-by-section resolution (the 2026-07-10 second-round refinement):
//  1. when the complete historical root lies below the current root, relocate by the historical root first;
//  2. every other section tries a direct prefix comparison: a section path equal to the root records "", one below the root records the path with the prefix stripped, and a hit is used;
//  3. the sections that all missed form their own set: the longest common directory prefix of their paths is taken
//     as the "root at creation", and each section stripped of it is its move-invariant relative directory;
//  4. when the missing set has no common prefix, leave it empty rather than guess.
//
// The first version's "any hit section abandons all relocation" is retired: an agent inserting a new section
// (headed by the current root) inside a moved repository triggered a mixed hit and every existing section went
// blind, while "a new section after a move" is almost the only real cause of mixed roots; under the per-section rule the old and the new world each stand.
func resolveSectionRels(doc *Document, repoRoot string) map[*Section]string {
	readings := resolveSectionReadings(doc, repoRoot)
	rels := make(map[*Section]string, len(readings))
	for sec, reading := range readings {
		rels[sec] = reading.rel
	}
	return rels
}

// sectionReading is how one directory section resolves against the repository:
// its directory relative to the root, and whether that came from the legacy
// reading of its header (the old writer's first section) rather than the
// extended one. The writer needs the second fact to recover the historical root
// from the same path the reader used.
type sectionReading struct {
	rel    string
	legacy bool
}

// path returns the header path this reading was computed from.
func (r sectionReading) path(sec *Section) string {
	if r.legacy {
		return sec.LegacyAbsPath
	}
	return sec.AbsPath
}

func resolveSectionReadings(doc *Document, repoRoot string) map[*Section]sectionReading {
	root := normalizeRootPath(repoRoot)
	first := firstDirectorySection(doc)
	var directories []*Section
	for _, sec := range doc.Sections {
		if sec.AbsPath != "" {
			directories = append(directories, sec)
		}
	}

	// The old writer's index is recognised from its text alone, before the runtime
	// root is consulted at all. A clone has no repository root to compare against,
	// so any reading that used the root here would make the origin and its clones
	// resolve the same bytes to different paths; four reviews each found one such
	// reading. What this costs is deliberate: where those bytes were written under a
	// clean root (a top-level directory whose name begins with a cut character, filed
	// first), the origin reads them as the old reader did rather than as it could.
	if readings, ok := oldWriterReadings(directories, first); ok {
		return readings
	}

	// A nested worktree's recorded root is a child of its primary checkout.
	// Its root section and descendants form one historical family, even though
	// direct matching would file every Entry under the worktree path. A lone
	// section cannot distinguish that root from an ordinary subdirectory.
	if len(directories) > 1 && first != nil {
		recordedRoot := normalizeRootPath(first.AbsPath)
		if rel, ok := relUnder(recordedRoot, root); ok && rel != "" {
			if relocated, hasRoot := relocateExtended(directories); hasRoot && len(relocated) == len(directories) {
				if reading, rooted := relocated[first]; rooted && reading.rel == "" {
					return relocated
				}
			}
		}
	}

	readings := map[*Section]sectionReading{}
	var missed []*Section

	// First pass: direct prefix comparison per section; the sections that miss are collected into the subset
	for _, sec := range directories {
		if rel, ok := relUnder(normalizeRootPath(sec.AbsPath), root); ok {
			readings[sec] = sectionReading{rel: rel}
			continue
		}
		missed = append(missed, sec)
	}
	if len(missed) == 0 {
		return readings
	}

	// In-place healing for an index that mixes the old writer's family with sections
	// some other writer spelled under the runtime root, which the text-only test
	// above rejects: the first section resolves directly, its legacy reading falls
	// outside the repository root and is the truncation of that root, and the missed
	// sections hang from that truncated root.
	if _, direct := readings[first]; first != nil && direct {
		legacy := normalizeRootPath(first.LegacyAbsPath)
		_, inside := relUnder(legacy, root)
		if legacy != "" && legacy != normalizeRootPath(first.AbsPath) && !inside && isLegacyTruncationOf(legacy, root) {
			var still []*Section
			for _, sec := range missed {
				if rel, ok := relUnder(normalizeRootPath(sec.AbsPath), legacy); ok {
					readings[sec] = sectionReading{rel: rel}
					continue
				}
				still = append(still, sec)
			}
			if len(still) < len(missed) {
				readings[first] = sectionReading{rel: "", legacy: true}
			}
			missed = still
			if len(missed) == 0 {
				return readings
			}
		}
	}

	// Second pass: compute the longest common directory prefix within the missing subset and relocate. Try the root
	// under the extended reading first; when no section sits at the root under that reading, try the "old writer" reading over the subset, which wins when it holds.
	extended, extendedHasRoot := relocateExtended(missed)
	chosen := extended
	if !extendedHasRoot {
		if legacy, ok := oldWriterReadings(missed, first); ok {
			chosen = legacy
		}
	}
	for sec, reading := range chosen {
		readings[sec] = reading
	}
	return readings
}

// firstDirectorySection returns the first section that carries a directory path.
func firstDirectorySection(doc *Document) *Section {
	for _, sec := range doc.Sections {
		if sec.AbsPath != "" {
			return sec
		}
	}
	return nil
}

// relocateExtended resolves the missed sections against the longest common
// directory prefix of their effective paths, and reports whether any of them is
// the root section under that prefix.
func relocateExtended(missed []*Section) (map[*Section]sectionReading, bool) {
	prefix := commonDirPrefixOf(missed, func(sec *Section) string { return sec.AbsPath })
	if prefix == "" {
		return nil, false
	}
	out := map[*Section]sectionReading{}
	hasRoot := false
	for _, sec := range missed {
		if rel, ok := relUnder(normalizeRootPath(sec.AbsPath), prefix); ok {
			out[sec] = sectionReading{rel: rel}
			hasRoot = hasRoot || rel == ""
		}
	}
	return out, hasRoot
}

// oldWriterReadings reads sections as an index the old writer (every release up to
// v0.1.0-rc13) left behind under a root holding a cut character: that root was
// written in full exactly once, by the first directory section, and every later
// section was appended under the truncated root the parser read back. The old
// reader resolved that first section at the truncated root, whichever directory
// its header really names, and the old writer filed root files under it; every
// other section resolves below the truncated root by its effective path, so that a
// directory whose own name holds a cut character still reads in full.
//
// It is a function of the text alone, and it holds only when exactly that shape
// is present:
//
//   - the first directory section is among the sections, reads as their common
//     legacy prefix under the original reading, and as something else under the
//     extended one;
//   - every other section resolves strictly below that prefix. One outside it is a
//     second full-spelled section, which the old writer never wrote; one at the
//     prefix itself is a plain root, which it never wrote beside its first section
//     either, and which therefore means siblings such as app/ and "app (old)/".
//
// The first section's effective path may lie beside the prefix
// (/tmp/example project/repo against /tmp/example) or under it, when the cut
// character begins a path segment (/w/(x)/repo against /w); both are the same
// writer, and nothing further is asked of the under case. An earlier version also
// required the effective paths to share the prefix there, to tell the old writer
// from an index whose sections all carry the full root. No released writer
// produces such an index, and the requirement broke one this writer does produce:
// under /w/(x)/repo a directory named (x), the root's own cut segment, puts every
// other section under /w/(x), so the origin (which falls back to the runtime
// root) and a clone (which cannot) read the same bytes differently.
//
// A single full-spelled section qualifies on its own: the old reader resolved it
// at the truncated root, and so does every clone.
//
// The writer keeps these bytes unambiguous from its side: a new index begins with
// its root section, so the first directory section of an index this writer
// created never reads as something else at a plain prefix of the rest.
func oldWriterReadings(sections []*Section, first *Section) (map[*Section]sectionReading, bool) {
	if first == nil || len(sections) == 0 {
		return nil, false
	}
	prefix := commonDirPrefixOf(sections, func(sec *Section) string { return sec.LegacyAbsPath })
	if prefix == "" {
		return nil, false
	}
	out := map[*Section]sectionReading{}
	fullSpelled := false
	for _, sec := range sections {
		effective := normalizeRootPath(sec.AbsPath)
		if sec == first {
			if normalizeRootPath(sec.LegacyAbsPath) != prefix || effective == prefix {
				return nil, false
			}
			out[sec] = sectionReading{rel: "", legacy: true}
			fullSpelled = true
			continue
		}
		rel, ok := relUnder(effective, prefix)
		if !ok || rel == "" {
			return nil, false
		}
		out[sec] = sectionReading{rel: rel}
	}
	if !fullSpelled {
		return nil, false
	}
	return out, true
}

// relUnder reports the path of p relative to base when p is base or lies under it.
func relUnder(p, base string) (string, bool) {
	switch {
	case base == "":
		return "", false
	case p == base:
		return "", true
	case strings.HasPrefix(p, base+"/"):
		return strings.TrimPrefix(p, base+"/"), true
	}
	return "", false
}

// commonDirPrefixOf computes the longest common directory prefix of the (normalized) section paths.
// It compares level by level on "/", never character by character (so /opt/httpx and /opt/httpx2 are not cut to /opt/httpx).
// Different Windows drives (C: and D:) differ at the first level, so the common prefix is empty.
// When every path is identical (a single-section index) the common prefix is the path itself, which strips to the root section.
//
// The caller selects which reading of each header is compared: the effective
// path, or the legacy one when an index written by the old writer is relocated.
func commonDirPrefixOf(sections []*Section, pathOf func(*Section) string) string {
	if len(sections) == 0 {
		return ""
	}
	parts := strings.Split(normalizeRootPath(pathOf(sections[0])), "/")
	for _, sec := range sections[1:] {
		cur := strings.Split(normalizeRootPath(pathOf(sec)), "/")
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
	// A bare root ("/" splits to ["", ""] and joins back to "" or "/") is not a meaningful root at creation
	if prefix == "" || prefix == "/" {
		return ""
	}
	return prefix
}

// ResolveRelPaths computes every entry's RelPath (forward slashes) against the repository root.
// Section resolution (including relocation of a moved repository) goes through resolveSectionRels; the entries of
// a section that cannot be resolved keep an empty RelPath (and take no part in path matching).
func ResolveRelPaths(doc *Document, repoRoot string) {
	rels := resolveSectionRels(doc, repoRoot)
	for _, sec := range doc.Sections {
		relDir, ok := rels[sec]
		if !ok {
			continue // the section could not be resolved (the missing subset has no common prefix); its entries take no part in rel matching
		}
		for _, e := range sec.Entries {
			// The filename itself may carry a directory prefix (scripts/check-public-text.sh, say) or be a directory entry (trailing slash)
			name := strings.TrimSpace(e.Filename)
			joined := path.Join(relDir, strings.TrimSuffix(name, "/"))
			if strings.HasSuffix(name, "/") {
				joined += "/"
			}
			e.RelPath = joined
		}
	}
}

// FindSectionForPath locates the directory section relPath belongs to: an exact directory match (the semantics of the platform's insertIntoDirSection).
// It hits only when the section's relative directory equals relPath's parent directory exactly; among several sections of one directory the first in document order wins.
// The exact match is a deliberate tightening: the earlier prefix match made the root section (empty secRel) a wildcard that
// swallowed every new-directory insertion (smoke scenario 4), and a multi-path header such as "internal/ and testdata/" mismatched neighbouring directories by prefix.
// Section resolution shares resolveSectionRels with ResolveRelPaths (a hidden twin): inside a moved repository the
// insertion lookup and the rel resolution must agree, so "verify recognizes the section but the insertion lookup misses" cannot happen.
// It returns nil when nothing matches, and the caller takes the "append a new section header at the end" branch.
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

// FindEntry looks up an entry by exact repository-relative path.
// ResolveRelPaths must run first; only an exact rel_path match counts, and a filename-only match may serve hints
// but never an automatic write (the defence against editing a same-named file). It returns nil when nothing matches.
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

// FindEntriesByFilename looks up entries by filename (the last segment), for hints and disambiguation display only, never for an automatic write
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

// newWarning builds a Warning whose text is always valid UTF-8. The fragments
// quoted from the index (a suspicious line, a duplicated file name) carry
// whatever bytes the file holds, and a Warning travels on into JSON responses
// and CLI lines; invalid bytes become U+FFFD here, once, instead of being
// coerced or refused downstream.
func newWarning(lineNo int, msg string) Warning {
	return Warning{LineNo: lineNo, Msg: strings.ToValidUTF8(msg, "\uFFFD")}
}

// truncateForWarn truncates warning text so an overlong line does not flood the output (truncating by rune so multi-byte characters are not cut)
func truncateForWarn(line string) string {
	runes := []rune(line)
	if len(runes) <= 60 {
		return line
	}
	return string(runes[:60]) + "…"
}
