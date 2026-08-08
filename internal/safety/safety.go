// 禁区词与过度主张扫描 —— D3 纪律从人的自觉到机器闸门的编译
// 索引条目: safety.go[Safe.Claims.9.IP.S]
//
// 用途: 扫描一切将公开的文本(README/Spec 正文/模板渲染产物/MCP 工具描述源码),
// 命中禁区词或过度主张即报告。被 scripts/check-public-text.sh 与 CI 调用。
//
// 匹配双轨(误伤教训: TRIES 大小写不敏感子串匹配曾把 get_entries/Entries 全部误报):
//
//	substringTerms —— 长词短语,大小写不敏感子串匹配;
//	wordTerms      —— 短专名(TRIES 等),大小写敏感 + \b 词边界正则匹配。
//
// 机器词表只在 internal/machinecontract/lexical.go 定义；本包负责执行匹配，
// Shell只保留兼容入口，Spec只解释治理目的而不复制机器集合。
package safety

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Hit 一次命中
type Hit struct {
	Term string // 命中的词
	Kind string // forbidden / overclaim
	Line int    // 行号,从 1 起
	Text string // 命中行(截断)
}

// CheckForbiddenClaims 扫描文本,返回全部命中。
func CheckForbiddenClaims(text string) []Hit {
	hits := []Hit{}
	terms := machinecontract.PublicTextTerms()
	wordPatterns := map[string]*regexp.Regexp{}
	for _, term := range terms {
		if term.Mode == machinecontract.TextMatchWordExact {
			wordPatterns[term.Text] = regexp.MustCompile(`\b` + regexp.QuoteMeta(term.Text) + `\b`)
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, term := range terms {
			matched := false
			switch term.Mode {
			case machinecontract.TextMatchSubstringFold:
				matched = strings.Contains(lower, strings.ToLower(term.Text))
			case machinecontract.TextMatchWordExact:
				matched = wordPatterns[term.Text].MatchString(line)
			}
			if matched {
				hits = append(hits, Hit{Term: term.Text, Kind: term.Kind, Line: i + 1, Text: truncate(line, 80)})
			}
		}
	}
	return hits
}

// FormatHits 命中结果的人读渲染;空命中返回空串
func FormatHits(source string, hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d 处命中\n", source, len(hits))
	for _, h := range hits {
		fmt.Fprintf(&b, "  L%d [%s] %q: %s\n", h.Line, h.Kind, h.Term, h.Text)
	}
	return b.String()
}

// truncate 按 rune 截断防切碎多字节字符
func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
