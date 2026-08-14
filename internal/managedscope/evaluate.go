package managedscope

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type BuildOptions struct {
	WalkOptions     fs.WalkOptions
	HighRiskOptIn   []string
	CurationExclude []string
}

func Build(repositoryRoot string, policy Policy, options BuildOptions) (*Evaluation, error) {
	normalized, err := Normalize(policy)
	if err != nil {
		return nil, err
	}
	baseIdentity, err := Identity(normalized)
	if err != nil {
		return nil, err
	}
	walkOptions := options.WalkOptions
	if len(options.HighRiskOptIn) > 0 {
		walkOptions.HighRiskOptIn = append([]string{}, options.HighRiskOptIn...)
	}
	walkOptions.IncludeIgnoredCandidates = true
	inventory, err := fs.BuildSafeInventory(repositoryRoot, walkOptions)
	if err != nil {
		return nil, err
	}
	ignored := make(map[string]bool, len(inventory.IgnoredPaths))
	for _, rel := range inventory.IgnoredPaths {
		ignored[rel] = true
	}
	tracked := make(map[string]bool, len(inventory.TrackedPaths))
	for _, rel := range inventory.TrackedPaths {
		tracked[rel] = true
	}
	curated := make(map[string]bool, len(options.CurationExclude))
	for _, rel := range options.CurationExclude {
		clean, normalizeErr := fs.NormalizeRelPath(rel)
		if normalizeErr != nil {
			return nil, fmt.Errorf("managed_scope_curation_path_invalid")
		}
		curated[clean] = true
	}
	curatedPaths := make([]string, 0, len(curated))
	for rel := range curated {
		curatedPaths = append(curatedPaths, rel)
	}
	sort.Strings(curatedPaths)
	caseSensitive := filesystemCaseSensitive(repositoryRoot, inventory.ManagedCandidates)
	identity := evaluationIdentity(baseIdentity, inventory.Summary.RulesIdentity, curatedPaths, caseSensitive)
	optedIn := make(map[string]bool, len(walkOptions.HighRiskOptIn))
	for _, rel := range walkOptions.HighRiskOptIn {
		if clean, normalizeErr := fs.NormalizeRelPath(rel); normalizeErr == nil {
			optedIn[clean] = true
		}
	}
	result := &Evaluation{Version: machinecontract.ManagedScopeEvaluationV2, PolicyIdentity: identity,
		SafeInventory: inventory.Summary, Index: []PathEvaluation{}, Observe: []PathEvaluation{}, Exclude: []PathEvaluation{},
		CaseSensitive: caseSensitive}
	// 大小写语义等价证明: 逐路径用相反语义重评一次, 只有全部角色与指纹参与
	// 完全一致时, 相反语义下的身份才是本次应用范围的合法替代身份。这允许
	// Linux 建立的 Baseline 被 Windows 检出原样接受; 真实的匹配差异保持空值。
	caseEquivalent := true
	for _, rel := range inventory.ManagedCandidates {
		evaluation := EvaluatePathWithCase(normalized, rel, tracked[rel], ignored[rel], curated[rel], caseSensitive)
		if caseEquivalent {
			alternate := EvaluatePathWithCase(normalized, rel, tracked[rel], ignored[rel], curated[rel], !caseSensitive)
			if alternate.Role != evaluation.Role ||
				alternate.EntersWholeIndex != evaluation.EntersWholeIndex ||
				alternate.EntersObserveFingerprint != evaluation.EntersObserveFingerprint {
				caseEquivalent = false
			}
		}
		if optedIn[rel] {
			evaluation.SafetyStatus = "high_risk_exact_opt_in"
			evaluation.Reason = "approved high-risk exact opt-in; " + evaluation.Reason
		}
		appendEvaluation(result, evaluation)
	}
	if caseEquivalent {
		result.AlternatePolicyIdentity = evaluationIdentity(baseIdentity, inventory.Summary.RulesIdentity, curatedPaths, !caseSensitive)
	}
	for _, excluded := range inventory.Exclusions {
		evaluation := PathEvaluation{Version: machinecontract.ManagedScopeEvaluationV2, Path: excluded.PathSummary,
			Role: machinecontract.ScopeRoleExclude, RuleSource: machinecontract.ScopeRuleSafety, RulePriority: sourcePriority(machinecontract.ScopeRuleSafety),
			SafetyStatus: excluded.Category, GitStatus: gitStatus(excluded.GitTracked, excluded.Category == fs.SafetyIgnored),
			ReadsContent: false, EntersWholeIndex: false, EntersObserveFingerprint: false,
			Reason: excluded.RuleSource + ":" + excluded.Category, CaseSensitive: caseSensitive}
		appendEvaluation(result, evaluation)
		result.SafetyExcluded++
	}
	sortEvaluations(result.Index)
	sortEvaluations(result.Observe)
	sortEvaluations(result.Exclude)
	result.IndexCount, result.ObserveCount, result.ExcludeCount = len(result.Index), len(result.Observe), len(result.Exclude)
	result.RequiredHumanReview = inventory.Summary.RequiredHumanReview
	return result, nil
}

func evaluationIdentity(policyIdentity, inventoryRulesIdentity string, curated []string, caseSensitive bool) string {
	hash := sha256.New()
	for _, value := range append([]string{"managed-scope-applied-identity/v2", policyIdentity, inventoryRulesIdentity, fmt.Sprint(caseSensitive)}, curated...) {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func EvaluatePath(policy Policy, rel string, gitIgnored, curationExcluded bool) PathEvaluation {
	return EvaluatePathWithCase(policy, rel, false, gitIgnored, curationExcluded, true)
}

func EvaluatePathWithCase(policy Policy, rel string, gitTracked, gitIgnored, curationExcluded, caseSensitive bool) PathEvaluation {
	role := machinecontract.ScopeRoleIndex
	reason := "default production role"
	priority := 100
	source := "default"
	if policy.Profile == machinecontract.ScopeProfileCustom {
		role, reason = machinecontract.ScopeRoleExclude, "custom profile requires an explicit matching rule"
	}
	if gitIgnored {
		role, reason, priority, source = machinecontract.ScopeRoleExclude, "Git ignored path", 150, "git_ignored"
	}
	var winner *Rule
	rules := append(profileRules(policy.Profile), policy.Rules...)
	if curationExcluded {
		rules = append(rules, Rule{RuleID: "curation-exact-" + safeRuleSuffix(rel), Action: machinecontract.ScopeRoleExclude,
			Pattern: rel, PatternKind: machinecontract.ScopePatternFile, Reason: "exact Curation exclusion", Source: machinecontract.ScopeRuleCuration,
			CreatedBy: "curation", Order: 0, Enabled: true})
	}
	for index := range rules {
		rule := rules[index]
		if !MatchWithCase(rule, rel, caseSensitive) {
			continue
		}
		candidatePriority := sourcePriority(rule.Source)
		if candidatePriority < priority || (candidatePriority == priority && winner != nil && rule.Order < winner.Order) {
			continue
		}
		copyRule := rule
		winner = &copyRule
		role, reason, source, priority = rule.Action, rule.Reason, rule.Source, candidatePriority
	}
	safety := "safe_inventory_allowed"
	result := PathEvaluation{Version: machinecontract.ManagedScopeEvaluationV2, Path: rel, Role: role, MatchedRule: winner,
		RuleSource: source, RulePriority: priority, SafetyStatus: safety, GitStatus: gitStatus(gitTracked, gitIgnored),
		ReadsContent: role != machinecontract.ScopeRoleExclude, EntersWholeIndex: role == machinecontract.ScopeRoleIndex,
		EntersObserveFingerprint: role == machinecontract.ScopeRoleObserve, Reason: reason, CaseSensitive: caseSensitive}
	return result
}

func filesystemCaseSensitive(root string, candidates []string) bool {
	paths := []string{filepath.Clean(root)}
	for _, rel := range candidates {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
		if len(paths) >= 9 {
			break
		}
	}
	for _, original := range paths {
		directory, base := filepath.Dir(original), filepath.Base(original)
		alternate := toggleASCII(base)
		if alternate == base {
			continue
		}
		originalInfo, originalErr := os.Lstat(original)
		alternateInfo, alternateErr := os.Lstat(filepath.Join(directory, alternate))
		if originalErr == nil && alternateErr == nil && os.SameFile(originalInfo, alternateInfo) {
			return false
		}
	}
	return runtime.GOOS != "windows"
}

func toggleASCII(value string) string {
	for index, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
			return value[:index] + strings.ToUpper(string(character)) + value[index+1:]
		case character >= 'A' && character <= 'Z':
			return value[:index] + strings.ToLower(string(character)) + value[index+1:]
		}
	}
	return value
}

func profileRules(profile string) []Rule {
	if profile == machinecontract.ScopeProfileCustom {
		return []Rule{}
	}
	if profile == machinecontract.ScopeProfileFull {
		return []Rule{{RuleID: "profile-full-index", Action: machinecontract.ScopeRoleIndex, Pattern: "**", PatternKind: machinecontract.ScopePatternGlob,
			Reason: "full profile indexes every Safe Inventory candidate", Source: machinecontract.ScopeRuleProfile,
			CreatedBy: "aoci-profile", Order: 1, Enabled: true}}
	}
	rules := []Rule{}
	order := 1
	add := func(id, action, pattern, reason string) {
		rules = append(rules, Rule{RuleID: id, Action: action, Pattern: pattern, PatternKind: machinecontract.ScopePatternGlob,
			Reason: reason, Source: machinecontract.ScopeRuleProfile, CreatedBy: "aoci-profile", Order: order, Enabled: true})
		order++
	}
	for _, item := range []struct{ id, pattern string }{
		{"profile-production-go-tests", "**/*_test.go"}, {"profile-production-tests", "tests/**"},
		{"profile-production-test-dir", "**/tests/**"}, {"profile-production-integration-tests", "integration-tests/**"},
		{"profile-production-test-scripts", "**/*_test.sh"}, {"profile-production-python-test-prefix", "**/test_*.py"},
		{"profile-production-shell-test-prefix", "**/test-*.sh"}, {"profile-production-shell-test-suffix", "**/*-test.sh"},
		{"profile-production-python-test-suffix", "**/*_test.py"}, {"profile-production-js-tests", "**/*.test.js"},
		{"profile-production-ts-tests", "**/*.test.ts"}, {"profile-production-js-specs", "**/*.spec.js"},
		{"profile-production-ts-specs", "**/*.spec.ts"}, {"profile-production-jsx-tests", "**/*.test.jsx"},
		{"profile-production-tsx-tests", "**/*.test.tsx"}, {"profile-production-powershell-tests", "**/*_test.ps1"},
		{"profile-production-windows-tests", "**/*_test.cmd"}, {"profile-production-blackbox-tests", "**/*blackbox_test*"},
	} {
		add(item.id, machinecontract.ScopeRoleObserve, item.pattern, "production profile observes test responsibilities")
	}
	for _, item := range []struct{ id, pattern string }{
		{"profile-production-testdata", "testdata/**"}, {"profile-production-nested-testdata", "**/testdata/**"},
		{"profile-production-fixtures", "fixtures/**"}, {"profile-production-nested-fixtures", "**/fixtures/**"},
		{"profile-production-golden", "golden/**"}, {"profile-production-nested-golden", "**/golden/**"},
		{"profile-production-snapshots", "snapshots/**"}, {"profile-production-nested-snapshots", "**/snapshots/**"},
		{"profile-production-golden-files", "**/*.golden"}, {"profile-production-snapshot-files", "**/*.snap"},
	} {
		add(item.id, machinecontract.ScopeRoleExclude, item.pattern, "production profile excludes test data and generated comparison bodies")
	}
	return rules
}

func appendEvaluation(result *Evaluation, evaluation PathEvaluation) {
	switch evaluation.Role {
	case machinecontract.ScopeRoleIndex:
		result.Index = append(result.Index, evaluation)
	case machinecontract.ScopeRoleObserve:
		result.Observe = append(result.Observe, evaluation)
	default:
		result.Exclude = append(result.Exclude, evaluation)
	}
}

func sortEvaluations(values []PathEvaluation) {
	sort.Slice(values, func(i, j int) bool { return values[i].Path < values[j].Path })
}

func gitStatus(tracked, ignored bool) string {
	if ignored {
		return "ignored"
	}
	if tracked {
		return "tracked"
	}
	return "untracked_or_non_git"
}

func safeRuleSuffix(value string) string {
	result := ""
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			result += string(r)
		} else {
			result += "-"
		}
	}
	if len(result) > 96 {
		result = result[:96]
	}
	return result
}
