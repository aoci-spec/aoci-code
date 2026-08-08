// 索引条目: index_header_apply.go(待补录)
//
// `aoci index header apply [run_id]`是Header生命周期唯一允许修改正式索引的动作。
//
// 正式写入前的三层一致性:
//   - R52核对审阅Run与应用Run;
//   - R60-D核对Host-Agent Generation Plan仍是当前Plan;
//   - Header P-23核对最近Diff内容摘要。
//
// 闸门顺序:
//   - R52;
//   - 同轮读取Header草稿快照;
//   - 读取Manifest;
//   - Host-Agent Generation Plan;
//   - Header P-23;
//   - Safety禁区词;
//   - ValidateHeaderText结构校验;
//   - 跨进程写锁;
//   - 锁内重新读取正式索引并ReplaceHeader;
//   - 备份与原子写入;
//   - Baseline前移;
//   - Ledger和applied_at。
//
// 显式run_id只能裁决Run选择，不能绕过Generation Plan或P-23内容核对。
//
// 非Host-Agent草稿及旧草稿继续使用原兼容规则。
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/safety"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

var (
	saveHeaderBaseline = baseline.SaveUnderIndexLock
	writeHeaderIndex   = hooks.BackupThenWriteCAS
	markHeaderApplied  = draft.MarkApplied
)

// newHeaderApplyCmd构造`aoci index header apply [run_id]`。
func newHeaderApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [run_id]",
		Short: cliMessage("cli.short.index_header_apply"),
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

			runID, err := resolveHeaderRunID(
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
				"aoci index header apply",
				"header_diff",
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

			snapshot, err := loadHeaderDraftSnapshot(
				repoRoot,
				runID,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err: fmt.Errorf("%s", cliMessage(
						"header.snapshot.read_failed",
						runID,
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			// 恢复意图、Manifest终态、正式索引与Baseline必须在同一写锁视图中
			// 判定。锁外预读会让并发重试携带已经被前一进程完成的陈旧恢复状态。
			lock, lockErr := afs.AcquireIndexLock(repoRoot)
			if lockErr != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"header.apply.lock_failed",
						localeSafeCLIDetail(lockErr.Error()),
					)),
				}
			}
			defer func() {
				if releaseErr := lock.Release(); releaseErr != nil {
					fmt.Fprintln(os.Stderr, cliMessage(
						"header.apply.lock_release_warning",
						localeSafeCLIDetail(releaseErr.Error()),
					))
				}
			}()

			manifest, manifestErr := draft.LoadManifest(
				repoRoot,
				runID,
			)
			recovery, recoveryErr := loadHeaderRecovery(repoRoot, runID)
			if recoveryErr != nil && !os.IsNotExist(recoveryErr) {
				return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
					"header.apply.recovery_read_failed",
					localeSafeCLIDetail(recoveryErr.Error()),
				))}
			}
			if os.IsNotExist(recoveryErr) {
				recovery = nil
			}
			recoveringHeader := false
			if recovery != nil {
				if recovery.DraftSHA256 != snapshot.Hash {
					return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
						"header.apply.recovery_hash_drift",
					))}
				}
				currentData, readErr := os.ReadFile(
					config.AOCIPaths(repoRoot, cfg.IndexPath).IndexPath,
				)
				if readErr != nil {
					return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
						"header.apply.recovery_postimage_read_failed",
						localeSafeCLIDetail(readErr.Error()),
					))}
				}
				currentSHA := rawContentSHA256(currentData)
				switch currentSHA {
				case recovery.PostIndexSHA256:
					recoveringHeader = true
				case recovery.PreIndexSHA256:
					// 上次在CAS前中断；仍按原Generation Plan重新经过全部闸门。
				default:
					return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
						"header.apply.recovery_index_drift",
					))}
				}
			}
			if manifestErr == nil && manifest.AppliedAt != "" {
				if recovery != nil {
					if !recoveringHeader {
						return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
							"header.apply.completed_preimage",
						))}
					}
					if cleanupErr := cleanupHeaderRecovery(repoRoot, runID); cleanupErr != nil {
						return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
							"header.apply.completed_cleanup_failed",
							localeSafeCLIDetail(cleanupErr.Error()),
						))}
					}
				}
				if err := ensureManagedAgentsLocale(repoRoot, cfg.Locale); err != nil {
					return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
						"locale.header_apply.already_agents_failed", localeSafeCLIDetail(err.Error()),
					))}
				}
				if err := config.AdvanceLocaleMigration(repoRoot, true, nil, nil); err != nil {
					return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
						"locale.header_apply.already_state_failed", localeSafeCLIDetail(err.Error()),
					))}
				}
				if err := draft.ClearLegacyHeaderIntent(repoRoot, runID); err != nil {
					return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
						"header.apply.completed_cleanup_failed",
						localeSafeCLIDetail(err.Error()),
					))}
				}
				fmt.Fprint(output, cliMessage("header.apply.already_completed", runID))
				return nil
			}
			switch {
			case manifestErr == nil:
				if recoveringHeader {
					fmt.Fprintln(output, cliMessage("header.apply.recovery_confirmed"))
				} else {
					expectedPlanStage := agentPlanStageHeaderRequired
					intent := snapshot.Intent
					if manifest.HeaderIntent != "" {
						if intent != "" && intent != manifest.HeaderIntent {
							return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
								"header.stage.intent_invalid",
								manifest.HeaderIntent,
							))}
						}
						intent = manifest.HeaderIntent
					}
					if intent == agentHeaderStageIntentSemanticRefresh {
						expectedPlanStage = agentPlanStageAligned
					}
					generationNote, generationErr :=
						guardHostAgentGenerationPlan(
							cmd,
							repoRoot,
							cfg,
							manifest,
							draft.KindHeader,
							expectedPlanStage,
						)
					if generationErr != nil {
						return generationErr
					}
					if generationNote != "" {
						fmt.Fprintln(output, generationNote)
					}
				}

				contentWarning, contentGuardErr :=
					guardReviewedHeaderHash(
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
					fmt.Fprint(output, cliMessage(
						"header.apply.review_ok",
						shortHeaderDraftHash(snapshot.Hash),
					))
				}

				fmt.Fprint(output, cliMessage(
					"header.apply.target_created",
					runID,
					manifest.CreatedAt,
				))

			case errors.Is(manifestErr, os.ErrNotExist):
				fmt.Fprintln(output, cliMessage("header.apply.legacy_manifest"))
				fmt.Fprint(output, cliMessage("header.apply.target", runID))

			default:
				return &ExitError{
					Code: ExitConfig,
					Err: fmt.Errorf("%s", cliMessage(
						"header.manifest.read_failed",
						runID,
						localeSafeCLIDetail(manifestErr.Error()),
					)),
				}
			}

			fmt.Fprint(output, cliMessage(
				"header.apply.hash",
				shortHeaderDraftHash(snapshot.Hash),
			))

			newHeader := snapshot.Text

			if hits := safety.CheckForbiddenClaims(
				newHeader,
			); len(hits) > 0 {
				return &ExitError{
					Code: ExitInvalid,
					Err: fmt.Errorf("%s", cliMessage(
						"header.apply.forbidden",
						localeSafeCLIDetail(safety.FormatHits(
							"header draft "+runID,
							hits,
						)),
					)),
				}
			}

			if line, message := index.ValidateHeaderText(
				newHeader,
			); line > 0 {
				return &ExitError{
					Code: ExitInvalid,
					Err: fmt.Errorf("%s", cliMessage(
						"header.apply.structure_invalid",
						line,
						localeSafeCLIDetail(message),
					)),
				}
			}
			headerLocale, explicitLocale, localeErr := index.DetectLocale(newHeader)
			requireExplicitLocale := cfg.Locale != textassets.LegacyLocale || cfg.LocaleMigration != nil
			if localeErr != nil || (requireExplicitLocale && !explicitLocale) || headerLocale != cfg.Locale {
				if localeErr == nil {
					localeErr = fmt.Errorf("%s", cliMessage("locale.header_marker_required", cfg.Locale))
				}
				return &ExitError{Code: ExitInvalid, Err: localeErr}
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

			preIndexSHA256 := rawContentSHA256([]byte(doc.RawText))
			if !recoveringHeader && manifest != nil &&
				manifest.GenerationSource == draft.GenerationSourceHostAgent &&
				preIndexSHA256 != manifest.IndexSHA256 {
				return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
					"header.apply.generation_stale",
				))}
			}
			newText := doc.RawText
			if recoveringHeader {
				if recovery == nil || preIndexSHA256 != recovery.PostIndexSHA256 {
					return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
						"header.apply.recovery_external_drift",
					))}
				}
			} else if snapshot.ManagedIndexText != "" {
				candidateHeader, _ := index.ExtractHeader(snapshot.ManagedIndexText)
				if strings.TrimSpace(candidateHeader) != strings.TrimSpace(newHeader) {
					return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
						"locale.header_apply.managed_header_mismatch",
					))}
				}
				if _, validateErr := validateLocaleIndexCandidate(
					doc.RawText,
					snapshot.ManagedIndexText,
					repoRoot,
					cfg.Locale,
					cfg.LocaleMigration,
				); validateErr != nil {
					return &ExitError{Code: ExitInvalid, Err: validateErr}
				}
				newText = snapshot.ManagedIndexText
			} else if cfg.LocaleMigration != nil {
				return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
					"locale.header_apply.candidate_required",
				))}
			} else {
				newText, err = index.ReplaceHeader(doc.RawText, newHeader)
				if err != nil {
					return &ExitError{Code: ExitInvalid, Err: err}
				}
			}
			headerBaseline, exists, baselineLoadErr := baseline.Load(repoRoot)
			if baselineLoadErr != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"header.apply.baseline_read_failed",
						localeSafeCLIDetail(baselineLoadErr.Error()),
					)),
				}
			}
			if !exists || headerBaseline == nil {
				headerBaseline = baseline.NewBaseline(nil)
			}
			expectedIndexSHA256 := rawContentSHA256([]byte(newText))
			wroteHeader := false
			if !recoveringHeader && newText != doc.RawText {
				if recovery != nil &&
					(recovery.PreIndexSHA256 != preIndexSHA256 ||
						recovery.PostIndexSHA256 != expectedIndexSHA256) {
					return &ExitError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
						"header.apply.recovery_binding_drift",
					))}
				}
				if recovery == nil {
					recovery = &headerApplyRecovery{
						Version: 1, RunID: runID, DraftSHA256: snapshot.Hash,
						PreIndexSHA256: preIndexSHA256, PostIndexSHA256: expectedIndexSHA256,
					}
					if err := saveHeaderRecovery(repoRoot, *recovery); err != nil {
						return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
							"header.apply.recovery_save_failed",
							localeSafeCLIDetail(err.Error()),
						))}
					}
				}
				if err := writeHeaderIndex(indexPath, []byte(newText), preIndexSHA256); err != nil {
					current, hashErr := baseline.HashFile(indexPath)
					postimageWritten := hashErr == nil && current.SHA256 == expectedIndexSHA256
					preimagePreserved := hashErr == nil && current.SHA256 == preIndexSHA256
					var cleanupErr error
					if preimagePreserved {
						cleanupErr = cleanupHeaderRecovery(repoRoot, runID)
					}
					code := ExitInternal
					if errors.Is(err, afs.ErrAtomicCASConflict) {
						code = ExitInvalid
					}
					if cleanupErr != nil {
						return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
							"header.apply.write_cleanup_failed",
							localeSafeCLIDetail(err.Error()),
							localeSafeCLIDetail(cleanupErr.Error()),
						))}
					}
					if postimageWritten {
						return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
							"header.apply.cas_recovery_pending",
							localeSafeCLIDetail(err.Error()),
						))}
					}
					return &ExitError{
						Code: code,
						Err: fmt.Errorf("%s", cliMessage(
							"header.apply.write_failed",
							localeSafeCLIDetail(err.Error()),
						)),
					}
				}
				wroteHeader = true
			}

			baselineNote, baselineErr := updateHeaderIndexBaseline(
				repoRoot,
				cfg,
				indexPath,
				headerBaseline,
				expectedIndexSHA256,
			)
			if baselineErr != nil {
				ledger.Append(
					repoRoot,
					cfg.LedgerEnabled,
					ledger.Event{
						Op:         "header_apply",
						Source:     ledger.SourceHuman,
						PathsCount: 1,
						AppliedCount: func() int {
							if wroteHeader {
								return 1
							}
							return 0
						}(),
						DurationMs: time.Since(start).Milliseconds(),
						DraftRunID: runID,
						Result:     ledger.ResultError,
						FailCode:   "baseline_incomplete",
					},
				)
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"header.apply.baseline_incomplete",
						localeSafeCLIDetail(baselineErr.Error()),
					)),
				}
			}

			if manifest != nil {
				if markErr := markHeaderApplied(
					repoRoot,
					runID,
				); markErr != nil {
					ledger.Append(
						repoRoot,
						cfg.LedgerEnabled,
						ledger.Event{
							Op:         "header_apply",
							Source:     ledger.SourceHuman,
							PathsCount: 1,
							AppliedCount: func() int {
								if wroteHeader {
									return 1
								}
								return 0
							}(),
							DurationMs: time.Since(start).Milliseconds(),
							DraftRunID: runID,
							Result:     ledger.ResultError,
							FailCode:   "manifest_incomplete",
						},
					)
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"header.apply.application_incomplete",
							localeSafeCLIDetail(markErr.Error()),
						)),
					}
				}
			}
			if recovery != nil {
				if cleanupErr := cleanupHeaderRecovery(repoRoot, runID); cleanupErr != nil {
					return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
						"header.apply.recovery_cleanup_failed",
						localeSafeCLIDetail(cleanupErr.Error()),
					))}
				}
			}
			if err := ensureManagedAgentsLocale(repoRoot, cfg.Locale); err != nil {
				return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
					"locale.header_apply.agents_failed", localeSafeCLIDetail(err.Error()),
				))}
			}
			if err := config.AdvanceLocaleMigration(repoRoot, true, nil, nil); err != nil {
				return &ExitError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
					"locale.header_apply.state_failed", localeSafeCLIDetail(err.Error()),
				))}
			}

			ledger.Append(
				repoRoot,
				cfg.LedgerEnabled,
				ledger.Event{
					Op:         "header_apply",
					Source:     ledger.SourceHuman,
					PathsCount: 1,
					AppliedCount: func() int {
						if wroteHeader {
							return 1
						}
						return 0
					}(),
					RecoveredCount: func() int {
						if recoveringHeader {
							return 1
						}
						return 0
					}(),
					DurationMs: time.Since(start).Milliseconds(),
					DraftRunID: runID,
					Result:     ledger.ResultOK,
				},
			)

			if recoveringHeader {
				fmt.Fprint(output, cliMessage("header.apply.recovered", runID, cfg.IndexPath))
			} else {
				fmt.Fprint(output, cliMessage("header.apply.applied", runID, cfg.IndexPath))
			}
			fmt.Fprintln(
				output,
				baselineNote,
			)
			if wroteHeader {
				fmt.Fprintln(output, cliMessage("header.apply.backup", cfg.IndexPath))
			}
			fmt.Fprintln(output, cliMessage("header.apply.verify_next"))
			return nil
		},
	}
}

// updateHeaderIndexBaseline在Header正式写入后前移索引自身指纹。调用方已在
// 写前加载并验证Baseline；这里将成功同时绑定到预期postimage与Baseline落盘。
func updateHeaderIndexBaseline(
	repoRoot string,
	cfg *config.Config,
	indexPath string,
	currentBaseline *baseline.Baseline,
	expectedIndexSHA256 string,
) (string, error) {

	fingerprint, hashErr := baseline.HashFile(
		indexPath,
	)
	if hashErr != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"header.baseline.hash_failed",
			localeSafeCLIDetail(hashErr.Error()),
		))
	}
	if fingerprint.SHA256 != expectedIndexSHA256 {
		return "", fmt.Errorf("%s", cliMessage("header.baseline.postimage_changed"))
	}

	baseline.UpdateOne(
		currentBaseline,
		cfg.IndexPath,
		fingerprint,
	)

	if saveErr := saveHeaderBaseline(
		repoRoot,
		currentBaseline,
	); saveErr != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"header.baseline.save_failed",
			localeSafeCLIDetail(saveErr.Error()),
		))
	}
	confirmed, confirmErr := baseline.HashFile(indexPath)
	if confirmErr != nil || confirmed.SHA256 != expectedIndexSHA256 {
		if confirmErr != nil {
			return "", fmt.Errorf("%s", cliMessage(
				"header.baseline.confirm_failed",
				localeSafeCLIDetail(confirmErr.Error()),
			))
		}
		return "", fmt.Errorf("%s", cliMessage("header.baseline.changed_during_save"))
	}

	return cliMessage("header.baseline.advanced", cfg.IndexPath), nil
}

func rawContentSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
