package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/spf13/cobra"
)

type initialScopeRuleResult struct {
	RuleID  string `json:"rule_id"`
	Pattern string `json:"pattern"`
	Added   bool   `json:"added"`
}

var errInitialScopeRulesUnchanged = errors.New("managed_scope_initial_rules_unchanged")

func addInitialScopeRules(cmd *cobra.Command, root string, template managedscope.Rule, patterns []string) error {
	if !strings.EqualFold(strings.TrimSpace(template.Action), machinecontract.ScopeRoleExclude) ||
		!strings.EqualFold(strings.TrimSpace(template.PatternKind), machinecontract.ScopePatternDirectory) || !template.Enabled {
		return &ExitError{Code: ExitInvalid, MachineCode: "managed_scope_batch_requires_directory_excludes", Msg: cliMessage("scope.rule_batch_requires_directory_excludes")}
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return managedScopeExitError(err)
	}
	batch := managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction)
	for _, input := range patterns {
		pattern, err := managedscope.NormalizePattern(input, machinecontract.ScopePatternDirectory)
		if err != nil {
			return managedScopeExitError(fmt.Errorf("managed_scope_rule_pattern_invalid: %q (%w)", input, err))
		}
		if err := validateInitialScopeDirectory(canonicalRoot, pattern); err != nil {
			return managedScopeExitError(err)
		}
		rule := template
		digest := sha256.Sum256([]byte(pattern))
		rule.RuleID = fmt.Sprintf("%s-%x", strings.TrimSpace(template.RuleID), digest[:8])
		rule.Pattern = pattern
		batch.Rules = append(batch.Rules, rule)
	}
	batch, err = managedscope.Normalize(batch)
	if err != nil {
		return managedScopeExitError(err)
	}

	results := make([]initialScopeRuleResult, 0, len(batch.Rules))
	err = config.MutateInitialManagedScope(root, func(policy *managedscope.Policy) error {
		// The config CAS can retry against a newer policy.
		results = results[:0]
		existing := make(map[string]managedscope.Rule, len(policy.Rules))
		for _, rule := range policy.Rules {
			existing[rule.RuleID] = rule
		}
		before := len(policy.Rules)
		for _, rule := range batch.Rules {
			previous, found := existing[rule.RuleID]
			if found && !reflect.DeepEqual(previous, rule) {
				return fmt.Errorf("managed_scope_rule_exists: %s", rule.RuleID)
			}
			if !found {
				policy.Rules = append(policy.Rules, rule)
			}
			results = append(results, initialScopeRuleResult{RuleID: rule.RuleID, Pattern: rule.Pattern, Added: !found})
		}
		if len(policy.Rules) == before {
			return errInitialScopeRulesUnchanged
		}
		return nil
	})
	if errors.Is(err, config.ErrManagedScopeInitialBaselineExists) {
		return &ExitError{Code: ExitInvalid, MachineCode: "managed_scope_initial_baseline_exists", Msg: cliMessage("scope.rule_batch_baseline_exists")}
	}
	if err != nil && !errors.Is(err, errInitialScopeRulesUnchanged) {
		return managedScopeExitError(err)
	}
	if flagJSON {
		return writePlannerJSON(cmd, map[string]any{"version": machinecontract.ManagedScopePolicyV2,
			"status": "ready_for_initial_scan", "rules": results, "baseline_advanced": false, "index_changed": false})
	}
	for _, result := range results {
		if result.Added {
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.rule_batch_added", result.RuleID, result.Pattern))
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.rule_batch_existing", result.RuleID, result.Pattern))
		}
	}
	return nil
}

func validateInitialScopeDirectory(root, pattern string) error {
	first, _, _ := strings.Cut(pattern, "/")
	if _, formal := cognition.FormalAssetOwner(pattern); formal || strings.EqualFold(first, ".aoci") {
		return fmt.Errorf("managed_scope_rule_targets_formal_asset: %s", pattern)
	}
	rel := filepath.FromSlash(pattern)
	if !utf8.ValidString(pattern) || !filepath.IsLocal(rel) {
		return fmt.Errorf("managed_scope_rule_invalid_directory: %q", pattern)
	}
	target := filepath.Join(root, rel)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil || resolved != target {
		return fmt.Errorf("managed_scope_rule_invalid_directory: %q", pattern)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("managed_scope_rule_invalid_directory: %q", pattern)
	}
	return nil
}
