// S field C-quota check: checks the S content length against the limit for the entry's C importance (Warning level)
// Index entry: quota.go[IVL8S]
//
// Two-layer quota source (the 2026-07-13 operator decision, the same philosophy as the E-threshold ownership):
// the repository header's "#S配额:" declaration wins (user-defined), and an undeclared C band falls back to the
// machine-contract default; gaps are filled with defaults rather than skipped. The default numbers come from the single internal/machinecontract source.
//
// Header declaration form: #S配额: C9-8≤N C7-4≤N C3-1≤N
// Line recognition goes through the single parseDictLine (dict.go, the same three-form tolerance as P-21/P-22: a
// space after #, a full-width colon, a parenthesized note on the dimension name) and never holds a second copy;
// a field word tolerates the C9-C8 double-C spelling, a single band C5≤100, full-width ≤＜＝ normalized, a Chinese
// suffix such as "字" ending the number naturally, and <N read as ≤N-1; a C band declared twice takes the later value (written per C); a word outside these forms is skipped rather than guessed.
//
// Visible skips (R51/14.6): SawSQuotaLine tells three states apart: no declaration line (defaults, the normal
// state) / a line present with no parseable field (defaults still apply, and the upper layer can say "a
// declaration is present but the machine cannot parse it", the D97 typing) / a line present and extractable (the
// declared values apply). An unparseable declaration is never silently treated as absent; the upper-layer wording lands as score.go and the other callers are next read.
//
// Motivation: an over-quota S is one of the ways an index rots; a curator (human or model) overwrites under the
// urge that "everything matters", and without a machine gate only self-discipline holds. The prompt layer already
// teaches the model the quota; this function makes the same rule a machine-executable check, so the promise and the gate agree.
//
// Level Warning rather than Error: the quota is curation discipline, not a structural constraint; a hard refusal
// would block every write to an existing over-quota entry (even a change to another field), while a warning lets
// the person see the debt and keeps the decision with them, the same policy as the ValidateEntryLine evolution-narrative warning.
//
// C extraction is deliberately self-contained and does not call ParseTags: it relies only on the dictionary's anchor
// rule "B and D use no digits, so the first digit in a tag is C" (which holds for the compact and the dotted form
// alike), a rule more stable than any parsing implementation; C is one character 1-9, so the first digit character suffices.
//
// Compatibility contract: CheckSQuota(line) keeps its signature and semantics (equivalent to a nil threshold table
// taking the default quotas), so existing callers (validator.go and the others) change nothing; the header
// declaration takes effect only once a caller moves to CheckSQuotaWith and passes the ExtractSQuotaThresholds
// result, caller by caller as each is next read (R50).
//
// internal/cognition moved into this file in rc6: objects.go used to hold a fourth copy of the quota judgement
// (calling machinecontract.DefaultSQuotaForC directly and blind to the header declaration), so "the limit the
// system told the model" and "the limit the write path refused at" could differ: a repository that had relaxed its
// declaration was told 500 and refused at 200, and no edit could clear it. That path now takes its value from
// the single LimitForC, and no second C-band lookup is ever rebuilt outside this file.
package index

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// SQuotaThresholds is the S quota table extracted from the header (key = C importance 1-9, value = character limit)
type SQuotaThresholds struct {
	quotas  map[int]int
	sawLine bool // whether the S配额 line was present (including the present-but-unparseable form, for the R51 visible-skip typing)
	// unrecognizedName records the spelling of a dimension name this parser does not accept, as written. It serves
	// diagnostics only and never takes part in a value: the line parses today exactly as it did before this field.
	unrecognizedName string
}

// UnrecognizedName returns the S quota dimension spelling this parser rejected,
// exactly as the operator wrote it, or the empty string. A rejected spelling is
// otherwise indistinguishable from no declaration at all, which is how an
// operator's declared numbers became the machine defaults in silence.
func (t *SQuotaThresholds) UnrecognizedName() string {
	if t == nil {
		return ""
	}
	return t.unrecognizedName
}

// HasQuotas reports whether the table holds at least one usable declaration.
func (t *SQuotaThresholds) HasQuotas() bool {
	return t != nil && len(t.quotas) > 0
}

// SawSQuotaLine reports whether the header holds a dictionary line recognized as S配额 (whether or not field extraction succeeded).
// Combined with HasQuotas it yields three states: no line (defaults, normal) / a line without declarations (present
// but machine-unparseable, which the upper layer should name rather than silently default) / a line with declarations (the custom values apply).
func (t *SQuotaThresholds) SawSQuotaLine() bool {
	return t != nil && t.sawLine
}

// sQuotaFullWidth normalizes the quota field operators: ≤ becomes <=, and full-width ＜＝ become half-width.
// Full-width digits are not normalized (no live sample, no guessing of forms).
var sQuotaFullWidth = strings.NewReplacer("≤", "<=", "＜", "<", "＝", "=")

// quotaForC is the two-layer lookup: a header declaration for the C band is used (custom=true),
// otherwise the spec default applies (gaps filled with defaults, the operator-decision semantics).
func quotaForC(c int, th *SQuotaThresholds) (quota int, custom bool) {
	if th.HasQuotas() {
		if q, ok := th.quotas[c]; ok {
			return q, true
		}
	}
	return machinecontract.DefaultSQuotaForC(c), false
}

// LimitForC resolves the effective S rune limit for one C band FOR AN
// ERROR-LEVEL GATE, and deliberately differs from quotaForC: a declared band
// wins only when it is LOOSER than the machine default.
//
// The asymmetry is load-bearing, not an oversight. CheckSQuotaWith is
// Warning-level curation discipline and honours a declaration in both
// directions, because a warning an operator asked for costs nothing. An
// Error-level gate refuses the whole Volume, so honouring a tightening there
// would make an already-persisted Volume unloadable the moment an operator
// narrows their own header — taking Verify, Check, Guide, Maintain and Overview
// down together, recoverable only by hand-editing the Meta asset.
//
// The floor makes the gate monotone-loosening under EVERY possible declaration,
// which is the property that lets this change ship: nothing that loads today can
// fail after the upgrade, and a repository that declared a wider band and could
// not load its own authored Entries now can.
func (t *SQuotaThresholds) LimitForC(c int) (limit int, declared bool) {
	quota, custom := quotaForC(c, t)
	if machine := machinecontract.DefaultSQuotaForC(c); quota < machine {
		return machine, custom
	}
	return quota, custom
}

// ExtractSQuotaThresholds extracts the S quota declaration from the header text.
// Line recognition goes through the single parseDictLine (dimension S配额, three-form tolerance); the content is split
// on whitespace and parsed word by word, and an unparseable word is skipped (the line still sets sawLine for the typing).
func ExtractSQuotaThresholds(headerText string) *SQuotaThresholds {
	t := &SQuotaThresholds{quotas: map[int]int{}}
	for _, line := range strings.Split(headerText, "\n") {
		dim, content, ok := parseDictLine(line)
		if !ok || dim != "S配额" {
			if !ok {
				// State two, not state one: the declaration is present and the
				// machine cannot read it. Values are untouched.
				if canonical, written, near := DimensionNameNearMiss(line); near && canonical == "S配额" {
					t.sawLine = true
					if t.unrecognizedName == "" {
						t.unrecognizedName = written
					}
				}
			}
			continue
		}
		t.unrecognizedName = ""
		t.sawLine = true
		for _, word := range strings.Fields(content) {
			cmin, cmax, q, ok := parseSQuotaField(word)
			if !ok {
				continue
			}
			for c := cmin; c <= cmax; c++ {
				t.quotas[c] = q
			}
		}
	}
	return t
}

// EffectiveSQuotaContract renders the complete effective S quota authority in
// descending C order. Parsed Meta declarations win per C band and every gap is
// filled from machinecontract, so older Meta assets without a quota line still
// produce the current machine contract without changing their formal bytes.
func EffectiveSQuotaContract(headerText string) string {
	thresholds := ExtractSQuotaThresholds(headerText)
	type band struct {
		minC  int
		maxC  int
		quota int
	}
	bands := []band{}
	for importance := 9; importance >= 1; importance-- {
		quota, _ := quotaForC(importance, thresholds)
		if len(bands) > 0 && bands[len(bands)-1].quota == quota && bands[len(bands)-1].minC == importance+1 {
			bands[len(bands)-1].minC = importance
			continue
		}
		bands = append(bands, band{minC: importance, maxC: importance, quota: quota})
	}
	parts := make([]string, 0, len(bands))
	for _, current := range bands {
		label := fmt.Sprintf("C%d", current.maxC)
		if current.minC != current.maxC {
			label += fmt.Sprintf("-%d", current.minC)
		}
		parts = append(parts, fmt.Sprintf("%s≤%d", label, current.quota))
	}
	return strings.Join(parts, " ")
}

// parseSQuotaField parses one quota word: C9-8≤N / C9-C8≤N / C5≤N / C3-1≤N字.
// C ranges over 1-9 (outside is refused); the two ends of a range are sorted (9-8 equals 8-9); the operators ≤
// (normalized to <=), <=, and = are accepted, a bare < reads as ≤N-1; a non-digit after the number (such as "字") ends it naturally; the quota must be positive.
func parseSQuotaField(word string) (cmin, cmax, quota int, ok bool) {
	w := sQuotaFullWidth.Replace(word)
	if !strings.HasPrefix(w, "C") {
		return 0, 0, 0, false
	}
	c1, rest, okInt := leadingIntRest(w[len("C"):])
	if !okInt || c1 < 1 || c1 > 9 {
		return 0, 0, 0, false
	}
	c2 := c1
	if strings.HasPrefix(rest, "-") {
		rest = strings.TrimPrefix(rest[len("-"):], "C")
		c2, rest, okInt = leadingIntRest(rest)
		if !okInt || c2 < 1 || c2 > 9 {
			return 0, 0, 0, false
		}
	}
	strict := false
	switch {
	case strings.HasPrefix(rest, "<="):
		rest = rest[len("<="):]
	case strings.HasPrefix(rest, "="):
		rest = rest[len("="):]
	case strings.HasPrefix(rest, "<"):
		rest = rest[len("<"):]
		strict = true // <N means ≤N-1
	default:
		return 0, 0, 0, false
	}
	q, okInt := leadingInt(rest)
	if !okInt || q <= 0 {
		return 0, 0, 0, false
	}
	if strict {
		q--
	}
	if q <= 0 {
		return 0, 0, 0, false
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	return c1, c2, q, true
}

// extractCFromTags extracts the C importance from the tag string: the first digit character is C (the dictionary anchor rule).
// No digit returns 0 (a non-standard tag, matching ParseTags's downgrade policy: no quota judgement).
func extractCFromTags(tags string) int {
	for _, r := range tags {
		if r >= '1' && r <= '9' {
			return int(r - '0')
		}
		if r == '0' {
			return 0 // C ranges over 1-9; a 0 reads as non-standard
		}
	}
	return 0
}

// sContentLength counts the total characters (runes) of the S segments of an entry line:
// the line is split on " | " and the segments prefixed S:/S1:/S2:/S3: count (matching the S-variant merge of
// splitFRAS); several S variant segments add up; the prefix itself and the "-" placeholder count as content
// (the placeholder is one character with no real effect on the quota, and no special case keeps the function simple).
func sContentLength(line string) int {
	total := 0
	for _, seg := range strings.Split(line, " | ") {
		rest, ok := cutSPrefix(seg)
		if ok {
			total += utf8.RuneCountInString(rest)
		}
	}
	return total
}

// cutSPrefix reports whether a segment is an S segment (S: or S followed by one digit and a colon) and returns its content.
func cutSPrefix(seg string) (string, bool) {
	if strings.HasPrefix(seg, "S:") {
		return seg[len("S:"):], true
	}
	if len(seg) >= 3 && seg[0] == 'S' && seg[1] >= '0' && seg[1] <= '9' && seg[2] == ':' {
		return seg[3:], true
	}
	return "", false
}

// CheckSQuotaWith checks one entry line's S field against the given quota table.
// A nil th, or one without declarations, takes the spec defaults for every band; a declaration for the C band is
// used and the wording names its source (so the writer can tell default discipline from the repository's own decision).
// nil means no violation or undecidable (no tag structure / no C in the tag; structure belongs to ValidateEntryLine
// and is not reported twice); over quota returns a Warning-level Violation.
func CheckSQuotaWith(line string, th *SQuotaThresholds) *Violation {
	// Extract the [tags]: structure with the entry lexer (EntryTagSpan), so a [ inside the filename is not taken for a tag
	_, tagStart, tagEnd, ok := EntryTagSpan(line)
	if !ok {
		return nil
	}
	c := extractCFromTags(line[tagStart:tagEnd])
	if c == 0 {
		return nil // a non-standard tag is downgraded, no quota judgement
	}
	quota, custom := quotaForC(c, th)
	got := sContentLength(line)
	if got <= quota {
		return nil
	}
	src := ""
	if custom {
		src = ",仓库头部S配额声明"
	}
	return &Violation{
		Level: LevelWarning,
		Msg: fmt.Sprintf("S字段%d字超C%d配额(上限%d字%s,配额是上限不是目标): 请压缩为当前状态高熵约束,或上调C需说明理由",
			got, c, quota, src),
	}
}

// CheckSQuota checks one entry line against the spec default quotas (the compatibility entry, equivalent to
// CheckSQuotaWith(line, nil); existing callers migrate at no cost).
func CheckSQuota(line string) *Violation {
	return CheckSQuotaWith(line, nil)
}
