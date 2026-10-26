package managedscope

import "github.com/aoci-spec/aoci-code/internal/machinecontract"

// StarterRuleIDPrefix names the rules StarterRules writes; every one of them is
// `starter-vendored-<slug>`, so a reader of `aoci scope rule list` can tell them
// from the rules a team wrote.
const StarterRuleIDPrefix = "starter-vendored-"

// StarterRuleCreatedBy is the CreatedBy every starter rule carries.
const StarterRuleCreatedBy = "aoci-init"

const starterRuleOrderBase = 100

const starterRuleReason = "vendored static assets and generated bundles are observed, not indexed; " +
	"delete this rule before the first scan if the path holds your own code"

const starterThirdPartyReason = "third-party source trees are observed, not indexed; " +
	"delete this rule before the first scan if the path holds your own code"

const starterAssetReason = "vector images and public certificates are observed, not indexed; " +
	"delete this rule before the first scan to author Entries for them"

const starterLockfileReason = "dependency lockfiles are generated pins and are observed, not indexed; " +
	"the manifest beside each one carries its semantics"

// StarterRules are the observe rules a NEW production-profile repository is
// born with. On a real Java repository (RuoYi, 702 files) `aoci init` with the
// production profile put every file into the index role — 141 third-party files
// under static/ajax/libs/, 52 minified bundles, 17 fonts — and the model then
// spent a third of the first build authoring Entries for jQuery and Bootstrap.
// The profile's built-in rules (profileRules in evaluate.go) know only test
// files and test data, and they are deliberately not extended here:
//
//   - Built-in rules are appended at evaluation time and are not part of the
//     persisted policy, so they never enter Identity. A new built-in rule would
//     flip index to observe in every EXISTING repository at its next scan; that
//     is a coverage reduction, which needs a Scope Change with human approval,
//     and the upgrade axis asserts that a repository built by a released binary
//     stays governable without one.
//   - DefaultPolicy stays unchanged for the same reason: a config.json whose
//     managed_scope block carries no rules resolves through it, and old
//     repositories are compared against the identity their Baseline receipt
//     already holds.
//
// So the rules are materialized once, as ordinary persisted rules, by
// config.SetNewProjectGovernance, which init calls only after proving that no
// config.json existed. An existing repository never sees them, they show up in
// `aoci scope rule list` like any other rule, and `aoci scope rule remove
// <rule-id>` before the first scan drops one without a Scope Change.
//
// Source is `builtin`, the lowest priority above the bare default. Two other
// choices were considered and rejected against sourcePriority:
//
//   - `profile` (300) would tie with the profile's own test and fixture rules,
//     and a tie is decided by Order, so a starter rule at Order 100 would beat
//     `**/testdata/**` (Order 20) under static/libs/ and turn an excluded
//     fixture into an observed one.
//   - `user` (500) would tie with the rule an operator adds through
//     `aoci scope rule add`, whose Order defaults to 0; the operator's index
//     rule for static/lib would then lose to the starter rule at Order 100.
//
// At `builtin` (200) the profile rules keep winning on an overlapping path and
// any user rule overrides a starter rule at any Order, so a team whose own code
// lives under static/lib reclaims it with one `--action index` rule.
//
// No starter rule names a `vendor`, `dist`, or `build` directory: in a new
// repository those names are configured exclusions (exclude_root_dirs) at the
// root and wherever git ignores them, a hard boundary before rules run, while
// a tracked nested module of that name is source by design (#104); a team
// that wants such a module out adds its own rule.
//
// The second group (0.2.0) carries the same reasoning to the material a first
// build otherwise spends its rounds on without gaining system cognition: a
// vendored third_party or third-party tree (this repository's own carried
// connector is 57 Entries, an eighth of its index), SVG markup, public
// certificates (private keys are already a built-in sensitive exclusion), and
// the generated lockfiles of the common package managers, whose manifests stay
// indexed. Appending keeps the first seven rules' ids and orders, so a
// repository initialized before 0.2.0 compares equal to the rules it carries.
//
// Any profile other than production returns nil: `full` indexes everything by
// definition and `custom` starts from an empty rule set the team owns.
func StarterRules(profile string) []Rule {
	if profile != machinecontract.ScopeProfileProduction {
		return nil
	}
	items := []struct{ slug, pattern, reason string }{
		{"static-libs", "**/static/**/libs/**", starterRuleReason},
		{"static-lib", "**/static/**/lib/**", starterRuleReason},
		{"static-plugins", "**/static/**/plugins/**", starterRuleReason},
		{"min-js", "**/*.min.js", starterRuleReason},
		{"min-css", "**/*.min.css", starterRuleReason},
		{"source-map", "**/*.map", starterRuleReason},
		{"fonts", "**/fonts/**", starterRuleReason},
		{"third-party", "**/third_party/**", starterThirdPartyReason},
		{"third-party-dash", "**/third-party/**", starterThirdPartyReason},
		{"svg", "**/*.svg", starterAssetReason},
		{"certificate-crt", "**/*.crt", starterAssetReason},
		{"certificate-cer", "**/*.cer", starterAssetReason},
		{"npm-lock", "**/package-lock.json", starterLockfileReason},
		{"yarn-lock", "**/yarn.lock", starterLockfileReason},
		{"pnpm-lock", "**/pnpm-lock.yaml", starterLockfileReason},
		{"cargo-lock", "**/Cargo.lock", starterLockfileReason},
		{"poetry-lock", "**/poetry.lock", starterLockfileReason},
		{"gemfile-lock", "**/Gemfile.lock", starterLockfileReason},
		{"composer-lock", "**/composer.lock", starterLockfileReason},
		{"go-sum", "**/go.sum", starterLockfileReason},
	}
	rules := make([]Rule, 0, len(items))
	for index, item := range items {
		rules = append(rules, Rule{RuleID: StarterRuleIDPrefix + item.slug, Action: machinecontract.ScopeRoleObserve,
			Pattern: item.pattern, PatternKind: machinecontract.ScopePatternGlob, Reason: item.reason,
			Source: machinecontract.ScopeRuleBuiltin, CreatedBy: StarterRuleCreatedBy,
			Order: starterRuleOrderBase + index, Enabled: true})
	}
	return rules
}
