// Forbidden-word and overclaim scanning: the D3 discipline compiled from human self-restraint into a machine gate
// Index entry: safety.go[Safe.Claims.9.IP.S]
//
// Use: scan every text about to be published (README / Spec body / rendered templates / MCP tool description source)
// and report any forbidden word or overclaim hit. Called by scripts/check-public-text.sh and CI.
//
// Two matching tracks (a collateral-damage lesson: the case-insensitive substring match of TRIES once misreported every get_entries/Entries):
//
//	substringTerms: long words and phrases, case-insensitive substring match;
//	wordTerms:      short proper names (TRIES and the like), case-sensitive plus \b word-boundary regular expression match.
//
// The machine vocabulary is defined only in internal/machinecontract/lexical.go; this package performs the matching,
// the Shell keeps only a compatibility entry, and the Spec explains the governance purpose without copying the machine set.
package safety

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Hit is one match
type Hit struct {
	Term string // the matched term
	Kind string // forbidden / overclaim
	Line int    // line number, starting at 1
	Text string // the matched line (truncated)
}

// CheckForbiddenClaims scans the text and returns every hit.
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

// FormatHits is the human-readable rendering of hits; no hits returns an empty string
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

// truncate truncates by rune so a multi-byte character is not cut apart
func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
