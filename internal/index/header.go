// Index header-area primitives: extract / structural validation / whole replacement / line-level diff
// Index entry: header.go[IHD8S]
//
// Header-area definition (the same source as Parse in parser.go): every line before the first ===...=== line
// of the text (a directory section header or a separator, judged by consistencyDirRe / consistencySepRe).
// Parse collects those lines into Document.HeaderLines; this file's boundary judgement reuses the same pair
// of regular expressions directly, so "the header" always names the same text on the parsing and the editing side.
//
// This file is pure text transformation (whole text in, new whole text out) and never writes to disk;
// the caller writes through internal/fs.AtomicWrite (the same discipline as editor.go).
//
// Design constraints:
//   - The BOM reading matches Parse (P-18): ExtractHeader/ReplaceHeader strip a leading UTF-8 BOM before
//     processing. With a first line of ===...=== carrying a BOM, the boundary judgement would file the section
//     header under the header area, and ReplaceHeader would swallow the first directory section in the name of
//     "changing the header"; reading is lenient, and ReplaceHeader writes back without the BOM (the spec says
//     UTF-8 without BOM, the write side heals it in passing, a one-time diff with the caller's backup in place);
//   - ReplaceHeader touches only the header area; the entry area (from the first === line to the end) keeps every byte;
//   - Two hard structural rules for a new header (ValidateHeaderText, aligned with the prompt layer's promise to the model):
//     first, no line of the ===...=== shape, since that shape moves the header/entry boundary forward, changing
//     the document structure in the name of "changing the header";
//     second, every non-empty line must start with #, since the header is explanatory content in the three-way
//     split, and this rule also catches entry-shaped lines and explanatory prose from the model (neither starts with #);
//     both rules bind only the NEW header; reading a historical index with bare header lines is unaffected (the
//     spec's compatibility form "a header line without # is not collected" still holds, and an old file's header is extracted and kept as it is);
//   - The newline discipline matches editor.go: the original newline style (LF/CRLF) and the presence of a trailing
//     newline are detected and kept per file, so one write-back cannot produce a whole-file false diff; a new
//     header offered in LF or CRLF is reassembled in the target file's original style;
//   - Content reasonableness (forbidden words, dictionary quality) is not this layer's job; the structural
//     primitives stay "dumb", and discipline checks belong to the upper layer (the safety gate and the apply command).
package index

import (
	"fmt"
	"strings"
)

// headerBoundary returns the header/entry boundary index (0-based):
// the index of the first line matching the directory-header or separator regular expression; len(lines) when the text has no === line.
// The judgement matches Parse's inHeader flip condition character for character (the two package regular expressions are shared).
func headerBoundary(lines []string) int {
	for i, line := range lines {
		if consistencySepRe.MatchString(line) || consistencyDirRe.MatchString(line) {
			return i
		}
	}
	return len(lines)
}

// ExtractHeader extracts the header-area text.
// The returned header is joined with LF (for display, diff, and draft comparison; it does not carry the original file's newline style);
// hasSections=false means the text has no === line at all (such as a very early skeleton), and the header is then the whole text.
// Note: extraction performs no validation; the bare header lines of a historical index are extracted as they are (read old, write new).
// A leading BOM is stripped before processing (P-18, the same reading as Parse; otherwise a BOM-carrying first-line
// section header would be filed under the header by the boundary judgement).
func ExtractHeader(text string) (header string, hasSections bool) {
	lines, _, _ := splitPreserve(stripBOM(text))
	b := headerBoundary(lines)
	return strings.Join(lines[:b], "\n"), b < len(lines)
}

// ValidateHeaderText validates the structural legality of a candidate header text, shared by ReplaceHeader and
// the upper-layer apply/draft prechecks. Two hard structural constraints (see the package comment):
//  1. no line of the ===...=== shape (it changes the header/entry boundary);
//  2. every non-empty line must start with # (the three-way-split header discipline, which also catches entry lines and prose lines).
//
// It returns the first offending line's number (1-based) and an explanation; a fully legal header returns (0, "").
// An empty header is structurally legal (whether clearing is allowed is the caller's decision), and this function lets it pass.
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

// ReplaceHeader replaces the header area of text with newHeader as a whole.
// The entry area (from the first === line to the end) keeps every byte; the original newline style and trailing newline are kept.
// newHeader may be in LF or CRLF style (it is folded to LF internally and reassembled in the original style);
// an empty newHeader clears the header (structurally legal; content reasonableness is the caller's job).
// A leading BOM is stripped before processing (P-18): with a BOM the boundary judgement misfires and swallows the first directory section;
// the stripped text is written back without the BOM, a heal in passing on the write side (the spec says UTF-8 without BOM),
// a one-time diff with the caller's pipeline backup in place.
func ReplaceHeader(text, newHeader string) (string, error) {
	if ln, msg := ValidateHeaderText(newHeader); ln > 0 {
		return "", fmt.Errorf("新头部第 %d 行非法: %s", ln, msg)
	}
	lines, eol, trailingNL := splitPreserve(stripBOM(text))
	b := headerBoundary(lines)

	// The new header is split on LF; its trailing newline is stripped, and joinPreserve restores the original style uniformly
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
