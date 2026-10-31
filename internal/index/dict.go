// Tag dictionary extraction and validation: extracts the ABCDE dictionary from the index header and checks entry tags against it
// Index entry: dict.go[IVL8M]
//
// Position (the D40 CLI-side reference implementation): an invented tag does not break parsing (ParseTags still
// splits it) but is void at the semantic layer, since an agent's attribute query does not know what an invented
// symbol means. Two platform audits showed that the absence of this gate was the root cause of the recurring
// invented-tag disease; the second live run (2026-07-09) confirmed the same gap on the CLI side (a DC7Meta with an illegal E slot entered the formal index). This file is that gate's reference implementation, which the platform copies.
//
// Single split source: validation splits tags with ParseTags (types.go) and never writes a second heuristic;
// there is one split rule, and dictionary validation judges only "is the split-out symbol in the dictionary".
//
// Level Warning (the quota gate's philosophy): a repository with existing invented tags must not have its writes
// blocked; the AI bulk-generation scenario (incremental, with no legacy burden) is raised to a hard refusal at the
// CLI layer, where D40's original "refuse what is outside the dictionary" semantics are honoured in entries apply.
//
// Cross-dimension confusion exposed (P-14, the 2026-07-12 httpx live run): an AI-drafted dictionary once buried an
// A/B semantic overlap, and the root cause of ten dict refusals at cold start was a symbol in the wrong dimension
// (A semantics written into the B slot), not a real invention; the old wording only said "not in the dictionary",
// and an agent read it as "extend the dictionary or change the symbol". The fix: when the offending symbol exists
// in another dimension's dictionary, the wording names "an X-dimension symbol, probably misplaced", turning the
// diagnosis from "symbol missing" into "symbol misplaced"; prevention at the drafting end is rule 5 of the prompt asset header-dict-rules.txt (A/B semantics do not overlap), closing in from both ends.
//
// Dictionary line recognition (the P-21 fix, the 2026-07-12 httpx-rerun live run): the old implementation matched
// the line after stripping # with a hard HasPrefix(t,"A层级:"); the real AI-drafted form
// "#A层级(python,架构角色): I-接口(...)" put a parenthesized note between the dimension name and the colon, the
// prefix match failed, the A and B dimensions extracted nothing, HasDict read false, maintain paused dispatch with
// "dictionary not established", and CheckTagsAgainstDict skipped every entry silently (a dictionary-gate false
// green, the same family as P-15's silent exemption, this time the dictionary gate itself). This repository's
// header is in the compact form that both paths accept, so the tests here stayed green while the real machine
// broke. The fix, parseDictLine with three-form tolerance: TrimSpace after stripping # (tolerating "# A层级"),
// the first colon in the line counts whether half- or full-width, and the text before the colon must equal the
// dimension name exactly after its parenthesized note is stripped (exact equality protects the other "xxx:" header
// lines). The content side strips the note before collecting symbols too: example file path words inside a note (httpx/_api.py) were once collected by first letter as symbols (httpx entered the A dictionary, symbol pollution that loosened the dictionary).
//
// The dimension-name allowlist holds dimensions shared across gates (the P-22 batch): E规模 is consumed by escale.go
// (threshold extraction) and S配额 by quota.go (quota extraction); parseDictLine is the single implementation of all
// dictionary line recognition, a new recognizable dimension changes only this function's allowlist, and the consuming
// logic stays in each gate's own file; this file's ExtractTagDict consumes A层级/B模块/C重要度/D特征/E规模 (S配额 is
// not a tag dimension and falls through its switch).
//
// Dictionary line form (lenient extraction): the header A/B/C/D/E lines (tolerating the variants above); the content
// is split on whitespace, a letter axis takes the leading ASCII letters and the C axis one digit, which covers the
// "X-Script(说明)" (hyphen-separated), "I索引核心" (symbol run into Chinese), and "AI-AI增强" (multi-letter symbol) forms.
// D trait is an optional axis; when the dictionary declares no D, an Entry may only omit D.
package index

import (
	"fmt"
	"github.com/aoci-spec/aoci-code/textassets"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// TagDict is the tag dictionary extracted from the header (key = legal symbol)
type TagDict struct {
	A map[string]bool // layer symbols
	B map[string]bool // module symbols
	C map[string]bool // importance symbols
	D map[string]bool // optional trait symbols
	E map[string]bool // scale symbols

	declared    map[string]bool
	nearMisses  []DimensionNearMiss
	malformed   []string
	definitions map[string]map[string]string
	conflicts   []tagDictionaryConflict
}

// tagDictionaryConflict records one symbol that is assigned more than one
// meaning inside the same axis. A conflicting dictionary is never widened by
// unioning the declarations.
type tagDictionaryConflict struct {
	Axis   string
	Symbol string
	First  string
	Second string
}

// TagViolation is a deterministic axis-level decision produced after the tag
// has been split by ParseTags. It is shared by Legacy warnings and Volumes
// candidate/projected validation; it does not introduce another tag parser.
type TagViolation struct {
	Axis     string
	Value    string
	Expected string
	Actual   string
	Cause    string
}

// HasDict reports whether the dictionary can be used for validation (A and B each have at least one symbol).
// It returns false when the header has no dictionary lines or extraction is empty, and the caller should skip validation (undecidable is not a false report).
func (d *TagDict) HasDict() bool {
	return d != nil && len(d.A) > 0 && len(d.B) > 0
}

// HasObjectContract reports whether the dictionary is a complete authority for
// a Volumes v1 object. D remains optional; when it is absent the only legal D
// representation is omission.
func (d *TagDict) HasObjectContract() bool {
	return d != nil && len(d.ObjectContractProblems()) == 0
}

// ObjectContractProblems returns deterministic machine facts for a missing,
// malformed, or internally conflicting Volumes object dictionary.
func (d *TagDict) ObjectContractProblems() []string {
	if d == nil {
		return []string{"dictionary=missing"}
	}
	var problems []string
	for _, axis := range []string{"A", "B", "C", "E"} {
		if !d.declared[axis] || len(d.axis(axis)) == 0 {
			problems = append(problems, "axis="+axis+";state=missing_or_unparseable")
		}
	}
	if d.declared["D"] && len(d.D) == 0 {
		problems = append(problems, "axis=D;state=unparseable")
	}
	problems = append(problems, d.malformed...)
	for _, conflict := range d.conflicts {
		problems = append(problems, fmt.Sprintf(
			"axis=%s;symbol=%s;state=conflict;definitions=%s,%s",
			conflict.Axis, conflict.Symbol, conflict.First, conflict.Second,
		))
	}
	sort.Strings(problems)
	return uniqueStrings(problems)
}

// Contract describes the exact current dictionary facts in a stable order.
func (d *TagDict) Contract() string {
	if d == nil {
		return "dictionary=missing"
	}
	return strings.Join([]string{
		"format=compact(A+B+C+[D]+E)",
		"A=" + joinedSymbols(d.A),
		"B=" + joinedSymbols(d.B),
		"C=" + joinedSymbols(d.C),
		"D=" + optionalSymbols(d.D),
		"E=" + joinedSymbols(d.E),
	}, ";")
}

// Definition returns the exact parsed meaning bound to one axis symbol. It is
// used only when a consumer must distinguish an official semantic calibration
// from a custom dictionary that happens to reuse the same letters.
func (d *TagDict) Definition(axis, symbol string) (string, bool) {
	if d == nil || d.definitions[axis] == nil {
		return "", false
	}
	definition, ok := d.definitions[axis][symbol]
	return definition, ok
}

func (d *TagDict) axis(axis string) map[string]bool {
	switch axis {
	case "A":
		return d.A
	case "B":
		return d.B
	case "C":
		return d.C
	case "D":
		return d.D
	case "E":
		return d.E
	default:
		return nil
	}
}

// stripParens removes the parenthesized segments of a text (half-width () and full-width （）, nesting counted by depth).
// Two uses (P-21): on the dimension-name side it lets "A层级(python,架构角色)" compare exactly with "A层级"; on the
// content side it keeps example paths with spaces inside a note from being collected as symbols.
// An unmatched closing parenthesis is kept as it is and the depth never goes negative (so malformed input cannot swallow the rest of the text).
func stripParens(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '（':
			depth++
		case ')', '）':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// parseDictLine tries to recognize a line as a dictionary line (the single P-21 implementation).
// A hit returns (dimension name, the content after the colon, true); the three tolerated forms are a space after #,
// a full-width colon, and a parenthesized note between the dimension name and the colon; the dimension name,
// stripped of its note and trimmed, must equal A层级/B模块/C重要度/D特征/E规模/S配额 exactly (exact equality protects the other header lines that hold a colon).
// Consumers: A/B/C/D/E belong to ExtractTagDict, the E thresholds are consumed separately by escale.go, and
// S配额 belongs to quota.go's ExtractSQuotaThresholds (a dimension added in the P-22 batch).
func parseDictLine(line string) (dim, content string, ok bool) {
	t := strings.TrimSpace(line)
	t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
	// The first colon in the line: whichever of half- and full-width comes first
	ih := strings.Index(t, ":")
	iw := strings.Index(t, "：")
	idx, clen := -1, 0
	switch {
	case ih >= 0 && (iw < 0 || ih < iw):
		idx, clen = ih, len(":")
	case iw >= 0:
		idx, clen = iw, len("：")
	}
	if idx < 0 {
		return "", "", false
	}
	name := strings.TrimSpace(stripParens(t[:idx]))
	if canonical, hit := canonicalDimensionName(name); hit {
		return canonical, t[idx+clen:], true
	}
	return "", "", false
}

// canonicalDimensionNames maps every accepted spelling to its canonical
// dimension. Adding a spelling here changes how existing Meta assets parse, so
// it is a formal cognition behaviour change and not a convenience edit.
var canonicalDimensionNames = map[string]string{
	"A层级": "A层级", "A Layer": "A层级",
	"B模块": "B模块", "B Module": "B模块",
	"C重要度": "C重要度", "C Importance": "C重要度",
	"D特征": "D特征", "D Trait": "D特征",
	"E规模": "E规模", "E Scale": "E规模",
	"S配额": "S配额", "S quota": "S配额", "S Quota": "S配额",
}

func canonicalDimensionName(name string) (string, bool) {
	canonical, ok := canonicalDimensionNames[name]
	return canonical, ok
}

// normalizeDimensionName folds a written name for near-miss comparison only:
// ASCII case, ASCII hyphen and underscore, and all spaces are removed. It is
// never used to accept a line.
func normalizeDimensionName(name string) string {
	var out strings.Builder
	for _, r := range name {
		switch {
		case r == '-' || r == '_' || r == ' ' || r == '\t' || r == '\u3000':
			continue
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r + ('a' - 'A'))
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

var normalizedDimensionNames = func() map[string]string {
	result := map[string]string{}
	for spelling, canonical := range canonicalDimensionNames {
		result[normalizeDimensionName(spelling)] = canonical
	}
	return result
}()

// DimensionNameNearMiss reports whether a header line names a dimension in a
// spelling this parser does not accept, returning the canonical dimension it
// was reaching for and the spelling exactly as written.
//
// It exists because rejection here is not neutral. For S the operator's declared
// quota silently becomes the machine default with no message at all. For E it is
// worse: the dictionary substitutes the LMST fallback and then reports the
// operator's own declared symbol as illegal, "see the E scale line in the
// header" — pointing at the line it threw away — and the write path treats that
// as a hard refusal. Neither state is one the operator can act on.
//
// The predicate is normalized equality and deliberately nothing looser. The
// same header carries "#F scope:", "#R scope:", "#A scope:", "#S discipline:"
// and "#S-Admission:", so a prefix or edit-distance heuristic would tell every
// AOCI repository it had misspelled a dimension. This never accepts a line: a
// near miss parses exactly as it parses today.
func DimensionNameNearMiss(line string) (canonical, written string, near bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "#") {
		return "", "", false
	}
	t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
	ih := strings.Index(t, ":")
	iw := strings.Index(t, "：")
	idx := -1
	switch {
	case ih >= 0 && (iw < 0 || ih < iw):
		idx = ih
	case iw >= 0:
		idx = iw
	}
	if idx < 0 {
		return "", "", false
	}
	name := strings.TrimSpace(stripParens(t[:idx]))
	if _, exact := canonicalDimensionName(name); exact {
		return "", "", false
	}
	if canonical, ok := normalizedDimensionNames[normalizeDimensionName(name)]; ok {
		return canonical, name, true
	}
	return "", "", false
}

// DisplayDimensionSpelling renders an operator-written dimension name for a
// diagnostic in the given Locale.
//
// The written name is the operator's own header text and may carry any script,
// including the Han typos the near-miss folder exists to catch — "#E 规模:"
// with an ideographic space normalizes onto the canonical name. An ASCII-locale
// diagnostic that embeds it is discarded whole by the runtime and replaced with
// a generic message, so the operator loses the one fact that would let them
// find their line. Escaping keeps the exact text, unambiguous and greppable,
// inside a channel that only carries ASCII.
func DisplayDimensionSpelling(written, locale string) string {
	if locale == textassets.LegacyLocale || !containsNonASCII(written) {
		return written
	}
	return strconv.QuoteToASCII(written)
}

// AcceptedDimensionSpellingsForLocale lists the accepted spellings a diagnostic
// rendered in the given Locale may quote.
//
// This exists because a diagnostic is delivered only if it matches its Locale.
// The canonical spelling of every dimension is Chinese, so quoting the complete
// set inside an en-US message made that message carry Han text, and the runtime
// discarded the whole refusal and replaced it with a generic one naming the
// wrong file. An interpolated argument has to match the Locale of the message
// carrying it, not merely be true.
//
// The test is "is this the Han Locale", not "is this en-US", so a Locale added
// later gets the ASCII-only list by default rather than inheriting this bug.
// Every dimension has at least one ASCII spelling, so that list is never empty.
func AcceptedDimensionSpellingsForLocale(canonical, locale string) []string {
	all := AcceptedDimensionSpellings(canonical)
	if locale == textassets.LegacyLocale {
		return all
	}
	ascii := make([]string, 0, len(all))
	for _, spelling := range all {
		if !containsNonASCII(spelling) {
			ascii = append(ascii, spelling)
		}
	}
	if len(ascii) == 0 {
		return all
	}
	return ascii
}

func containsNonASCII(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

// AcceptedDimensionSpellings lists the accepted spellings for one canonical
// dimension, so a diagnostic can quote them instead of restating them.
func AcceptedDimensionSpellings(canonical string) []string {
	result := []string{}
	for spelling, mapped := range canonicalDimensionNames {
		if mapped == canonical {
			result = append(result, spelling)
		}
	}
	sort.Strings(result)
	return result
}

// DimensionNearMiss is one header line that named a dimension in a spelling the
// parser does not accept, recorded so a diagnostic can quote it.
type DimensionNearMiss struct {
	Canonical string
	Written   string
}

// UnrecognizedDimensionNames returns the near misses recorded while extracting
// this dictionary, for axes that received no acceptable line at all. An axis
// that was also declared correctly is not reported: the stray line changed
// nothing, and naming it would be noise.
func (d *TagDict) UnrecognizedDimensionNames() []DimensionNearMiss {
	if d == nil {
		return nil
	}
	return append([]DimensionNearMiss{}, d.nearMisses...)
}

// ExtractTagDict extracts the dictionary from the header text.
// Line recognition goes through parseDictLine (three-form tolerance, see the P-21 note);
// a missing E line falls back to the default LMST (the four values the spec fixes).
func ExtractTagDict(headerText string) *TagDict {
	d := &TagDict{
		A:           map[string]bool{},
		B:           map[string]bool{},
		C:           map[string]bool{},
		D:           map[string]bool{},
		E:           map[string]bool{},
		declared:    map[string]bool{},
		definitions: map[string]map[string]string{},
	}
	candidates := []DimensionNearMiss{}
	exact := map[string]bool{}
	for _, line := range strings.Split(headerText, "\n") {
		dim, content, ok := parseDictLine(line)
		if !ok {
			// A spelling this parser rejects is recorded, never accepted. The
			// values below are exactly the values a build without this recording
			// produces.
			if canonical, written, near := DimensionNameNearMiss(line); near {
				candidates = append(candidates, DimensionNearMiss{Canonical: canonical, Written: written})
			}
			continue
		}
		exact[dim] = true
		// The content side strips the parenthesized note before collecting symbols (P-21: example path words inside a note must not pollute the dictionary);
		// the S配额 dimension has no branch and falls through (its consumption belongs to quota.go)
		switch dim {
		case "A层级":
			d.collectDimension("A", stripParens(content))
		case "B模块":
			d.collectDimension("B", stripParens(content))
		case "C重要度":
			d.collectDimension("C", stripParens(content))
		case "D特征":
			d.collectDimension("D", stripParens(content))
		case "E规模":
			d.collectDimension("E", stripParens(content))
		}
	}
	// Report a near miss only for an axis that received no acceptable line, and
	// never for D. A stray line beside a correct declaration changed nothing, and
	// D is optional, so a Meta carrying only a misspelled D loads today and must
	// keep loading without a new diagnostic. Under this guard the parsed verdict
	// is provably unchanged: HasObjectContract is already false whenever A, B, C
	// or E is undeclared.
	for _, candidate := range candidates {
		if candidate.Canonical == "D特征" || exact[candidate.Canonical] {
			continue
		}
		d.nearMisses = append(d.nearMisses, candidate)
	}
	if len(d.E) == 0 {
		for _, s := range []string{"L", "M", "S", "T"} {
			d.E[s] = true
		}
	}
	return d
}

// ExtractScopedTagDict selects exactly one named dictionary from a Volumes
// Meta asset and delegates every axis parse to ExtractTagDict. A missing or
// duplicate section is unusable instead of being silently merged.
func ExtractScopedTagDict(metaText, profile string) *TagDict {
	startMarker := "#[Tag dictionary: " + profile + "]"
	sectionCount := 0
	collect := false
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(metaText, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#[Tag dictionary:") {
			collect = trimmed == startMarker
			if collect {
				sectionCount++
			}
			continue
		}
		if collect && sectionCount == 1 {
			lines = append(lines, line)
		}
	}
	if sectionCount == 0 {
		return nil
	}
	dictionary := ExtractTagDict(strings.Join(lines, "\n"))
	if sectionCount != 1 {
		dictionary.malformed = append(dictionary.malformed,
			fmt.Sprintf("profile=%s;state=duplicate;count=%d", profile, sectionCount))
	}
	return dictionary
}

func (d *TagDict) collectDimension(axis, content string) {
	d.declared[axis] = true
	if d.definitions[axis] == nil {
		d.definitions[axis] = map[string]string{}
	}
	words := strings.Fields(content)
	if len(words) == 0 {
		d.malformed = append(d.malformed, "axis="+axis+";state=unparseable")
		return
	}
	accepted := 0
	for _, word := range words {
		symbol := leadingASCIILetters(word)
		if axis == "C" {
			symbol = leadingASCIIDigits(word)
		}
		if !validDictionarySymbol(axis, symbol) {
			continue
		}
		accepted++
		d.axis(axis)[symbol] = true
		definition := strings.TrimSpace(strings.TrimPrefix(word, symbol))
		if previous, ok := d.definitions[axis][symbol]; ok && previous != definition {
			d.conflicts = append(d.conflicts, tagDictionaryConflict{
				Axis: axis, Symbol: symbol, First: previous, Second: definition,
			})
		} else {
			d.definitions[axis][symbol] = definition
		}
	}
	if accepted == 0 {
		d.malformed = append(d.malformed, "axis="+axis+";state=unparseable")
	}
}

func validDictionarySymbol(axis, symbol string) bool {
	if symbol == "" {
		return false
	}
	if axis == "C" {
		return len(symbol) == 1 && symbol[0] >= '1' && symbol[0] <= '9'
	}
	if axis == "A" || axis == "E" {
		return len(symbol) == 1
	}
	return true
}

// leadingASCIILetters takes the run of ASCII letters at the start of a word
func leadingASCIILetters(word string) string {
	end := 0
	for end < len(word) {
		c := word[end]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			end++
			continue
		}
		break
	}
	return word[:end]
}

func leadingASCIIDigits(word string) string {
	end := 0
	for end < len(word) && word[end] >= '0' && word[end] <= '9' {
		end++
	}
	return word[:end]
}

// crossDimHint is the cross-dimension misplacement hint (P-14): when the offending symbol exists in another
// dimension's dictionary it names the probable misplacement; when it exists in no dimension it returns empty (keeping the "not in the dictionary" meaning).
func crossDimHint(sym string, dict *TagDict, selfDim string) string {
	var others []string
	if selfDim != "A" && dict.A[sym] {
		others = append(others, "A层级")
	}
	if selfDim != "B" && dict.B[sym] {
		others = append(others, "B模块")
	}
	if selfDim != "C" && dict.C[sym] {
		others = append(others, "C重要度")
	}
	if selfDim != "D" && dict.D[sym] {
		others = append(others, "D特征")
	}
	if selfDim != "E" && dict.E[sym] {
		others = append(others, "E规模")
	}
	if len(others) == 0 {
		return ""
	}
	return "(该符号是" + strings.Join(others, "/") + "字典的既有符号,疑似维度错位——五维各司其职,请核对标签内符号顺序而非扩典)"
}

// CheckTagsAgainstDict checks an entry line's tags against the dictionary (Warning level).
// The split goes through ParseTags; a non-standard tag (empty map) is not judged, since structure belongs to ValidateEntryLine.
// nil means conforming or undecidable; a violation returns a Warning with a per-dimension explanation (naming a
// probable misplacement when the symbol exists in another dimension's dictionary, P-14).
func CheckTagsAgainstDict(line string, dict *TagDict) *Violation {
	if !dict.HasDict() {
		return nil
	}
	_, tagStart, tagEnd, ok := EntryTagSpan(line)
	if !ok {
		return nil
	}
	tags := ParseTags(line[tagStart:tagEnd])
	if len(tags) == 0 {
		return nil // a non-standard tag is downgraded, no dictionary judgement
	}
	violations := ValidateTagAgainstDict(tags, "", dict)
	var probs []string
	for _, violation := range violations {
		// Legacy headers predate C/D dictionary declarations. Preserve their
		// warning contract while validating the extra axes whenever declared.
		if (violation.Axis == "C" || violation.Axis == "D") && !dict.declared[violation.Axis] {
			continue
		}
		legacyCause := violation.Cause
		switch violation.Axis {
		case "A":
			legacyCause = "A层级符号" + violation.Value + "不在字典"
		case "B":
			legacyCause = "B模块符号" + violation.Value + "不在字典"
		case "E":
			legacyCause = "E规模符号" + violation.Value + "非法(合法值见头部E规模行)"
		}
		probs = append(probs, legacyCause+crossDimHint(violation.Value, dict, violation.Axis))
	}
	if len(probs) == 0 {
		return nil
	}
	return &Violation{
		Level: LevelWarning,
		Msg:   "标签字典违规: " + strings.Join(probs, ";") + " —— 臆造符号不破坏解析但语义作废,请用头部字典既有符号或先扩典",
	}
}

// ValidateTagAgainstDict validates one ParseTags result against one current
// dictionary. tagRaw is included only in the returned machine facts.
func ValidateTagAgainstDict(tags map[string]string, tagRaw string, dict *TagDict) []TagViolation {
	if dict == nil || len(tags) == 0 {
		return nil
	}
	var violations []TagViolation
	labels := map[string]string{"A": "A Layer", "B": "B Module", "C": "C Importance", "D": "D Trait", "E": "E Scale"}
	for _, axis := range []string{"A", "B", "C", "D", "E"} {
		value := tags[axis]
		allowed := dict.axis(axis)
		legal := value != "" && allowed[value]
		if axis == "D" && value == "" && len(allowed) == 0 {
			legal = true
		}
		if legal {
			continue
		}
		if axis == "D" && value == "" {
			continue
		}
		actualValue := value
		if actualValue == "" {
			actualValue = "-"
		}
		violations = append(violations, TagViolation{
			Axis: axis, Value: value, Expected: dict.Contract(),
			Actual: tagActual(tagRaw, tags),
			Cause:  fmt.Sprintf("%s value %s is not declared by the current Meta dictionary", labels[axis], actualValue),
		})
	}
	return violations
}

func tagActual(raw string, tags map[string]string) string {
	value := func(axis string) string {
		if tags[axis] == "" {
			return "-"
		}
		return tags[axis]
	}
	return strings.Join([]string{
		"tag=" + raw, "A=" + value("A"), "B=" + value("B"),
		"C=" + value("C"), "D=" + value("D"), "E=" + value("E"),
	}, ";")
}

func joinedSymbols(values map[string]bool) string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func optionalSymbols(values map[string]bool) string {
	if len(values) == 0 {
		return "-"
	}
	return joinedSymbols(values)
}

func uniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
