package managedscope

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A file or directory pattern is compared byte for byte, so "[" is an ordinary
// character there. Dynamic-route names (pages/[id].vue, app/[locale]/) became
// indexable with the extended Entry reading, and an operator has to be able to
// name one in a rule; "*" and "?" stay reserved for the glob kind.
func TestFileAndDirectoryPatternsMayHoldBrackets(t *testing.T) {
	file, err := NormalizePattern("pages/docs/[...id].jsx", machinecontract.ScopePatternFile)
	if err != nil || file != "pages/docs/[...id].jsx" {
		t.Fatalf("file pattern: %q %v", file, err)
	}
	directory, err := NormalizePattern("app/[locale]/", machinecontract.ScopePatternDirectory)
	if err != nil || directory != "app/[locale]" {
		t.Fatalf("directory pattern: %q %v", directory, err)
	}
	for _, reserved := range []string{"pages/*.jsx", "pages/?.jsx"} {
		if _, err := NormalizePattern(reserved, machinecontract.ScopePatternFile); err == nil {
			t.Fatalf("%q must still require the glob kind", reserved)
		}
	}
	// The CLI defaults to the glob kind, where "[" is refused. The refusal has to
	// say which kind does accept the name, or the operator is left guessing.
	if _, err := NormalizePattern("app/[locale]", machinecontract.ScopePatternGlob); err == nil ||
		!strings.Contains(err.Error(), "character_classes_not_supported") || !strings.Contains(err.Error(), "kind file or directory") {
		t.Fatalf("a glob holding a bracket must be refused with the way out: %v", err)
	}
	rule := Rule{RuleID: "route", Action: machinecontract.ScopeRoleExclude, Pattern: file, PatternKind: machinecontract.ScopePatternFile, Enabled: true}
	if !Match(rule, "pages/docs/[...id].jsx") || Match(rule, "pages/docs/[...slug].jsx") {
		t.Fatal("a bracketed file pattern matches exactly its own path")
	}
	tree := Rule{RuleID: "locale", Action: machinecontract.ScopeRoleExclude, Pattern: directory, PatternKind: machinecontract.ScopePatternDirectory, Enabled: true}
	if !Match(tree, "app/[locale]/page.tsx") || Match(tree, "app/locale/page.tsx") {
		t.Fatal("a bracketed directory pattern matches its own subtree only")
	}
}

// The operator never calls NormalizePattern; the rule goes through Normalize,
// which used to answer only "managed_scope_rule_pattern_invalid: <rule>" and
// drop the reason. The way out has to survive that hop (third review: the new
// guidance never reached `aoci scope rule add`, whose default kind is glob).
func TestPolicyNormalizeCarriesThePatternReason(t *testing.T) {
	policy := DefaultPolicy(machinecontract.ScopeProfileCustom)
	policy.Rules = []Rule{{RuleID: "route-group", Action: machinecontract.ScopeRoleExclude, Pattern: "pages/[a-z]*.go",
		PatternKind: machinecontract.ScopePatternGlob, Reason: "route", Source: machinecontract.ScopeRuleUser,
		CreatedBy: "test", Order: 1, Enabled: true}}
	_, err := Normalize(policy)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_rule_pattern_invalid: route-group") ||
		!strings.Contains(err.Error(), "kind file or directory") {
		t.Fatalf("the refusal must keep its code, name the rule, and carry the way out: %v", err)
	}
}
