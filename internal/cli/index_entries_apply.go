// 索引条目: index_entries_apply.go(待补录)
//
// `aoci index entries apply`应用已审阅的Entries草稿，并追加Application审计。
//
// 正式写入前的三层一致性:
//   - R52: 审阅Run与Apply Run一致;
//   - R60-D: Host-Agent草稿的Generation Plan仍是当前Plan;
//   - P-23: 当前草稿内容仍是最近一次Check/Diff审阅的内容。
//
// 原子应用顺序:
//  1. 解析仓库、配置和Run;
//  2. R52核对Run;
//  3. 读取Manifest;
//  4. Host-Agent Generation Plan核对;
//  5. 一次读取全部Entries草稿形成内存快照;
//  6. P-23核对Review Hash;
//  7. 在调用原子内核前对全部条目执行字典硬闸;
//  8. 把全部可应用候选交给ApplyUpdateEntriesAtomic;
//  9. 原子内核在同一内存索引上规划整批，一次写锁、一次CAS、一次落盘;
//
// 10. 追加批次级Application和entries_apply Ledger。
//
// 任一条目规划失败、路径重复、索引CAS冲突或字典违规，都使整批正式索引
// 零写入。低层原子内核记录update_entries_batch，上层命令记录entries_apply；
// 不再为同一批次产生逐条update_entry事件。
//
// 原子边界覆盖正式索引文件。Baseline、Ledger和Manifest Application是写后
// 审计副产品；若正式索引已成功写入但审计副产品失败，命令如实返回内部错误，
// 不虚构回滚。
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/spf13/cobra"
)

// entriesAtomicInput是人工Entries Apply交给原子内核前的完整批次输入。
type entriesAtomicInput struct {
	Items        []mcptools.AtomicUpdateItem
	Actionable   int
	DictFailures []string
}

var appendManualEntriesApplication = draft.AppendApplication

// newEntriesApplyCmd构造`aoci index entries apply [run_id]`。
func newEntriesApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [run_id]",
		Short: cliMessage("cli.short.index_entries_apply"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			start := time.Now()

			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			runID, err := resolveEntriesRunID(
				repoRoot,
				args,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			output := cmd.OutOrStdout()

			runWarning, runGuardErr := guardImplicitApply(
				repoRoot,
				runID,
				len(args) > 0,
				"aoci index entries apply",
				"entries_diff",
				"entries_check",
			)
			if runGuardErr != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  runGuardErr,
				}
			}
			if runWarning != "" {
				fmt.Fprintln(
					output,
					runWarning,
				)
			}

			manifest, err := draft.LoadManifest(
				repoRoot,
				runID,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			generationNote, generationErr :=
				guardHostAgentGenerationPlan(
					cmd,
					repoRoot,
					cfg,
					manifest,
					draft.KindEntries,
					agentPlanStageEntriesRequired,
				)
			if generationErr != nil {
				return generationErr
			}
			if generationNote != "" {
				fmt.Fprintln(
					output,
					generationNote,
				)
			}

			snapshot, err := loadEntryDraftSnapshot(
				repoRoot,
				runID,
				manifest,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  fmt.Errorf("%s", cliMessage("entries.apply.snapshot_read_failed", localeSafeCLIDetail(err.Error()))),
				}
			}

			contentWarning, contentGuardErr :=
				guardReviewedDraftHash(
					manifest,
					snapshot.Hash,
				)
			if contentGuardErr != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  contentGuardErr,
				}
			}
			if contentWarning != "" {
				fmt.Fprintln(
					output,
					contentWarning,
				)
			} else {
				fmt.Fprint(
					output,
					cliMessage("entries.apply.review_ok", shortDraftHash(snapshot.Hash)),
				)
			}

			doc, _, err := loadIndexForCLI(
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

			headerText, _ := index.ExtractHeader(
				doc.RawText,
			)
			dict := index.ExtractTagDict(
				headerText,
			)

			atomicInput, err := buildEntriesAtomicInput(
				manifest,
				snapshot,
				dict,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err:  err,
				}
			}

			fmt.Fprint(
				output,
				cliMessage("entries.apply.target", runID, manifest.CreatedAt, atomicInput.Actionable),
			)
			fmt.Fprint(
				output,
				cliMessage("entries.apply.draft_hash", shortDraftHash(snapshot.Hash)),
			)

			if len(atomicInput.DictFailures) > 0 {
				for _, failure := range atomicInput.DictFailures {
					fmt.Fprintln(
						output,
						"✗ "+failure,
					)
				}

				auditErr := recordManualEntriesApplication(
					repoRoot,
					cfg,
					runID,
					snapshot.Hash,
					atomicInput.Actionable,
					0,
					0,
					atomicInput.Actionable,
					"dict",
					false,
					time.Since(start),
				)

				fmt.Fprintln(
					output,
					"──────────────────────────────",
				)
				fmt.Fprint(
					output,
					cliMessage("entries.apply.summary_rejected", atomicInput.Actionable),
				)

				if auditErr != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"entries.apply.dictionary_audit_failed",
							localeSafeCLIDetail(auditErr.Error()),
						)),
					}
				}

				return &ExitError{
					Code: ExitInvalid,
					Msg:  "",
				}
			}

			batchOutcome, applyFail :=
				mcptools.ApplyUpdateEntriesAtomicBoundRetained(
					repoRoot,
					atomicInput.Items,
					ledger.SourceHuman,
					false,
					func() string {
						if manifest.GenerationSource == draft.GenerationSourceHostAgent {
							return manifest.IndexSHA256
						}
						return ""
					}(),
				)
			if applyFail != nil {
				rejectKind := entriesAtomicRejectKind(
					applyFail.Code,
				)

				fmt.Fprint(
					output,
					cliMessage(
						"entries.apply.batch_failed",
						applyFail.Code,
						localeSafeCLIDetail(applyFail.Msg),
					),
				)
				if applyFail.Hint != "" {
					fmt.Fprintln(
						output,
						cliMessage("entries.apply.hint", localeSafeCLIDetail(applyFail.Hint)),
					)
				}

				auditErr := recordManualEntriesApplication(
					repoRoot,
					cfg,
					runID,
					snapshot.Hash,
					len(atomicInput.Items),
					0,
					0,
					len(atomicInput.Items),
					rejectKind,
					false,
					time.Since(start),
				)

				fmt.Fprintln(
					output,
					"──────────────────────────────",
				)
				fmt.Fprint(
					output,
					cliMessage("entries.apply.summary_rejected", len(atomicInput.Items)),
				)

				if auditErr != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"entries.apply.failure_and_audit_failed",
							applyFail.Code,
							localeSafeCLIDetail(applyFail.Msg),
							localeSafeCLIDetail(auditErr.Error()),
						)),
					}
				}

				return &ExitError{
					Code: exitCodeForFail(
						applyFail.Code,
					),
					Err: formatManualAtomicApplyFail(
						applyFail,
					),
				}
			}

			fmt.Fprint(
				output,
				mcptools.RenderAtomicBatchOutcome(
					batchOutcome,
				),
			)
			appliedCount := len(atomicInput.Items)
			recoveredCount := 0
			if batchOutcome != nil {
				appliedCount = batchOutcome.AppliedCount
				recoveredCount = batchOutcome.RecoveredCount
			}
			if batchOutcome != nil && !batchOutcome.BaselineComplete {
				auditErr := recordManualEntriesApplication(
					repoRoot,
					cfg,
					runID,
					snapshot.Hash,
					len(atomicInput.Items),
					appliedCount,
					recoveredCount,
					0,
					"baseline_incomplete",
					false,
					time.Since(start),
				)
				if auditErr != nil {
					return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
						"entries.apply.baseline_and_audit_failed",
						localeSafeCLIDetail(auditErr.Error()),
					))}
				}
				return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
					"entries.apply.baseline_incomplete",
					localeSafeCLIDetail(batchOutcome.BaselineNote),
				))}
			}
			completedLocalePaths := make([]string, 0, len(atomicInput.Items))
			for _, item := range atomicInput.Items {
				completedLocalePaths = append(completedLocalePaths, item.Path)
			}
			if err := config.AdvanceLocaleMigration(
				repoRoot,
				false,
				completedLocalePaths,
				nil,
			); err != nil {
				return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
					"entries.apply.locale_advance_failed",
					localeSafeCLIDetail(err.Error()),
				))}
			}

			auditErr := recordManualEntriesApplication(
				repoRoot,
				cfg,
				runID,
				snapshot.Hash,
				len(atomicInput.Items),
				appliedCount,
				recoveredCount,
				0,
				"",
				true,
				time.Since(start),
			)
			if auditErr != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"entries.apply.application_audit_failed",
						localeSafeCLIDetail(auditErr.Error()),
					)),
				}
			}
			if cleanupErr := mcptools.CompleteUpdateEntriesAtomicRecovery(
				repoRoot,
				atomicInput.Items,
			); cleanupErr != nil {
				return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
					"entries.recovery_receipt.cleanup_failed", cleanupErr,
				))}
			}

			fmt.Fprintln(
				output,
				"──────────────────────────────",
			)
			fmt.Fprint(
				output,
				cliMessage("entries.apply.summary_success", appliedCount),
			)
			fmt.Fprintln(
				output,
				cliMessage("entries.apply.verify_hint"),
			)
			return nil
		},
	}
}

// buildEntriesAtomicInput从同一草稿快照构造人工原子批次。
//
// 非drafted/warned的Generation状态沿用历史人工语义跳过，不进入本次尝试。
// 字典违规先全部收集，任一违规即由上层整批拒绝，不把剩余合法项送入原子内核。
func buildEntriesAtomicInput(
	manifest *draft.Manifest,
	snapshot *entryDraftSnapshot,
	dict *index.TagDict,
) (*entriesAtomicInput, error) {
	if manifest == nil {
		return nil, fmt.Errorf("%s", cliMessage("entries.apply.input_manifest_empty"))
	}
	if snapshot == nil {
		return nil, fmt.Errorf("%s", cliMessage("entries.apply.input_snapshot_empty"))
	}

	result := &entriesAtomicInput{
		Items:        []mcptools.AtomicUpdateItem{},
		DictFailures: []string{},
	}

	for _, status := range manifest.Entries {
		if status.Status != "drafted" &&
			status.Status != "warned" {
			continue
		}

		result.Actionable++
		sourceSHA256 := strings.ToLower(strings.TrimSpace(status.SourceSHA256))
		if !validEntrySourceSHA256(sourceSHA256) {
			return nil, fmt.Errorf("%s", cliMessage("entries.apply.source_sha_missing", status.Path))
		}

		line, err := snapshot.line(
			status.Path,
		)
		if err != nil {
			return nil, fmt.Errorf("%s", cliMessage(
				"entries.apply.draft_read_failed",
				status.Path,
				localeSafeCLIDetail(err.Error()),
			))
		}

		if violation := index.CheckTagsAgainstDict(
			line,
			dict,
		); violation != nil {
			result.DictFailures = append(
				result.DictFailures,
				cliMessage(
					"entries.apply.dictionary_failure",
					status.Path,
					localeSafeCLIDetail(violation.Msg),
				),
			)
			continue
		}

		result.Items = append(
			result.Items,
			mcptools.AtomicUpdateItem{
				Path:         status.Path,
				NewEntry:     line,
				SourceSHA256: sourceSHA256,
			},
		)
	}

	if result.Actionable == 0 {
		return nil, fmt.Errorf("%s", cliMessage("entries.apply.input_empty"))
	}

	return result, nil
}

// recordManualEntriesApplication追加人工批次Application和上层Ledger。
//
// Ledger写失败由ledger包自行向stderr警告；Manifest审计失败返回调用方裁决。
func recordManualEntriesApplication(
	repoRoot string,
	cfg *config.Config,
	runID,
	draftHash string,
	pathsCount,
	applied,
	recovered,
	rejected int,
	rejectKinds string,
	markApplied bool,
	duration time.Duration,
) error {
	result := ledger.ResultOK
	if rejectKinds == "baseline_incomplete" {
		result = ledger.ResultError
	} else if rejected > 0 {
		result = ledger.ResultRejected
		if rejectKinds == "conflict" {
			result = ledger.ResultConflict
		}
	}
	auditErr := appendManualEntriesApplication(
		repoRoot,
		runID,
		draft.ApplicationRecord{
			DraftHash:   draftHash,
			PathsCount:  pathsCount,
			Applied:     applied,
			Recovered:   recovered,
			Rejected:    rejected,
			RejectKinds: rejectKinds,
		},
		markApplied,
	)
	ledgerRejectKinds := rejectKinds
	if auditErr != nil {
		result = ledger.ResultError
		if ledgerRejectKinds == "" {
			ledgerRejectKinds = "application_audit"
		} else {
			ledgerRejectKinds += ",application_audit"
		}
	}

	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:             "entries_apply",
			Source:         ledger.SourceHuman,
			PathsCount:     pathsCount,
			DurationMs:     duration.Milliseconds(),
			DraftRunID:     runID,
			AppliedCount:   applied,
			RecoveredCount: recovered,
			RejectedCount:  rejected,
			RejectKinds:    ledgerRejectKinds,
			Result:         result,
		},
	)

	return auditErr
}

// entriesAtomicRejectKind把底层结构化失败码压缩为批次审计原因。
func entriesAtomicRejectKind(
	code string,
) string {
	switch code {
	case "write_conflict":
		return "conflict"
	case "bad_args", "index_invalid", "path_unsafe":
		return "format"
	default:
		return "other"
	}
}

// formatManualAtomicApplyFail保留底层分类码与可操作建议。
func formatManualAtomicApplyFail(
	failure *mcptools.Fail,
) error {
	if failure == nil {
		return fmt.Errorf("%s", cliMessage("entries.apply.failure_unknown"))
	}
	if failure.Hint == "" {
		return fmt.Errorf("%s", cliMessage(
			"entries.apply.failure",
			failure.Code,
			localeSafeCLIDetail(failure.Msg),
		))
	}
	return fmt.Errorf("%s", cliMessage(
		"entries.apply.failure_with_hint",
		failure.Code,
		localeSafeCLIDetail(failure.Msg),
		localeSafeCLIDetail(failure.Hint),
	))
}
