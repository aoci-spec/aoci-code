package managedscope

import (
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// starterPolicy is what config.SetNewProjectGovernance persists for a new
// production repository: the unchanged DefaultPolicy plus the starter rules.
func starterPolicy(t *testing.T) Policy {
	t.Helper()
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	policy.Rules = append(policy.Rules, StarterRules(machinecontract.ScopeProfileProduction)...)
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatalf("Normalize refused the starter rules: %v", err)
	}
	return normalized
}

func TestStarterRulesExistOnlyForProductionInAFixedOrder(t *testing.T) {
	// No vendor, dist, or build pattern: artifact exclusions belong to the
	// repository's persisted inventory policy, not the starter rule set.
	want := []string{"**/static/**/libs/**", "**/static/**/lib/**", "**/static/**/plugins/**",
		"**/*.min.js", "**/*.min.css", "**/*.map", "**/fonts/**"}
	rules := StarterRules(machinecontract.ScopeProfileProduction)
	if len(rules) != len(want) {
		t.Fatalf("production starter rules: got %d, want %d: %+v", len(rules), len(want), rules)
	}
	seen := map[string]bool{}
	for index, rule := range rules {
		if rule.Pattern != want[index] {
			t.Fatalf("rule %d pattern=%q, want %q", index, rule.Pattern, want[index])
		}
		if !strings.HasPrefix(rule.RuleID, StarterRuleIDPrefix) || !ruleIDPattern.MatchString(rule.RuleID) || seen[rule.RuleID] {
			t.Fatalf("rule %d id %q is not a unique starter id Normalize accepts", index, rule.RuleID)
		}
		seen[rule.RuleID] = true
		if rule.Action != machinecontract.ScopeRoleObserve || rule.PatternKind != machinecontract.ScopePatternGlob ||
			rule.Source != machinecontract.ScopeRuleBuiltin || rule.CreatedBy != StarterRuleCreatedBy ||
			rule.Order != 100+index || !rule.Enabled || rule.DecisionBasis != "" || rule.Reason == "" || len(rule.Exceptions) != 0 {
			t.Fatalf("rule %d does not carry the starter contract: %+v", index, rule)
		}
	}
	for _, profile := range []string{machinecontract.ScopeProfileFull, machinecontract.ScopeProfileCustom, "", "legacy"} {
		if got := StarterRules(profile); got != nil {
			t.Fatalf("profile %q must not receive starter rules: %+v", profile, got)
		}
	}
}

func TestStarterRulesNormalizeWithoutTouchingDefaultPolicy(t *testing.T) {
	normalized := starterPolicy(t)
	if len(normalized.Rules) != 7 {
		t.Fatalf("Normalize kept %d rules, want 7: %+v", len(normalized.Rules), normalized.Rules)
	}
	for _, rule := range normalized.Rules {
		if !strings.HasPrefix(rule.RuleID, StarterRuleIDPrefix) || rule.Source != machinecontract.ScopeRuleBuiltin {
			t.Fatalf("Normalize rewrote a starter rule: %+v", rule)
		}
	}
	// DefaultPolicy is the preimage every old config without rules still
	// resolves; a starter rule inside it would move those identities.
	if rules := DefaultPolicy(machinecontract.ScopeProfileProduction).Rules; len(rules) != 0 {
		t.Fatalf("DefaultPolicy(production) must stay rule-free: %+v", rules)
	}
}

func TestStarterRulesEvaluatePath(t *testing.T) {
	policy := starterPolicy(t)
	for path, role := range map[string]string{
		"static/ajax/libs/jquery.min.js": machinecontract.ScopeRoleObserve,
		"static/js/plugins/x.js":         machinecontract.ScopeRoleObserve,
		"assets/fonts/a.woff2":           machinecontract.ScopeRoleObserve,
		"dist/app.map":                   machinecontract.ScopeRoleObserve,
		"static/ruoyi/js/ry-ui.js":       machinecontract.ScopeRoleIndex,
		"src/lib/util.ts":                machinecontract.ScopeRoleIndex,
		"src/app.go":                     machinecontract.ScopeRoleIndex,
	} {
		got := EvaluatePath(policy, path, false, false)
		if got.Role != role {
			t.Fatalf("%s role=%q, want %q: %+v", path, got.Role, role, got)
		}
		if role == machinecontract.ScopeRoleObserve && (got.MatchedRule == nil ||
			!strings.HasPrefix(got.MatchedRule.RuleID, StarterRuleIDPrefix) ||
			got.RuleSource != machinecontract.ScopeRuleBuiltin || got.RulePriority != sourcePriority(machinecontract.ScopeRuleBuiltin)) {
			t.Fatalf("%s must be observed by a starter rule at builtin priority: %+v", path, got)
		}
		if role == machinecontract.ScopeRoleIndex && got.MatchedRule != nil {
			t.Fatalf("%s is the project's own code and must not match any rule: %+v", path, got)
		}
	}
	// builtin sits below profile: the production profile's own fixture rule
	// keeps winning under a vendored directory instead of being demoted to
	// observe by a starter rule with a larger Order.
	fixture := EvaluatePath(policy, "static/ajax/libs/testdata/case.json", false, false)
	if fixture.Role != machinecontract.ScopeRoleExclude || fixture.RuleSource != machinecontract.ScopeRuleProfile {
		t.Fatalf("profile rule must outrank a starter rule: %+v", fixture)
	}
}

func TestStarterRulesBuildKeepsInitialApprovalAutomatic(t *testing.T) {
	root := initialApprovalFixture(t, map[string]string{
		"static/ajax/libs/jquery.min.js": "/*! jQuery */!function(e){}();\n",
		"static/js/plugins/x.js":         "(function(){})();\n",
		"assets/fonts/a.woff2":           "not a real font\n",
		"static/js/app.js.map":           "{\"version\":3}\n",
		"dist/app.map":                   "{\"version\":3}\n",
		"static/ruoyi/js/ry-ui.js":       "window.ry = {};\n",
		"src/lib/util.ts":                "export const util = 1;\n",
		"src/app.go":                     "package src\n",
	})
	// dist is an exclude_dirs entry, which init persists; the options carry it
	// the way a repository's configuration does.
	evaluation, err := Build(root, starterPolicy(t), BuildOptions{WalkOptions: afs.WalkOptions{ExcludeDirs: []string{"dist"}}})
	if err != nil {
		t.Fatal(err)
	}
	proposal := BuildProposal(evaluation, machinecontract.ScopeProfileProduction, 0)
	if proposal.RequiresHumanApproval {
		t.Fatalf("starter rules must not turn a plain initialization into a human decision: %+v", proposal)
	}
	for path, role := range map[string]string{
		"static/ajax/libs/jquery.min.js": machinecontract.ScopeRoleObserve,
		"static/js/plugins/x.js":         machinecontract.ScopeRoleObserve,
		"assets/fonts/a.woff2":           machinecontract.ScopeRoleObserve,
		"static/js/app.js.map":           machinecontract.ScopeRoleObserve,
		"static/ruoyi/js/ry-ui.js":       machinecontract.ScopeRoleIndex,
		"src/lib/util.ts":                machinecontract.ScopeRoleIndex,
		"src/app.go":                     machinecontract.ScopeRoleIndex,
	} {
		item, ok := evaluationForPath(evaluation, path)
		if !ok || item.Role != role {
			t.Fatalf("%s role=%q found=%v, want %q", path, item.Role, ok, role)
		}
	}
	// The configured exclusion refuses dist/ before any rule runs, so a map
	// file there is neither indexed nor observed, whatever the starter rule says.
	for _, group := range [][]PathEvaluation{evaluation.Index, evaluation.Observe} {
		for _, item := range group {
			if item.Path == "dist/app.map" {
				t.Fatalf("dist/app.map escaped the safety boundary: %+v", item)
			}
		}
	}
}

func TestUserRuleOverridesStarterRule(t *testing.T) {
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	policy.Rules = append(policy.Rules, StarterRules(machinecontract.ScopeProfileProduction)...)
	// Exactly what `aoci scope rule add team-static-lib --action index
	// --pattern '**/static/**/lib/**'` writes, including the default Order 0.
	policy.Rules = append(policy.Rules, Rule{RuleID: "team-static-lib", Action: machinecontract.ScopeRoleIndex,
		Pattern: "**/static/**/lib/**", PatternKind: machinecontract.ScopePatternGlob, Reason: "static/lib is our own code",
		Source: machinecontract.ScopeRuleUser, CreatedBy: "human", Order: 0, Enabled: true})
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	got := EvaluatePath(normalized, "static/ruoyi/lib/own.js", false, false)
	if got.Role != machinecontract.ScopeRoleIndex || got.MatchedRule == nil || got.MatchedRule.RuleID != "team-static-lib" ||
		got.RuleSource != machinecontract.ScopeRuleUser {
		t.Fatalf("a user rule at Order 0 must override the starter rule: %+v", got)
	}
	if sibling := EvaluatePath(normalized, "static/ruoyi/libs/jquery.js", false, false); sibling.Role != machinecontract.ScopeRoleObserve {
		t.Fatalf("the other starter rules must stay in force: %+v", sibling)
	}
}
