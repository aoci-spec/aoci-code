package managedscope

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// The regexp compiler defines the dialect; the matcher must agree with it on
// every pattern and path, errors included. Patterns come from the starter
// policy, a handcrafted set, and a generator; paths from a corpus of real
// shapes and a generator over the characters that matter ('/', '\n', '.', a
// multibyte rune, an invalid byte).
func globOraclePatterns() []string {
	patterns := []string{"**/*.min.js", "**/*.go", "src/**", "**/dist/**", "*.txt", "a/?/b", "**", "a**b", "a/**/b", "**/x", "x/**/",
		"", "*", "?", "**/", "/**", "a/*/*.go", "*/*", "**/**", "***", "a*b*c", "a?", "?a", "**/*.*", "docs/**/*.md",
		"vendor/**", "third_party/**", "node_modules/**", "*.lock", "**/*.snap", "a/b/c", ".github/**", "é/**", "**/é*"}
	for _, rule := range StarterRules(machinecontract.ScopeProfileProduction) {
		patterns = append(patterns, rule.Pattern)
		patterns = append(patterns, rule.Exceptions...)
	}
	return patterns
}

func globOraclePaths() []string {
	paths := []string{"", "a", "a/b", "a/b/c", "src/main.go", "src/x/main.go", "main.go", "dist/app.min.js", "web/dist/x/app.min.js",
		"a/x/b", "a//b", "a/b/", "/a", "x", "a/x", "x/", "x/y/", "a\nb", "a/\n/b", "\n", "aé", "é/x", "a.min.js", ".min.js",
		"vendor/lib/x.go", "third_party/x", "docs/a/b.md", "docs/b.md", "node_modules/pkg/index.js", "a/b/c/d/e/f/g/h.txt",
		string([]byte{0xff}) + "/x", "a" + string([]byte{0xff}), "pages/[id].vue"}
	return paths
}

func randomGlobString(r *rand.Rand, alphabet []string, max int) string {
	var b strings.Builder
	for n := r.Intn(max + 1); n > 0; n-- {
		b.WriteString(alphabet[r.Intn(len(alphabet))])
	}
	return b.String()
}

func TestGlobMatchAgreesWithTheRegexpCompiler(t *testing.T) {
	patterns := globOraclePatterns()
	paths := globOraclePaths()
	r := rand.New(rand.NewSource(20261030))
	patternAlphabet := []string{"a", "b", "/", "*", "**", "?", ".", "x", "é", "**/"}
	pathAlphabet := []string{"a", "b", "/", ".", "x", "é", "\n", string([]byte{0xff}), "ab", "a/b"}
	for i := 0; i < 400; i++ {
		patterns = append(patterns, randomGlobString(r, patternAlphabet, 8))
	}
	for i := 0; i < 400; i++ {
		paths = append(paths, randomGlobString(r, pathAlphabet, 10))
	}
	checked := 0
	for _, pattern := range patterns {
		oracle, oracleErr := buildGlobRegexp(pattern)
		program, programErr := compileGlobProgram(pattern)
		if (oracleErr == nil) != (programErr == nil) {
			t.Fatalf("pattern %q: oracle err=%v matcher err=%v", pattern, oracleErr, programErr)
		}
		if oracleErr != nil {
			continue
		}
		for _, rel := range paths {
			want := oracle.MatchString(rel)
			got := program.match(rel)
			if want != got {
				t.Fatalf("pattern %q path %q: regexp says %t, matcher says %t", pattern, rel, want, got)
			}
			checked++
		}
	}
	if checked < 100000 {
		t.Fatalf("too few pairs checked: %d", checked)
	}
}

// FuzzGlobMatch is the same oracle under the fuzzer: any divergence between
// the regexp definition and the matcher is a bug in the matcher.
func FuzzGlobMatch(f *testing.F) {
	for _, pattern := range globOraclePatterns() {
		for _, rel := range []string{"a/b", "src/main.go", "a\nb", ""} {
			f.Add(pattern, rel)
		}
	}
	f.Fuzz(func(t *testing.T, pattern, rel string) {
		oracle, oracleErr := buildGlobRegexp(pattern)
		program, programErr := compileGlobProgram(pattern)
		if (oracleErr == nil) != (programErr == nil) {
			t.Fatalf("pattern %q: oracle err=%v matcher err=%v", pattern, oracleErr, programErr)
		}
		if oracleErr != nil {
			return
		}
		if want, got := oracle.MatchString(rel), program.match(rel); want != got {
			t.Fatalf("pattern %q path %q: regexp %t matcher %t", pattern, rel, want, got)
		}
	})
}
