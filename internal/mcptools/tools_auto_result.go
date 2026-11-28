// Compact machine results and performance facts of the Auto path.
package mcptools

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

const (
	autoStatusApplied        = machinecontract.AutoStatusApplied
	autoStatusRepairRequired = machinecontract.AutoStatusRepairRequired
	autoStatusStopped        = machinecontract.AutoStatusStopped
)

type autoMetrics struct {
	DeterministicMs   int64 `json:"deterministic_ms"`
	AOCIToolCalls     int   `json:"aoci_tool_calls"`
	ShellAOCICalls    int   `json:"shell_aoci_calls"`
	OverviewReads     int   `json:"overview_reads"`
	LocalRecalls      int   `json:"local_recalls"`
	SemanticFiles     int   `json:"semantic_files"`
	FormatOnlyFiles   int   `json:"format_only_files"`
	DuplicateApplies  int   `json:"duplicate_applies"`
	RepeatedMaintains int   `json:"repeated_maintains"`
}

type autoCandidate struct {
	Path                      string                      `json:"path"`
	Kind                      string                      `json:"kind"`
	ExistingEntry             string                      `json:"existing_entry,omitempty"`
	CurationRole              string                      `json:"curation_role,omitempty"`
	CurationReason            string                      `json:"curation_reason,omitempty"`
	ProfileReason             string                      `json:"profile_reason,omitempty"`
	SourceSHA256              string                      `json:"source_sha256,omitempty"`
	ScopeRole                 string                      `json:"scope_role,omitempty"`
	ObserveEvidenceChanged    bool                        `json:"observe_evidence_changed,omitempty"`
	RTargetTokens             int                         `json:"r_target_tokens,omitempty"`
	RMaxTokens                int                         `json:"r_max_tokens,omitempty"`
	STargetTokens             int                         `json:"s_target_tokens,omitempty"`
	SMaxTokens                int                         `json:"s_max_tokens,omitempty"`
	WholeIndexTokens          int                         `json:"whole_index_tokens,omitempty"`
	ProjectedWholeIndexTokens int                         `json:"projected_whole_index_tokens,omitempty"`
	RemainingTokens           int                         `json:"remaining_tokens,omitempty"`
	ProjectionPending         bool                        `json:"projection_pending,omitempty"`
	RFieldBands               []cognitionbudget.FieldBand `json:"r_field_bands,omitempty"`
	SFieldBands               []cognitionbudget.FieldBand `json:"s_field_bands,omitempty"`
}

type autoManagedGovernance struct {
	ScopePolicyIdentity string                  `json:"scope_policy_identity"`
	IndexCount          int                     `json:"index_count"`
	ObserveCount        int                     `json:"observe_count"`
	ExcludeCount        int                     `json:"exclude_count"`
	ObservedNew         []string                `json:"observed_new"`
	ObservedChanged     []string                `json:"observed_changed"`
	ObservedRemoved     []string                `json:"observed_removed"`
	CognitionBudget     *cognitionbudget.Report `json:"cognition_budget,omitempty"`
}

// machineFindings preserves legacy string findings for unrelated Maintain and
// stopped results while emitting the shared object schema whenever candidate
// repair evidence is present.
type machineFindings []cognition.RepairFinding

func (findings machineFindings) MarshalJSON() ([]byte, error) {
	detailed := false
	for _, finding := range findings {
		if finding.CandidateIndex != 0 || finding.Path != "" || finding.CanonicalObjectIdentity != "" ||
			finding.Domain != "" || finding.Field != "" || finding.Expected != "" ||
			finding.Actual != "" || finding.SafeRepairAction != "" {
			detailed = true
			break
		}
	}
	if detailed {
		return json.Marshal([]cognition.RepairFinding(findings))
	}
	legacy := make([]string, 0, len(findings))
	for _, finding := range findings {
		cause := finding.Cause
		if cause == "" {
			cause = finding.Message
		}
		legacy = append(legacy, cause)
	}
	return json.Marshal(legacy)
}

func (findings *machineFindings) UnmarshalJSON(data []byte) error {
	var detailed []cognition.RepairFinding
	if err := json.Unmarshal(data, &detailed); err == nil {
		*findings = machineFindings(detailed)
		return nil
	}
	var legacy []string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	result := make(machineFindings, 0, len(legacy))
	for _, cause := range legacy {
		result = append(result, genericMachineFinding("machine_finding", cause))
	}
	*findings = result
	return nil
}

// applyFindingsTruncation describes the complete list behind the sample.
type applyFindingsTruncation struct {
	Limit     int            `json:"limit"`
	ByteLimit int            `json:"byte_limit"`
	Total     int            `json:"total"`
	ByKind    map[string]int `json:"by_kind"`
}

func renderBoundedApplyResult(result autoResult, findings []string, budget int) string {
	limit := machinecontract.MaintainTransportListLimit
	sample := genericMachineFindings(findings[:min(len(findings), limit)])
	if len(findings) <= limit {
		result.Findings = sample
		if wire := renderAutoResult(result); len(wire) <= budget || len(findings) == 0 {
			return wire
		}
	}
	byKind := make(map[string]int)
	for _, finding := range findings {
		// Alignment findings use "kind: detail" or a bare machine code.
		kind, _, _ := strings.Cut(finding, ": ")
		byKind[kind]++
	}
	result.FindingsTruncated = &applyFindingsTruncation{Limit: limit, ByteLimit: budget, Total: len(findings), ByKind: byKind}
	// Like Maintain, measure the actual serialized tool text with its newline.
	// With the totals fixed, every extra finding only increases the wire size.
	firstOverBudget := sort.Search(len(sample)+1, func(count int) bool {
		result.Findings = sample[:count]
		return len(renderAutoResult(result)) > budget
	})
	result.Findings = sample[:max(0, firstOverBudget-1)]
	return renderAutoResult(result)
}

type autoResult struct {
	Version                 int      `json:"version"`
	Status                  string   `json:"status"`
	Aligned                 bool     `json:"aligned"`
	RefreshStatus           string   `json:"refresh_status,omitempty"`
	RefreshReasons          []string `json:"refresh_reasons,omitempty"`
	Attempted               int      `json:"attempted"`
	Applied                 int      `json:"applied"`
	Remaining               int      `json:"remaining"`
	omitRemaining           bool
	FormalWritesStarted     bool                         `json:"formal_writes_started"`
	FindingCount            int                          `json:"finding_count"`
	Receipt                 cognitionReceipt             `json:"cognition_receipt"`
	Metrics                 autoMetrics                  `json:"metrics"`
	Audit                   *autoAudit                   `json:"audit,omitempty"`
	FormatOnlyApplied       []string                     `json:"format_only_applied,omitempty"`
	Candidates              []autoCandidate              `json:"candidates,omitempty"`
	Findings                machineFindings              `json:"findings"`
	FindingsTruncated       *applyFindingsTruncation     `json:"findings_truncated,omitempty"`
	PreserveOtherCandidates bool                         `json:"preserve_other_candidates"`
	RetryScope              []string                     `json:"retry_scope"`
	NextAction              string                       `json:"next_action"`
	NextCommands            []string                     `json:"next_commands,omitempty"`
	TerminalProof           *TerminalProof               `json:"terminal_proof,omitempty"`
	RootOverview            *rootOverviewInvite          `json:"root_overview,omitempty"`
	RootOverviewResult      *rootOverviewResult          `json:"root_overview_result,omitempty"`
	ObserveReview           *batchObserveReview          `json:"observe_review,omitempty"`
	ObserveReviewCurrent    *observeReviewListing        `json:"observe_review_current,omitempty"`
	HostDialogRequest       *toolCallRequest             `json:"host_dialog_request,omitempty"`
	ManagedGovernance       *autoManagedGovernance       `json:"managed_governance,omitempty"`
	CodePlan                *codebatch.Plan              `json:"code_plan,omitempty"`
	Stop                    *GlobalStopFacts             `json:"stop,omitempty"`
	Optimization            *cognitionOptimizationStatus `json:"optimization,omitempty"`
}

func applyAutoRefreshOutcome(result *autoResult, session *cognitionRefreshSession) {
	if result == nil || session == nil {
		return
	}
	receipt, status, reasons := session.autoRefreshOutcome(result.Receipt, result.Aligned)
	result.Receipt = receipt
	result.RefreshStatus = status
	result.RefreshReasons = reasons
	if result.Aligned && status == machinecontract.RefreshStatusReadyForOverview {
		result.NextAction = refreshNextAction(status)
	}
}

func renderAutoResult(result autoResult) string {
	if result.Findings == nil {
		result.Findings = machineFindings{}
	}
	if result.RetryScope == nil {
		result.RetryScope = []string{}
	}
	result.FindingCount = len(result.Findings)
	if result.FindingsTruncated != nil {
		result.FindingCount = result.FindingsTruncated.Total
	}
	var wire any = result
	if result.omitRemaining {
		// An unavailable snapshot has no count. Omit just that response's
		// field so ordinary replies still distinguish a measured zero.
		wire = struct {
			autoResult
			Remaining *int `json:"remaining,omitempty"`
		}{autoResult: result}
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return `{"version":1,"status":"` + autoStatusStopped +
			`","aligned":false,"next_action":` +
			mustJSONText(writeMessage("maintain.auto.marshal_failed")) + `}`
	}
	return string(data) + "\n"
}

func genericMachineFinding(ruleCode, cause string) cognition.RepairFinding {
	return cognition.RepairFinding{
		RuleCode: ruleCode, Cause: cause, Message: cause, Code: ruleCode,
	}
}

func genericMachineFindings(causes []string) machineFindings {
	result := make(machineFindings, 0, len(causes))
	for _, cause := range causes {
		result = append(result, genericMachineFinding("machine_finding", cause))
	}
	return result
}

func mustJSONText(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// autoApplyNextAction generates the model-visible explanation from the same structured terminal state.
// A successful request whose entries were already in place (a duplicate submission or an unchanged resubmission) is not a formal write, and Applied=0 must be stated explicitly;
// the host must not infer this call's write count from Attempted or the first-application count.
func autoApplyNextAction(
	volumes bool,
	aligned bool,
	applied,
	duplicateApplies int,
) string {
	if !aligned {
		return mcpContract(
			textassets.ContractMaintainActionApplyRemaining,
		)
	}
	if volumes {
		if applied == 0 && duplicateApplies > 0 {
			return mcpContract(
				textassets.ContractMaintainActionApplyDuplicateFinalProof,
			)
		}
		return mcpContract(
			textassets.ContractMaintainActionApplyFinalProof,
		)
	}
	if applied == 0 && duplicateApplies > 0 {
		return mcpContract(
			textassets.ContractMaintainActionApplyDuplicate,
		)
	}
	return mcpContract(
		textassets.ContractMaintainActionAligned,
	)
}

func elapsedMilliseconds(start time.Time) int64 {
	elapsed := time.Since(start).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	return elapsed
}
