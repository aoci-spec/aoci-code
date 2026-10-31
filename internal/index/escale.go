// E-scale validation: checks an entry's E scale against the file's actual line count by the header dictionary's thresholds (Warning level)
// Index entry: escale.go[IVF7M]
//
// Motivation (the 2026-07-11 bootstrap live run): one human review batch caught three E-scale mismatches (index.go
// at 306 lines tagged L but really M, index_entries.go at 409 lines tagged M but really L, init_test.go at about
// 150 lines tagged T but really S), and the nine-reviewer httpx run had 2 more among 16 edits. The E scale drifts as
// files grow naturally; the line count is machine-readable and the boundaries live in the header dictionary, so it is fully decidable, and leaving it to human review was a machine gate idling.
//
// The level is Warning (the same philosophy as the quota gate): scale drift usually comes from file growth with
// the entry's meaning still right; a hard refusal would block every write unrelated to E, while a warning lets the
// writer see it and update it in passing, keeping the decision with the human.
//
// Threshold dictionary driven (the same philosophy as the dict gate): the boundaries are not hard-coded but parsed
// from the header's E-scale line; a repository whose header declares no numeric thresholds is undecidable and skipped,
// lenient rather than wrong (HasThresholds has the same semantics as HasDict). Three numeric forms are supported:
// >N / N-M / <N (also >=N / <=N and the full-width ＞＜＝); any other symbol reads as no threshold and takes no part in the judgement.
//
// Threshold ownership (the 2026-07-13 operator decision, a P-22 side ruling): the E-scale line-count boundaries follow
// the repository header's declaration; users may customize them, the numbers in the spec are only the default
// template, and this gate does not check against the spec's numbers; a header declaring >400 or >300 is that repository's legal boundary.
//
// Single line recognition (the P-22 fix, 2026-07-13): the old implementation carried its own HasPrefix(s,"E规模:")
// matching, the same disease the dict gate had before P-21: any AI-drafted variant (a space after #, a full-width
// colon, a dimension name with a parenthesized note such as "E规模(行数):") matched nothing, HasThresholds read
// false, and the scale gate skipped silently (the third false-green case, after the P-15 missing tag and the P-21
// dictionary line). The fix routes line recognition through the single parseDictLine (dict.go, the P-21 three-form
// tolerance) and this gate never holds a second copy of line recognition (one fact source for the criterion); the
// content side is deliberately asymmetric to the dict gate: dict must strip the parenthesized note when it collects
// symbols so example paths do not pollute it, while this gate must keep the note when it takes numbers (a threshold
// may be written inside parentheses, such as "L大(>400行)"); the asymmetry is design, not an omission.
// A Chinese suffix after the number (">400行") is tolerated naturally (leadingInt stops at the first non-digit).
//
// Visible skips (R51/14.6): sawLine records whether the E-scale line was present; "present but the thresholds cannot
// be extracted" and "the line does not exist" are two states for the upper layers (score/maintain) to show apart
// (the same typing as P-21's D97), against silent-skip false greens; the upper-layer wording lands with the next score.go change.
//
// Boundary tolerance (deliberate): adjacent scales sharing a boundary value is a common dictionary form (M中200-400 and
// S小100-200 overlap at 200 lines); when a line count falls into several scales, an entry tagged with any of them
// conforms; it warns only when the tagged scale does not contain the line count at all and the line count does have
// a scale; a line count in a dictionary gap (no scale contains it) is skipped too, since the dictionary's completeness belongs to the curator, not to this gate.
//
// The E symbol split reuses ParseTags (the dict gate's discipline): there is one split rule, and this gate judges only
// "does the scale of the split-out E symbol contain this line count"; symbol validity belongs to the dict gate and is not reported twice.
//
// A single reverse lookup (R60-F.9-A4, 2026-07-18): the "line count to its scale set" lookup used to be inline in
// CheckEScale and is now the exported ExpectedEScaleSymbols, which the Guide layer uses to prefill expected_e for
// Entries targets (a deterministic field the model should not guess; in the HTTPX live run the agent kept guessing E
// because expected appeared only in the Warning afterwards, too late); CheckEScale calls it, keeping one criterion with no second copy of the logic.
package index

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// escaleRange is one E symbol's line-count interval (closed)
type escaleRange struct {
	min int
	max int
}

// EScaleThresholds is the E-scale threshold table extracted from the header (key = E symbol)
type EScaleThresholds struct {
	ranges  map[string]escaleRange
	sawLine bool // whether the E-scale line was present (including the present-but-unextractable form, for the R51 visible-skip typing)
}

// HasThresholds reports whether the table can be used for validation (at least one symbol has a legal numeric threshold).
// It returns false when the header's E-scale line declares no numbers (a skeleton placeholder such as "L大 M中 S小 T微"),
// and the caller should skip the check (undecidable is not a false report).
func (t *EScaleThresholds) HasThresholds() bool {
	return t != nil && len(t.ranges) > 0
}

// Symbols returns the declared machine-readable bands in stable order. It is
// also the upper bound on any ExpectedEScaleSymbols result for wire sizing.
func (t *EScaleThresholds) Symbols() []string {
	if t == nil {
		return nil
	}
	symbols := make([]string, 0, len(t.ranges))
	for symbol := range t.ranges {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

// SawEScaleLine reports whether the header holds a dictionary line recognized as E-scale (whether or not thresholds were extracted).
// Combined with HasThresholds it yields three states (R51 visible-skip typing):
// no line (undeclared) / a line without thresholds (present but machine-unparseable, which the upper layer should name
// rather than report as undeclared) / a line with thresholds (decidable).
func (t *EScaleThresholds) SawEScaleLine() bool {
	return t != nil && t.sawLine
}

// escaleFullWidthOps normalizes full-width comparison operators (a common variant from AI drafts and Chinese input methods).
// Full-width digits are not normalized: no live sample exists, and guessing forms would violate lenient-without-inventing.
var escaleFullWidthOps = strings.NewReplacer("＞", ">", "＜", "<", "＝", "=")

// ExtractEScaleThresholds extracts the E-scale thresholds from the header text.
// Line recognition goes through the single parseDictLine (P-22, sharing the dict gate's tolerance: a space after #, a
// full-width colon, a dimension name with a parenthesized note); the content is split on whitespace, each word takes
// its leading ASCII letters as the symbol and parses the numeric form from the rest. The content side does not strip
// the parenthesized note (deliberately asymmetric to the dict gate, since a threshold may sit inside parentheses); a word without a symbol prefix (a bare "(>400)") is skipped rather than guessed.
func ExtractEScaleThresholds(headerText string) *EScaleThresholds {
	t := &EScaleThresholds{ranges: map[string]escaleRange{}}
	for _, line := range strings.Split(headerText, "\n") {
		dim, content, ok := parseDictLine(line)
		if !ok || dim != "E规模" {
			continue
		}
		t.sawLine = true
		for _, word := range strings.Fields(content) {
			sym := leadingASCIILetters(word)
			if sym == "" {
				continue
			}
			if r, ok := parseEScaleSpec(word[len(sym):]); ok {
				t.ranges[sym] = r
			}
		}
	}
	return t
}

// parseEScaleSpec parses one scale word's numeric form: >N / >=N / N-M / <N / <=N.
// rest may carry a Chinese description prefix (such as "大>400") and a parenthesized note (such as "大(>400行)"); the
// comparison operator and digits are located by ASCII byte, which is safe because UTF-8 multi-byte sequences hold
// no ASCII byte; full-width operators are normalized to half-width first; a non-digit suffix after the number (such as "行") ends it naturally.
// A bare number (no interval meaning) and a reversed interval are not guessed and return false.
func parseEScaleSpec(rest string) (escaleRange, bool) {
	rest = escaleFullWidthOps.Replace(rest)
	i := strings.IndexAny(rest, "<>0123456789")
	if i < 0 {
		return escaleRange{}, false
	}
	switch rest[i] {
	case '>':
		j := i + 1
		eq := false
		if j < len(rest) && rest[j] == '=' {
			eq = true
			j++
		}
		n, ok := leadingInt(rest[j:])
		if !ok {
			return escaleRange{}, false
		}
		if eq {
			return escaleRange{min: n, max: math.MaxInt}, true
		}
		return escaleRange{min: n + 1, max: math.MaxInt}, true
	case '<':
		j := i + 1
		eq := false
		if j < len(rest) && rest[j] == '=' {
			eq = true
			j++
		}
		n, ok := leadingInt(rest[j:])
		if !ok {
			return escaleRange{}, false
		}
		if eq {
			return escaleRange{min: 0, max: n}, true
		}
		return escaleRange{min: 0, max: n - 1}, true
	default: // starts with a digit: only the N-M interval form counts
		n1, rest2, ok := leadingIntRest(rest[i:])
		if !ok {
			return escaleRange{}, false
		}
		if !strings.HasPrefix(rest2, "-") {
			return escaleRange{}, false
		}
		n2, ok := leadingInt(rest2[1:])
		if !ok || n2 < n1 {
			return escaleRange{}, false
		}
		return escaleRange{min: n1, max: n2}, true
	}
}

// leadingInt takes the leading digits of the string as an integer; no digits returns false.
func leadingInt(s string) (int, bool) {
	n, _, ok := leadingIntRest(s)
	return n, ok
}

// leadingIntRest takes the leading digits of the string as an integer and returns the remainder.
func leadingIntRest(s string) (int, string, bool) {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, s, false
	}
	return n, s[end:], true
}

// ExpectedEScaleSymbols returns every E symbol whose threshold contains the given line count (in a stable order).
// It is the one fact source of the reverse lookup (A4): CheckEScale and the Guide prefill share it and never keep a
// second copy of the interval judgement. An unusable table or a line count in a dictionary gap returns an empty slice (no guessing).
func ExpectedEScaleSymbols(fileLines int, th *EScaleThresholds) []string {
	if !th.HasThresholds() {
		return nil
	}
	var expected []string
	for sym, sr := range th.ranges {
		if fileLines >= sr.min && fileLines <= sr.max {
			expected = append(expected, sym)
		}
	}
	sort.Strings(expected)
	return expected
}

// CheckEScale checks an entry's E scale against the file's actual line count (Warning level).
// fileLines comes from the caller (the fact source is fs.CountFileLines, whose semantics count a last line without a
// newline as one); skipping directory entries and files not on disk is the caller's judgement too.
// nil means conforming or undecidable: table unusable / no tag structure / non-standard tag / E symbol without a
// threshold / line count in a dictionary gap / line count inside the tagged scale (boundary overlap included).
func CheckEScale(line string, fileLines int, th *EScaleThresholds) *Violation {
	mismatch := CheckEScaleDetail(line, fileLines, th)
	if mismatch == nil {
		return nil
	}
	return &Violation{
		Level: LevelWarning,
		Msg: fmt.Sprintf("E规模档位错配: 文件%d行按字典应为%s,条目标注%s —— 文件生长跨档属正常,请顺手更新E位",
			mismatch.FileLines, strings.Join(mismatch.Expected, "/"), mismatch.Actual),
	}
}

// EScaleMismatch is the structured verdict of CheckEScale for callers that need localized wording;
// the judgement itself still lives only in CheckEScaleDetail.
type EScaleMismatch struct {
	FileLines int      // the file's actual line count
	Expected  []string // the scale symbols the line count belongs to by the dictionary (sorted)
	Actual    string   // the E symbol the entry is tagged with
}

// CheckEScaleDetail is the same judgement as CheckEScale, returning a structured result instead of a sentence.
func CheckEScaleDetail(line string, fileLines int, th *EScaleThresholds) *EScaleMismatch {
	if !th.HasThresholds() {
		return nil
	}
	_, tagStart, tagEnd, ok := EntryTagSpan(line)
	if !ok {
		return nil
	}
	tags := ParseTags(line[tagStart:tagEnd])
	if len(tags) == 0 {
		return nil // a non-standard tag is downgraded, no scale judgement (the quota/dict policy)
	}
	e := tags["E"]
	if e == "" {
		return nil
	}
	r, ok := th.ranges[e]
	if !ok {
		return nil // the symbol declares no threshold, undecidable (symbol validity belongs to the dict gate)
	}
	if fileLines >= r.min && fileLines <= r.max {
		return nil // the tagged scale contains the line count: conforming (boundary overlap tolerated)
	}
	// The scales the line count actually belongs to come from the single reverse lookup (the A4 single point)
	expected := ExpectedEScaleSymbols(fileLines, th)
	if len(expected) == 0 {
		return nil // a dictionary gap: the line count has no scale, and this gate does not overrule
	}
	return &EScaleMismatch{FileLines: fileLines, Expected: expected, Actual: e}
}

// ScopedDictionaryText returns the dictionary text of the Meta Volume that applies to one object profile: the common
// preamble before the first "#[Tag dictionary:" header (the S quota and the other protocol lines live there) plus
// the "#[Tag dictionary: <profile>]" section itself. Meta has one section per object kind, each with its own E-scale
// line, and extracting thresholds from the whole volume would let the last section (database) overwrite code's;
// text without section headers (a Legacy index header) is returned as it is.
func ScopedDictionaryText(metaText, profile string) string {
	marker := "#[Tag dictionary: " + profile + "]"
	var out []string
	inSections, collect := false, false
	for _, line := range strings.Split(strings.ReplaceAll(metaText, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#[Tag dictionary:") {
			inSections = true
			collect = trimmed == marker
			if collect {
				out = append(out, line)
			}
			continue
		}
		if !inSections || collect {
			out = append(out, line)
		}
	}
	if !inSections {
		return metaText
	}
	return strings.Join(out, "\n")
}
