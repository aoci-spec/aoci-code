// aoci_maintain高层Auto维护调度。
//
// 事实来源:
//   - 四态来自baseline.DetectWith;
//   - Missing的Raw/Actionable/Included/Excluded/Skipped/Pending/StaleDecision
//     全部来自internal/curation.BuildClassification;
//   - report仅是派发泄压阀，不是文件级exclude决策;
//   - 已有条目的Stale与Orphan仍遵循条目优先规则，不受Missing策展过滤。
//
// 换行宽容:
//   - 漂移判定消费团队line_ending_tolerance;
//   - 纯CRLF/LF表示差异不进入Stale，不派发更新任务;
//   - 团队显式严格模式和真实内容变化仍正常派发;
//   - 本工具不修改原始SHA、Baseline、正式索引或Stage绑定。
//
// 本工具只会自动前移已由规范格式器证明的format-only Baseline；真实语义变化
// 仍只派发给宿主模型，索引候选由aoci_update_entry原子批量治理。
package mcptools

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
	"github.com/aoci-spec/aoci-code/internal/runmanifest"
	"github.com/aoci-spec/aoci-code/textassets"
)

const maintainBatchLimit = 200

var maintainRuntimeContractIDs = []textassets.ID{
	textassets.ContractMaintainDictionaryUnparseable,
	textassets.ContractMaintainDictionaryMissing,
	textassets.ContractMaintainActionRepositoryFailure,
	textassets.ContractMaintainActionDictionaryUnparseable,
	textassets.ContractMaintainActionDictionaryMissing,
	textassets.ContractMaintainActionSnapshotFailure,
	textassets.ContractMaintainActionCurationInvalid,
	textassets.ContractMaintainActionFormatOnlyFailure,
	textassets.ContractMaintainActionBlocked,
	textassets.ContractMaintainActionCandidates,
	textassets.ContractMaintainActionAligned,
}

type maintainTask struct {
	rel            string
	kind           string
	oldLine        string
	curationRole   string
	curationReason string
	profileReason  string
	sourceSHA256   string
}

// maintainCurationState仅保留Auto终态判定需要的策展停点事实。
type maintainCurationState struct {
	pendingMissing   int
	pending          []curation.PendingCandidate
	technicalSkipped []curation.SkippedMissing
}

// maintainToolDescription返回aoci_maintain的稳定MCP公开说明。
func maintainToolDescription() string {
	return mcpContract(textassets.ContractMaintainToolDescription)
}

// maintainDictionaryUnparseableMessage返回字典疑似存在但无法解析时的恢复说明。
func maintainDictionaryUnparseableMessage() string {
	return mcpContract(textassets.ContractMaintainDictionaryUnparseable)
}

// maintainDictionaryMissingMessage返回索引头尚未建立可用字典时的恢复说明。
func maintainDictionaryMissingMessage() string {
	return mcpContract(textassets.ContractMaintainDictionaryMissing)
}

func loadReportedPaths(
	path string,
) map[string]bool {
	result := map[string]bool{}

	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}

	for _, line := range strings.Split(
		string(data),
		"\n",
	) {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		var record struct {
			Path string `json:"path"`
		}

		if err := json.Unmarshal(
			[]byte(line),
			&record,
		); err != nil ||
			record.Path == "" {
			continue
		}

		result[record.Path] = true
	}

	return result
}

func handleMaintain(root string) *mcp.CallToolResult {
	return handleMaintainWithVersion(root, "unknown")
}

func handleMaintainWithVersion(
	root,
	mcpServiceVersion string,
	refreshSessions ...*cognitionRefreshSession,
) *mcp.CallToolResult {
	var refreshSession *cognitionRefreshSession
	if len(refreshSessions) > 0 {
		refreshSession = refreshSessions[0]
	}
	if fail := validateWriteMessages(requiredMaintainWriteMessages); fail != nil {
		return failResult(fail)
	}
	if err := validateMCPContracts(maintainRuntimeContractIDs...); err != nil {
		return errResult(
			errInternal,
			localeSafeWriteDetail(err.Error()),
			writeMessage("entry.write.hint.contract_assets"),
		)
	}
	start := time.Now()

	repository, fail := loadRepoCtx(root)
	if fail != nil {
		return maintainStoppedResult(
			root, mcpServiceVersion, "", start,
			"["+fail.Code+"] "+fail.Msg,
			mcpContract(
				textassets.ContractMaintainActionRepositoryFailure,
			),
		)
	}
	pendingRun, pendingErr := runmanifest.LatestPending(root, runmanifest.KindEntries)
	if pendingErr != nil || pendingRun != "" {
		return maintainStoppedResult(
			root, mcpServiceVersion, repository.text, start,
			mcpMessage("entries.pending_recovery", pendingRun, pendingRun),
			mcpContract(textassets.ContractMaintainActionBlocked),
		)
	}

	headerText, _ := index.ExtractHeader(
		repository.text,
	)

	dictionary := index.ExtractTagDict(
		headerText,
	)

	if dictionary == nil ||
		!dictionary.HasDict() {
		ledger.Append(
			root,
			repository.cfg.LedgerEnabled,
			ledger.Event{
				Op: "maintain",
				DurationMs: time.
					Since(start).
					Milliseconds(),
				Source: ledger.SourceAgent,
			},
		)

		if strings.Contains(
			headerText,
			"A层级",
		) ||
			strings.Contains(
				headerText,
				"B模块",
			) ||
			strings.Contains(
				headerText,
				"A Layer",
			) ||
			strings.Contains(
				headerText,
				"B Module",
			) {
			return maintainStoppedResult(
				root, mcpServiceVersion, repository.text, start,
				maintainDictionaryUnparseableMessage(),
				mcpContract(
					textassets.ContractMaintainActionDictionaryUnparseable,
				),
			)
		}

		return maintainStoppedResult(
			root, mcpServiceVersion, repository.text, start,
			maintainDictionaryMissingMessage(),
			mcpContract(
				textassets.ContractMaintainActionDictionaryMissing,
			),
		)
	}

	state, err := managedstate.Load(root, repository.cfg)
	if err != nil {
		return maintainStoppedResult(
			root, mcpServiceVersion, repository.text, start,
			writeMessage(
				"maintain.snapshot_failed",
				localeSafeWriteDetail(err.Error()),
			),
			mcpContract(
				textassets.ContractMaintainActionSnapshotFailure,
			),
		)
	}
	snapshot, warnings := state.Snapshot, state.Warnings
	detected := &baseline.DetectResult{Missing: []string{}, Orphan: []string{}, Stale: []string{}, Unbaselined: []string{},
		LineEndingOnly: []string{}, ObservedNew: []string{}, ObservedChanged: []string{}, ObservedRemoved: []string{}, Warnings: []string{}}
	if !state.ScopeChangeRequired {
		detected, err = managedstate.Detect(root, repository.cfg, repository.doc, state)
		if err != nil {
			return maintainStoppedResult(root, mcpServiceVersion, repository.text, start,
				"managed_scope_detect_failed: "+localeSafeWriteDetail(err.Error()),
				mcpContract(textassets.ContractMaintainActionBlocked))
		}
	}
	var managedGovernance *autoManagedGovernance
	if !state.Legacy {
		budgetReport, budgetErr := cognitionbudget.Build(root, []byte(repository.text), repository.cfg.EffectiveCognitionBudget())
		if budgetErr != nil {
			return maintainStoppedResult(root, mcpServiceVersion, repository.text, start,
				"cognition_budget_invalid: "+localeSafeWriteDetail(budgetErr.Error()),
				mcpContract(textassets.ContractMaintainActionBlocked))
		}
		managedGovernance = &autoManagedGovernance{ScopePolicyIdentity: state.DesiredPolicyIdentity,
			ObservedNew: append([]string{}, detected.ObservedNew...), ObservedChanged: append([]string{}, detected.ObservedChanged...),
			ObservedRemoved: append([]string{}, detected.ObservedRemoved...), CognitionBudget: budgetReport}
		if state.Evaluation != nil {
			managedGovernance.IndexCount, managedGovernance.ObserveCount, managedGovernance.ExcludeCount =
				state.Evaluation.IndexCount, state.Evaluation.ObserveCount, state.Evaluation.ExcludeCount
		}
		finding := ""
		switch {
		case state.ScopeChangeRequired:
			finding = "scope_change_required"
		case repository.cfg.EffectiveManagedScope().ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired &&
			(len(detected.ObservedNew)+len(detected.ObservedChanged)+len(detected.ObservedRemoved) > 0):
			finding = "observed_evidence_review_required"
		case budgetReport.WholeIndexTokens > budgetReport.MaxTokens:
			finding = "cognition_compression_required"
		case len(budgetReport.Violations) > 0:
			finding = "budget_exceeded"
		}
		if finding != "" {
			result := autoResult{Version: 1, Status: autoStatusStopped, Aligned: false,
				Receipt:  newCognitionReceipt(root, mcpServiceVersion, repository.text, cognitionScopeRepositoryFull),
				Metrics:  autoMetrics{DeterministicMs: elapsedMilliseconds(start), AOCIToolCalls: 1},
				Findings: machineFindings{genericMachineFinding("maintain_blocked", finding)}, NextAction: mcpContract(textassets.ContractMaintainActionBlocked),
				ManagedGovernance: managedGovernance}
			return textResult(renderAutoResult(result))
		}
	}

	// 所有只读策展停点必须在format-only正式写入前完成。
	// 否则无效curation会返回stopped，却隐瞒已前移的Baseline。
	classification, curationDocument, _, err :=
		curation.BuildClassification(
			root,
			repository.cfg,
			detected.Missing,
		)
	if err != nil {
		return maintainStoppedResult(
			root, mcpServiceVersion, repository.text, start,
			writeMessage(
				"maintain.curation_invalid",
				localeSafeWriteDetail(err.Error()),
			),
			mcpContract(
				textassets.ContractMaintainActionCurationInvalid,
			),
		)
	}

	formatOnly := formatOnlyCandidates(
		repository.bl,
		snapshot,
		detected.Stale,
		repository.cfg.LineEndingTolerance,
	)
	if fail := applyFormatOnlyBatch(
		root,
		repository,
		snapshot,
		formatOnly,
	); fail != nil {
		return textResult(renderAutoResult(autoResult{
			Version: 1,
			Status:  autoStatusStopped,
			Aligned: false,
			Receipt: newCognitionReceipt(
				root, mcpServiceVersion, repository.text, cognitionScopeRepositoryFull,
			),
			Metrics: autoMetrics{
				DeterministicMs: elapsedMilliseconds(start),
				AOCIToolCalls:   1,
				FormatOnlyFiles: len(formatOnly),
			},
			Findings: machineFindings{genericMachineFinding(fail.Code, "["+fail.Code+"] "+fail.Msg)},
			NextAction: mcpContract(
				textassets.ContractMaintainActionFormatOnlyFailure,
			),
		}))
	}
	if len(formatOnly) > 0 {
		formatSet := make(map[string]bool, len(formatOnly))
		for _, rel := range formatOnly {
			formatSet[rel] = true
		}
		semanticStale := detected.Stale[:0]
		for _, rel := range detected.Stale {
			if !formatSet[rel] {
				semanticStale = append(semanticStale, rel)
			}
		}
		detected.Stale = semanticStale
	}
	semanticChangeCount := buildSemanticChangeFacts(
		repository,
		detected,
		classification,
		formatOnly,
		warnings,
	).Count

	curationState := buildMaintainCurationState(classification)

	reported := loadReportedPaths(
		repository.paths.ReportsPath,
	)

	reportedSkipped := 0
	tasks := []maintainTask{}
	indexSelfStale := false

	for _, relativePath := range detected.Stale {
		if relativePath ==
			repository.cfg.IndexPath {
			indexSelfStale = true
			continue
		}

		if reported[relativePath] {
			reportedSkipped++
			continue
		}

		oldLine := ""

		if entry := index.FindEntry(
			repository.doc,
			relativePath,
		); entry != nil {
			oldLine = entry.FullLine
		}

		tasks = append(
			tasks,
			maintainTask{
				rel:          relativePath,
				kind:         writeMessage("maintain.candidate.update"),
				oldLine:      oldLine,
				sourceSHA256: snapshot[relativePath].SHA256,
			},
		)
	}

	included := make(
		map[string]bool,
		len(classification.Included),
	)

	for _, relativePath := range classification.Included {
		included[relativePath] = true
	}

	for _, relativePath := range classification.Actionable {
		if relativePath ==
			repository.cfg.IndexPath {
			continue
		}

		if reported[relativePath] {
			reportedSkipped++
			continue
		}

		task := maintainTask{
			rel:          relativePath,
			kind:         writeMessage("maintain.candidate.add"),
			sourceSHA256: snapshot[relativePath].SHA256,
		}

		if included[relativePath] {
			decision, found :=
				curation.DecisionByPath(
					curationDocument,
					relativePath,
				)

			profile :=
				classification.Profiles[relativePath]

			if found {
				task.kind = writeMessage("maintain.candidate.add_include")
				task.curationRole =
					decision.Role
				task.curationReason =
					decision.Reason
				task.profileReason =
					profile.Reason
				if profile.SourceSHA256 != "" {
					task.sourceSHA256 = profile.SourceSHA256
				}
			}
		}

		tasks = append(
			tasks,
			task,
		)
	}

	orphans := make(
		[]string,
		0,
		len(detected.Orphan),
	)

	for _, relativePath := range detected.Orphan {
		if reported[relativePath] {
			reportedSkipped++
			continue
		}

		orphans = append(
			orphans,
			relativePath,
		)
	}

	total := len(tasks)
	batch := tasks

	if len(batch) > maintainBatchLimit {
		batch = batch[:maintainBatchLimit]
	}

	result := buildMaintainAutoResult(
		root,
		mcpServiceVersion,
		repository.text,
		batch,
		total,
		orphans,
		detected.Unbaselined,
		indexSelfStale,
		reportedSkipped,
		warnings,
		curationState,
		formatOnly,
		start,
	)
	result.ManagedGovernance = managedGovernance
	if managedGovernance != nil && managedGovernance.CognitionBudget != nil {
		observedChanged := len(managedGovernance.ObservedNew)+len(managedGovernance.ObservedChanged)+len(managedGovernance.ObservedRemoved) > 0
		for position := range result.Candidates {
			candidate := &result.Candidates[position]
			policy := repository.cfg.EffectiveCognitionBudget()
			candidate.ScopeRole = machinecontract.ScopeRoleIndex
			candidate.ObserveEvidenceChanged = observedChanged
			candidate.WholeIndexTokens = managedGovernance.CognitionBudget.WholeIndexTokens
			candidate.ProjectedWholeIndexTokens = managedGovernance.CognitionBudget.WholeIndexTokens
			candidate.RemainingTokens = managedGovernance.CognitionBudget.MaxTokens - managedGovernance.CognitionBudget.WholeIndexTokens
			candidate.ProjectionPending = true
			candidate.RFieldBands = append([]cognitionbudget.FieldBand{}, policy.R...)
			candidate.SFieldBands = append([]cognitionbudget.FieldBand{}, policy.S...)
			if entry, ok := index.ParseEntryLine(candidate.ExistingEntry, 1); ok {
				importance, _ := strconv.Atoi(entry.TagsParsed["C"])
				if band, found := cognitionbudget.LimitFor(policy.R, importance); found {
					candidate.RTargetTokens, candidate.RMaxTokens = band.TargetTokens, band.MaxTokens
				}
				if band, found := cognitionbudget.LimitFor(policy.S, importance); found {
					candidate.STargetTokens, candidate.SMaxTokens = band.TargetTokens, band.MaxTokens
				}
			}
		}
	}
	result.Metrics.SemanticFiles = semanticChangeCount
	if refreshSession != nil {
		refreshSession.noteSemanticThreshold(
			semanticChangeCount,
			repository.cfg.CognitionRefreshThreshold,
		)
		applyAutoRefreshOutcome(&result, refreshSession)
	}
	if result.Aligned && len(formatOnly) == 0 {
		events, _ := ledger.Recent(root, 1)
		if len(events) == 1 && events[0].Op == "maintain" &&
			!events[0].DriftWarned {
			result.Metrics.RepeatedMaintains = 1
		}
	}

	driftWarned :=
		len(detected.Stale) > 0 ||
			len(classification.Actionable) > 0 ||
			len(classification.Pending) > 0

	ledger.Append(
		root,
		repository.cfg.LedgerEnabled,
		ledger.Event{
			Op:                "maintain",
			PathsCount:        len(batch),
			DurationMs:        result.Metrics.DeterministicMs,
			DriftWarned:       driftWarned,
			Source:            ledger.SourceAgent,
			AppliedCount:      len(formatOnly),
			AOCIToolCalls:     result.Metrics.AOCIToolCalls,
			ShellAOCICalls:    result.Metrics.ShellAOCICalls,
			OverviewReads:     result.Metrics.OverviewReads,
			LocalRecalls:      result.Metrics.LocalRecalls,
			SemanticFiles:     result.Metrics.SemanticFiles,
			FormatOnlyFiles:   result.Metrics.FormatOnlyFiles,
			DuplicateApplies:  result.Metrics.DuplicateApplies,
			RepeatedMaintains: result.Metrics.RepeatedMaintains,
		},
	)

	return textResult(renderAutoResult(result))
}

func maintainStoppedResult(
	root,
	mcpServiceVersion,
	indexText string,
	start time.Time,
	finding,
	nextAction string,
) *mcp.CallToolResult {
	return textResult(renderAutoResult(autoResult{
		Version: 1,
		Status:  autoStatusStopped,
		Aligned: false,
		Receipt: newCognitionReceipt(
			root, mcpServiceVersion, indexText, cognitionScopeRepositoryFull,
		),
		Metrics: autoMetrics{
			DeterministicMs: elapsedMilliseconds(start),
			AOCIToolCalls:   1,
		},
		Findings:   machineFindings{genericMachineFinding("maintain_blocked", finding)},
		NextAction: nextAction,
	}))
}

func buildMaintainAutoResult(
	root,
	mcpServiceVersion,
	indexText string,
	batch []maintainTask,
	total int,
	orphans,
	unbaselined []string,
	indexSelfStale bool,
	reportedSkipped int,
	warnings []string,
	curationState maintainCurationState,
	formatOnly []string,
	start time.Time,
) autoResult {
	result := autoResult{
		Version: 1,
		Receipt: newCognitionReceipt(
			root, mcpServiceVersion, indexText, cognitionScopeRepositoryFull,
		),
		Metrics: autoMetrics{
			DeterministicMs: elapsedMilliseconds(start),
			AOCIToolCalls:   1,
			SemanticFiles:   total,
			FormatOnlyFiles: len(formatOnly),
		},
		FormatOnlyApplied: append([]string{}, formatOnly...),
		Candidates:        make([]autoCandidate, 0, len(batch)),
		Findings:          machineFindings{},
	}
	for _, task := range batch {
		result.Candidates = append(result.Candidates, autoCandidate{
			Path:           task.rel,
			Kind:           task.kind,
			ExistingEntry:  task.oldLine,
			CurationRole:   task.curationRole,
			CurationReason: task.curationReason,
			ProfileReason:  task.profileReason,
			SourceSHA256:   task.sourceSHA256,
		})
	}
	for _, rel := range orphans {
		result.Findings = append(result.Findings, genericMachineFinding("orphan", "orphan: "+rel))
	}
	for _, rel := range unbaselined {
		result.Findings = append(result.Findings, genericMachineFinding("unbaselined", "unbaselined: "+rel))
	}
	for _, pending := range curationState.pending {
		result.Findings = append(result.Findings, genericMachineFinding("pending_curation", "pending_curation: "+pending.Path))
	}
	for _, skipped := range curationState.technicalSkipped {
		result.Findings = append(result.Findings, genericMachineFinding("technical_skip", "technical_skip: "+skipped.Path))
	}
	for _, warning := range warnings {
		result.Findings = append(result.Findings, genericMachineFinding("warning", "warning: "+warning))
	}
	if indexSelfStale {
		result.Findings = append(result.Findings, genericMachineFinding("index_self_stale", "index_self_stale"))
	}
	if reportedSkipped > 0 {
		result.Findings = append(result.Findings, genericMachineFinding("reported_pending", "reported_pending"))
	}

	blocked := len(orphans) > 0 || len(unbaselined) > 0 ||
		curationState.pendingMissing > 0 ||
		len(curationState.technicalSkipped) > 0 ||
		indexSelfStale || reportedSkipped > 0 || len(warnings) > 0
	switch {
	case blocked:
		result.Status = autoStatusStopped
		result.NextAction = mcpContract(
			textassets.ContractMaintainActionBlocked,
		)
	case total > 0:
		result.Status = autoStatusRepairRequired
		result.NextAction = mcpContract(
			textassets.ContractMaintainActionCandidates,
		)
	default:
		result.Status = autoStatusApplied
		result.Aligned = true
		result.NextAction = mcpContract(
			textassets.ContractMaintainActionAligned,
		)
	}
	return result
}

func registerMaintainTool(
	server *mcp.Server,
	root,
	mcpServiceVersion string,
	descriptions mcpToolDescriptions,
	inputSchemas mcpInputSchemas,
	refreshSession *cognitionRefreshSession,
) {
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "aoci_maintain",
			Description: descriptions[textassets.ContractMaintainToolDescription],
			InputSchema: inputSchemas["aoci_maintain"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			input maintainIn,
		) (
			*mcp.CallToolResult,
			any,
			error,
		) {
			result := guard(
				func() *mcp.CallToolResult {
					return handleMaintainInput(root, mcpServiceVersion, input, refreshSession)
				},
			)

			return result, nil, nil
		},
	)
}
