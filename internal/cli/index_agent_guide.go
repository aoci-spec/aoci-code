// `aoci index agent guide --agent <name> --json`把确定性Plan翻译为宿主
// Agent可以执行的阶段指南、命令集合和请求模板。
//
// Guide保持纯读，不读取源码正文、不调用模型、不创建草稿、不修改正式资产。
// 领域构造完成后，输出前才注入当前可执行文件路径、平台调用纪律和协议上限。
//
// JSON输出经writeAgentGuideJSON使用公共Plan视图，增加规范Missing字段别名，
// 但不会改变嵌套Plan的plan_id。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"regexp"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

const agentGuideVersion = 1

// agentStageNameRe bounds the --agent label: an audit label, not authentication.
var agentStageNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// newIndexAgentCmd mounts `aoci index agent`. Since 0.2.0 the group holds only
// guide: the Legacy plan, stage, check, diff, apply, and curation commands left
// with the Legacy authoring surfaces, and Volumes authoring runs through MCP.
func newIndexAgentCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "agent",
		Short: cliMessage("cli.short.agent"),
		Long:  indexAgentLongHelp(),
	}
	command.AddCommand(newIndexAgentGuideCmd())
	return command
}

// Guide modes. The Legacy stage modes (execute, prepare_and_review, observe)
// left with the Legacy authoring surfaces; a Volumes Guide is blocked,
// governance-bound, or complete, and a Legacy layout is always blocked.
const (
	agentGuideModeBlocked  = "blocked"
	agentGuideModeComplete = "complete"
)

type agentGuideCommands struct {
	Guide            string `json:"guide"`
	Onboard          string `json:"onboard,omitempty"`
	Plan             string `json:"plan,omitempty"`
	Scan             string `json:"scan,omitempty"`
	HeaderShow       string `json:"header_show,omitempty"`
	HeaderStage      string `json:"header_stage,omitempty"`
	EntriesStage     string `json:"entries_stage,omitempty"`
	CurationStage    string `json:"curation_stage,omitempty"`
	CurationDiff     string `json:"curation_diff,omitempty"`
	CurationApply    string `json:"curation_apply,omitempty"`
	Check            string `json:"check,omitempty"`
	Diff             string `json:"diff,omitempty"`
	Apply            string `json:"apply,omitempty"`
	Verify           string `json:"verify,omitempty"`
	ScopePreview     string `json:"scope_preview,omitempty"`
	ScopeStatus      string `json:"scope_status,omitempty"`
	ScopeActivate    string `json:"scope_activate,omitempty"`
	ScopeAcknowledge string `json:"scope_acknowledge,omitempty"`
	ScopeBudget      string `json:"scope_budget,omitempty"`
}

type volumeAgentGuide struct {
	Version           int                        `json:"version"`
	Agent             string                     `json:"agent"`
	Mode              string                     `json:"mode"`
	Stage             string                     `json:"stage"`
	Complete          bool                       `json:"complete"`
	NextAction        string                     `json:"next_action"`
	ExecutableTargets int                        `json:"executable_targets"`
	AffectedDomains   []string                   `json:"affected_domains"`
	Findings          []volumegovernance.Finding `json:"findings"`
	Governance        *volumegovernance.Facts    `json:"governance,omitempty"`
	Instructions      []string                   `json:"instructions,omitempty"`
	Commands          agentGuideCommands         `json:"commands"`
	AuthoringMeta     string                     `json:"authoring_meta,omitempty"`
	Stop              *volumeGuideStopFacts      `json:"stop,omitempty"`
	Batch             *volumeGuideBatch          `json:"authoring_batch,omitempty"`
}

type volumeGuideBatch struct {
	TotalTargets         int    `json:"total_targets"`
	MaxEntries           int    `json:"max_entries"`
	TransportBudgetBytes int    `json:"transport_budget_bytes,omitempty"`
	Included             int    `json:"included"`
	Remaining            int    `json:"remaining"`
	ContinuationRequired bool   `json:"continuation_required"`
	NextAction           string `json:"next_action"`
}

type volumeGuideStopFacts struct {
	AffectedAsset  string `json:"affected_asset"`
	Field          string `json:"field"`
	RuleCode       string `json:"rule_code"`
	Expected       string `json:"expected"`
	Actual         string `json:"actual"`
	Cause          string `json:"cause"`
	SafeNextAction string `json:"safe_next_action"`
}

func newIndexAgentGuideCmd() *cobra.Command {
	var agentName string

	command := &cobra.Command{
		Use:   "guide",
		Short: cliMessage("cli.short.agent_guide"),
		Args:  cobra.NoArgs,
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			agentName = strings.TrimSpace(
				agentName,
			)
			if !agentStageNameRe.MatchString(
				agentName,
			) {
				return &ExitError{
					Code: ExitConfig,
					Err:  fmt.Errorf("%s", cliMessage("guide.agent_required")),
				}
			}

			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			cfg, err := config.Load(
				repoRoot,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}
			set, cognitionErr := cognition.Load(repoRoot, cfg.IndexPath)
			if cognitionErr != nil {
				if set != nil && set.LayoutMode == cognition.LayoutVolumesV1 {
					if stop := volumeMetaDictionaryStop(cognitionErr); stop != nil {
						return writeVolumeMetaBlockedGuide(cmd, agentName, stop)
					}
				}
				return &ExitError{Code: ExitConfig, Err: cognitionErr}
			}
			if set.LayoutMode == cognition.LayoutVolumesV1 {
				return writeVolumeAgentGuide(cmd, repoRoot, cfg, set, agentName)
			}
			return writeLegacyReadOnlyGuide(cmd, repoRoot, agentName)
		},
	}

	command.Flags().StringVar(
		&agentName,
		"agent",
		"",
		cliMessage("cli.flag.guide_agent"),
	)

	return command
}

func writeVolumeAgentGuide(cmd *cobra.Command, root string, cfg *config.Config, set *cognition.Set, agent string) error {
	guide, err := buildVolumeAgentGuide(root, cfg, set, agent)
	if err != nil {
		return err
	}
	if flagJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(guide)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), cliMessage("guide.volumes_summary",
		guide.Stage, guide.Complete, guide.NextAction, strings.Join(guide.AffectedDomains, ",")))
	return err
}

// buildVolumeAgentGuide is the one place a Volumes Guide is assembled. The
// CLI writes it and the local status page shows it; one builder, so the two
// can never disagree about what the next step is.
func buildVolumeAgentGuide(root string, cfg *config.Config, set *cognition.Set, agent string) (*volumeAgentGuide, error) {
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		return nil, &ExitError{Code: ExitInvalid, Err: err}
	}
	mode := "governance"
	if facts.Result == volumegovernance.ResultAligned {
		mode = "complete"
	}
	guide := volumeAgentGuide{Version: agentGuideVersion, Agent: agent, Mode: mode,
		Stage: facts.Result, Complete: facts.GovernanceAligned, NextAction: facts.NextRequiredAction,
		ExecutableTargets: executableFindingCount(facts), AffectedDomains: append([]string{}, facts.AffectedDomains...),
		Findings: append([]volumegovernance.Finding{}, facts.Findings...), Governance: facts,
		Commands: agentGuideCommands{Guide: "aoci index agent guide --agent " + agent + " --json", HeaderShow: "aoci index header show", Verify: "aoci verify --json"}}
	if !facts.GovernanceAligned {
		guide.Instructions = append(guide.Instructions, cliMessage("guide.volumes_instruction_runtime_identity"))
	}
	if facts.Result == volumegovernance.ResultBlocked {
		applyVolumeBlockedRemediation(&guide, facts)
	}
	if facts.Result == volumegovernance.ResultAuthoringRequired && len(facts.AffectedDomains) > 0 {
		guide.Commands.Check = "aoci check --json"
		work := volumegovernance.CodeAuthoringWorkFor(set, facts.CodeDrift)
		total := len(work.Targets) +
			facts.DatabaseCognition.Summary.Missing + facts.DatabaseCognition.Summary.Stale + facts.DatabaseCognition.Summary.Unbaselined
		// Guide projects the batch Maintain will actually issue, cut by the
		// transport budget under the team cap through the same selection, so
		// the model sees one number for how much a round asks of it.
		batchLimit := cfg.CodeCognitionBatchLimit()
		included := mcptools.PlanCodeBatchIncluded(root, cfg, set, work)
		databaseTotal := facts.DatabaseCognition.Summary.Missing + facts.DatabaseCognition.Summary.Stale + facts.DatabaseCognition.Summary.Unbaselined
		if capacity := batchLimit - included; databaseTotal > 0 && capacity > 0 {
			objectLimit, _ := cfg.DatabaseCognitionBatchLimits()
			if objectLimit > capacity {
				objectLimit = capacity
			}
			if databaseTotal < objectLimit {
				objectLimit = databaseTotal
			}
			included += objectLimit
		}
		guide.Batch = &volumeGuideBatch{TotalTargets: total, MaxEntries: batchLimit,
			TransportBudgetBytes: cfg.MaintainTransportBudget(),
			Included:             included, Remaining: total - included, ContinuationRequired: total > included,
			NextAction: machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch}
		guide.NextAction = guide.Batch.NextAction
		contract, contractErr := authoringcontract.Build(set.Meta.Raw, facts.AffectedDomains, textassets.ActiveLocale())
		if contractErr != nil {
			return nil, &ExitError{Code: ExitInvalid, Err: contractErr}
		}
		guide.AuthoringMeta = contract.AuthoringMeta
		guide.Instructions = append(guide.Instructions, cliMessage("guide.volumes_instruction_maintain"))
		guide.Instructions = append(guide.Instructions, contract.Instructions...)
	}
	if facts.GovernanceAligned {
		guide.ExecutableTargets = 0
	}
	if err := finalizeVolumeAgentGuideRuntimeContract(&guide); err != nil {
		return nil, &ExitError{Code: ExitInternal, Err: err}
	}
	return &guide, nil
}

// applyVolumeBlockedRemediation turns a blocked Volumes Guide into an
// executable one for the blockers a host can clear itself. Without it a fresh
// Volumes v1 repository that was initialized but never scanned answered Guide
// with bare finding codes (baseline_missing, scope_change_required) and no
// command — the dead end of issue #8, while the Legacy Guide has carried a
// baseline_first stage with exactly this remediation all along. The stage stays
// "blocked"; the stop facts, the scan command, and the instructions are additive.
func applyVolumeBlockedRemediation(guide *volumeAgentGuide, facts *volumegovernance.Facts) {
	baselineMissing, scopeChange, observedPending, budgetExceeded, recoveryPending := false, false, false, false, false
	for _, finding := range facts.Findings {
		switch finding.Code {
		case "baseline_missing":
			baselineMissing = true
		case "recovery_pending":
			recoveryPending = true
		case "scope_change_required":
			scopeChange = true
		case "observed_pending":
			observedPending = true
		case "cognition_budget_exceeded":
			budgetExceeded = true
		}
	}
	locale := textassets.ActiveLocale()
	if baselineMissing {
		instructions := renderGuideLines(locale, textassets.ContractGuideBaselineFirstInstructions, nil)
		guide.Commands.Scan = "aoci " + machinecontract.RemediationCommandScan
		guide.Stop = &volumeGuideStopFacts{
			AffectedAsset: ".aoci/baseline.json", Field: "baseline", RuleCode: "baseline_missing",
			Expected: "governed_baseline_present", Actual: "baseline=absent",
			Cause:          renderGuideText(locale, textassets.ContractGuideBaselineFirstMessage, nil),
			SafeNextAction: strings.Join(instructions, " "),
		}
		guide.Instructions = append(guide.Instructions, instructions...)
		return
	}
	if recoveryPending {
		// The receipt is named and so is the closure that fits its kind; until
		// v0.1.0-rc15 a repository in this state showed recovery_pending with no
		// file and no next step, or nothing at all (#74).
		files := append([]string{}, facts.PendingTransactionFiles...)
		actions := make([]string, 0, len(files))
		for _, file := range files {
			receipt, _ := cognitiontxn.ParseReceiptName(file)
			actions = append(actions, recoveryPendingAction(receipt.Operation, file))
		}
		affected := ".aoci/transactions"
		if len(files) > 0 {
			affected = ".aoci/transactions/" + files[0]
		}
		guide.Stop = &volumeGuideStopFacts{
			AffectedAsset: affected, Field: "transactions", RuleCode: "recovery_pending",
			Expected: "no_pending_receipt", Actual: "pending=" + strings.Join(files, ","),
			Cause:          cliMessage("guide.recovery_pending.cause", strings.Join(files, ", ")),
			SafeNextAction: strings.Join(actions, " "),
		}
		guide.Instructions = append(guide.Instructions, actions...)
		return
	}
	if scopeChange {
		guide.Commands.ScopePreview = "aoci " + machinecontract.RemediationCommandScopeStatus
		guide.Commands.ScopeActivate = "aoci " + machinecontract.RemediationCommandScopeActivate
		guide.Instructions = append(guide.Instructions, cliMessage("guide.volumes_instruction_scope_change"))
	}
	// observe_change_policy defaults to review_required, so an ordinary change to
	// any Observe-role file blocks authoring. The Legacy Plan already routes that
	// state to scope acknowledge; without this the Volumes Guide reported a bare
	// observed_pending finding and no command, which is the same dead end as
	// baseline_missing was before issue #8.
	if observedPending {
		instructions := []string{
			cliMessage("guide.observed_review_instruction_evidence"),
			cliMessage("guide.observed_review_instruction_ack"),
		}
		guide.Commands.ScopeStatus = "aoci " + machinecontract.RemediationCommandScopeStatus
		guide.Commands.ScopeAcknowledge = "aoci " + machinecontract.RemediationCommandScopeAcknowledge
		guide.Stop = &volumeGuideStopFacts{
			AffectedAsset: ".aoci/baseline.json", Field: "observe", RuleCode: "observed_pending",
			Expected: "observe_fingerprints_reviewed_and_acknowledged", Actual: "observe=pending_review",
			Cause:          cliMessage("guide.observed_review_required"),
			SafeNextAction: strings.Join(instructions, " "),
		}
		guide.Instructions = append(guide.Instructions, instructions...)
	}
	// A repository over its Whole-Index budget was handed a bare finding and, at
	// most, `aoci scope status`. The only instruction it received said to compress
	// cognition — which is the wrong remediation for a repository that legitimately
	// grew, and it was the only one offered. The budget is a project-owned policy
	// and `aoci scope budget set` has existed all along; naming it in exactly one
	// documentation line is not handing back a remediation the repository can run.
	if budgetExceeded {
		instructions := []string{
			cliMessage("guide.budget_instruction_scope_levers"),
			cliMessage("guide.budget_instruction_raise"),
		}
		guide.Commands.ScopeStatus = "aoci " + machinecontract.RemediationCommandScopeStatus
		guide.Commands.ScopeBudget = "aoci " + machinecontract.RemediationCommandScopeBudgetSet
		guide.Stop = &volumeGuideStopFacts{
			AffectedAsset: ".aoci/config.json", Field: "whole_index", RuleCode: "cognition_budget_exceeded",
			Expected: "whole_index_within_project_budget",
			Actual: fmt.Sprintf("whole_index_tokens=%d;max_tokens=%d;violations=%d",
				facts.Budget.WholeIndexTokens, facts.Budget.MaxTokens, len(facts.Budget.Violations)),
			Cause: cliMessage("guide.budget_blocked_cause",
				facts.Budget.WholeIndexTokens, facts.Budget.MaxTokens),
			SafeNextAction: strings.Join(instructions, " "),
		}
		guide.Instructions = append(guide.Instructions, instructions...)
	}
}

func volumeMetaDictionaryStop(err error) *volumeGuideStopFacts {
	var validationErr *cognition.ValidationError
	if !errors.As(err, &validationErr) {
		return nil
	}
	for _, finding := range validationErr.Findings {
		if finding.AssetID != cognition.ScopeMeta || !strings.HasPrefix(finding.Code, "meta_tag_dictionary_") {
			continue
		}
		return &volumeGuideStopFacts{
			AffectedAsset: "aoci.meta.txt", Field: "tag_dictionary", RuleCode: finding.Code,
			Expected:       "one_parseable_conflict_free_A_B_C_optional_D_E_dictionary_per_enabled_object_volume",
			Actual:         "asset=meta;rule_code=" + finding.Code + ";detail=" + finding.Message,
			Cause:          cliMessage("mcp.error.meta_tag_dictionary_unavailable", finding.Code, localeSafeCLIDetail(finding.Message)),
			SafeNextAction: cliMessage("mcp.error.meta_tag_dictionary_repair_action"),
		}
	}
	return nil
}

func writeVolumeMetaBlockedGuide(cmd *cobra.Command, agent string, stop *volumeGuideStopFacts) error {
	guide := volumeAgentGuide{
		Version: agentGuideVersion, Agent: agent, Mode: "governance", Stage: volumegovernance.ResultBlocked,
		Complete: false, NextAction: "repair_meta_tag_dictionary", ExecutableTargets: 0,
		AffectedDomains: []string{cognition.ScopeMeta}, Findings: []volumegovernance.Finding{},
		Instructions: []string{stop.SafeNextAction}, Commands: agentGuideCommands{Guide: "aoci index agent guide --agent " + agent + " --json"}, Stop: stop,
	}
	if flagJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(guide)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), stop.SafeNextAction)
	return err
}

// executableFindingCount counts the findings a model or operator can act on.
// Informational findings (skipped sources, curation exclusions, line-ending
// equivalence) describe the repository and demand nothing, so promising them
// as targets would send a model looking for work that does not exist.
func executableFindingCount(facts *volumegovernance.Facts) int {
	count := 0
	for _, finding := range facts.Findings {
		if !volumegovernance.Informational(finding.Code) {
			count++
		}
	}
	return count
}

// recoveryPendingAction names the closure that fits a pending receipt kind.
func recoveryPendingAction(operation, file string) string {
	switch operation {
	case "remove":
		return cliMessage("guide.recovery_pending.remove", file)
	case "entries":
		return cliMessage("guide.recovery_pending.entries", file)
	case "header":
		return cliMessage("guide.recovery_pending.header", file)
	case "bootstrap":
		return cliMessage("guide.recovery_pending.bootstrap", file)
	case "database-bootstrap":
		return cliMessage("guide.recovery_pending.database-bootstrap", file)
	case "migration":
		return cliMessage("guide.recovery_pending.migration", file)
	case "reversal":
		return cliMessage("guide.recovery_pending.reversal", file)
	case "scope":
		return cliMessage("guide.recovery_pending.scope", file)
	}
	return cliMessage("guide.recovery_pending.unknown", file)
}

// writeLegacyReadOnlyGuide is the one Guide a Legacy single-file layout gets
// from 0.2.0 on. Its authoring surfaces are retired, so the Guide is blocked
// with a single next action, the Legacy-to-Volumes migration; verify, check,
// Overview, and search keep reading the layout.
func writeLegacyReadOnlyGuide(cmd *cobra.Command, root, agent string) error {
	guide := buildLegacyReadOnlyGuide(root, agent)
	if err := finalizeVolumeAgentGuideRuntimeContract(guide); err != nil {
		return &ExitError{Code: ExitInternal, Err: err}
	}
	if flagJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(guide)
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), cliMessage("guide.volumes_summary",
		guide.Stage, guide.Complete, guide.NextAction, strings.Join(guide.AffectedDomains, ",")))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), cliMessage("guide.legacy_read_only.message"))
	return err
}

// buildLegacyReadOnlyGuide names the migration as the one next action and,
// when transaction receipts are pending, the closure each receipt needs first:
// the onboard command refuses while any receipt is pending, so the operator
// learns the order here instead of from that refusal.
func buildLegacyReadOnlyGuide(root, agent string) *volumeAgentGuide {
	findings := []volumegovernance.Finding{{Code: machinecontract.StatusLegacyReadOnlyMigrateRequired}}
	instructions := []string{
		cliMessage("guide.legacy_read_only.instruction_migrate"),
		cliMessage("guide.legacy_read_only.instruction_read"),
	}
	if pending, err := cognitiontxn.Pending(root); err == nil && len(pending) > 0 {
		files := make([]string, 0, len(pending))
		for _, receipt := range pending {
			files = append(files, receipt.Filename)
			instructions = append(instructions, recoveryPendingAction(receipt.Operation, receipt.Filename))
		}
		findings = append(findings, volumegovernance.Finding{
			Code:   "recovery_pending",
			Target: strings.Join(files, ","),
			Cause:  cliMessage("guide.recovery_pending.cause", strings.Join(files, ", ")),
		})
	}
	return &volumeAgentGuide{
		Version:         agentGuideVersion,
		Agent:           agent,
		Mode:            agentGuideModeBlocked,
		Stage:           machinecontract.StatusLegacyReadOnlyMigrateRequired,
		Complete:        false,
		NextAction:      machinecontract.MaintainActionMigrateLegacyRepository,
		AffectedDomains: []string{},
		Findings:        findings,
		Instructions:    instructions,
		Commands: agentGuideCommands{
			Guide:   "aoci index agent guide --agent " + agent + " --json",
			Onboard: "aoci " + machinecontract.RemediationCommandCognitionOnboardStart,
			Verify:  "aoci " + machinecontract.RemediationCommandVerify,
			Check:   "aoci " + machinecontract.RemediationCommandAggregateCheck,
		},
	}
}
