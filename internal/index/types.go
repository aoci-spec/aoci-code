// Index document structures and tag parsing
// Index entry: types.go[ITY9S]
//
// Core conventions:
//   - Entry.FullLine keeps the original whole line and is the canonical form for replacement and comparison;
//     the split fields F/R/Api/S serve search and display only and are never used to re-render a write
//     (re-rendering would lose the original spacing and punctuation, creating false diffs and spoiling the "the diff is the change record" promise).
//   - Unknown lines, comments, and blank lines are kept as they are in Section.RawLines; an edit touches only the target entry line.
//   - ParseTags accepts two forms: with a dot it splits on dots, otherwise it follows the platform's compact rule character by character;
//     a parse failure leaves TagsParsed empty without an error (tolerating hand-written non-standard tags; search degrades to full-text matching).
//   - The comma-separated form is non-standard (the 2026-07-13 operator decision, one of three: downgrade as non-standard,
//     refuse hard, or parse tolerantly; downgrade won): [I,CL,C8,EL] is a form the drafting model invented outside the
//     protocol during the dictionary gate's false-green period in the httpx-rerun live run; the old heuristic split it into
//     a semantically garbage but non-empty map (the B dimension became ",CL,C"), a third state between "legal" and
//     "non-standard" where the dict gate warned but the P-15 unparseable warning did not fire. The reasoning: a hard
//     refusal blocks every write of an existing repository (against the warn-and-pass philosophy); tolerant parsing would
//     license a malformed form born in a gate's false-green period and widen the three-way parsing surface; reading it as
//     non-standard sends the empty map into the P-15 warning chain (naming the tag and stating that both gates skipped,
//     a visible skip), a path a real machine proved drives the agent to correct itself.
//     Protocol wording: the legal forms are the compact run and the dotted form only; a comma (half- or full-width) makes it non-standard.
package index

import "strings"

// Document is the parse result of one complete AOCI index text
type Document struct {
	// HeaderLines are all the original lines before the first directory section (the header rules, the tag dictionary, and so on), kept as they are
	HeaderLines []string
	// Sections are every section (directory and non-directory alike), in order of appearance
	Sections []*Section
	// RawText is the original full text at parse time (folded to LF), for whole-document editor operations
	RawText string
}

// Section is one segment delimited by a ===...=== line
type Section struct {
	// Name is the descriptive text of the header apart from the path (may be empty)
	Name string
	// AbsPath is the absolute path extracted from the header (starting with /); empty for a non-directory section
	AbsPath string
	// LegacyAbsPath is what the original three-regex grammar reads from the same
	// header: the path stops at the first whitespace, "=", "(" or "（". It equals
	// AbsPath unless the extended reading applied. Indexes written while only that
	// reading existed carry truncated roots in their later sections, and this is
	// the evidence that lets them keep resolving.
	LegacyAbsPath string
	// HeaderLine is the original whole header line
	HeaderLine string
	// Entries are the entries parsed successfully in this section; only a directory section (AbsPath non-empty) collects entries
	Entries []*Entry
	// RawLines are all the original lines of the section (header, comments, blank lines, entry lines), kept as they are as the reference for a whole-document write
	RawLines []string
	// StartLine is the header's line number in the full text (starting at 1)
	StartLine int
}

// Entry is one index entry (a single line)
type Entry struct {
	// RelPath is the path relative to the repository root (forward slashes); ResolveRelPaths fills it against repoRoot,
	// and it is empty when the section path is not below the repository root
	RelPath string
	// Filename is the filename part before the [ of the entry line (it may carry a directory prefix such as scripts/db_backup.sh,
	// or be a directory entry such as verify_history/)
	Filename string
	// TagsRaw is the original tag string inside the square brackets
	TagsRaw string
	// TagsParsed is the five-dimension tag parse keyed A/B/C/D/E; an empty map when parsing failed
	TagsParsed map[string]string
	// F/R/Api/S are the four split parts (search and display only; the S1/S2/S3 variants merge into S)
	F   string
	R   string
	Api string
	S   string
	// FullLine is the original whole line (canonical; replacement and comparison use it)
	FullLine string
	// LineNo is the line number in the full text (starting at 1)
	LineNo int
}

// Warning is a non-fatal problem met while parsing (no line is dropped, only recorded)
type Warning struct {
	LineNo int    // line number, starting at 1
	Msg    string // description of the problem
}

// ParseTags parses tags in either of two forms.
// Dotted form (contains "."): [Index.Types.9.S] with 4 parts = A.B.C.E; [FS.Paths.9.Xp.S] with 5 parts = A.B.C.D.E;
// any other part count is non-standard and returns an empty map.
// Compact form (no "."): the platform heuristic: the first character is the A layer, up to the first digit is the B
// module, that digit is the C importance, the last character is the E scale, and whatever lies between the digit
// and the last character is the D trait (may be empty); no digit is non-standard and returns an empty map. Example WA9JM: A=W, B=A, C=9, D=J, E=M.
//
// The comma form is non-standard (operator decision, see the file header): a half- or full-width comma inside the
// tag (such as [I,CL,C8,EL]) returns an empty map at once, entering no heuristic split of either form, so no
// semantically garbage non-empty map can put the entry into a third state between "legal" and "non-standard"; the
// empty map fires the P-15 unparseable warning, and the skip of the dict and escale gates is visible to the writer.
//
// Dictionary discipline (the heuristic's precondition): the compact form's B module and D trait symbols use no digits,
// since "the first digit is C" is the split anchor and a B holding a digit (V2, say) would be mis-split as the importance.
// The dictionary's authors keep this constraint (the tool does not check it), and the Spec declares it too.
func ParseTags(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}
	}
	// The comma form is non-standard (half- and full-width; the decision is in the file header)
	if strings.ContainsAny(raw, ",，") {
		return map[string]string{}
	}
	if strings.Contains(raw, ".") {
		return parseDotTags(raw)
	}
	return parseCompactTags(raw)
}

// parseDotTags parses the dotted form
func parseDotTags(raw string) map[string]string {
	segs := strings.Split(raw, ".")
	out := map[string]string{}
	switch len(segs) {
	case 4: // A.B.C.E
		out["A"], out["B"], out["C"], out["E"] = segs[0], segs[1], segs[2], segs[3]
	case 5: // A.B.C.D.E
		out["A"], out["B"], out["C"], out["D"], out["E"] = segs[0], segs[1], segs[2], segs[3], segs[4]
	default:
		return map[string]string{} // a non-standard part count, downgraded to no tags
	}
	// Any empty part is non-standard too
	for _, v := range out {
		if strings.TrimSpace(v) == "" {
			return map[string]string{}
		}
	}
	return out
}

// parseCompactTags parses the platform's compact form heuristically (meta-protocol compatibility: the CLI reads the platform's old indexes)
func parseCompactTags(raw string) map[string]string {
	runes := []rune(raw)
	if len(runes) < 3 {
		return map[string]string{}
	}
	// Locate the first digit (the C importance)
	digitIdx := -1
	for i, r := range runes {
		if r >= '0' && r <= '9' {
			digitIdx = i
			break
		}
	}
	// No digit, a digit in first place (missing A and B), or a digit in last place (missing E) are all non-standard
	if digitIdx <= 0 || digitIdx >= len(runes)-1 {
		return map[string]string{}
	}
	out := map[string]string{}
	out["A"] = string(runes[0])
	if digitIdx > 1 {
		out["B"] = string(runes[1:digitIdx])
	} else {
		out["B"] = "" // a digit right after the first character: a platform tag has at least one B character, tolerated as empty here
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
