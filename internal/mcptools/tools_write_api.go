// 单条回写的公共调用面、MCP注册与结果渲染。
package mcptools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

type updateEntryIn struct {
	Path         string              `json:"path,omitempty"`
	ObjectRef    string              `json:"object_ref,omitempty"`
	NewEntry     string              `json:"new_entry,omitempty"`
	SourceSHA256 string              `json:"source_sha256,omitempty"`
	CandidateID  string              `json:"candidate_id,omitempty"`
	BatchID      string              `json:"batch_id,omitempty"`
	CodeBatchID  string              `json:"code_batch_id,omitempty"`
	Entries      []updateEntryItemIn `json:"entries,omitempty"`
}

var requiredEntryWriteMessages = map[string][]any{
	"entry.write.localized_detail_unavailable":                    nil,
	"entry.write.localized_detail_with_facts":                     {"facts"},
	"entry.write.context_empty":                                   nil,
	"entry.write.hint.reload":                                     nil,
	"entry.write.validation_failed":                               {"detail"},
	"entry.write.hint.validation":                                 nil,
	"entry.write.dictionary_failed":                               {"detail"},
	"entry.write.hint.dictionary":                                 nil,
	"entry.write.budget_failed":                                   {"code", "projection"},
	"entry.write.budget_projection_failed":                        {"detail"},
	"entry.write.hint.budget_reauthor":                            nil,
	"entry.write.warning.normalized_filename":                     nil,
	"entry.write.action.replace":                                  nil,
	"entry.write.action.insert":                                   nil,
	"entry.write.hint.refresh_entry":                              nil,
	"entry.write.path_rejected":                                   {"path", "detail"},
	"entry.write.hint.relative_path":                              nil,
	"entry.write.lock_timeout":                                    {"detail"},
	"entry.write.hint.lock_timeout":                               nil,
	"entry.write.lock_failed":                                     {"detail"},
	"entry.write.hint.lock_failed":                                nil,
	"entry.write.lock_release_warning":                            {"detail"},
	"entry.write.cas_read_failed":                                 {"detail"},
	"entry.write.hint.check_index":                                nil,
	"entry.write.cas_stale":                                       nil,
	"entry.write.hint.replan":                                     nil,
	"entry.write.baseline_read_failed":                            {"detail"},
	"entry.write.hint.baseline_read":                              nil,
	"entry.write.index_write_failed":                              {"detail"},
	"entry.write.hint.disk_permissions":                           nil,
	"entry.write.postimage_unconfirmed":                           nil,
	"entry.write.hint.external_index":                             nil,
	"entry.write.hash_unavailable":                                nil,
	"entry.write.hint.retry_same_candidate":                       nil,
	"entry.write.baseline_save_failed":                            {"detail"},
	"entry.write.baseline_advanced_target":                        nil,
	"entry.write.baseline_advanced_index":                         nil,
	"entry.write.baseline_postimage_changed":                      nil,
	"entry.write.hint.baseline_postimage_changed":                 nil,
	"entry.write.preview_note":                                    nil,
	"entry.write.preview_heading":                                 {"replace", "path"},
	"entry.write.applied_heading":                                 {"replace", "path"},
	"entry.write.warning":                                         {"detail"},
	"entry.write.diff_new":                                        nil,
	"entry.write.mcp.mixed_fields":                                nil,
	"entry.write.mcp.incomplete_input":                            nil,
	"entry.write.hint.contract_assets":                            nil,
	"entry.write.mcp.source_binding_required":                     {"path"},
	"entry.write.mcp.hint.source_binding":                         nil,
	"entry.write.diff.insert_note":                                nil,
	"entry.volume.impact_failed":                                  {"detail"},
	"entry.volume.hint.regenerate_candidate":                      nil,
	"entry.volume.cross_write_not_supported":                      nil,
	"entry.volume.target_not_supported":                           {"volume"},
	"entry.volume.cross_guard_required":                           {"volume"},
	"entry.volume.hint.cross_guard_required":                      nil,
	"entry.volume.source_conflict":                                {"object"},
	"entry.volume.target_conflict":                                {"volume"},
	"entry.volume.guard_unavailable":                              {"volume"},
	"entry.volume.guard_stale":                                    {"volume"},
	"entry.volume.guard_changed_after_write":                      {"volume"},
	"entry.volume.projected_invalid":                              {"detail"},
	"entry.volume.recovery_required":                              nil,
	"entry.volume.baseline_advanced":                              {1, "volumes"},
	"entry.batch.empty":                                           nil,
	"entry.batch.path_invalid":                                    {1, "path", "detail"},
	"entry.batch.hint.paths_relative":                             nil,
	"entry.batch.duplicate_path":                                  {"path"},
	"entry.batch.hint.unique_paths":                               nil,
	"entry.batch.source_hash_failed":                              {"path", "detail"},
	"entry.batch.source_cas_conflict":                             {"path"},
	"entry.batch.hint.refresh_binding":                            nil,
	"entry.batch.item_plan_failed":                                {1, 1, "path", "detail"},
	"entry.batch.reparse_failed":                                  {1},
	"entry.batch.hint.inspect_structure":                          nil,
	"entry.batch.lock_timeout":                                    {"detail"},
	"entry.batch.hint.lock_timeout":                               nil,
	"entry.batch.lock_failed":                                     {"detail"},
	"entry.batch.lock_release_warning":                            {"detail"},
	"entry.batch.cas_read_failed":                                 {"detail"},
	"entry.batch.cas_stale":                                       nil,
	"entry.batch.hint.replan":                                     nil,
	"entry.batch.baseline_read_failed":                            {"detail"},
	"entry.batch.hint.baseline_read":                              nil,
	"entry.batch.prewrite_source_conflict":                        {"path"},
	"entry.batch.recovery_save_failed":                            {"detail"},
	"entry.batch.recovery_cleanup_preimage_failed":                {"detail"},
	"entry.batch.postimage_recovery_pending":                      nil,
	"entry.batch.index_write_failed":                              {"detail"},
	"entry.batch.postimage_unconfirmed":                           nil,
	"entry.batch.postimage_unconfirmed_detail":                    {"detail"},
	"entry.batch.baseline_postimage_changed":                      nil,
	"entry.batch.baseline_save_failed":                            {"detail"},
	"entry.batch.source_drift":                                    {"paths"},
	"entry.batch.baseline_unchanged":                              nil,
	"entry.batch.baseline_partial":                                {1, 1, true},
	"entry.batch.baseline_advanced":                               {1, 1, true},
	"entry.batch.recovery_cleanup_completed_failed":               {"detail"},
	"entry.batch.reconcile_lock_failed":                           {"detail"},
	"entry.batch.reconcile_lock_release_warning":                  {"detail"},
	"entry.batch.reconcile_index_changed":                         nil,
	"entry.batch.reconcile_baseline_read_failed":                  {"detail"},
	"entry.batch.reconcile_hash_failed":                           {"path", "detail"},
	"entry.batch.reconcile_postimage_changed":                     nil,
	"entry.batch.reconcile_missing_binding":                       {"path"},
	"entry.batch.hint.rebuild_binding":                            nil,
	"entry.batch.reconcile_source_changed":                        {"path"},
	"entry.batch.hint.regenerate_candidate":                       nil,
	"entry.batch.already_resolved":                                nil,
	"entry.batch.reconcile_baseline_save_failed":                  {"detail"},
	"entry.batch.reconcile_baseline_postimage_changed":            nil,
	"entry.batch.reconcile_source_drift":                          {"paths"},
	"entry.batch.reconcile_completed":                             nil,
	"entry.batch.generation_plan_stale":                           nil,
	"entry.batch.hint.generation_plan_stale":                      nil,
	"entry.batch.preview_item":                                    nil,
	"entry.batch.preview_complete":                                nil,
	"entry.batch.recovery_cleanup_failed":                         {"detail"},
	"entry.batch.duplicate_heading":                               nil,
	"entry.repair.cause.too_many_items":                           {"A", 7, 6},
	"entry.repair.cause.too_long":                                 {"A", 401, 400},
	"entry.repair.cause.empty":                                    {"A"},
	"entry.repair.cause.structure":                                nil,
	"entry.repair.cause.tag":                                      nil,
	"entry.repair.cause.tag_compact":                              {"C.G.7.T"},
	"entry.repair.cause.relation_canonical":                       {"main.go"},
	"entry.repair.cause.identity":                                 nil,
	"entry.repair.cause.volume":                                   nil,
	"entry.repair.cause.tag_dictionary":                           nil,
	"entry.repair.cause.duplicate":                                nil,
	"entry.repair.cause.code_path":                                nil,
	"entry.repair.cause.code_candidate_id":                        nil,
	"entry.repair.cause.code_source_sha256":                       nil,
	"entry.repair.cause.code_batch_id":                            nil,
	"entry.repair.action.f_runes":                                 {160},
	"entry.repair.action.r_items":                                 {8},
	"entry.repair.action.r_runes":                                 {360},
	"entry.repair.action.a_items":                                 {6},
	"entry.repair.action.a_runes":                                 {400},
	"entry.repair.action.s_runes":                                 {200},
	"entry.repair.action.structure":                               {"FRAS"},
	"entry.repair.action.tag":                                     nil,
	"entry.repair.action.r_relation":                              nil,
	"entry.repair.action.identity":                                nil,
	"entry.repair.action.volume":                                  nil,
	"entry.repair.action.duplicate":                               nil,
	"entry.repair.action.code_path":                               nil,
	"entry.repair.action.code_candidate_id":                       nil,
	"entry.repair.action.code_source_sha256":                      nil,
	"entry.repair.action.code_batch_id":                           nil,
	"entry.repair.action.candidate":                               {"FRAS"},
	"entry.transaction.read_failed":                               {"detail"},
	"entry.transaction.pending_header":                            nil,
	"entry.transaction.hint.recover_header":                       nil,
	"entries.recovery_receipt.postimage_mismatch":                 nil,
	"entries.recovery_receipt.invalid":                            {"detail"},
	"entries.governance_receipt.persist_failed":                   {"detail"},
	"entries.recovery_receipt.governance_persist_failed":          {"detail"},
	"entries.recovery_receipt.baseline_governance_persist_failed": {"detail"},
}

var requiredReportMessages = map[string][]any{
	"entry.write.localized_detail_unavailable": nil,
	"entry.write.localized_detail_with_facts":  {"facts"},
	"report.path_rejected":                     {"path", "detail"},
	"report.hint.relative_path":                nil,
	"report.note_empty":                        nil,
	"report.write_failed":                      {"detail"},
	"report.recorded":                          {"path", "note", 1},
	"ledger.localized_detail_unavailable":      nil,
	"ledger.localized_detail_with_facts":       {"facts"},
	"ledger.marshal_failed":                    {"detail"},
	"ledger.mkdir_failed":                      {"detail"},
	"ledger.open_failed":                       {"detail"},
	"ledger.write_failed":                      {"detail"},
}

var requiredRemoveMessages = map[string][]any{
	"entry.write.localized_detail_unavailable": nil,
	"entry.write.localized_detail_with_facts":  {"facts"},
	"remove.path_invalid":                      {"detail"},
	"remove.hint.relative_path":                nil,
	"remove.recovery_read_failed":              {"detail"},
	"remove.recovery_entry_reappeared":         nil,
	"remove.hint.new_decision":                 nil,
	"remove.already_completed":                 {"path"},
	"remove.hint.no_repeat":                    nil,
	"remove.recovery_postimage_drift":          nil,
	"remove.hint.inspect_recovery":             nil,
	"remove.entry_missing":                     {"path"},
	"remove.hint.confirm_entry":                nil,
	"remove.transform_failed":                  {"detail"},
	"remove.hint.refresh":                      nil,
	"remove.live_target":                       {"path"},
	"remove.hint.live_target":                  nil,
	"remove.orphan_check_failed":               {"path", "detail"},
	"remove.hint.orphan_check":                 nil,
	"remove.lock_timeout":                      {"detail"},
	"remove.hint.lock_timeout":                 nil,
	"remove.lock_failed":                       {"detail"},
	"remove.hint.lock_failed":                  nil,
	"remove.lock_release_warning":              {"detail"},
	"remove.cas_read_failed":                   {"detail"},
	"remove.hint.check_index":                  nil,
	"remove.cas_stale":                         nil,
	"remove.hint.replan":                       nil,
	"remove.baseline_read_failed":              {"detail"},
	"remove.hint.baseline_read":                nil,
	"remove.recovery_save_failed":              {"detail"},
	"remove.hint.index_unchanged":              nil,
	"remove.recovery_cleanup_unapplied_failed": {"detail"},
	"remove.hint.transaction_permissions":      nil,
	"remove.postimage_recovery_incomplete":     {"detail"},
	"remove.hint.resume_baseline":              nil,
	"remove.index_write_failed":                {"detail"},
	"remove.hint.disk_permissions":             nil,
	"remove.index_hash_failed":                 {"detail"},
	"remove.hint.no_repeat_repair":             nil,
	"remove.postimage_changed":                 nil,
	"remove.hint.preserve_recovery":            nil,
	"remove.baseline_save_failed":              {"detail"},
	"remove.hint.retry_baseline":               nil,
	"remove.baseline_postimage_changed":        nil,
	"remove.hint.inspect_external":             nil,
	"remove.completion_marker_failed":          {"detail"},
	"remove.hint.retry_recovery":               nil,
	"remove.recovery_cleanup_failed":           {"detail"},
	"remove.hint.retry_cleanup":                nil,
	"remove.recovery_invalid":                  nil,
	"remove.completed_recovery_read_failed":    {"detail"},
	"remove.completed_recovery_cleanup_failed": {"detail"},
	"remove.preview":                           {"path", "entry"},
	"remove.applied":                           {"path", "entry"},
	"remove.ownership_repair_applied":          {"path", "owner", true, true, "entry"},
	"ledger.localized_detail_unavailable":      nil,
	"ledger.localized_detail_with_facts":       {"facts"},
	"ledger.marshal_failed":                    {"detail"},
	"ledger.mkdir_failed":                      {"detail"},
	"ledger.open_failed":                       {"detail"},
	"ledger.write_failed":                      {"detail"},
}

var requiredMaintainWriteMessages = map[string][]any{
	"entry.write.localized_detail_unavailable":        nil,
	"entry.write.localized_detail_with_facts":         {"facts"},
	"entry.write.hint.contract_assets":                nil,
	"maintain.snapshot_failed":                        {"detail"},
	"maintain.curation_invalid":                       {"detail"},
	"maintain.candidate.update":                       nil,
	"maintain.candidate.add":                          nil,
	"maintain.candidate.add_include":                  nil,
	"maintain.format_only.baseline_missing":           nil,
	"maintain.format_only.lock_failed":                {"detail"},
	"maintain.format_only.baseline_read_failed":       nil,
	"maintain.format_only.cas_conflict":               {"path"},
	"maintain.format_only.source_read_failed":         {"path", "detail"},
	"maintain.format_only.source_changed":             {"path"},
	"maintain.format_only.baseline_save_failed":       {"detail"},
	"maintain.format_only.source_changed_during_save": {"path"},
	"maintain.auto.marshal_failed":                    nil,
	"ledger.localized_detail_unavailable":             nil,
	"ledger.localized_detail_with_facts":              {"facts"},
	"ledger.marshal_failed":                           {"detail"},
	"ledger.mkdir_failed":                             {"detail"},
	"ledger.open_failed":                              {"detail"},
	"ledger.write_failed":                             {"detail"},
}

// validateEntryWriteMessages resolves the complete Entry-write catalog before
// planning or side effects. This makes a missing key, unknown Locale, malformed
// bundle, or format-signature mismatch a structured fail-closed result.
func validateEntryWriteMessages() *Fail {
	return validateWriteMessages(requiredEntryWriteMessages)
}

func validateWriteMessages(required map[string][]any) *Fail {
	for key, args := range required {
		if _, err := textassets.Message(textassets.ActiveLocale(), key, args...); err != nil {
			return &Fail{Code: errInternal, Msg: "entry_write_text_catalog_invalid:" + key}
		}
	}
	return nil
}

// writeMessage resolves one user-visible Entry-write message from the active
// Locale. Catalog defects are programming errors: panicking keeps the write
// path fail-closed, and the MCP guard converts the panic into a classified
// internal error instead of continuing with mixed or partial text.
func writeMessage(key string, args ...any) string {
	value, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		panic("entry_write_text_asset_error:" + key)
	}
	return value
}

// localeSafeWriteDetail preserves diagnostics already valid for the active
// Locale. en-US suppresses Han-bearing component diagnostics because exposing
// an untranslated validator or filesystem message would create a mixed-Locale
// response. Stable codes, paths, hashes, and ASCII diagnostics remain intact.
func localeSafeWriteDetail(detail string) string {
	hasHan := strings.ContainsFunc(detail, func(character rune) bool {
		return unicode.Is(unicode.Han, character)
	})
	hasASCII := strings.ContainsFunc(detail, func(character rune) bool {
		return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
	})
	if (textassets.ActiveLocale() == textassets.DefaultLocale && hasHan) ||
		(textassets.ActiveLocale() == textassets.LegacyLocale && !hasHan && hasASCII) {
		if facts := textassets.DiagnosticFacts(detail); facts != "" {
			return writeMessage("entry.write.localized_detail_with_facts", facts)
		}
		return writeMessage("entry.write.localized_detail_unavailable")
	}
	return detail
}

type updateEntryItemIn struct {
	Path         string `json:"path,omitempty"`
	ObjectRef    string `json:"object_ref,omitempty"`
	NewEntry     string `json:"new_entry"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	CandidateID  string `json:"candidate_id,omitempty"`
	BatchID      string `json:"-"`
}

func ApplyUpdateEntry(
	root,
	rawPath,
	rawEntry,
	source string,
	dryRun bool,
) (*UpdateOutcome, *Fail) {
	start := time.Now()

	plan, fail := planUpdateEntry(
		root,
		rawPath,
		rawEntry,
	)
	if fail != nil {
		if !dryRun {
			appendWriteFailEvent(
				root,
				"update_entry",
				source,
				fail.Code,
				start,
			)
		}

		return nil, fail
	}

	plan.out.DryRun = dryRun

	if dryRun {
		plan.out.BaselineNote =
			writeMessage("entry.write.preview_note")

		return plan.out, nil
	}

	if fail := commitPlan(
		root,
		source,
		plan,
	); fail != nil {
		appendWriteFailEvent(
			root,
			"update_entry",
			source,
			fail.Code,
			start,
		)

		return nil, fail
	}

	return plan.out, nil
}

func RenderOutcome(
	outcome *UpdateOutcome,
) string {
	var builder strings.Builder

	if outcome.DryRun {
		builder.WriteString(writeMessage("entry.write.preview_heading", outcome.Action, outcome.Rel) + "\n")
	} else {
		builder.WriteString(writeMessage("entry.write.applied_heading", outcome.Action, outcome.Rel) + "\n")
	}

	builder.WriteString(
		outcome.Diff,
	)

	if outcome.BaselineNote != "" {
		builder.WriteString(
			outcome.BaselineNote +
				"\n",
		)
	}

	for _, warning := range outcome.Warnings {
		builder.WriteString(writeMessage("entry.write.warning", localeSafeWriteDetail(warning)) + "\n")
	}

	return builder.String()
}

func registerWriteTools(
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
			Name:        "aoci_update_entry",
			Description: descriptions[textassets.ContractMCPUpdateDescription],
			InputSchema: inputSchemas["aoci_update_entry"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			in updateEntryIn,
		) (*mcp.CallToolResult, any, error) {
			return guard(func() *mcp.CallToolResult {
				if len(in.Entries) > 0 {
					if in.Path != "" || in.ObjectRef != "" || in.NewEntry != "" || in.SourceSHA256 != "" || in.CandidateID != "" {
						return failResult(&Fail{Code: errBadArgs, Msg: writeMessage("entry.write.mcp.mixed_fields")})
					}
					return handleMCPUpdateBatch(
						root,
						mcpServiceVersion,
						withVolumeBatchIDs(in.Entries, in.CodeBatchID, in.BatchID),
						refreshSession,
					)
				}
				if (in.Path == "") == (in.ObjectRef == "") || in.NewEntry == "" {
					return failResult(&Fail{Code: errBadArgs, Msg: writeMessage("entry.write.mcp.incomplete_input")})
				}
				return handleMCPUpdateSingle(
					root,
					mcpServiceVersion,
					in,
					refreshSession,
				)
			}), nil, nil
		},
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "aoci_report",
			Description: descriptions[textassets.ContractMCPReportDescription],
			InputSchema: inputSchemas["aoci_report"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			in reportIn,
		) (*mcp.CallToolResult, any, error) {
			return guard(func() *mcp.CallToolResult {
				return handleReport(
					root,
					in,
				)
			}), nil, nil
		},
	)
}

func handleMCPUpdateSingle(
	root,
	mcpServiceVersion string,
	in updateEntryIn,
	refreshSessions ...*cognitionRefreshSession,
) *mcp.CallToolResult {
	return handleMCPUpdateBatch(root, mcpServiceVersion, []updateEntryItemIn{{
		Path:         in.Path,
		ObjectRef:    in.ObjectRef,
		NewEntry:     in.NewEntry,
		SourceSHA256: in.SourceSHA256,
		CandidateID:  in.CandidateID,
		BatchID:      map[bool]string{true: in.CodeBatchID, false: in.BatchID}[in.Path != ""],
	}}, refreshSessions...)
}

// cognitionOptimizationTransactionComplete recognizes only an archived v4
// transaction whose exact formal participants remain at the committed
// postimage. An active transaction still requires the existing recovery path;
// absence of both active and archived evidence is a fresh Apply.
func cognitionOptimizationTransactionComplete(
	root string,
	items []AtomicUpdateItem,
	batchID string,
) (bool, error) {
	normalized, err := normalizeAtomicRecoveryItems(items)
	if err != nil {
		return false, err
	}
	batchKey := atomicBatchKey(normalized)
	if _, err = loadAtomicBatchRecovery(root, batchKey); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("cognition_optimization_transaction_invalid: %w", err)
	}
	recovery, err := loadEntriesRecoveryIncludingArchive(root, batchKey)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cognition_optimization_transaction_invalid: %w", err)
	}
	if recovery.Version != 4 || recovery.CodeBatchID != batchID ||
		inspectVolumeTargetStates(root, recoveryVolumeTargets(recovery)) != "postimage" ||
		recoveryGuardMismatch(root, recovery) != "" {
		return false, fmt.Errorf("cognition_optimization_transaction_postimage_invalid")
	}
	return true, nil
}

func handleMCPUpdateBatch(
	root,
	mcpServiceVersion string,
	input []updateEntryItemIn,
	refreshSessions ...*cognitionRefreshSession,
) *mcp.CallToolResult {
	var refreshSession *cognitionRefreshSession
	if len(refreshSessions) > 0 {
		refreshSession = refreshSessions[0]
	}
	if err := validateMCPContracts(
		textassets.ContractMaintainActionUpdateRepair,
		textassets.ContractMaintainActionUpdateStopped,
		textassets.ContractMaintainActionBaselineReplay,
		textassets.ContractMaintainActionApplyRemaining,
		textassets.ContractMaintainActionApplyDuplicate,
		textassets.ContractMaintainActionApplyFinalProof,
		textassets.ContractMaintainActionApplyDuplicateFinalProof,
		textassets.ContractMaintainActionAligned,
	); err != nil {
		return errResult(errInternal, localeSafeWriteDetail(err.Error()), writeMessage("entry.write.hint.contract_assets"))
	}
	start := time.Now()
	items := make([]AtomicUpdateItem, 0, len(input))
	var bindingFail *Fail
	for _, item := range input {
		item.SourceSHA256 = strings.ToLower(strings.TrimSpace(item.SourceSHA256))
		item.CandidateID = strings.ToLower(strings.TrimSpace(item.CandidateID))
		codeReceipt := item.Path != "" && item.SourceSHA256 != "" && item.CandidateID != "" && item.BatchID != ""
		databaseReceipt := item.ObjectRef != "" && item.CandidateID != "" && item.BatchID != ""
		legacyBinding := len(item.SourceSHA256) == 64 && strings.Trim(item.SourceSHA256, "0123456789abcdef") == ""
		if !codeReceipt && !databaseReceipt && !legacyBinding {
			bindingFail = &Fail{
				Code: errBadArgs,
				Msg:  writeMessage("entry.write.mcp.source_binding_required", item.Path),
				Hint: writeMessage("entry.write.mcp.hint.source_binding"),
			}
			break
		}
		if databaseReceipt && item.SourceSHA256 != "" {
			bindingFail = &Fail{Code: errBadArgs, Msg: "database_candidate_binding_fields_conflict"}
			break
		}
		items = append(items, AtomicUpdateItem{
			Path: item.Path, ObjectRef: item.ObjectRef, NewEntry: item.NewEntry,
			SourceSHA256: item.SourceSHA256, CandidateID: item.CandidateID, BatchID: strings.ToLower(strings.TrimSpace(item.BatchID)),
		})
	}
	var outcome *AtomicBatchOutcome
	fail := bindingFail
	var optimizationContext *cognitionOptimizationUpdateContext
	optimizationTransactionComplete := false
	optimizationRecoveryNeedsCompletion := false
	optimizationFormalWritesStarted := false
	if fail == nil {
		var optimizationErr error
		optimizationContext, optimizationErr = prepareCognitionOptimizationUpdate(root, input)
		if optimizationErr != nil {
			fail = &Fail{Code: errCandidateInvalid, Msg: optimizationErr.Error()}
		}
	}
	if fail == nil && optimizationContext != nil && !optimizationContext.AlreadyAdvanced && optimizationContext.Replaced > 0 {
		var transactionErr error
		optimizationTransactionComplete, transactionErr = cognitionOptimizationTransactionComplete(
			root, items, optimizationContext.BatchID,
		)
		if transactionErr != nil {
			fail = &Fail{Code: errWriteConflict, Msg: transactionErr.Error()}
		}
	}
	if fail == nil {
		// Keep the existing Entries recovery active until the optimization
		// checkpoint has advanced. If that final draft CAS fails after the formal
		// postimage is durable, retrying the same machine batch can then use the
		// existing recovery proof instead of inventing a second transaction.
		if optimizationContext != nil {
			if optimizationContext.AlreadyAdvanced {
				// The exact normalized submission was already applied and its draft
				// progress was durably advanced. Do not enter the formal Apply path
				// again. This minimal outcome only scopes the existing read-only
				// Volumes alignment inspector to Code; it does not claim a write.
				outcome = &AtomicBatchOutcome{Items: make([]*UpdateOutcome, len(input)), Volume: cognition.ScopeCode,
					Volumes: []string{cognition.ScopeCode}, BaselineComplete: true, AlreadyApplied: true}
			} else if optimizationContext.Replaced == 0 || optimizationTransactionComplete {
				// Classification and all current source/candidate/CAS bindings still
				// pass through the existing Update planner, but an all-no_change
				// batch and an already archived exact postimage must not reconcile or
				// rewrite the formal Index/Baseline.
				outcome, fail = ApplyUpdateEntriesAtomicBound(root, items, ledger.SourceAgent, true, "")
				if fail == nil && optimizationTransactionComplete {
					outcome.AlreadyApplied = true
				}
			} else {
				outcome, fail = ApplyUpdateEntriesAtomicBoundRetained(root, items, ledger.SourceAgent, false, "")
				if fail == nil {
					optimizationRecoveryNeedsCompletion = true
					// A replacement entered the formal transaction path. This remains
					// true for postimage recovery where only a Baseline participant may
					// need completion.
					optimizationFormalWritesStarted = true
				}
			}
		} else {
			outcome, fail = ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
		}
	}
	if fail != nil {
		status := autoStatusStopped
		if fail.Repairable || fail.Code == errBadArgs || fail.Code == errPathUnsafe || fail.Code == errCandidateInvalid {
			status = autoStatusRepairRequired
		}
		findings := fail.Findings
		if len(findings) == 0 && fail.GlobalStop == nil {
			findings = []cognition.RepairFinding{genericMachineFinding(fail.Code, "["+fail.Code+"] "+fail.Msg)}
		}
		findings = LocalizeRepairFindings(findings)
		retryScope := []string{}
		preserveOtherCandidates := false
		remaining := 0
		if status == autoStatusRepairRequired {
			retryScope = repairRetryScope(findings)
			preserveOtherCandidates = len(fail.Findings) > 0
			remaining = len(input)
		}
		metrics := autoMetrics{DeterministicMs: elapsedMilliseconds(start), AOCIToolCalls: 1, SemanticFiles: len(input)}
		appendAutoFinalizeEvent(root, status, metrics)
		nextAction := map[bool]string{
			true:  mcpContract(textassets.ContractMaintainActionUpdateRepair),
			false: mcpContract(textassets.ContractMaintainActionUpdateStopped),
		}[status == autoStatusRepairRequired]
		if fail.GlobalStop != nil {
			nextAction = fail.GlobalStop.SafeNextAction
		}
		if fail.CodePlan != nil {
			status = autoStatusStopped
			nextAction = fail.CodePlan.NextAction
			remaining = fail.CodePlan.TotalTargets
			preserveOtherCandidates = true
		}
		return textResult(renderAutoResult(autoResult{
			Version:                 1,
			Status:                  status,
			Aligned:                 false,
			Attempted:               len(input),
			Applied:                 0,
			Remaining:               remaining,
			FormalWritesStarted:     fail.FormalWritesStarted,
			Receipt:                 currentWriteCognitionReceipt(root, mcpServiceVersion),
			Metrics:                 metrics,
			Findings:                machineFindings(findings),
			PreserveOtherCandidates: preserveOtherCandidates,
			RetryScope:              retryScope,
			NextAction:              nextAction,
			CodePlan:                fail.CodePlan,
			Stop:                    fail.GlobalStop,
		}))
	}
	if outcome != nil && !outcome.BaselineComplete {
		metrics := autoMetrics{
			DeterministicMs: elapsedMilliseconds(start),
			AOCIToolCalls:   1,
			SemanticFiles:   len(input),
		}
		appendAutoFinalizeEvent(root, autoStatusStopped, metrics)
		return textResult(renderAutoResult(autoResult{
			Version:             1,
			Status:              autoStatusStopped,
			Aligned:             false,
			Attempted:           len(input),
			Applied:             outcome.AppliedCount,
			FormalWritesStarted: outcome.AppliedCount > 0 || optimizationFormalWritesStarted,
			Receipt:             currentWriteCognitionReceipt(root, mcpServiceVersion),
			Metrics:             metrics,
			Audit:               buildAutoAudit(input, outcome),
			Findings:            machineFindings{genericMachineFinding("baseline_incomplete", outcome.BaselineNote)},
			NextAction: mcpContract(
				textassets.ContractMaintainActionBaselineReplay,
			),
		}))
	}
	aligned, remaining, findings, receipt := inspectAutoAlignment(root, mcpServiceVersion, outcome)
	var optimization *cognitionOptimizationStatus
	var optimizationErr error
	if optimizationContext != nil && !aligned {
		optimizationErr = fmt.Errorf("ordinary governance is not aligned after the optimization batch")
	}
	if optimizationContext != nil && !optimizationContext.AlreadyAdvanced && optimizationRecoveryNeedsCompletion && optimizationErr == nil {
		// Archive the retained existing transaction proof before advancing the
		// draft checkpoint. If the subsequent checkpoint CAS fails, the archived
		// receipt still proves the exact postimage for an idempotent same-batch
		// retry.
		if cleanupErr := CompleteUpdateEntriesAtomicRecovery(root, items); cleanupErr != nil {
			metrics := autoMetrics{DeterministicMs: elapsedMilliseconds(start), AOCIToolCalls: 1, SemanticFiles: len(input)}
			appendAutoFinalizeEvent(root, autoStatusStopped, metrics)
			checkpoint := optimizationContext.Checkpoint.Checkpoint
			return textResult(renderAutoResult(autoResult{
				Version: 1, Status: autoStatusStopped, Aligned: false, Attempted: len(input), Applied: optimizationContext.Replaced,
				FormalWritesStarted: optimizationFormalWritesStarted, Receipt: currentWriteCognitionReceipt(root, mcpServiceVersion), Metrics: metrics,
				Findings:   machineFindings{genericMachineFinding("cognition_optimization_recovery_cleanup_failed", cleanupErr.Error())},
				NextAction: "retry_same_cognition_optimization_batch",
				Optimization: &cognitionOptimizationStatus{Version: cognitionOptimizationVersion,
					OptimizationID: checkpoint.OptimizationID, State: "recovery_cleanup_required",
					CurrentBatchID: checkpoint.CurrentBatchID,
					TotalTargets:   checkpoint.ReviewedCount + len(checkpoint.RemainingObjectRefs), Included: len(input),
					Reviewed: checkpoint.ReviewedCount, NoChange: checkpoint.NoChangeCount, Replaced: checkpoint.ReplacedCount,
					Remaining: len(checkpoint.RemainingObjectRefs), ContinuationRequired: true},
			}))
		}
	}
	if optimizationErr == nil {
		optimization, optimizationErr = advanceCognitionOptimizationUpdate(root, optimizationContext)
	}
	if optimizationErr != nil {
		metrics := autoMetrics{DeterministicMs: elapsedMilliseconds(start), AOCIToolCalls: 1, SemanticFiles: len(input)}
		applied := 0
		if optimizationContext != nil && (outcome == nil || !outcome.AlreadyApplied) {
			applied = optimizationContext.Replaced
		} else if outcome != nil {
			applied = outcome.AppliedCount
		}
		appendAutoFinalizeEvent(root, autoStatusStopped, metrics)
		checkpoint := optimizationContext.Checkpoint.Checkpoint
		return textResult(renderAutoResult(autoResult{
			Version: 1, Status: autoStatusStopped, Aligned: false, Attempted: len(input), Applied: applied,
			FormalWritesStarted: optimizationFormalWritesStarted, Receipt: currentWriteCognitionReceipt(root, mcpServiceVersion), Metrics: metrics,
			Findings:   machineFindings{genericMachineFinding("cognition_optimization_checkpoint_advance_failed", optimizationErr.Error())},
			NextAction: "retry_same_cognition_optimization_batch",
			Optimization: &cognitionOptimizationStatus{Version: cognitionOptimizationVersion,
				OptimizationID: checkpoint.OptimizationID, State: "checkpoint_recovery_required",
				CurrentBatchID: checkpoint.CurrentBatchID,
				TotalTargets:   checkpoint.ReviewedCount + len(checkpoint.RemainingObjectRefs), Included: len(input),
				Reviewed: checkpoint.ReviewedCount, NoChange: checkpoint.NoChangeCount, Replaced: checkpoint.ReplacedCount,
				Remaining: len(checkpoint.RemainingObjectRefs), ContinuationRequired: true},
		}))
	}
	duplicate := 0
	applied := 0
	if outcome != nil {
		applied = outcome.AppliedCount
		if outcome.AlreadyApplied {
			duplicate = 1
		}
	}
	if optimizationContext != nil {
		// The existing transaction reports every submitted item when a mixed
		// Code Volume postimage is committed. Optimization exposes the narrower
		// semantic replacement count while retaining the original atomic batch
		// and audit evidence internally.
		if optimizationContext.AlreadyAdvanced {
			applied = 0
			duplicate = 1
		} else if outcome == nil || !outcome.AlreadyApplied {
			applied = optimizationContext.Replaced
		} else {
			applied = 0
		}
	}
	metrics := autoMetrics{
		DeterministicMs:  elapsedMilliseconds(start),
		AOCIToolCalls:    1,
		SemanticFiles:    len(input),
		DuplicateApplies: duplicate,
	}
	audit := buildAutoAudit(input, outcome)
	appendAutoFinalizeEvent(root, autoStatusApplied, metrics)
	result := autoResult{
		Version:             1,
		Status:              autoStatusApplied,
		Aligned:             aligned,
		Attempted:           len(input),
		Applied:             applied,
		Remaining:           remaining,
		FormalWritesStarted: map[bool]bool{true: optimizationFormalWritesStarted, false: applied > 0}[optimizationContext != nil],
		Receipt:             receipt,
		Metrics:             metrics,
		Audit:               audit,
		Findings:            genericMachineFindings(findings),
		NextAction: autoApplyNextAction(
			outcome != nil && len(outcome.Volumes) > 0,
			aligned,
			applied,
			duplicate,
		),
		Optimization: optimization,
	}
	if optimization != nil {
		result.Remaining = optimization.Remaining
		if optimization.ContinuationRequired {
			result.NextAction = "call_aoci_maintain_with_cognition_optimization"
		} else {
			result.NextAction = "none"
		}
	}
	applyAutoRefreshOutcome(&result, refreshSession)
	return textResult(renderAutoResult(result))
}

func withVolumeBatchIDs(input []updateEntryItemIn, codeBatchID, databaseBatchID string) []updateEntryItemIn {
	result := append([]updateEntryItemIn{}, input...)
	for index := range result {
		if result[index].ObjectRef != "" {
			result[index].BatchID = databaseBatchID
		} else if result[index].Path != "" {
			result[index].BatchID = codeBatchID
		}
	}
	return result
}

func appendAutoFinalizeEvent(root, status string, metrics autoMetrics) {
	cfg, err := config.Load(root)
	if err != nil || cfg == nil {
		return
	}
	result := ledger.ResultOK
	if status == autoStatusRepairRequired {
		result = ledger.ResultRepairRequired
	} else if status == autoStatusStopped {
		result = ledger.ResultError
	}
	ledger.Append(root, cfg.LedgerEnabled, ledger.Event{
		Op:                "auto_finalize",
		DurationMs:        metrics.DeterministicMs,
		Source:            ledger.SourceAgent,
		Result:            result,
		AOCIToolCalls:     metrics.AOCIToolCalls,
		ShellAOCICalls:    metrics.ShellAOCICalls,
		OverviewReads:     metrics.OverviewReads,
		LocalRecalls:      metrics.LocalRecalls,
		SemanticFiles:     metrics.SemanticFiles,
		FormatOnlyFiles:   metrics.FormatOnlyFiles,
		DuplicateApplies:  metrics.DuplicateApplies,
		RepeatedMaintains: metrics.RepeatedMaintains,
	})
}
