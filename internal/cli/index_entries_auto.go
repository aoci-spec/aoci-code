// Entries Auto共享收口内核。
//
// Endpoint Auto与Host-Agent Auto在模型候选已经安全进入标准Entries Draft后，
// 共同复用本文件完成：
//
//	Check → Diff审计 → P-23持久化核对 → 原子Apply → Application审计
//
// 状态分层：
//   - applied：当前批次已经完成原子Apply；
//   - repair_required：候选内容存在可修复错误，正式资产零写入；
//   - stopped：一致性、审计、写入或运行环境错误，无法安全自动继续。
//
// 本文件不调用模型、不生成业务语义、不运行Verify或下一轮Guide，也不承担
// 网络断线和宿主会话恢复。草稿在进入本内核前已经落盘，因此任一后续失败
// 都必须保留当前Run和审计证据，绝不删除草稿或伪造回滚。
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

const (
	entriesAutoStatusApplied        = machinecontract.AutoStatusApplied
	entriesAutoStatusRepairRequired = machinecontract.AutoStatusRepairRequired
	entriesAutoStatusStopped        = machinecontract.AutoStatusStopped

	entriesAutoStepGenerationPlan = "generation_plan"
	entriesAutoStepCheck          = "check"
	entriesAutoStepDiff           = "diff"
	entriesAutoStepApply          = "apply"
	entriesAutoStepAudit          = "audit"
)

var appendEntriesAutoApplication = draft.AppendApplication

// entriesAutoFinalizeError描述自动收口失败的稳定机器信息。
//
// repair_required由Host-Agent解释为可自动恢复状态，不进入Error；共享内核仍
// 返回ExitInvalid，使没有宿主修复循环的Endpoint调用方保持既有失败语义。
type entriesAutoFinalizeError struct {
	ExitCode int    `json:"exit_code"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// entriesAutoFinalizeResult是一次自动收口的稳定结构化结果。
//
// Findings只在Check拒绝或跳过时保存需要模型修正的逐目标机器判定；正常成功
// 路径不重复输出完整条目。
type entriesAutoFinalizeResult struct {
	Status                  string                    `json:"status"`
	FailedStep              string                    `json:"failed_step,omitempty"`
	RunID                   string                    `json:"run_id"`
	DraftHash               string                    `json:"draft_hash,omitempty"`
	Checked                 int                       `json:"checked"`
	Passed                  int                       `json:"passed"`
	Warned                  int                       `json:"warned"`
	Rejected                int                       `json:"rejected"`
	Skipped                 int                       `json:"skipped"`
	DiffReviewed            int                       `json:"diff_reviewed"`
	Attempted               int                       `json:"attempted"`
	Applied                 int                       `json:"applied"`
	Remaining               int                       `json:"remaining"`
	FormalWritesStarted     bool                      `json:"formal_writes_started"`
	FindingCount            int                       `json:"finding_count"`
	Recovered               int                       `json:"recovered"`
	AssetWritten            bool                      `json:"asset_written"`
	AuditRecorded           bool                      `json:"audit_recorded"`
	RejectKinds             string                    `json:"reject_kinds,omitempty"`
	BaselineNote            string                    `json:"baseline_note,omitempty"`
	Recovery                string                    `json:"recovery,omitempty"`
	Findings                []cognition.RepairFinding `json:"findings"`
	PreserveOtherCandidates bool                      `json:"preserve_other_candidates"`
	RetryScope              []string                  `json:"retry_scope"`
	NextAction              string                    `json:"next_action,omitempty"`
	Error                   *entriesAutoFinalizeError `json:"error,omitempty"`
}

func (result entriesAutoFinalizeResult) MarshalJSON() ([]byte, error) {
	if result.Findings == nil {
		result.Findings = []cognition.RepairFinding{}
	}
	if result.RetryScope == nil {
		result.RetryScope = []string{}
	}
	result.FindingCount = len(result.Findings)
	type wireEntriesAutoFinalizeResult entriesAutoFinalizeResult
	return json.Marshal(wireEntriesAutoFinalizeResult(result))
}

// setEntriesAutoFinalizeError把Go错误补入已经形成的终止型业务结果。
//
// repair_required在Host-Agent层被解释为成功返回的修复导航，不调用本函数。
// 静默ExitError表示详细硬拒已经位于Findings中，此时不能把“exit code 2”
// 当作实际错误说明。
func setEntriesAutoFinalizeError(
	result *entriesAutoFinalizeResult,
	err error,
) {
	if result == nil ||
		err == nil {
		return
	}

	exitCode := executionExitCode(
		err,
	)
	message := localeSafeCLIDetail(err.Error())

	if isSilentReportedError(err) {
		switch result.FailedStep {
		case entriesAutoStepCheck:
			message = cliMessage("entries.auto.error.check_rejected")

		case entriesAutoStepDiff:
			message = cliMessage("entries.auto.error.diff_incomplete")

		case entriesAutoStepApply:
			message = cliMessage("entries.auto.error.apply_incomplete")

		default:
			message = cliMessage("entries.auto.error.incomplete")
		}
	}

	result.Error = &entriesAutoFinalizeError{
		ExitCode: exitCode,
		Code: classifyCLIErrorCode(
			err,
			exitCode,
		),
		Message: message,
	}
}

// runEntriesAutoFinalize对一个已经正式保存的Entries Run执行唯一自动收口链。
//
// 候选内容错误返回repair_required结果并保留Run；共享内核同时返回ExitInvalid，
// 由Host-Agent Stage抑制为退出码0并驱动模型自动修复。Endpoint调用方没有宿主
// 修复循环，继续观察既有非零错误，不会把零写入误报为成功完成。
func runEntriesAutoFinalize(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	runID string,
	expectedCount int,
	source string,
	out io.Writer,
) (returned *entriesAutoFinalizeResult, returnedErr error) {
	result := &entriesAutoFinalizeResult{
		Status:     entriesAutoStatusStopped,
		RunID:      runID,
		Findings:   []cognition.RepairFinding{},
		RetryScope: []string{},
	}

	if out == nil {
		out = io.Discard
	}

	if cfg == nil ||
		doc == nil {
		result.FailedStep = entriesAutoStepCheck
		result.Recovery = cliMessage("entries.auto.recovery.config_or_index")

		return result, &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("entries.auto.config_or_index_empty")),
		}
	}

	if source != ledger.SourceAgent &&
		source != ledger.SourceCLIAI {
		result.FailedStep = entriesAutoStepCheck
		result.Recovery = cliMessage("entries.auto.recovery.audit_source")

		return result, &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("entries.auto.audit_source_invalid", source)),
		}
	}
	lease, leaseErr := afs.AcquireEntriesRunLock(repoRoot, runID)
	if leaseErr != nil {
		result.FailedStep = entriesAutoStepCheck
		result.Recovery = cliMessage("entries.auto.recovery.lease_unavailable")
		return result, &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
			"entries.auto.lease_acquire_failed", localeSafeCLIDetail(leaseErr.Error())))}
	}
	defer func() {
		if releaseErr := lease.Release(); releaseErr != nil && returnedErr == nil {
			returned.Status = entriesAutoStatusStopped
			returned.FailedStep = entriesAutoStepAudit
			returned.Recovery = cliMessage("entries.auto.recovery.lease_release")
			returnedErr = &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
				"entries.auto.lease_release_failed", localeSafeCLIDetail(releaseErr.Error())))}
		}
	}()

	manifest, err := validateEntriesAutoManifest(
		repoRoot,
		runID,
		expectedCount,
	)
	if err != nil {
		result.FailedStep = entriesAutoStepCheck
		result.Recovery = cliMessage("entries.auto.recovery.manifest")

		return result, &ExitError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	result.Attempted = len(manifest.Entries)
	result.Remaining = result.Attempted
	if manifest.AppliedAt != "" {
		return finishCompletedEntriesAuto(repoRoot, runID, source, cfg, manifest, result)
	}

	checkResult, err := runEntriesCheckCore(
		repoRoot,
		runID,
		cfg,
		doc,
		out,
		source,
	)
	if err != nil {
		result.FailedStep = entriesAutoStepCheck
		result.Recovery = cliMessage("entries.auto.recovery.check_assets")

		return result, err
	}

	if checkResult == nil ||
		checkResult.Manifest == nil ||
		checkResult.Snapshot == nil {
		result.FailedStep = entriesAutoStepCheck
		result.Recovery = cliMessage("entries.auto.recovery.check_state")

		return result, &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("entries.auto.check_result_incomplete")),
		}
	}

	result.DraftHash = checkResult.Snapshot.Hash
	result.Checked = len(checkResult.Items)
	result.Passed = checkResult.Review.Passed
	result.Warned = checkResult.Review.Warned
	result.Rejected = checkResult.Review.Rejected
	result.Skipped = checkResult.Review.Skipped

	if result.Rejected > 0 ||
		result.Skipped > 0 {
		result.Status = entriesAutoStatusRepairRequired
		result.FailedStep = entriesAutoStepCheck
		result.Findings = entriesAutoRejectedFindings(
			checkResult.Items,
		)
		result.FindingCount = len(result.Findings)
		result.PreserveOtherCandidates = true
		result.RetryScope = mcptools.RepairRetryScope(result.Findings)
		result.NextAction = cliMessage("entries.auto.recovery.repair_findings")
		result.Recovery = cliMessage("entries.auto.recovery.repair_findings")

		fmt.Fprintln(
			out,
			cliMessage("entries.auto.check_repair_required"),
		)

		return result, &ExitError{
			Code: ExitInvalid,
			Msg:  "",
		}
	}

	diffReport, err := appendPersistedEntriesAutoDiffReview(
		repoRoot,
		cfg,
		doc,
		checkResult,
		source,
		out,
	)
	if err != nil {
		result.FailedStep = entriesAutoStepDiff
		result.Recovery = cliMessage("entries.auto.recovery.diff_audit")

		return result, &ExitError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	result.DiffReviewed = diffReport.Reviewed

	if err := validatePersistedAutoReview(
		repoRoot,
		checkResult,
	); err != nil {
		result.FailedStep = entriesAutoStepDiff
		result.Recovery = cliMessage("entries.auto.recovery.review_drift")

		return result, &ExitError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	items, err := atomicItemsFromReviewedSnapshot(
		checkResult,
	)
	if err != nil {
		result.FailedStep = entriesAutoStepApply
		result.Recovery = cliMessage("entries.auto.recovery.apply_input")

		return result, &ExitError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	result.Attempted = len(items)

	applyStart := time.Now()
	expectedIndexSHA256 := ""
	applyManifest, manifestErr := draft.LoadManifest(repoRoot, runID)
	if manifestErr != nil {
		result.FailedStep = entriesAutoStepApply
		result.Recovery = cliMessage("entries.auto.recovery.apply_manifest")
		return result, &ExitError{Code: ExitInternal, Err: manifestErr}
	}
	if applyManifest.GenerationSource == draft.GenerationSourceHostAgent {
		expectedIndexSHA256 = applyManifest.IndexSHA256
	}

	batchOutcome, applyFail :=
		mcptools.ApplyUpdateEntriesAtomicBoundRetained(
			repoRoot,
			items,
			source,
			false,
			expectedIndexSHA256,
		)

	if applyFail != nil {
		rejectKind := autoRejectKind(
			applyFail.Code,
		)

		result.FailedStep = entriesAutoStepApply
		result.Rejected = len(items)
		result.RejectKinds = rejectKind
		result.Recovery = cliMessage("entries.auto.recovery.apply_failure")

		auditErr := appendEntriesAutoApplication(
			repoRoot,
			runID,
			draft.ApplicationRecord{
				DraftHash:   checkResult.Snapshot.Hash,
				PathsCount:  len(items),
				Rejected:    len(items),
				RejectKinds: rejectKind,
			},
			false,
		)

		if auditErr != nil {
			result.RejectKinds = mergeAutoRejectKinds(rejectKind, "application_audit")
			appendEntriesAutoApplyLedger(
				repoRoot, cfg, runID, source, len(items), 0, 0, len(items),
				result.RejectKinds, time.Since(applyStart),
			)
			result.FailedStep = entriesAutoStepAudit
			result.Recovery = cliMessage("entries.auto.recovery.failure_audit")

			return result, &ExitError{
				Code: ExitInternal,
				Err: fmt.Errorf("%s", cliMessage(
					"entries.auto.apply_and_audit_failed",
					applyFail.Code,
					localeSafeCLIDetail(applyFail.Msg),
					localeSafeCLIDetail(auditErr.Error()),
				)),
			}
		}

		appendEntriesAutoApplyLedger(
			repoRoot, cfg, runID, source, len(items), 0, 0, len(items),
			rejectKind, time.Since(applyStart),
		)
		result.AuditRecorded = true
		if applyFail.Repairable {
			result.Status = entriesAutoStatusRepairRequired
			result.Findings = mcptools.LocalizeRepairFindings(applyFail.Findings)
			result.FindingCount = len(result.Findings)
			result.PreserveOtherCandidates = true
			result.RetryScope = mcptools.RepairRetryScope(result.Findings)
			result.NextAction = cliMessage("entries.auto.recovery.repair_findings")
			result.Recovery = result.NextAction
			return result, &ExitError{Code: ExitInvalid, Msg: ""}
		}

		return result, &ExitError{
			Code:        autoFailExitCode(applyFail.Code),
			MachineCode: applyFail.Code,
			Err: formatAutoApplyFail(
				applyFail,
			),
		}
	}

	appliedCount := len(items)
	recoveredCount := 0
	if batchOutcome != nil {
		appliedCount = batchOutcome.AppliedCount
		recoveredCount = batchOutcome.RecoveredCount
	}
	result.AssetWritten = appliedCount > 0
	result.FormalWritesStarted = appliedCount > 0
	result.Applied = appliedCount
	result.Remaining = len(items) - appliedCount
	result.Recovered = recoveredCount

	if batchOutcome != nil {
		result.BaselineNote =
			batchOutcome.BaselineNote

		fmt.Fprint(
			out,
			mcptools.RenderAtomicBatchOutcome(
				batchOutcome,
			),
		)
	}
	if batchOutcome != nil && !batchOutcome.BaselineComplete {
		result.Status = entriesAutoStatusStopped
		result.FailedStep = entriesAutoStepApply
		result.Recovery = cliMessage("entries.auto.recovery.baseline_incomplete")
		auditErr := appendEntriesAutoApplication(
			repoRoot,
			runID,
			draft.ApplicationRecord{
				DraftHash:   checkResult.Snapshot.Hash,
				PathsCount:  len(items),
				Applied:     appliedCount,
				Recovered:   recoveredCount,
				RejectKinds: "baseline_incomplete",
			},
			false,
		)
		result.RejectKinds = "baseline_incomplete"
		if auditErr != nil {
			result.RejectKinds = mergeAutoRejectKinds(
				result.RejectKinds,
				"application_audit",
			)
		}
		appendEntriesAutoApplyLedger(
			repoRoot, cfg, runID, source, len(items), appliedCount, recoveredCount, 0,
			result.RejectKinds, time.Since(applyStart),
		)
		if auditErr != nil {
			result.FailedStep = entriesAutoStepAudit
			result.Recovery = cliMessage("entries.auto.recovery.baseline_audit_failed")
			return result, &ExitError{
				Code: ExitInternal,
				Err: fmt.Errorf("%s", cliMessage(
					"entries.auto.baseline_and_audit_failed",
					localeSafeCLIDetail(result.BaselineNote),
					localeSafeCLIDetail(auditErr.Error()),
				)),
			}
		}
		result.AuditRecorded = true
		return result, &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.auto.baseline_incomplete",
				localeSafeCLIDetail(result.BaselineNote),
			)),
		}
	}
	completedLocalePaths := make([]string, 0, len(items))
	for _, item := range items {
		completedLocalePaths = append(completedLocalePaths, item.Path)
	}
	if err := config.AdvanceLocaleMigration(
		repoRoot,
		false,
		completedLocalePaths,
		nil,
	); err != nil {
		result.FailedStep = entriesAutoStepAudit
		result.Recovery = cliMessage("entries.auto.recovery.locale_advance")
		return result, &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
			"entries.auto.locale_advance_failed", localeSafeCLIDetail(err.Error())))}
	}

	if err := appendEntriesAutoApplication(
		repoRoot,
		runID,
		draft.ApplicationRecord{
			DraftHash:  checkResult.Snapshot.Hash,
			PathsCount: len(items),
			Applied:    appliedCount,
			Recovered:  recoveredCount,
		},
		true,
	); err != nil {
		result.FailedStep = entriesAutoStepAudit
		result.Recovery = cliMessage("entries.auto.recovery.application_audit")
		appendEntriesAutoApplyLedger(
			repoRoot, cfg, runID, source, len(items), appliedCount, recoveredCount, 0,
			"application_audit", time.Since(applyStart),
		)

		return result, &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.auto.application_audit_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	result.AuditRecorded = true
	if cleanupErr := completeEntriesAutoRecovery(
		repoRoot,
		items,
	); cleanupErr != nil {
		result.FailedStep = entriesAutoStepAudit
		result.Recovery = cliMessage("entries.recovery_receipt.cleanup_retry")
		return result, &ExitError{Code: ExitInternal, Err: cleanupErr}
	}

	appendEntriesAutoApplyLedger(
		repoRoot,
		cfg,
		runID,
		source,
		len(items),
		appliedCount,
		recoveredCount,
		0,
		"",
		time.Since(applyStart),
	)

	result.Status = entriesAutoStatusApplied
	result.NextAction = cliMessage("entries.auto.applied", appliedCount, shortDraftHash(checkResult.Snapshot.Hash))

	fmt.Fprint(
		out,
		cliMessage(
			"entries.auto.applied",
			appliedCount,
			shortDraftHash(checkResult.Snapshot.Hash),
		),
	)

	return result, nil
}

func mergeAutoRejectKinds(current, extra string) string {
	if current == "" {
		return extra
	}
	for _, kind := range strings.Split(current, ",") {
		if strings.TrimSpace(kind) == extra {
			return current
		}
	}
	return current + "," + extra
}

// validateEntriesAutoManifest在Check前锁定批次完整性。
func validateEntriesAutoManifest(
	repoRoot,
	runID string,
	expectedCount int,
) (*draft.Manifest, error) {
	if runID == "" {
		return nil, fmt.Errorf("%s", cliMessage("entries.auto.run_id_missing"))
	}

	if expectedCount <= 0 {
		return nil, fmt.Errorf("%s", cliMessage("entries.auto.target_count_invalid", expectedCount))
	}

	manifest, err := draft.LoadManifest(
		repoRoot,
		runID,
	)
	if err != nil {
		return nil, err
	}

	if manifest.Kind != draft.KindEntries {
		return nil, fmt.Errorf("%s", cliMessage("entries.auto.kind_invalid", manifest.Kind))
	}

	if len(manifest.Entries) != expectedCount {
		return nil, fmt.Errorf("%s", cliMessage(
			"entries.auto.batch_incomplete",
			expectedCount,
			len(manifest.Entries),
		))
	}

	for _, status := range manifest.Entries {
		if status.Status != "drafted" &&
			status.Status != "warned" {
			return nil, fmt.Errorf("%s", cliMessage(
				"entries.auto.generation_status_invalid",
				status.Path,
				status.Status,
			))
		}
	}

	return manifest, nil
}

// entriesAutoRejectedFindings把Check拒绝项投影到共享的候选Finding Schema。
func entriesAutoRejectedFindings(
	items []entriesCheckItem,
) []cognition.RepairFinding {
	result := []cognition.RepairFinding{}

	for itemIndex, item := range items {
		if item.Outcome == "rejected" ||
			item.Outcome == "skipped" {
			findings := append(append([]entriesFinding{}, item.Errors...), item.Warnings...)
			for _, finding := range findings {
				cause := finding.Cause
				if cause == "" {
					cause = finding.Message
				}
				ruleCode := finding.RuleCode
				if ruleCode == "" {
					ruleCode = finding.Code
				}
				field := finding.Field
				if field == "" {
					field = "entry"
				}
				result = append(result, cognition.RepairFinding{
					CandidateIndex:          itemIndex + 1,
					Path:                    item.Path,
					CanonicalObjectIdentity: "code:" + item.Path,
					Domain:                  "code",
					Field:                   field,
					RuleCode:                ruleCode,
					Expected:                finding.Expected,
					Actual:                  finding.Actual,
					Cause:                   cause,
					SafeRepairAction:        cliMessage("entries.auto.recovery.repair_findings"),
					Code:                    finding.Code,
					Message:                 cause,
				})
			}
		}
	}

	return mcptools.LocalizeRepairFindings(result)
}

// appendEntriesAutoApplyLedger按真实调用来源记录共享Auto Apply。
func appendEntriesAutoApplyLedger(
	repoRoot string,
	cfg *config.Config,
	runID,
	source string,
	pathsCount,
	applied,
	recovered,
	rejected int,
	rejectKinds string,
	duration time.Duration,
) {
	result := ledger.ResultOK
	if strings.Contains(rejectKinds, "baseline_incomplete") {
		result = ledger.ResultError
	} else if strings.Contains(rejectKinds, "application_audit") {
		result = ledger.ResultError
	} else if rejected > 0 {
		result = ledger.ResultRejected
		if rejectKinds == "conflict" {
			result = ledger.ResultConflict
		}
	}
	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:             "entries_apply",
			Source:         source,
			PathsCount:     pathsCount,
			DurationMs:     duration.Milliseconds(),
			DraftRunID:     runID,
			AppliedCount:   applied,
			RecoveredCount: recovered,
			RejectedCount:  rejected,
			RejectKinds:    rejectKinds,
			Result:         result,
		},
	)
}
