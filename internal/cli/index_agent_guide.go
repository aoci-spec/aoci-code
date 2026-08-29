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
	"strings"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

const agentGuideVersion = 1

type agentGuideCommands struct {
	Guide            string `json:"guide"`
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
	ScopeAcknowledge string `json:"scope_acknowledge,omitempty"`
	ScopeBudget      string `json:"scope_budget,omitempty"`
}

type agentGuideBatch struct {
	MaxEntries int               `json:"max_entries"`
	Included   int               `json:"included"`
	Remaining  int               `json:"remaining"`
	Targets    []agentPlanTarget `json:"targets"`
}

type agentGuideCurationBatch struct {
	MaxDecisions int                       `json:"max_decisions"`
	Included     int                       `json:"included"`
	Remaining    int                       `json:"remaining"`
	Targets      []agentPlanCurationTarget `json:"targets"`
}

type agentGuideNextAction struct {
	Action                   string            `json:"action"`
	Command                  string            `json:"command,omitempty"`
	ToolName                 string            `json:"tool_name,omitempty"`
	RequiredParameters       map[string]string `json:"required_parameters"`
	Agent                    string            `json:"agent"`
	SchemaVersion            string            `json:"schema_version"`
	RequestFile              string            `json:"request_file,omitempty"`
	ExpectedPreimage         string            `json:"expected_preimage"`
	PlanOrRunIdentity        string            `json:"plan_or_run_identity"`
	TTYRequired              bool              `json:"tty_required"`
	AutomaticallyRetryable   bool              `json:"automatically_retryable"`
	TransportCorrectionLimit int               `json:"transport_schema_correction_limit"`
	SuccessNextAction        string            `json:"success_next_action"`
}

type agentGuide struct {
	Version int        `json:"version"`
	Agent   string     `json:"agent"`
	Mode    string     `json:"mode"`
	Plan    *agentPlan `json:"plan"`

	Complete         bool `json:"complete"`
	ApprovalRequired bool `json:"approval_required"`
	StopBeforeApply  bool `json:"stop_before_apply"`

	Message            string               `json:"message"`
	Instructions       []string             `json:"instructions"`
	Commands           agentGuideCommands   `json:"commands"`
	NextActionContract agentGuideNextAction `json:"next_action_contract"`

	HeaderStageRequest   *agentHeaderStageRequest   `json:"header_stage_request,omitempty"`
	EntriesStageRequest  *agentStageRequest         `json:"entries_stage_request,omitempty"`
	CurationStageRequest *agentCurationStageRequest `json:"curation_stage_request,omitempty"`

	Batch         *agentGuideBatch         `json:"batch,omitempty"`
	CurationBatch *agentGuideCurationBatch `json:"curation_batch,omitempty"`
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
			if err := guardPendingEntriesForAgent(repoRoot); err != nil {
				return &ExitError{Code: ExitInvalid, Err: err}
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

			doc, indexPath, err := loadIndexForCLI(
				cmd,
				repoRoot,
				cfg,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			plan, err := buildAgentPlan(
				repoRoot,
				cfg,
				doc,
				indexPath,
			)
			if err != nil {
				var buildErr *agentPlanBuildError

				if errors.As(
					err,
					&buildErr,
				) {
					return &ExitError{
						Code: buildErr.Code,
						Err:  buildErr.Err,
					}
				}

				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}

			guide, err := buildAgentGuide(
				agentName,
				plan,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}

			if err := finalizeAgentGuideRuntimeContract(guide); err != nil {
				return &ExitError{Code: ExitInternal, Err: err}
			}

			if flagJSON {
				return writeAgentGuideJSON(
					cmd.OutOrStdout(),
					guide,
				)
			}

			fmt.Fprint(
				cmd.OutOrStdout(),
				renderAgentGuide(guide),
			)
			return nil
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
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		return &ExitError{Code: ExitInvalid, Err: err}
	}
	mode := "governance"
	if facts.Result == volumegovernance.ResultAligned {
		mode = "complete"
	}
	guide := volumeAgentGuide{Version: agentGuideVersion, Agent: agent, Mode: mode,
		Stage: facts.Result, Complete: facts.GovernanceAligned, NextAction: facts.NextRequiredAction,
		ExecutableTargets: len(facts.Findings), AffectedDomains: append([]string{}, facts.AffectedDomains...),
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
		total := len(facts.CodeDrift.Missing) + len(facts.CodeDrift.Stale) + len(facts.CodeDrift.Unbaselined) +
			facts.DatabaseCognition.Summary.Missing + facts.DatabaseCognition.Summary.Stale + facts.DatabaseCognition.Summary.Unbaselined
		// Guide projects the same team batch size Maintain will plan with, so
		// the model sees one number for how much a round asks of it.
		batchLimit := cfg.CodeCognitionBatchLimit()
		included := total
		if included > batchLimit {
			included = batchLimit
		}
		guide.Batch = &volumeGuideBatch{TotalTargets: total, MaxEntries: batchLimit,
			Included: included, Remaining: total - included, ContinuationRequired: total > included,
			NextAction: machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch}
		guide.NextAction = guide.Batch.NextAction
		contract, contractErr := authoringcontract.Build(set.Meta.Raw, facts.AffectedDomains, textassets.ActiveLocale())
		if contractErr != nil {
			return &ExitError{Code: ExitInvalid, Err: contractErr}
		}
		guide.AuthoringMeta = contract.AuthoringMeta
		guide.Instructions = append(guide.Instructions, cliMessage("guide.volumes_instruction_maintain"))
		guide.Instructions = append(guide.Instructions, contract.Instructions...)
	}
	if facts.GovernanceAligned {
		guide.ExecutableTargets = 0
	}
	if err := finalizeVolumeAgentGuideRuntimeContract(&guide); err != nil {
		return &ExitError{Code: ExitInternal, Err: err}
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

// applyVolumeBlockedRemediation turns a blocked Volumes Guide into an
// executable one for the blockers a host can clear itself. Without it a fresh
// Volumes v1 repository that was initialized but never scanned answered Guide
// with bare finding codes (baseline_missing, scope_change_required) and no
// command — the dead end of issue #8, while the Legacy Guide has carried a
// baseline_first stage with exactly this remediation all along. The stage stays
// "blocked"; the stop facts, the scan command, and the instructions are additive.
func applyVolumeBlockedRemediation(guide *volumeAgentGuide, facts *volumegovernance.Facts) {
	baselineMissing, scopeChange, observedPending, budgetExceeded := false, false, false, false
	for _, finding := range facts.Findings {
		switch finding.Code {
		case "baseline_missing":
			baselineMissing = true
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
		guide.Commands.Scan = "aoci scan"
		guide.Stop = &volumeGuideStopFacts{
			AffectedAsset: ".aoci/baseline.json", Field: "baseline", RuleCode: "baseline_missing",
			Expected: "governed_baseline_present", Actual: "baseline=absent",
			Cause:          renderGuideText(locale, textassets.ContractGuideBaselineFirstMessage, nil),
			SafeNextAction: strings.Join(instructions, " "),
		}
		guide.Instructions = append(guide.Instructions, instructions...)
		return
	}
	if scopeChange {
		guide.Commands.ScopePreview = "aoci scope status --json"
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
		guide.Commands.ScopeStatus = "aoci scope status --json"
		guide.Commands.ScopeAcknowledge = "aoci scope acknowledge --reviewed-by {agent} --json"
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
		guide.Commands.ScopeStatus = "aoci scope status --json"
		guide.Commands.ScopeBudget = "aoci scope budget set --max-tokens {tokens}"
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

func buildAgentGuide(
	agentName string,
	plan *agentPlan,
) (*agentGuide, error) {
	agentName = strings.TrimSpace(
		agentName,
	)
	if !agentStageNameRe.MatchString(
		agentName,
	) {
		return nil, fmt.Errorf("%s", cliMessage("guide.agent_invalid", agentName))
	}
	if plan == nil {
		return nil, fmt.Errorf("%s", cliMessage("guide.plan_nil"))
	}
	policy, err := resolveAgentAutomationPolicy(
		plan.AutomationMode,
	)
	if err != nil {
		return nil, err
	}
	assetIDs, err := requiredGuideAssetIDs(plan, policy)
	if err != nil {
		return nil, err
	}
	if err := validateGuideAssets(assetIDs...); err != nil {
		return nil, err
	}

	guide := newAgentGuideBase(
		agentName,
		plan,
	)

	switch plan.Stage {
	case agentPlanStageBaselineRequired:
		buildBaselineGuide(
			guide,
			plan,
		)

	case agentPlanStageHeaderRequired:
		buildHeaderGuide(
			guide,
			plan,
			policy,
		)

	case agentPlanStageIndexReviewRequired:
		guide.Mode = agentGuideModeBlocked
		guide.ApprovalRequired = true
		guide.Message = indexReviewBlockedMessage()
		guide.Instructions = append(
			guide.Instructions,
			indexReviewBlockedInstructions()...,
		)

	case agentPlanStageEntriesRequired:
		buildEntriesGuide(
			guide,
			plan,
			policy,
		)

	case agentPlanStageCurationRequired:
		buildCurationGuide(
			guide,
			plan,
			policy,
		)

	case agentPlanStageOrphanReview:
		guide.Mode = agentGuideModeBlocked
		guide.ApprovalRequired = true
		guide.Message = orphanReviewBlockedMessage()
		guide.Instructions = append(
			guide.Instructions,
			orphanReviewBlockedInstructions()...,
		)

	case agentPlanStageScopeChangeRequired:
		guide.Mode = agentGuideModeBlocked
		guide.Message = cliMessage("guide.scope_change_required")
		guide.Commands.ScopePreview = "aoci scope preview --candidate-file {candidate_file} --json"
		guide.Commands.ScopeStatus = "aoci scope status --json"
		guide.Instructions = append(guide.Instructions,
			cliMessage("guide.scope_change_instruction_status"),
			cliMessage("guide.scope_change_instruction_preview"))

	case agentPlanStageObservedReview:
		guide.Mode = agentGuideModeBlocked
		guide.Message = cliMessage("guide.observed_review_required")
		guide.Commands.ScopeStatus = "aoci scope status --json"
		guide.Commands.ScopeAcknowledge = "aoci scope acknowledge --reviewed-by {agent} --json"
		guide.Instructions = append(guide.Instructions,
			cliMessage("guide.observed_review_instruction_evidence"),
			cliMessage("guide.observed_review_instruction_ack"))

	case agentPlanStageCompressionRequired, agentPlanStageBudgetExceeded:
		if plan.Governance == nil || plan.Governance.CognitionBudget == nil {
			return nil, fmt.Errorf("%s", cliMessage("guide.governance_budget_missing"))
		}
		guide.Mode = agentGuideModeBlocked
		guide.Message = cliMessage("guide.compression_required", plan.Governance.CognitionBudget.WholeIndexTokens,
			plan.Governance.CognitionBudget.MaxTokens, len(plan.Governance.CognitionBudget.Violations))
		guide.Commands.ScopeStatus = "aoci scope status --json"
		guide.Instructions = append(guide.Instructions,
			cliMessage("guide.compression_instruction_model"),
			cliMessage("guide.compression_instruction_gate"))

	case agentPlanStageAligned:
		buildAlignedGuide(
			guide,
			plan,
		)

	default:
		return nil, fmt.Errorf("%s", cliMessage("guide.stage_unknown", plan.Stage))
	}

	return guide, nil
}

func renderAgentGuide(
	guide *agentGuide,
) string {
	if guide == nil ||
		guide.Plan == nil {
		return ""
	}

	var builder strings.Builder

	fmt.Fprintf(
		&builder,
		"AOCI agent guide v%d\n",
		guide.Version,
	)
	fmt.Fprintf(
		&builder,
		"agent: %s | automation.mode: %s | mode: %s | stage: %s\n",
		guide.Agent,
		guide.Plan.AutomationMode,
		guide.Mode,
		guide.Plan.Stage,
	)
	fmt.Fprintf(
		&builder,
		"plan_id: %s\n",
		guide.Plan.PlanID,
	)
	fmt.Fprint(&builder, cliMessage(
		"guide.render.missing_summary",
		guide.Plan.Summary.Missing,
		guide.Plan.Summary.ActionableNew,
		guide.Plan.Summary.IncludedMissing,
		guide.Plan.Summary.CurationExcluded,
		guide.Plan.Summary.SkippedMissing,
		guide.Plan.Summary.PendingCuration,
	))
	fmt.Fprintf(
		&builder,
		"approval_required: %t | stop_before_apply: %t | complete: %t\n",
		guide.ApprovalRequired,
		guide.StopBeforeApply,
		guide.Complete,
	)

	builder.WriteString(cliMessage(
		"guide.render.conclusion",
		localeSafeCLIDetail(guide.Message),
	))

	for position, instruction := range guide.Instructions {
		fmt.Fprintf(
			&builder,
			"%d. %s\n",
			position+1,
			instruction,
		)
	}

	if guide.Batch != nil {
		fmt.Fprint(&builder, cliMessage(
			"guide.render.entries_batch",
			guide.Batch.Included,
			guide.Batch.Remaining,
			guide.Batch.MaxEntries,
		))
	}

	if guide.CurationBatch != nil {
		fmt.Fprint(&builder, cliMessage(
			"guide.render.curation_batch",
			guide.CurationBatch.Included,
			guide.CurationBatch.Remaining,
			guide.CurationBatch.MaxDecisions,
		))
	}

	builder.WriteString(cliMessage("guide.render.json_hint"))

	return builder.String()
}
