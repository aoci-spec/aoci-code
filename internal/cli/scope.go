package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/spf13/cobra"
)

type scopePolicyStatus struct {
	Version               string                      `json:"version"`
	Stage                 string                      `json:"stage"`
	DesiredPolicyIdentity string                      `json:"desired_policy_identity"`
	ActivePolicyIdentity  string                      `json:"active_policy_identity,omitempty"`
	PolicyIdentityAligned bool                        `json:"policy_identity_aligned"`
	DesiredBudgetIdentity string                      `json:"desired_budget_identity"`
	ActiveBudgetIdentity  string                      `json:"active_budget_identity,omitempty"`
	BudgetIdentityAligned bool                        `json:"budget_identity_aligned"`
	Policy                managedscope.Policy         `json:"policy"`
	Evaluation            *managedscope.Evaluation    `json:"evaluation"`
	Drift                 *baseline.DetectResult      `json:"drift,omitempty"`
	Budget                *cognitionbudget.Report     `json:"budget,omitempty"`
	IndexCount            int                         `json:"index_count"`
	ObserveCount          int                         `json:"observe_count"`
	ExcludeCount          int                         `json:"exclude_count"`
	ObservedPendingReview int                         `json:"observed_pending_review"`
	AuthoringTargets      int                         `json:"authoring_targets"`
	RequiresHumanReview   int                         `json:"requires_human_review"`
	AppliedManagedScope   *baseline.ManagedScopeState `json:"applied_managed_scope,omitempty"`
	ObserveChangePolicy   string                      `json:"observe_change_policy"`
}

func init() {
	registerCommand(newScopeCmd())
}

func newScopeCmd() *cobra.Command {
	command := &cobra.Command{Use: "scope", Short: cliMessage("cli.short.scope")}
	command.AddCommand(newScopeShowCmd(), newScopeExplainCmd(), newScopeRuleCmd(), newScopeSafetyCmd(), newScopePlanCmd(), newScopePreviewCmd(),
		newScopeBudgetCmd(), newScopeObservePolicyCmd(), newScopeApprovalModeCmd(), newScopeApproveCmd(), newScopeAuthorizeCmd(), newScopeApplyCmd(), newScopeStatusCmd(),
		newScopeResumeCmd(), newScopeRollbackCmd(), newScopeAcknowledgeCmd())
	return command
}

func newScopeApprovalModeCmd() *cobra.Command {
	return &cobra.Command{Use: "approval-mode <inherit|auto|review>", Short: cliMessage("cli.short.scope_approval_mode"),
		Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			err = config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				policy.ApprovalMode = args[0]
				return nil
			})
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeRuleMutation(cmd, "approval-mode", "updated")
		}}
}

func newScopeBudgetCmd() *cobra.Command {
	command := &cobra.Command{Use: "budget", Short: cliMessage("cli.short.scope_budget")}
	command.AddCommand(&cobra.Command{Use: "show", Short: cliMessage("cli.short.scope_budget_show"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		cfg, err := config.LoadReadOnly(root)
		if err != nil {
			return managedScopeExitError(err)
		}
		return writePlannerJSON(cmd, cfg.EffectiveCognitionBudget())
	}}, newScopeBudgetSetCmd())
	return command
}

func newScopeBudgetSetCmd() *cobra.Command {
	var mode, policyFile string
	var target, warning, maximum int
	command := &cobra.Command{Use: "set", Short: cliMessage("cli.short.scope_budget_set"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		err = config.MutateCognitionBudget(root, func(policy *cognitionbudget.Policy) error {
			if policyFile != "" {
				data, readErr := readPlannerInput(policyFile)
				if readErr != nil {
					return readErr
				}
				decoder := json.NewDecoder(strings.NewReader(string(data)))
				decoder.DisallowUnknownFields()
				if decodeErr := decoder.Decode(policy); decodeErr != nil {
					return fmt.Errorf("cognition_budget_policy_invalid: %w", decodeErr)
				}
				return nil
			}
			if mode != "" {
				policy.Mode = mode
			}
			if cmd.Flags().Changed("target-tokens") {
				policy.WholeIndex.TargetTokens = target
			}
			if cmd.Flags().Changed("warning-tokens") {
				policy.WholeIndex.WarningTokens = warning
			}
			if cmd.Flags().Changed("max-tokens") {
				policy.WholeIndex.MaxTokens = maximum
			}
			return nil
		})
		if err != nil {
			return managedScopeExitError(err)
		}
		return writeScopeRuleMutation(cmd, "cognition-budget", "updated")
	}}
	command.Flags().StringVar(&mode, "mode", "", cliMessage("scope.flag.budget_mode"))
	command.Flags().IntVar(&target, "target-tokens", 0, cliMessage("scope.flag.target_tokens"))
	command.Flags().IntVar(&warning, "warning-tokens", 0, cliMessage("scope.flag.warning_tokens"))
	command.Flags().IntVar(&maximum, "max-tokens", 0, cliMessage("scope.flag.max_tokens"))
	command.Flags().StringVar(&policyFile, "policy-file", "", cliMessage("scope.flag.policy_file"))
	return command
}

func newScopeObservePolicyCmd() *cobra.Command {
	return &cobra.Command{Use: "observe-policy <review_required|informational>", Short: cliMessage("cli.short.scope_observe_policy"),
		Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			err = config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				policy.ObserveChangePolicy = args[0]
				return nil
			})
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeRuleMutation(cmd, "observe-change-policy", "updated")
		}}
}

func newScopeShowCmd() *cobra.Command {
	return &cobra.Command{Use: "show", Short: cliMessage("cli.short.scope_show"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		status, err := buildScopePolicyStatus(root, false)
		if err != nil {
			return managedScopeExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, status)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.show_summary", status.Policy.Profile, status.DesiredPolicyIdentity,
			status.ActivePolicyIdentity, status.PolicyIdentityAligned, status.IndexCount, status.ObserveCount, status.ExcludeCount))
		return nil
	}}
}

func newScopeExplainCmd() *cobra.Command {
	return &cobra.Command{Use: "explain <path>", Short: cliMessage("cli.short.scope_explain"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			cfg, err := config.LoadReadOnly(root)
			if err != nil {
				return managedScopeExitError(err)
			}
			rel, err := afs.NormalizeRelPath(args[0])
			if err != nil {
				return managedScopeExitError(fmt.Errorf("managed_scope_path_invalid"))
			}
			curationExclude, curationErr := managedScopeCurationExclusions(root, cfg)
			if curationErr != nil {
				return managedScopeExitError(curationErr)
			}
			evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
				WalkOptions: cfg.WalkOptions(), CurationExclude: curationExclude})
			if err != nil {
				return managedScopeExitError(err)
			}
			item, found := findScopeEvaluation(evaluation, rel)
			if !found {
				if category, source := afs.BuiltInSafetyCategory(rel); category != "" {
					item = managedscope.PathEvaluation{Version: machinecontract.ManagedScopeEvaluationV2, Path: rel,
						Role: machinecontract.ScopeRoleExclude, RuleSource: machinecontract.ScopeRuleSafety,
						RulePriority: 700, SafetyStatus: category, GitStatus: "future_or_absent", ReadsContent: false,
						EntersWholeIndex: false, EntersObserveFingerprint: false, Reason: source + ":" + category}
				} else {
					curated := containsScopePath(cfg.CurationExclude, rel)
					item = managedscope.EvaluatePathWithCase(cfg.EffectiveManagedScope(), rel, false, false, curated, evaluation.CaseSensitive)
					item.GitStatus = "future_or_absent"
				}
			}
			if flagJSON {
				return writePlannerJSON(cmd, item)
			}
			matched := "-"
			if item.MatchedRule != nil {
				matched = item.MatchedRule.RuleID
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.explain_summary", item.Path, item.Role, matched,
				item.RuleSource, item.RulePriority, item.SafetyStatus, item.ReadsContent, item.EntersWholeIndex,
				item.EntersObserveFingerprint, item.Reason))
			return nil
		}}
}

func newScopeRuleCmd() *cobra.Command {
	command := &cobra.Command{Use: "rule", Short: cliMessage("cli.short.scope_rule")}
	command.AddCommand(newScopeRuleListCmd(), newScopeRuleAddCmd(), newScopeRuleUpdateCmd(), newScopeRuleRemoveCmd(), newScopeRuleResetCmd())
	return command
}

func newScopeRuleListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: cliMessage("cli.short.scope_rule_list"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		cfg, err := config.LoadReadOnly(root)
		if err != nil {
			return managedScopeExitError(err)
		}
		policy := cfg.EffectiveManagedScope()
		if flagJSON {
			return writePlannerJSON(cmd, policy.Rules)
		}
		for _, rule := range policy.Rules {
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.rule_line", rule.RuleID, rule.Action, rule.PatternKind,
				rule.Pattern, rule.Source, rule.Order, rule.Enabled, rule.DecisionBasis, rule.Reason))
		}
		return nil
	}}
}

type scopeRuleFlags struct {
	action, pattern, kind, reason, decisionBasis, createdBy string
	order                                                   int
	enabled                                                 bool
}

func bindScopeRuleFlags(command *cobra.Command, flags *scopeRuleFlags, update bool) {
	command.Flags().StringVar(&flags.action, "action", "", cliMessage("scope.flag.action"))
	command.Flags().StringVar(&flags.pattern, "pattern", "", cliMessage("scope.flag.pattern"))
	command.Flags().StringVar(&flags.kind, "pattern-kind", "glob", cliMessage("scope.flag.pattern_kind"))
	command.Flags().StringVar(&flags.reason, "reason", "", cliMessage("scope.flag.reason"))
	command.Flags().StringVar(&flags.decisionBasis, "decision-basis", "", cliMessage("scope.flag.decision_basis"))
	command.Flags().StringVar(&flags.createdBy, "created-by", "human", cliMessage("scope.flag.created_by"))
	command.Flags().IntVar(&flags.order, "order", 0, cliMessage("scope.flag.order"))
	command.Flags().BoolVar(&flags.enabled, "enabled", true, cliMessage("scope.flag.enabled"))
	if !update {
		_ = command.MarkFlagRequired("action")
		_ = command.MarkFlagRequired("pattern")
		_ = command.MarkFlagRequired("reason")
	}
}

func newScopeRuleAddCmd() *cobra.Command {
	var flags scopeRuleFlags
	command := &cobra.Command{Use: "add <rule-id>", Short: cliMessage("cli.short.scope_rule_add"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			rule := managedscope.Rule{RuleID: args[0], Action: flags.action, Pattern: flags.pattern, PatternKind: flags.kind,
				Reason: flags.reason, DecisionBasis: flags.decisionBasis, Source: machinecontract.ScopeRuleUser, CreatedBy: flags.createdBy, Order: flags.order, Enabled: flags.enabled}
			err = config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				for _, existing := range policy.Rules {
					if existing.RuleID == rule.RuleID {
						return fmt.Errorf("managed_scope_rule_exists")
					}
				}
				policy.Rules = append(policy.Rules, rule)
				return nil
			})
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeRuleMutation(cmd, args[0], "added")
		}}
	bindScopeRuleFlags(command, &flags, false)
	return command
}

func newScopeRuleUpdateCmd() *cobra.Command {
	var flags scopeRuleFlags
	command := &cobra.Command{Use: "update <rule-id>", Short: cliMessage("cli.short.scope_rule_update"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			err = config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				for index := range policy.Rules {
					rule := &policy.Rules[index]
					if rule.RuleID != args[0] {
						continue
					}
					if cmd.Flags().Changed("action") {
						rule.Action = flags.action
					}
					if cmd.Flags().Changed("pattern") {
						rule.Pattern = flags.pattern
					}
					if cmd.Flags().Changed("pattern-kind") {
						rule.PatternKind = flags.kind
					}
					if cmd.Flags().Changed("reason") {
						rule.Reason = flags.reason
					}
					if cmd.Flags().Changed("decision-basis") {
						rule.DecisionBasis = flags.decisionBasis
					}
					if cmd.Flags().Changed("created-by") {
						rule.CreatedBy = flags.createdBy
					}
					if cmd.Flags().Changed("order") {
						rule.Order = flags.order
					}
					if cmd.Flags().Changed("enabled") {
						rule.Enabled = flags.enabled
					}
					return nil
				}
				return fmt.Errorf("managed_scope_rule_not_found")
			})
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeRuleMutation(cmd, args[0], "updated")
		}}
	bindScopeRuleFlags(command, &flags, true)
	return command
}

func newScopeRuleRemoveCmd() *cobra.Command {
	return &cobra.Command{Use: "remove <rule-id>", Short: cliMessage("cli.short.scope_rule_remove"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			err = config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				for index, rule := range policy.Rules {
					if rule.RuleID == args[0] {
						policy.Rules = append(policy.Rules[:index], policy.Rules[index+1:]...)
						return nil
					}
				}
				return fmt.Errorf("managed_scope_rule_not_found")
			})
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeRuleMutation(cmd, args[0], "removed")
		}}
}

func newScopeRuleResetCmd() *cobra.Command {
	return &cobra.Command{Use: "reset <production|full|custom>", Short: cliMessage("cli.short.scope_rule_reset"), Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			err = config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				*policy = managedscope.DefaultPolicy(args[0])
				return nil
			})
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeRuleMutation(cmd, args[0], "reset")
		}}
}

func writeScopeRuleMutation(cmd *cobra.Command, ruleID, operation string) error {
	result := map[string]any{"version": machinecontract.ManagedScopePolicyV2, "status": "scope_refresh_required",
		"operation": operation, "rule_id": ruleID, "baseline_advanced": false, "index_changed": false}
	if flagJSON {
		return writePlannerJSON(cmd, result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.rule_mutation", ruleID, operation))
	return nil
}

func newScopePlanCmd() *cobra.Command    { return newScopePlannerCmd(false) }
func newScopePreviewCmd() *cobra.Command { return newScopePlannerCmd(true) }

func newScopePlannerCmd(previewMode bool) *cobra.Command {
	var candidateFile, safetyApprovalFile, preparedAt string
	use, short := "plan", cliMessage("cli.short.scope_plan")
	if previewMode {
		use, short = "preview", cliMessage("cli.short.scope_preview")
	}
	command := &cobra.Command{Use: use, Short: short, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		if candidateFile != "" || safetyApprovalFile != "" {
			candidates := &scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
				Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{}}
			if candidateFile != "" {
				data, readErr := readPlannerInput(candidateFile)
				if readErr != nil {
					return managedScopeExitError(readErr)
				}
				candidates, err = scopechange.DecodeCandidateSet(data)
				if err != nil {
					return managedScopeExitError(err)
				}
			}
			if safetyApprovalFile != "" {
				data, readErr := os.ReadFile(safetyApprovalFile)
				if readErr != nil {
					return managedScopeExitError(readErr)
				}
				candidates.SafetyApproval, err = scopechange.DecodeSafetyApproval(data)
				if err != nil {
					return managedScopeExitError(err)
				}
			}
			if preparedAt == "" {
				preparedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
			}
			preview, buildErr := scopechange.Build(root, preparedAt, *candidates)
			if buildErr != nil {
				return managedScopeExitError(buildErr)
			}
			if previewMode {
				return writePlannerJSON(cmd, preview)
			}
			return writePlannerJSON(cmd, preview.Plan)
		}
		status, err := buildScopePolicyStatus(root, true)
		if err != nil {
			return managedScopeExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, status)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.preview_summary", status.Stage, status.IndexCount,
			status.ObserveCount, status.ExcludeCount, status.AuthoringTargets, status.RequiresHumanReview))
		return nil
	}}
	command.Flags().StringVar(&candidateFile, "candidate-file", "", cliMessage("scope.flag.candidate_file"))
	command.Flags().StringVar(&safetyApprovalFile, "safety-approval-file", "", cliMessage("scope.flag.safety_approval_file"))
	command.Flags().StringVar(&preparedAt, "prepared-at", "", cliMessage("scope.flag.prepared_at"))
	return command
}

func newScopeSafetyCmd() *cobra.Command {
	command := &cobra.Command{Use: "safety", Short: cliMessage("cli.short.scope_safety")}
	command.AddCommand(
		&cobra.Command{Use: "list", Short: cliMessage("cli.short.scope_safety_list"), RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			cfg, err := config.LoadReadOnly(root)
			if err != nil {
				return managedScopeExitError(err)
			}
			if flagJSON {
				return writePlannerJSON(cmd, cfg.SafeInventoryHighRiskOptIn)
			}
			for _, path := range cfg.SafeInventoryHighRiskOptIn {
				fmt.Fprintln(cmd.OutOrStdout(), path)
			}
			return nil
		}},
		newScopeSafetyMutationCmd(true), newScopeSafetyMutationCmd(false), newScopeSafetyApproveCmd(),
	)
	return command
}

func newScopeSafetyMutationCmd(add bool) *cobra.Command {
	use, key := "remove <path>", "removed"
	short := cliMessage("cli.short.scope_safety_remove")
	if add {
		use, key = "opt-in <path>", "added"
		short = cliMessage("cli.short.scope_safety_opt_in")
	}
	return &cobra.Command{Use: use, Short: short,
		Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			err = config.MutateSafeInventoryHighRiskOptIn(root, func(paths []string) ([]string, error) {
				if add {
					return append(paths, args[0]), nil
				}
				result := []string{}
				found := false
				for _, path := range paths {
					if path == args[0] {
						found = true
						continue
					}
					result = append(result, path)
				}
				if !found {
					return nil, fmt.Errorf("safe_inventory_high_risk_opt_in_not_found")
				}
				return result, nil
			})
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeRuleMutation(cmd, args[0], key)
		}}
}

func newScopeSafetyApproveCmd() *cobra.Command {
	var actor string
	command := &cobra.Command{Use: "approve", Short: cliMessage("cli.short.scope_safety_approve"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		cfg, err := config.LoadReadOnly(root)
		if err != nil {
			return managedScopeExitError(err)
		}
		if len(cfg.SafeInventoryHighRiskOptIn) == 0 {
			return managedScopeExitError(fmt.Errorf("managed_scope_high_risk_opt_in_empty"))
		}
		curationExclude, err := managedScopeCurationExclusions(root, cfg)
		if err != nil {
			return managedScopeExitError(err)
		}
		evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
			WalkOptions: cfg.WalkOptions(), CurationExclude: curationExclude})
		if err != nil {
			return managedScopeExitError(err)
		}
		phrase := "APPROVE HIGH RISK READ " + evaluation.PolicyIdentity
		exact := hostInteractionCommand("scope", "safety", "approve", "--actor", actor, "--json")
		if err := requireHumanPhrase(cmd, phrase, cliMessage("scope.safety_approval_prompt", strings.Join(cfg.SafeInventoryHighRiskOptIn, ","), phrase),
			exact, evaluation.PolicyIdentity, fmt.Sprintf("exact_paths=%d", len(cfg.SafeInventoryHighRiskOptIn)), "high risk content read"); err != nil {
			return managedScopeExitError(err)
		}
		approval, err := scopechange.NewSafetyApproval(evaluation.PolicyIdentity, cfg.SafeInventoryHighRiskOptIn, actor,
			time.Now().UTC().Truncate(time.Second).Format(time.RFC3339))
		if err != nil {
			return managedScopeExitError(err)
		}
		return writePlannerJSON(cmd, approval)
	}}
	command.Flags().StringVar(&actor, "actor", "", cliMessage("cognition.bootstrap.flag.actor"))
	_ = command.MarkFlagRequired("actor")
	return command
}

func newScopeStatusCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "status", Short: cliMessage("cli.short.scope_status"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		if transaction != "" {
			status, inspectErr := scopechange.Inspect(root, transaction)
			if inspectErr != nil {
				return managedScopeExitError(inspectErr)
			}
			return writePlannerJSON(cmd, status)
		}
		status, err := buildScopePolicyStatus(root, true)
		if err != nil {
			return managedScopeExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, status)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.status_summary", status.Stage, status.PolicyIdentityAligned,
			status.ObservedPendingReview, status.DesiredPolicyIdentity))
		return nil
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	return command
}

func newScopeApproveCmd() *cobra.Command {
	var previewFile, actor string
	command := &cobra.Command{Use: "approve", Short: cliMessage("cli.short.scope_approve"), RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := readPlannerInput(previewFile)
		if err != nil {
			return managedScopeExitError(err)
		}
		preview, err := scopechange.DecodePreview(data)
		if err != nil {
			return managedScopeExitError(err)
		}
		if !preview.Plan.InteractionRequired {
			return managedScopeExitError(fmt.Errorf("managed_scope_approval_not_required"))
		}
		exact := hostInteractionCommand("scope", "approve", "--preview-file", previewFile, "--actor", actor, "--json")
		effect := managedScopeApprovalEffect(preview)
		if err := requireHumanPhrase(cmd, preview.Plan.ConfirmationPhrase,
			cliMessage("scope.approval_prompt", effect, preview.Plan.PlanID, preview.Plan.ConfirmationPhrase), exact,
			preview.Plan.PlanID, fmt.Sprintf("entry_removals=%d", len(preview.Plan.EntryRemoves)), "managed scope apply"); err != nil {
			return managedScopeExitError(err)
		}
		approval, err := scopechange.NewApproval(preview, actor, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339))
		if err != nil {
			return managedScopeExitError(err)
		}
		return writePlannerJSON(cmd, approval)
	}}
	command.Flags().StringVar(&previewFile, "preview-file", "", cliMessage("scope.flag.preview_file"))
	command.Flags().StringVar(&actor, "actor", "", cliMessage("cognition.bootstrap.flag.actor"))
	_ = command.MarkFlagRequired("preview-file")
	_ = command.MarkFlagRequired("actor")
	return command
}

func newScopeAuthorizeCmd() *cobra.Command {
	var previewFile string
	command := &cobra.Command{Use: "authorize", Short: cliMessage("cli.short.scope_authorize"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		data, err := readPlannerInput(previewFile)
		if err != nil {
			return managedScopeExitError(err)
		}
		preview, err := scopechange.DecodePreview(data)
		if err != nil {
			return managedScopeExitError(err)
		}
		receipt, err := scopechange.NewPolicyBoundApproval(root, preview, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339))
		if err != nil {
			return managedScopeExitError(err)
		}
		return writePlannerJSON(cmd, receipt)
	}}
	command.Flags().StringVar(&previewFile, "preview-file", "", cliMessage("scope.flag.preview_file"))
	_ = command.MarkFlagRequired("preview-file")
	return command
}

func newScopeApplyCmd() *cobra.Command {
	var previewFile, approvalFile, authorizationFile string
	command := &cobra.Command{Use: "apply", Short: cliMessage("cli.short.scope_apply"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		data, err := readPlannerInput(previewFile)
		if err != nil {
			return managedScopeExitError(err)
		}
		preview, err := scopechange.DecodePreview(data)
		if err != nil {
			return managedScopeExitError(err)
		}
		var approval *scopechange.Approval
		var policyBound *scopechange.PolicyBoundApproval
		if approvalFile != "" {
			approvalBytes, readErr := os.ReadFile(approvalFile)
			if readErr != nil {
				return managedScopeExitError(readErr)
			}
			approval, err = scopechange.DecodeApproval(approvalBytes)
			if err != nil {
				return managedScopeExitError(err)
			}
		}
		if authorizationFile != "" {
			authorizationBytes, readErr := os.ReadFile(authorizationFile)
			if readErr != nil {
				return managedScopeExitError(readErr)
			}
			policyBound, err = scopechange.DecodePolicyBoundApproval(authorizationBytes)
			if err != nil {
				return managedScopeExitError(err)
			}
		}
		result, err := scopechange.ApplyAuthorized(root, preview, approval, policyBound)
		if err != nil {
			return managedScopeExitError(err)
		}
		return writeScopeChangeResult(cmd, result)
	}}
	command.Flags().StringVar(&previewFile, "preview-file", "", cliMessage("scope.flag.preview_file"))
	command.Flags().StringVar(&approvalFile, "approval-file", "", cliMessage("scope.flag.approval_file"))
	command.Flags().StringVar(&authorizationFile, "authorization-file", "", cliMessage("scope.flag.authorization_file"))
	_ = command.MarkFlagRequired("preview-file")
	return command
}

func newScopeResumeCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "resume", Short: cliMessage("cli.short.scope_resume"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		result, err := scopechange.Resume(root, transaction)
		if err != nil {
			return managedScopeExitError(err)
		}
		return writeScopeChangeResult(cmd, result)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func newScopeRollbackCmd() *cobra.Command {
	var transaction string
	command := &cobra.Command{Use: "rollback", Short: cliMessage("cli.short.scope_rollback"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		result, err := scopechange.Rollback(root, transaction)
		if err != nil {
			return managedScopeExitError(err)
		}
		return writeScopeChangeResult(cmd, result)
	}}
	command.Flags().StringVar(&transaction, "transaction", "", cliMessage("cognition.bootstrap.flag.transaction"))
	_ = command.MarkFlagRequired("transaction")
	return command
}

func newScopeAcknowledgeCmd() *cobra.Command {
	var reviewer string
	command := &cobra.Command{Use: "acknowledge", Short: cliMessage("cli.short.scope_acknowledge"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return managedScopeExitError(err)
		}
		status, err := buildScopePolicyStatus(root, true)
		if err != nil {
			return managedScopeExitError(err)
		}
		if !status.PolicyIdentityAligned || status.Drift == nil || status.ObservedPendingReview == 0 {
			return managedScopeExitError(fmt.Errorf("observed_evidence_review_not_pending"))
		}
		paths := append([]string{}, status.Drift.ObservedNew...)
		paths = append(paths, status.Drift.ObservedChanged...)
		paths = append(paths, status.Drift.ObservedRemoved...)
		sort.Strings(paths)
		candidates := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
			Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{},
			ObserveReview: &scopechange.ObserveReview{Paths: paths, ReviewStatus: scopechange.ReviewStatusReviewed, Reviewer: reviewer}}
		preparedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		preview, err := scopechange.Build(root, preparedAt, candidates)
		if err != nil {
			return managedScopeExitError(err)
		}
		result, err := scopechange.Apply(root, preview, nil)
		if err != nil {
			return managedScopeExitError(err)
		}
		return writeScopeChangeResult(cmd, result)
	}}
	command.Flags().StringVar(&reviewer, "reviewed-by", "", cliMessage("scope.flag.reviewed_by"))
	_ = command.MarkFlagRequired("reviewed-by")
	return command
}

func writeScopeChangeResult(cmd *cobra.Command, result *scopechange.Result) error {
	if flagJSON {
		return writePlannerJSON(cmd, result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.apply_summary", result.Status, result.TransactionID,
		result.IndexSHA256, result.BaselineSHA256))
	return nil
}

func buildScopePolicyStatus(root string, includeDrift bool) (*scopePolicyStatus, error) {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, err
	}
	policy := cfg.EffectiveManagedScope()
	curationExclude, err := managedScopeCurationExclusions(root, cfg)
	if err != nil {
		return nil, err
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions(), CurationExclude: curationExclude})
	if err != nil {
		return nil, err
	}
	desired := evaluation.PolicyIdentity
	desiredBudget, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		return nil, err
	}
	status := &scopePolicyStatus{Version: machinecontract.ManagedScopeStatusV2, Stage: "aligned", DesiredPolicyIdentity: desired,
		DesiredBudgetIdentity: desiredBudget,
		Policy:                policy, Evaluation: evaluation, IndexCount: evaluation.IndexCount, ObserveCount: evaluation.ObserveCount,
		ExcludeCount: evaluation.ExcludeCount, RequiresHumanReview: evaluation.RequiredHumanReview,
		ObserveChangePolicy: policy.ObserveChangePolicy}
	currentBaseline, exists, err := baseline.Load(root)
	if err != nil {
		return nil, err
	}
	if exists {
		status.AppliedManagedScope = currentBaseline.ManagedScope
		if currentBaseline.ManagedScope == nil {
			status.ActivePolicyIdentity, _ = managedscope.Identity(managedscope.LegacyPolicy())
		} else {
			status.ActivePolicyIdentity = currentBaseline.ManagedScope.PolicyIdentity
			status.ActiveBudgetIdentity = currentBaseline.ManagedScope.BudgetPolicyIdentity
		}
	}
	status.PolicyIdentityAligned = exists && status.ActivePolicyIdentity == desired
	status.BudgetIdentityAligned = exists && (status.ActiveBudgetIdentity == "" || status.ActiveBudgetIdentity == desiredBudget)
	if !status.PolicyIdentityAligned || !status.BudgetIdentityAligned {
		status.Stage = "scope_change_required"
	}
	raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(cfg.IndexPath)))
	if readErr == nil {
		status.Budget, err = cognitionbudget.Build(root, raw, cfg.EffectiveCognitionBudget())
		if err != nil {
			return nil, err
		}
	}
	if !includeDrift || !status.PolicyIdentityAligned || !status.BudgetIdentityAligned || !exists || readErr != nil {
		return status, nil
	}
	highRiskApproved := currentBaseline.ManagedScope != nil && currentBaseline.ManagedScope.HighRiskApprovalDigest != ""
	snapshot, err := managedscope.Snapshot(root, evaluation, managedscope.SnapshotOptions{HighRiskContentApproved: highRiskApproved})
	if err != nil {
		return nil, err
	}
	document, _ := index.Parse(string(raw))
	index.ResolveRelPaths(document, root)
	driftBaseline := currentBaseline
	if layout, layoutErr := cognition.DetectLayout(raw); layoutErr != nil {
		return nil, layoutErr
	} else if layout == cognition.LayoutVolumesV1 {
		set, loadErr := cognition.Load(root, cfg.IndexPath)
		if loadErr != nil {
			return nil, loadErr
		}
		if _, guardErr := scopechange.FormalCognitionBaselineGuards(root, cfg.IndexPath, currentBaseline); guardErr != nil {
			return nil, guardErr
		}
		code := set.Volumes[cognition.ScopeCode]
		if code == nil || code.State != cognition.AssetPresent || code.Document == nil {
			return nil, fmt.Errorf("managed_scope_code_volume_unavailable")
		}
		document = code.Document
		filteredSnapshot := make(map[string]baseline.Fingerprint, len(snapshot))
		for path, fingerprint := range snapshot {
			filteredSnapshot[path] = fingerprint
		}
		filteredBaseline := *currentBaseline
		filteredBaseline.Files = make(map[string]baseline.Fingerprint, len(currentBaseline.Files))
		for path, fingerprint := range currentBaseline.Files {
			filteredBaseline.Files[path] = fingerprint
		}
		formalPaths := map[string]bool{cfg.IndexPath: true}
		for _, id := range set.DeclaredOrder {
			if asset := set.Volumes[id]; asset != nil {
				formalPaths[asset.Descriptor.Path] = true
			}
		}
		for path := range formalPaths {
			delete(filteredSnapshot, path)
			delete(filteredBaseline.Files, path)
		}
		snapshot, driftBaseline = filteredSnapshot, &filteredBaseline
	}
	status.Drift = baseline.DetectManagedScope(root, document, driftBaseline, snapshot,
		afs.WalkOptions{HighRiskOptIn: cfg.SafeInventoryHighRiskOptIn}, cfg.LineEndingTolerance)
	status.ObservedPendingReview = len(status.Drift.ObservedNew) + len(status.Drift.ObservedChanged) + len(status.Drift.ObservedRemoved)
	status.AuthoringTargets = len(status.Drift.Missing)
	if status.ObservedPendingReview > 0 && policy.ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired {
		status.Stage = "observed_evidence_review_required"
	} else if status.Budget != nil && status.Budget.WholeIndexTokens > status.Budget.MaxTokens {
		status.Stage = "cognition_compression_required"
	} else if status.Budget != nil && len(status.Budget.Violations) > 0 {
		status.Stage = "budget_exceeded"
	} else if status.AuthoringTargets > 0 || len(status.Drift.Stale) > 0 {
		status.Stage = "authoring_required"
	}
	return status, nil
}

func findScopeEvaluation(evaluation *managedscope.Evaluation, rel string) (managedscope.PathEvaluation, bool) {
	for _, group := range [][]managedscope.PathEvaluation{evaluation.Index, evaluation.Observe, evaluation.Exclude} {
		index := sort.Search(len(group), func(i int) bool { return group[i].Path >= rel })
		if index < len(group) && group[index].Path == rel {
			return group[index], true
		}
	}
	return managedscope.PathEvaluation{}, false
}

func containsScopePath(values []string, rel string) bool {
	for _, value := range values {
		if strings.TrimPrefix(filepath.ToSlash(value), "./") == rel {
			return true
		}
	}
	return false
}

func managedScopeCurationExclusions(root string, cfg *config.Config) ([]string, error) {
	result := append([]string{}, cfg.CurationExclude...)
	document, _, _, err := curation.Load(root)
	if err != nil {
		return nil, err
	}
	value, exists, err := baseline.Load(root)
	if err != nil {
		return nil, err
	}
	if exists {
		for _, decision := range document.Decisions {
			fingerprint, found := value.Files[decision.Path]
			if found && decision.Decision == curation.DecisionExclude && fingerprint.SHA256 == decision.SourceSHA256 {
				result = append(result, decision.Path)
			}
		}
	}
	sort.Strings(result)
	deduplicated := result[:0]
	for _, path := range result {
		if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != path {
			deduplicated = append(deduplicated, path)
		}
	}
	return deduplicated, nil
}

func managedScopeExitError(err error) error {
	return &ExitError{Code: ExitInvalid, MachineCode: "managed_scope_invalid", Msg: cliMessage("scope.error", err.Error())}
}

// managedScopeApprovalEffect renders what a human is actually being asked to
// approve.
//
// A 64-hex confirmation phrase proves the approval is bound to one exact plan,
// but it tells the approver nothing about the consequence. A confirmation that
// cannot be understood is not a decision, so the prompt leads with the effects
// that matter — destroyed cognition, lost coverage, a weakened posture — and
// says so plainly when a change carries none of them.
func managedScopeApprovalEffect(preview *scopechange.Preview) string {
	plan := preview.Plan
	effects := []string{}
	if count := len(plan.EntryRemoves); count > 0 {
		effects = append(effects, cliMessage("scope.approval_effect.entry_removes", count))
	}
	if count := len(plan.CoverageReductions); count > 0 {
		effects = append(effects, cliMessage("scope.approval_effect.coverage", count))
	}
	if plan.Risk.ApprovalPolicyRelaxation {
		// Only the destination is named. Preview.Baseline is the postimage, so it
		// already carries the new posture and reading the origin from it would
		// print the same mode twice; the preimage mode is not part of the public
		// plan shape, and widening that shape to decorate one prompt is not worth
		// it when the approver already knows what they are leaving.
		effects = append(effects, cliMessage("scope.approval_effect.posture",
			plan.AuthorizationPolicy.EffectiveMode))
	}
	if plan.Risk.BudgetRelaxation {
		effects = append(effects, cliMessage("scope.approval_effect.budget"))
	}
	if plan.Risk.HighRiskOptIn {
		effects = append(effects, cliMessage("scope.approval_effect.high_risk"))
	}
	if len(effects) == 0 {
		return cliMessage("scope.approval_effect.none")
	}
	return strings.Join(effects, "; ")
}
