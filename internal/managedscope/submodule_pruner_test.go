package managedscope

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func userRule(id, action, kind, pattern string) Rule {
	return Rule{RuleID: id, Action: action, PatternKind: kind, Pattern: pattern, Source: machinecontract.ScopeRuleUser, Enabled: true}
}

func TestSubmodulePrunerOnlyPrunesAnUncontestedUserDirectoryExclusion(t *testing.T) {
	exclude := userRule("defer-svc", machinecontract.ScopeRoleExclude, machinecontract.ScopePatternDirectory, "svc")
	cases := []struct {
		name   string
		rules  []Rule
		rel    string
		pruned bool
	}{
		{"covering directory rule", []Rule{exclude}, "svc", true},
		{"parent directory rule", []Rule{userRule("defer-all", machinecontract.ScopeRoleExclude, machinecontract.ScopePatternDirectory, "vendor")}, "vendor/svc", true},
		{"sibling name is not a prefix", []Rule{exclude}, "svc-two", false},
		{"no rule", nil, "svc", false},
		{"index rule inside", []Rule{exclude, userRule("keep", machinecontract.ScopeRoleIndex, machinecontract.ScopePatternDirectory, "svc/app")}, "svc", false},
		{"observe file inside", []Rule{exclude, userRule("keep", machinecontract.ScopeRoleObserve, machinecontract.ScopePatternFile, "svc/go.sum")}, "svc", false},
		{"index rule elsewhere", []Rule{exclude, userRule("keep", machinecontract.ScopeRoleIndex, machinecontract.ScopePatternDirectory, "docs")}, "svc", true},
		{"user glob of another action", []Rule{exclude, userRule("keep", machinecontract.ScopeRoleIndex, machinecontract.ScopePatternGlob, "**/*.md")}, "svc", false},
		{"glob exclusion does not prune", []Rule{userRule("g", machinecontract.ScopeRoleExclude, machinecontract.ScopePatternGlob, "svc/**")}, "svc", false},
	}
	withExceptions := exclude
	withExceptions.Exceptions = []string{"svc/README.md"}
	cases = append(cases, struct {
		name   string
		rules  []Rule
		rel    string
		pruned bool
	}{"exclusion with exceptions", []Rule{withExceptions}, "svc", false})
	disabled := exclude
	disabled.Enabled = false
	cases = append(cases, struct {
		name   string
		rules  []Rule
		rel    string
		pruned bool
	}{"disabled exclusion", []Rule{disabled}, "svc", false})
	for _, c := range cases {
		source, ok := SubmodulePruner(Policy{Rules: c.rules})(c.rel)
		if ok != c.pruned {
			t.Fatalf("%s: pruned=%v want %v", c.name, ok, c.pruned)
		}
		if ok && source == "" {
			t.Fatalf("%s: a pruned submodule names its rule", c.name)
		}
	}
}
