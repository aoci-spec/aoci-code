package managedscope

import (
	"fmt"
	"sync"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var memoPatternCorpus = []string{
	"**/*.go", "internal/**/*.go", "*.md", "docs/*.md", "a?c.txt",
	"**/testdata/**", "cmd/aoci/*.go", "third_party/**", "*", "**",
}

var memoPathCorpus = []string{
	"main.go", "internal/cli/root.go", "docs/install.md", "abc.txt",
	"internal/index/testdata/x.txt", "third_party/x/y.go", "a/b/c/d.go", "",
}

// The memo is only allowed to be faster. Its verdicts must be the verdicts a
// cold compile produces, for every pattern and every path.
func TestGlobRegexpMemoAgreesWithAColdCompile(t *testing.T) {
	for _, pattern := range memoPatternCorpus {
		cold, coldErr := buildGlobRegexp(pattern)
		warm, warmErr := globRegexp(pattern)
		again, againErr := globRegexp(pattern)
		if (coldErr == nil) != (warmErr == nil) || (coldErr == nil) != (againErr == nil) {
			t.Fatalf("%q: error disagreement cold=%v warm=%v again=%v", pattern, coldErr, warmErr, againErr)
		}
		if coldErr != nil {
			continue
		}
		if cold.String() != warm.String() || cold.String() != again.String() {
			t.Fatalf("%q: compiled form disagreement", pattern)
		}
		for _, path := range memoPathCorpus {
			if cold.MatchString(path) != warm.MatchString(path) {
				t.Fatalf("%q vs %q: verdict disagreement", pattern, path)
			}
		}
	}
}

// The cache key is the pattern, never a path. If a repository path ever became a
// key the cache would grow with the repository and the governance identity would
// start depending on traversal order.
func TestGlobRegexpMemoIsBoundedByPolicyNotRepository(t *testing.T) {
	count := func() int {
		total := 0
		globRegexpCache.Range(func(any, any) bool { total++; return true })
		return total
	}
	rule := Rule{Enabled: true, PatternKind: machinecontract.ScopePatternGlob, Pattern: "bounded/**/*.go"}
	for index := 0; index < 10; index++ {
		Match(rule, fmt.Sprintf("bounded/a%d/b.go", index))
	}
	after10 := count()
	for index := 0; index < 1000; index++ {
		Match(rule, fmt.Sprintf("bounded/c%d/d.go", index))
	}
	if after1000 := count(); after1000 != after10 {
		t.Fatalf("cache grew with the path count: %d entries after 10 paths, %d after 1000", after10, after1000)
	}
}

// A pattern that is invalid must stay invalid on every later call, and Normalize
// must keep rejecting a policy that contains it even after it has been cached.
func TestGlobRegexpMemoKeepsInvalidPatternsInvalid(t *testing.T) {
	const bad = "internal/[abc]/x.go"
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := globRegexp(bad); err == nil {
			t.Fatalf("attempt %d: character class accepted", attempt)
		}
	}
	policy := DefaultPolicy(machinecontract.ScopeProfileCustom)
	policy.Rules = []Rule{{RuleID: "bad-pattern", Action: machinecontract.ScopeRoleIndex, Pattern: bad,
		PatternKind: machinecontract.ScopePatternGlob, Reason: "bad", Source: machinecontract.ScopeRuleUser,
		CreatedBy: "test", Order: 1, Enabled: true}}
	if _, err := Normalize(policy); err == nil {
		t.Fatal("Normalize accepted a policy carrying a pattern the cache had already seen")
	}
}

func TestGlobRegexpMemoIsRaceFree(t *testing.T) {
	rule := Rule{Enabled: true, PatternKind: machinecontract.ScopePatternGlob, Pattern: "racy/**/*.go"}
	serial := map[string]bool{}
	for _, path := range memoPathCorpus {
		serial[path] = Match(rule, path)
	}
	var group sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < 50; round++ {
				for _, path := range memoPathCorpus {
					if Match(rule, path) != serial[path] {
						t.Errorf("concurrent verdict differs for %q", path)
						return
					}
				}
			}
		}()
	}
	group.Wait()
}
