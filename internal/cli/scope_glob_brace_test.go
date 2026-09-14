package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// The glob compiler treats "{" as a literal, so a rule written with brace
// alternation was accepted and matched nothing. Adding or editing such a rule
// is refused with the working spellings; a persisted rule is not touched, so
// no repository stops loading over it.
func TestScopeRuleAddRefusesBraceAlternation(t *testing.T) {
	root := buildScopeCLIRepo(t)
	before, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runScopeCLI(t, root, "rule", "add", "user-images", "--action=observe", "--pattern=assets/*.{png,jpg}",
		"--pattern-kind=glob", "--reason=binary assets are observed", "--created-by=test")
	if err == nil || !strings.Contains(string(output)+err.Error(), "glob_brace_alternation_not_supported") {
		t.Fatalf("brace alternation was accepted: err=%v output=%s", err, output)
	}
	after, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.EffectiveManagedScope().Rules) != len(before.EffectiveManagedScope().Rules) {
		t.Fatal("a refused rule changed the policy")
	}
	if output, err := runScopeCLI(t, root, "rule", "add", "user-png", "--action=observe", "--pattern=assets/*.png",
		"--pattern-kind=glob", "--reason=binary assets are observed", "--created-by=test"); err != nil {
		t.Fatalf("a plain glob must still be accepted: %v: %s", err, output)
	}
	if output, err := runScopeCLI(t, root, "rule", "update", "user-png", "--pattern=assets/*.{png,jpg}"); err == nil ||
		!strings.Contains(string(output)+err.Error(), "glob_brace_alternation_not_supported") {
		t.Fatalf("brace alternation was accepted on update: err=%v output=%s", err, output)
	}
}
