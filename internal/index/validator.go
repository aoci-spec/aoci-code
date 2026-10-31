// Entry format and S field discipline validation (its own file, carrying a discipline that keeps growing)
// Index entry: validator.go[Index.Val.9.S]
//
// Levels:
//
//	Hard refusal (LevelError): multi-line / no filename[tags]: structure / filename differing from the last path segment
//	                           / a missing F R A S part / a repeated F R A part prefix (the escape form the eight-reviewer live run caught)
//	Warning (LevelWarning):   unparseable tags (a probable missing slot, P-15) / an evolution-narrative word (a hint to
//	                           restate as the current state, not blocking) / S over the C quota (see quota.go)
//
// Discipline: a failed validation is never corrected silently, since silent correction teaches the agent to keep
// sending dirty data; an actionable refusal reason is always returned for the caller (MCP tool or CLI) to pass to the writer.
//
// Two-layer quota wiring (the P-22 batch): ValidateEntryLineWith takes *SQuotaThresholds so the header "#S配额:"
// declaration takes effect (an undeclared band falls back to the spec default, see the decision note in quota.go);
// ValidateEntryLine is the thin compatibility shell (nil = all defaults), existing callers migrate at no cost,
// caller by caller to the With variant as each is next read (R50).
//
// Origin of the repeated-prefix hard refusal (live run eight, 2026-07-10): a model draft produced "R:a | R:b | R:c",
// three R parts, which neither the quota gate nor the dict gate covers, and a human diff review caught it; a repeated
// part structure is format damage, not curation debt (the same nature as a missing part), and under the single-value
// semantics of the split fields (Entry.F/R/Api/S) several parts must lose information silently, hence Error level.
// S is the exception: the S:/S1:/S2:/S3: variants are an existing protocol feature (splitFRAS merges them into S), a repeated bare S: belongs to that family, and S stays tolerant.
//
// Origin of the unparseable-tag warning (P-15, the 2026-07-12 httpx live run, two proofs UAU8/MM7): a four-character
// compact tag missing its E slot (UAU8, whose first digit 8 sits last) is judged non-standard by ParseTags and
// returns an empty map, while the dict and escale gates both skip on "the parse result is empty", so "one slot
// missing" and "no tag structure at all" were merged into one exemption, the agent wrote a wrong tag, and both gates
// let it through silently. The fix: this layer tells two states apart: a [tag] structure that exists but cannot be
// parsed is a probable missing slot or wrong form and gets an explicit Warning (stating that both gates skipped);
// the Warning is not raised to Error, since a repository with existing hand-written non-standard tags (the
// platform's old index form) must not have every write blocked, the quota gate's philosophy: warn, pass, keep the
// decision with the person. ParseTags itself is untouched: it is the single split source of the three-way sync (R6), and partial parsing is a protocol-level evolution for a later batch under a Spec decision.
//
// The evolution-narrative word list and the S character quotas both come from the single internal/machinecontract
// source; the Validator consumes the machine values, and Spec and Prompt explain the purpose without copying the machine set.
package index

import (
	"path"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Violation levels
const (
	LevelError   = "error"   // hard refusal: the write must be rejected
	LevelWarning = "warning" // warning: the write is allowed with a hint to fix
)

// Violation is one validation violation
type Violation struct {
	Level string // LevelError / LevelWarning
	Msg   string // actionable explanation for the writer
}

// HasError reports whether the violation list holds a hard refusal
func HasError(vs []Violation) bool {
	for _, v := range vs {
		if v.Level == LevelError {
			return true
		}
	}
	return false
}

// StripFences cleans an agent's write input: it removes Markdown code-block fences and leading and trailing blank lines and whitespace.
// Agents often return the entry wrapped in ``` fences, which are not entry content;
// only the first and last fence lines are stripped, and backticks inside the body are untouched.
func StripFences(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	// Trim the blank lines at both ends
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	lines = lines[start:end]
	// The fence lines at both ends (``` or ```lang)
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ValidateEntryLine validates one entry about to be written (the compatibility entry: the quota takes the spec default,
// equivalent to ValidateEntryLineWith(relPath, line, nil)).
func ValidateEntryLine(relPath, line string) []Violation {
	return ValidateEntryLineWith(relPath, line, nil)
}

// ValidateEntryLineWith validates one entry about to be written (the P-22 two-layer quota entry).
// relPath: the target file's path relative to the repository root (forward slashes); line: the candidate entry after StripFences;
// th: the header S配额 declaration table (nil, or an undeclared band, takes the spec default, see quota.go).
// It returns the violation list (which may hold errors and warnings together); an empty list means fully conforming.
func ValidateEntryLineWith(relPath, line string, th *SQuotaThresholds) []Violation {
	var vs []Violation

	// 1. Single-line constraint (the cornerstone of the index protocol: one entry per line, the diff is the change record)
	if strings.Contains(line, "\n") {
		vs = append(vs, Violation{LevelError,
			"条目必须为单行: 检测到换行符。请将全部内容压入一行,长内容用分号衔接"})
		return vs // with several lines the later line-level checks are meaningless; return at once
	}
	if strings.TrimSpace(line) == "" {
		vs = append(vs, Violation{LevelError, "条目为空"})
		return vs
	}

	// 2. Structure constraint: it must match filename[tags]: body
	m := matchEntryLine(line)
	if m == nil {
		vs = append(vs, Violation{LevelError,
			"条目结构不合规: 必须为 文件名[标签]: F:... | R:... | A:... | S:... 单行格式"})
		return vs
	}
	filename := strings.TrimSpace(m[1])
	rest := m[3]

	// 3. Filename consistency: the entry filename's last segment must equal relPath's last segment (the write-side gate against editing a same-named file)
	wantBase := path.Base(strings.TrimSuffix(strings.ReplaceAll(relPath, "\\", "/"), "/"))
	gotBase := path.Base(strings.TrimSuffix(filename, "/"))
	if wantBase != gotBase {
		vs = append(vs, Violation{LevelError,
			"文件名不一致: 条目写的是 " + gotBase + ",目标路径末段是 " + wantBase +
				"。条目文件名必须与 path 末段一致"})
	}

	// 4. Tag parseability (Warning level, P-15): a [tags] structure exists but ParseTags returns an empty map, a
	// probable missing slot (a four-character compact tag such as UAU8 missing its E) or a wrong form; in this state
	// the dict and escale gates both skip (their skip condition is "the parse result is empty"), so it must be
	// named explicitly here, turning a silent exemption into a visible reminder. A legal tag and a missing structure (already an Error above) do not fire this.
	tagsRaw := strings.TrimSpace(m[2])
	if len(ParseTags(tagsRaw)) == 0 {
		vs = append(vs, Violation{LevelWarning,
			"标签不可解析(\"" + tagsRaw + "\"): 无法按 ABCDE 规则切分 —— 紧凑形态要求" +
				"首字符A层级+B模块+数字C重要度+可选D特征+末位E规模(疑似缺位),点分形态要求4或5段。" +
				"此态下字典与档位核对均被跳过(盲区状态),请按索引头部字典补全标签"})
	}

	// 5. All four FRAS parts present, and the F/R/A part prefixes unique (a value may be "-"; the S1/S2 variants count as S).
	// Counting rather than booleans: a boolean only answers "present or not", never "how many"; live run eight's model
	// draft produced three R: parts (R:a | R:b | R:c), invisible to a boolean implementation, caught by a human review and raised to a counted hard refusal.
	// S is not counted for repetition: the S:/S1:/S2:/S3: variants are a protocol feature (splitFRAS merges them into S).
	cntF, cntR, cntA, hasS := 0, 0, 0, false
	for _, seg := range strings.Split(rest, " | ") {
		seg = strings.TrimSpace(seg)
		switch {
		case strings.HasPrefix(seg, "F:"):
			cntF++
		case strings.HasPrefix(seg, "R:"):
			cntR++
		case strings.HasPrefix(seg, "A:"):
			cntA++
		case strings.HasPrefix(seg, "S:") || regexpSVariant.MatchString(seg):
			hasS = true
		}
	}
	var missing []string
	if cntF == 0 {
		missing = append(missing, "F")
	}
	if cntR == 0 {
		missing = append(missing, "R")
	}
	if cntA == 0 {
		missing = append(missing, "A")
	}
	if !hasS {
		missing = append(missing, "S")
	}
	if len(missing) > 0 {
		vs = append(vs, Violation{LevelError,
			"缺少要素段: " + strings.Join(missing, "/") +
				"。四要素必须齐备(无内容用 - 占位,如 A:-),分隔符为「空格|空格」"})
	}
	var dup []string
	if cntF > 1 {
		dup = append(dup, "F×"+strconv.Itoa(cntF))
	}
	if cntR > 1 {
		dup = append(dup, "R×"+strconv.Itoa(cntR))
	}
	if cntA > 1 {
		dup = append(dup, "A×"+strconv.Itoa(cntA))
	}
	if len(dup) > 0 {
		vs = append(vs, Violation{LevelError,
			"要素段重复: " + strings.Join(dup, "/") +
				"。F/R/A 各只能出现一段,多个值请在段内用逗号或分号衔接(如 R:a.go,b.go)"})
	}

	// 6. Evolution-narrative detection (Warning level): S must state the current state
	for _, w := range machinecontract.EvolutionNarrativeTerms() {
		if strings.Contains(line, w) {
			vs = append(vs, Violation{LevelWarning,
				"疑似演进叙事(\"" + w + "\"): 条目应陈述文件此刻是什么,不是发生过什么;变更历史归 Git"})
			break // one warning is enough; do not flood word by word
		}
	}

	// 7. S over the C quota (Warning level, quota.go; it stacks with the evolution-narrative warning rather than excluding it;
	// a nil th takes the spec default, a declared band takes the declared value, the P-22 two-layer semantics)
	if v := CheckSQuotaWith(line, th); v != nil {
		vs = append(vs, *v)
	}
	return vs
}
