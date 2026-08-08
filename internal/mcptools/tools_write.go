// 单条回写的规划、校验、CAS和原子提交核心。
// 索引条目: tools_write.go[MWR9AL]
//
// 分层：
//   - prepareUpdateEntry：格式、R Warning、字典、S配额、E档位与索引变换规划；
//   - planUpdateEntry：读取正式索引并形成CAS计划；
//   - commitPlan：写锁、CAS、原子写入、Baseline前移与成功Ledger。
//
// 工具注册和结果渲染位于tools_write_api.go；
// report待办能力位于tools_report.go。
package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

// UpdateOutcome是一条目标的规划或应用结果。
type UpdateOutcome struct {
	Action       string
	Rel          string
	Diff         string
	BaselineNote string
	Warnings     []string
	DryRun       bool
}

type planResult struct {
	out       *UpdateOutcome
	newText   string
	rc        *repoCtx
	indexHash string
	start     time.Time
}

var (
	writeSingleIndex   = afs.AtomicWriteCAS
	saveSingleBaseline = baseline.SaveUnderIndexLock
)

func indexTextHash(
	text string,
) string {
	sum := sha256.Sum256(
		[]byte(text),
	)

	return hex.EncodeToString(
		sum[:],
	)
}

// resultForFailCode把Fail.Code映射为Ledger终态分类。
func resultForFailCode(
	code string,
) string {
	switch code {
	case errBadArgs,
		errPathUnsafe:
		return ledger.ResultRejected

	case errWriteConflict:
		return ledger.ResultConflict

	default:
		return ledger.ResultError
	}
}

// appendWriteFailEvent记录失败路径；配置加载失败时静默跳过。
func appendWriteFailEvent(
	root,
	op,
	source,
	failCode string,
	start time.Time,
) {
	cfg, err := config.Load(
		root,
	)
	if err != nil {
		return
	}

	ledger.Append(
		root,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:         op,
			DurationMs: time.Since(start).Milliseconds(),
			Source:     source,
			Result:     resultForFailCode(failCode),
			FailCode:   failCode,
		},
	)
}

// prepareUpdateEntry在调用方提供的当前索引上下文上规划一条更新。
//
// R关系异常全部是Warning，不阻断人工或Auto Apply；检查只查询候选显式列出的
// 少量路径，不遍历仓库、不读取目标正文、不调用模型。
func prepareUpdateEntry(
	root,
	rel,
	rawEntry string,
	rc *repoCtx,
) (*UpdateOutcome, string, *Fail) {
	if rc == nil ||
		rc.doc == nil {
		return nil, "", &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.write.context_empty"),
			Hint: writeMessage("entry.write.hint.reload"),
		}
	}

	line := index.StripFences(
		rawEntry,
	)
	if rc.cfg.CognitionBudget != nil {
		if budgetViolations := cognitionbudget.ValidateEntry(line, rc.cfg.EffectiveCognitionBudget()); len(budgetViolations) > 0 {
			detail, _ := json.Marshal(budgetViolations)
			return nil, "", &Fail{Code: errCandidateInvalid,
				Msg:  writeMessage("entry.write.budget_failed", budgetViolations[0].Code, string(detail)),
				Hint: writeMessage("entry.write.hint.budget_reauthor")}
		}
	}

	headerText, _ := index.ExtractHeader(
		rc.text,
	)

	sQuota := index.ExtractSQuotaThresholds(
		headerText,
	)

	violations := index.ValidateEntryLineWith(
		rel,
		line,
		sQuota,
	)

	if index.HasError(
		violations,
	) {
		messages := []string{}

		for _, violation := range violations {
			messages = append(
				messages,
				"["+violation.Level+"] "+
					localeSafeWriteDetail(violation.Msg),
			)
		}

		return nil, "", &Fail{
			Code: errBadArgs,
			Msg: writeMessage(
				"entry.write.validation_failed",
				strings.Join(
					messages,
					"\n",
				),
			),
			Hint: writeMessage("entry.write.hint.validation"),
		}
	}

	warnings := []string{}

	for _, violation := range violations {
		if violation.Level ==
			index.LevelWarning {
			warnings = append(
				warnings,
				localeSafeWriteDetail(violation.Msg),
			)
		}
	}

	for _, violation := range index.ValidateEntryRelations(
		root,
		rel,
		line,
	) {
		warnings = append(
			warnings,
			localeSafeWriteDetail(violation.Msg),
		)
	}

	if dictionary := index.ExtractTagDict(
		headerText,
	); dictionary != nil {
		if violation := index.CheckTagsAgainstDict(
			line,
			dictionary,
		); violation != nil {
			return nil, "", &Fail{
				Code: errBadArgs,
				Msg: writeMessage(
					"entry.write.dictionary_failed",
					localeSafeWriteDetail(violation.Msg),
				),
				Hint: writeMessage("entry.write.hint.dictionary"),
			}
		}
	}

	// 段内条目使用裸文件名；完整相对路径可做无歧义归一。
	if filenameEnd := strings.Index(
		line,
		"[",
	); filenameEnd > 0 {
		filename := strings.TrimSpace(
			line[:filenameEnd],
		)

		if strings.Contains(
			filename,
			"/",
		) &&
			filename == rel {
			line = path.Base(rel) +
				line[filenameEnd:]

			warnings = append(warnings, writeMessage("entry.write.warning.normalized_filename"))
		}
	}

	if index.ShouldCheckEScalePath(
		rel,
	) &&
		strings.TrimSpace(
			headerText,
		) != "" {
		thresholds := index.ExtractEScaleThresholds(
			headerText,
		)

		if thresholds.HasThresholds() {
			absolutePath := filepath.Join(
				root,
				filepath.FromSlash(rel),
			)

			if stat, statErr := os.Stat(
				absolutePath,
			); statErr == nil &&
				!stat.IsDir() {
				if lines, countErr :=
					afs.CountFileLines(
						absolutePath,
					); countErr == nil {
					if violation := index.CheckEScale(
						line,
						lines,
						thresholds,
					); violation != nil {
						warnings = append(
							warnings,
							violation.Msg,
						)
					}
				}
			}
		}
	}

	oldEntry := index.FindEntry(
		rc.doc,
		rel,
	)

	action := ""
	oldLine := ""
	newText := ""
	var transformErr error

	if oldEntry != nil {
		action = writeMessage("entry.write.action.replace")
		oldLine = oldEntry.FullLine

		newText, transformErr =
			index.ReplaceEntryForPath(
				rc.text,
				root,
				rel,
				oldEntry.FullLine,
				line,
			)

		if transformErr != nil {
			return nil, "", &Fail{
				Code: errWriteConflict,
				Msg:  transformErr.Error(),
				Hint: writeMessage("entry.write.hint.refresh_entry"),
			}
		}
	} else {
		action = writeMessage("entry.write.action.insert")

		newText, transformErr =
			index.InsertEntry(
				rc.text,
				rel,
				line,
				root,
			)

		if transformErr != nil {
			return nil, "", &Fail{
				Code: errBadArgs,
				Msg:  transformErr.Error(),
			}
		}
	}

	return &UpdateOutcome{
		Action: action,
		Rel:    rel,
		Diff: renderEntryWriteDiff(
			oldLine,
			line,
		),
		Warnings: warnings,
	}, newText, nil
}

func renderEntryWriteDiff(oldLine, newLine string) string {
	if strings.TrimSpace(oldLine) == "" {
		return "+ " + newLine + "\n" + writeMessage("entry.write.diff.insert_note") + "\n"
	}
	return index.RenderEntryDiff(
		oldLine,
		newLine,
		writeMessage("entry.write.diff_new"),
	)
}

func planUpdateEntry(
	root,
	rawPath,
	rawEntry string,
) (*planResult, *Fail) {
	start := time.Now()
	if fail := validateEntryWriteMessages(); fail != nil {
		return nil, fail
	}

	rel, err := afs.NormalizeRelPath(
		rawPath,
	)
	if err != nil {
		return nil, &Fail{
			Code: errPathUnsafe,
			Msg:  writeMessage("entry.write.path_rejected", rawPath, localeSafeWriteDetail(err.Error())),
			Hint: writeMessage("entry.write.hint.relative_path"),
		}
	}

	rc, fail := loadRepoCtx(
		root,
	)
	if fail != nil {
		return nil, fail
	}
	managedWriteState, targetFail := loadManagedWriteState(root, rc)
	if targetFail != nil {
		return nil, targetFail
	}
	if targetFail := validateManagedEntryTarget(managedWriteState, rel); targetFail != nil {
		return nil, targetFail
	}
	outcome, newText, fail :=
		prepareUpdateEntry(
			root,
			rel,
			rawEntry,
			rc,
		)

	if fail != nil {
		return nil, fail
	}
	if budgetFail := validateProjectedEntryBudget(root, rc, newText); budgetFail != nil {
		return nil, budgetFail
	}

	return &planResult{
		out:       outcome,
		newText:   newText,
		rc:        rc,
		indexHash: indexTextHash(rc.text),
		start:     start,
	}, nil
}

func loadManagedWriteState(root string, rc *repoCtx) (*managedstate.State, *Fail) {
	if rc == nil || rc.cfg == nil || (rc.cfg.ManagedScope == nil && rc.cfg.CognitionBudget == nil) {
		return nil, nil
	}
	state, err := managedstate.Load(root, rc.cfg)
	if err != nil {
		return nil, &Fail{Code: errCandidateInvalid, Msg: "managed_scope_state_invalid: " + localeSafeWriteDetail(err.Error())}
	}
	return state, nil
}

func validateManagedEntryTarget(state *managedstate.State, rel string) *Fail {
	if state == nil {
		return nil
	}
	if state.ScopeChangeRequired {
		return &Fail{Code: errCandidateInvalid, Msg: "scope_change_required"}
	}
	if state.Legacy || state.Evaluation == nil {
		return nil
	}
	for _, item := range state.Evaluation.Index {
		if item.Path == rel {
			return nil
		}
	}
	return &Fail{Code: errCandidateInvalid, Msg: "managed_scope_target_not_index: " + rel}
}

func asManagedIndexFingerprint(state *baseline.Baseline, fingerprint baseline.Fingerprint) baseline.Fingerprint {
	if state != nil && state.ManagedScope != nil {
		fingerprint.Role = machinecontract.ScopeRoleIndex
	}
	return fingerprint
}

func validateProjectedEntryBudget(root string, rc *repoCtx, projected string) *Fail {
	if rc == nil || rc.cfg == nil || rc.cfg.CognitionBudget == nil {
		return nil
	}
	policy := rc.cfg.EffectiveCognitionBudget()
	projection, err := cognitionbudget.ValidateProjected(root, []byte(rc.text), []byte(projected), policy)
	if err != nil {
		return &Fail{Code: errCandidateInvalid, Msg: writeMessage("entry.write.budget_projection_failed", localeSafeWriteDetail(err.Error())),
			Hint: writeMessage("entry.write.hint.budget_reauthor")}
	}
	wholeExceeded := projection.ProjectedWholeIndexTokens > projection.MaxTokens
	if !wholeExceeded && (policy.Mode != machinecontract.BudgetModeEnforce || projection.Allowed) {
		return nil
	}
	detail, _ := json.Marshal(projection)
	code := "whole_index_budget_exceeded"
	if !wholeExceeded && len(projection.Violations) > 0 {
		code = projection.Violations[0].Code
	}
	return &Fail{Code: errCandidateInvalid, Msg: writeMessage("entry.write.budget_failed", code, string(detail)),
		Hint: writeMessage("entry.write.hint.budget_reauthor")}
}

func commitPlan(
	root,
	source string,
	plan *planResult,
) *Fail {
	lock, lockErr := afs.AcquireIndexLock(
		root,
	)

	if lockErr != nil {
		if errors.Is(
			lockErr,
			afs.ErrLockTimeout,
		) {
			return &Fail{
				Code: errWriteConflict,
				Msg:  writeMessage("entry.write.lock_timeout", localeSafeWriteDetail(lockErr.Error())),
				Hint: writeMessage("entry.write.hint.lock_timeout"),
			}
		}

		return &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.write.lock_failed", localeSafeWriteDetail(lockErr.Error())),
			Hint: writeMessage("entry.write.hint.lock_failed"),
		}
	}

	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			fmt.Fprintln(os.Stderr, writeMessage(
				"entry.write.lock_release_warning",
				localeSafeWriteDetail(releaseErr.Error()),
			))
		}
	}()
	if transactionFail := pendingHeaderTransactionFail(root); transactionFail != nil {
		return transactionFail
	}

	currentText, readErr := os.ReadFile(
		plan.rc.paths.IndexPath,
	)
	if readErr != nil {
		return &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.write.cas_read_failed", localeSafeWriteDetail(readErr.Error())),
			Hint: writeMessage("entry.write.hint.check_index"),
		}
	}

	if indexTextHash(
		string(currentText),
	) != plan.indexHash {
		return &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.write.cas_stale"),
			Hint: writeMessage("entry.write.hint.replan"),
		}
	}
	baselineState, baselineExists, baselineErr := baseline.Load(root)
	if baselineErr != nil {
		return &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.write.baseline_read_failed", localeSafeWriteDetail(baselineErr.Error())),
			Hint: writeMessage("entry.write.hint.baseline_read"),
		}
	}
	if !baselineExists || baselineState == nil {
		baselineState = baseline.NewBaseline(nil)
	}

	if writeErr := writeSingleIndex(
		plan.rc.paths.IndexPath,
		[]byte(plan.newText),
		plan.indexHash,
	); writeErr != nil {
		code := errInternal
		if errors.Is(writeErr, afs.ErrAtomicCASConflict) {
			code = errWriteConflict
		}
		return &Fail{
			Code: code,
			Msg:  writeMessage("entry.write.index_write_failed", localeSafeWriteDetail(writeErr.Error())),
			Hint: writeMessage("entry.write.hint.disk_permissions"),
		}
	}
	expectedIndexSHA256 := indexTextHash(plan.newText)
	writtenIndex, writtenIndexErr := baseline.HashFile(plan.rc.paths.IndexPath)
	if writtenIndexErr != nil || writtenIndex.SHA256 != expectedIndexSHA256 {
		return &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.write.postimage_unconfirmed"),
			Hint: writeMessage("entry.write.hint.external_index"),
		}
	}

	targetMoved := false

	targetPath := filepath.Join(
		root,
		filepath.FromSlash(
			plan.out.Rel,
		),
	)

	if fingerprint, hashErr :=
		baseline.HashFile(
			targetPath,
		); hashErr == nil {
		fingerprint = asManagedIndexFingerprint(baselineState, fingerprint)
		baseline.UpdateOne(
			baselineState,
			plan.out.Rel,
			fingerprint,
		)

		targetMoved = true
	}

	indexMoved := false

	if fingerprint, hashErr :=
		baseline.HashFile(plan.rc.paths.IndexPath); hashErr == nil &&
		fingerprint.SHA256 == expectedIndexSHA256 {
		fingerprint = asManagedIndexFingerprint(baselineState, fingerprint)
		baseline.UpdateOne(
			baselineState,
			plan.rc.cfg.IndexPath,
			fingerprint,
		)

		indexMoved = true
	}

	switch {
	case !targetMoved &&
		!indexMoved:
		return &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.write.hash_unavailable"),
			Hint: writeMessage("entry.write.hint.retry_same_candidate"),
		}

	default:
		if saveErr := saveSingleBaseline(
			root,
			baselineState,
		); saveErr != nil {
			return &Fail{
				Code: errInternal,
				Msg:  writeMessage("entry.write.baseline_save_failed", localeSafeWriteDetail(saveErr.Error())),
				Hint: writeMessage("entry.write.hint.retry_same_candidate"),
			}
		} else if targetMoved {
			plan.out.BaselineNote =
				writeMessage("entry.write.baseline_advanced_target")
		} else {
			plan.out.BaselineNote =
				writeMessage("entry.write.baseline_advanced_index")
		}
	}
	confirmedIndex, confirmIndexErr := baseline.HashFile(plan.rc.paths.IndexPath)
	if confirmIndexErr != nil || confirmedIndex.SHA256 != expectedIndexSHA256 {
		return &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.write.baseline_postimage_changed"),
			Hint: writeMessage("entry.write.hint.baseline_postimage_changed"),
		}
	}

	ledger.Append(
		root,
		plan.rc.cfg.LedgerEnabled,
		ledger.Event{
			Op:         "update_entry",
			PathsCount: 1,
			DurationMs: time.Since(
				plan.start,
			).Milliseconds(),
			Source: source,
			Result: ledger.ResultOK,
		},
	)

	return nil
}
