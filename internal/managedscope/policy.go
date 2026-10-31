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
	"sync"

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
			// The reason travels with the code: a glob that holds "[" is refused with
			// the pattern kind that does accept it, and an operator who is only told
			// "invalid" cannot find that out.
			return Policy{}, fmt.Errorf("managed_scope_rule_pattern_invalid: %s (%v)", rule.RuleID, err)
		}
		exceptions := make([]string, 0, len(rule.Exceptions))
		for _, exception := range rule.Exceptions {
			normalized, normalizeErr := NormalizePattern(exception, machinecontract.ScopePatternGlob)
			if normalizeErr != nil {
				return Policy{}, fmt.Errorf("managed_scope_rule_exception_invalid: %s (%v)", rule.RuleID, normalizeErr)
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
	// A file or directory pattern is compared byte for byte, so "[" is an ordinary
	// character there: dynamic-route names such as pages/[id].vue and app/[locale]
	// have to be nameable. "*" and "?" stay reserved for the glob kind, where a
	// literal reading would silently disagree with what the author meant.
	if kind != machinecontract.ScopePatternGlob && strings.ContainsAny(value, "*?") {
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
		if _, err := globProgramFor(clean); err != nil {
			return "", err
		}
	}
	return clean, nil
}

// Match uses Git's path semantics: exact, case-sensitive, the same on every host.
// The case-insensitive branch that once existed made the match result vary with the host filesystem, so the same repository's
// governance identity depended on whose machine ran the scan; that is not a tolerable difference but a defect.
func Match(rule Rule, rel string) bool {
	rel, err := fs.NormalizeRelPath(rel)
	if err != nil || !rule.Enabled {
		return false
	}
	pattern := rule.Pattern
	exceptions := rule.Exceptions
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

// globMatch answers whether rel matches pattern under the glob dialect
// buildGlobRegexp defines. It no longer runs the regexp engine: on a tree of
// 13,850 files the starter rules cost 260,000 matches per evaluation and the
// backtracker spent half of every governance call in them. The program below
// is a token NFA with the same dialect, judged against buildGlobRegexp by a
// differential test and a fuzz target, so the regexp stays the definition.
func globMatch(pattern, rel string) (bool, error) {
	program, err := globProgramFor(pattern)
	if err != nil {
		return false, err
	}
	return program.match(rel), nil
}

type globCacheEntry struct {
	program *globProgram
	err     error
}

// globProgramCache memoizes glob compilation. THE KEY IS THE EXACT PATTERN
// BYTES and must never be normalized, case-folded, or combined with a path:
// making the key anything else is precisely the host-dependence that
// host_independent_paths_test.go exists to prevent.
//
// globProgramFor is a pure function of those bytes — no globals, no
// filesystem, no host probe — and a compiled program is read-only, so it is
// safe for concurrent use. Keys arrive only from Rule.Pattern and
// Rule.Exceptions, and curation exclusions enter as PatternKind file rather
// than glob, so no repository path ever becomes a key. The cache is therefore
// bounded by policy size, not by repository size.
//
// The error is cached deliberately. globMatch discards it, nothing compares
// these errors by identity, and Normalize still rejects the pattern on every
// later call, so a pattern that is invalid once stays invalid.
var globProgramCache sync.Map

func globProgramFor(pattern string) (*globProgram, error) {
	if cached, ok := globProgramCache.Load(pattern); ok {
		entry := cached.(globCacheEntry)
		return entry.program, entry.err
	}
	program, err := compileGlobProgram(pattern)
	actual, _ := globProgramCache.LoadOrStore(pattern, globCacheEntry{program: program, err: err})
	entry := actual.(globCacheEntry)
	return entry.program, entry.err
}

// buildGlobRegexp is the definition of the dialect and the oracle the matcher
// is judged against; production matching goes through compileGlobProgram.
func buildGlobRegexp(pattern string) (*regexp.Regexp, error) {
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
			return nil, fmt.Errorf("character_classes_not_supported: a glob cannot hold \"[\"; name a literal path such as pages/[id].vue or app/[locale] with pattern kind file or directory")
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
