package managedscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var ruleIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,127}$`)

func Normalize(policy Policy) (Policy, error) {
	if policy.Version == "" {
		policy.Version = machinecontract.ManagedScopePolicyV2
	}
	if policy.Version != machinecontract.ManagedScopePolicyV2 {
		return Policy{}, fmt.Errorf("managed_scope_policy_version_unsupported")
	}
	if !oneOf(policy.Profile, machinecontract.ScopeProfiles()) {
		return Policy{}, fmt.Errorf("managed_scope_profile_invalid")
	}
	if policy.ObserveChangePolicy == "" {
		policy.ObserveChangePolicy = machinecontract.ObserveChangeReviewRequired
	}
	if policy.ObserveChangePolicy != machinecontract.ObserveChangeReviewRequired &&
		policy.ObserveChangePolicy != machinecontract.ObserveChangeInformational {
		return Policy{}, fmt.Errorf("managed_scope_observe_change_policy_invalid")
	}
	if policy.ApprovalMode == "" {
		policy.ApprovalMode = machinecontract.ScopeApprovalModeInherit
	}
	policy.ApprovalMode = strings.ToLower(strings.TrimSpace(policy.ApprovalMode))
	if !oneOf(policy.ApprovalMode, machinecontract.ScopeApprovalModes()) {
		return Policy{}, fmt.Errorf("managed_scope_approval_mode_invalid")
	}
	if policy.ApprovalThresholds.EntryRemovalCount == 0 && policy.ApprovalThresholds.EntryRemovalPercent == 0 {
		policy.ApprovalThresholds = DefaultApprovalThresholds()
	}
	if policy.ApprovalThresholds.EntryRemovalCount < 1 || policy.ApprovalThresholds.EntryRemovalCount > 100000 ||
		policy.ApprovalThresholds.EntryRemovalPercent < 1 || policy.ApprovalThresholds.EntryRemovalPercent > 100 {
		return Policy{}, fmt.Errorf("managed_scope_approval_thresholds_invalid")
	}
	seen := map[string]bool{}
	for index := range policy.Rules {
		rule := &policy.Rules[index]
		rule.RuleID = strings.TrimSpace(rule.RuleID)
		rule.Action = strings.ToLower(strings.TrimSpace(rule.Action))
		rule.PatternKind = strings.ToLower(strings.TrimSpace(rule.PatternKind))
		rule.Source = strings.ToLower(strings.TrimSpace(rule.Source))
		rule.Reason = strings.TrimSpace(rule.Reason)
		rule.DecisionBasis = strings.ToLower(strings.TrimSpace(rule.DecisionBasis))
		rule.CreatedBy = strings.TrimSpace(rule.CreatedBy)
		if !ruleIDPattern.MatchString(rule.RuleID) || seen[rule.RuleID] {
			return Policy{}, fmt.Errorf("managed_scope_rule_id_invalid")
		}
		seen[rule.RuleID] = true
		if !oneOf(rule.Action, machinecontract.ScopeRoles()) ||
			!oneOf(rule.PatternKind, machinecontract.ScopePatternKinds()) ||
			!oneOf(rule.Source, machinecontract.ScopeRuleSources()) {
			return Policy{}, fmt.Errorf("managed_scope_rule_contract_invalid: %s", rule.RuleID)
		}
		if rule.Source == machinecontract.ScopeRuleSafety {
			return Policy{}, fmt.Errorf("managed_scope_user_policy_cannot_define_safety_rule")
		}
		if rule.Reason == "" || rule.CreatedBy == "" || rule.Order < 0 {
			return Policy{}, fmt.Errorf("managed_scope_rule_metadata_invalid: %s", rule.RuleID)
		}
		if rule.DecisionBasis != "" && !oneOf(rule.DecisionBasis, machinecontract.ScopeDecisionBases()) {
			return Policy{}, fmt.Errorf("managed_scope_rule_decision_basis_invalid: %s", rule.RuleID)
		}
		var err error
		rule.Pattern, err = NormalizePattern(rule.Pattern, rule.PatternKind)
		if err != nil {
			return Policy{}, fmt.Errorf("managed_scope_rule_pattern_invalid: %s", rule.RuleID)
		}
		exceptions := make([]string, 0, len(rule.Exceptions))
		for _, exception := range rule.Exceptions {
			normalized, normalizeErr := NormalizePattern(exception, machinecontract.ScopePatternGlob)
			if normalizeErr != nil {
				return Policy{}, fmt.Errorf("managed_scope_rule_exception_invalid: %s", rule.RuleID)
			}
			exceptions = append(exceptions, normalized)
		}
		sort.Strings(exceptions)
		rule.Exceptions = deduplicate(exceptions)
	}
	sort.SliceStable(policy.Rules, func(i, j int) bool {
		left, right := policy.Rules[i], policy.Rules[j]
		if sourcePriority(left.Source) != sourcePriority(right.Source) {
			return sourcePriority(left.Source) < sourcePriority(right.Source)
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.RuleID < right.RuleID
	})
	if policy.Rules == nil {
		policy.Rules = []Rule{}
	}
	return policy, nil
}

func Identity(policy Policy) (string, error) {
	normalized, err := Normalize(policy)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func NormalizePattern(value, kind string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if kind == machinecontract.ScopePatternDirectory {
		value = strings.TrimSuffix(value, "/")
	}
	if value == "" || strings.HasPrefix(value, "/") || regexp.MustCompile(`^[A-Za-z]:`).MatchString(value) {
		return "", fmt.Errorf("path_not_repository_relative")
	}
	if kind != machinecontract.ScopePatternGlob && strings.ContainsAny(value, "*?[") {
		return "", fmt.Errorf("wildcard_requires_glob_kind")
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." || part == "" {
			return "", fmt.Errorf("path_escape_or_empty_segment")
		}
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path_escape")
	}
	if kind == machinecontract.ScopePatternGlob {
		if _, err := globRegexp(clean); err != nil {
			return "", err
		}
	}
	return clean, nil
}

func Match(rule Rule, rel string) bool {
	return MatchWithCase(rule, rel, true)
}

func MatchWithCase(rule Rule, rel string, caseSensitive bool) bool {
	rel, err := fs.NormalizeRelPath(rel)
	if err != nil || !rule.Enabled {
		return false
	}
	pattern := rule.Pattern
	exceptions := rule.Exceptions
	if !caseSensitive {
		rel = strings.ToLower(rel)
		pattern = strings.ToLower(pattern)
		exceptions = append([]string{}, rule.Exceptions...)
		for index := range exceptions {
			exceptions[index] = strings.ToLower(exceptions[index])
		}
	}
	for _, exception := range exceptions {
		if matched, _ := globMatch(exception, rel); matched {
			return false
		}
	}
	switch rule.PatternKind {
	case machinecontract.ScopePatternFile:
		return rel == pattern
	case machinecontract.ScopePatternDirectory:
		return rel == pattern || strings.HasPrefix(rel, pattern+"/")
	case machinecontract.ScopePatternGlob:
		matched, _ := globMatch(pattern, rel)
		return matched
	default:
		return false
	}
}

func globMatch(pattern, rel string) (bool, error) {
	compiled, err := globRegexp(pattern)
	if err != nil {
		return false, err
	}
	return compiled.MatchString(rel), nil
}

func globRegexp(pattern string) (*regexp.Regexp, error) {
	var out strings.Builder
	out.WriteString("^")
	runes := []rune(pattern)
	for index := 0; index < len(runes); index++ {
		r := runes[index]
		switch r {
		case '*':
			if index+1 < len(runes) && runes[index+1] == '*' {
				index++
				if index+1 < len(runes) && runes[index+1] == '/' {
					index++
					out.WriteString("(?:.*/)?")
				} else {
					out.WriteString(".*")
				}
			} else {
				out.WriteString("[^/]*")
			}
		case '?':
			out.WriteString("[^/]")
		case '[':
			return nil, fmt.Errorf("character_classes_not_supported")
		default:
			out.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	out.WriteString("$")
	return regexp.Compile(out.String())
}

func sourcePriority(source string) int {
	switch source {
	case machinecontract.ScopeRuleSafety:
		return 700
	case machinecontract.ScopeRuleUser:
		return 500
	case machinecontract.ScopeRuleCuration:
		return 400
	case machinecontract.ScopeRuleProfile:
		return 300
	case machinecontract.ScopeRuleBuiltin:
		return 200
	default:
		return 0
	}
}

func oneOf(value string, values []string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func deduplicate(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
