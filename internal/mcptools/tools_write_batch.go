// D69-2A 原子批量条目回写。
//
// 索引条目: tools_write_batch.go[MWR9AM]
//
// 与人工 entries apply 的差异:
//   - 人工管线保留逐条部分成功语义；
//   - 本管线最初为无人值守 auto 设计,现同时服务人工 Entries Apply 与
//     Host-Agent 治理链(整批复用): 全部目标先在同一份内存索引上完成规划,
//     任一目标失败即零写入；全部通过后取一次写锁、做一次 CAS、AtomicWrite 一次。
//
// 原子边界是正式索引文件：要么整批新文本一次落盘，要么正式索引零变化。
// 基线与 ledger 是写后副产品，失败只形成提示，不回滚已成功的索引原子写入。
//
// 失败路径落账(R60-F.9-A1,2026-07-18): 与单条管线同款 —— 规划拒绝与
// CAS/锁冲突经 appendWriteFailEvent 落账(op=update_entries_batch),
// 计时提升到 ApplyUpdateEntriesAtomic 顶层;dry-run 纯读不落账。
package mcptools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// 测试只在明确的落盘边界注入中断或并发变化；生产路径始终指向原子实现。
var (
	writeAtomicIndex   = afs.AtomicWriteCAS
	saveAtomicBaseline = baseline.SaveUnderIndexLock
)

// AtomicUpdateItem 是原子批量回写的一条输入。
type AtomicUpdateItem struct {
	Path         string
	ObjectRef    string
	NewEntry     string
	SourceSHA256 string
	CandidateID  string
	BatchID      string
}

// AtomicBatchOutcome 是整批成功规划或应用结果。
type AtomicBatchOutcome struct {
	Items            []*UpdateOutcome
	Volume           string
	Volumes          []string
	BaselineNote     string
	BaselineComplete bool
	DryRun           bool
	AlreadyApplied   bool
	AppliedCount     int
	RecoveredCount   int
}

func (plan *atomicBatchPlan) volumeID() string {
	if plan != nil && plan.changeEnvelope != nil && len(plan.changeEnvelope.Volumes) == 1 {
		return plan.changeEnvelope.Volumes[0]
	}
	return ""
}

func (plan *atomicBatchPlan) volumeIDs() []string {
	if plan != nil && plan.changeEnvelope != nil {
		return append([]string{}, plan.changeEnvelope.Volumes...)
	}
	return nil
}

type normalizedAtomicItem struct {
	rel                    string
	objectRef              string
	newEntry               string
	sourceSHA256           string
	candidateID            string
	batchID                string
	originalCandidateIndex int
}

type atomicBatchPlan struct {
	outcomes        []*UpdateOutcome
	finalText       string
	rels            []string
	normalizedItems []normalizedAtomicItem
	rc              *repoCtx
	indexHash       string
	// indexSourceMismatch is retained through planning because an Entries
	// batch may manage the index file itself. The mismatch is accepted only
	// when a durable transaction proves that the current file is this exact
	// batch's postimage; every ordinary or unproven stale binding still fails.
	indexSourceMismatch bool
	changeEnvelope      *cognitionChangeEnvelope
	volumePlan          *cognitionVolumeBatchPlan
	batchKey            string
	start               time.Time
}

// mismatchedBoundSources在正式副作用边界复核模型候选绑定的源码。
// 读取失败与摘要变化都视为漂移，确保旧指纹不会洗白并发写入。
func mismatchedBoundSources(
	root string,
	items []normalizedAtomicItem,
	indexPath string,
) []string {
	drifted := []string{}
	for _, item := range items {
		if item.sourceSHA256 == "" {
			continue
		}
		// The formal index is both the transaction target and, when indexed,
		// a managed source. Its preimage binding is checked before the write;
		// after the write the expected postimage checks below are authoritative.
		if item.rel == indexPath {
			continue
		}
		fingerprint, err := baseline.HashFile(filepath.Join(
			root,
			filepath.FromSlash(item.rel),
		))
		if err != nil || fingerprint.SHA256 != item.sourceSHA256 {
			drifted = append(drifted, item.rel)
		}
	}
	return drifted
}

func mergeDriftedPaths(existing []string, more ...string) []string {
	seen := make(map[string]bool, len(existing)+len(more))
	merged := make([]string, 0, len(existing)+len(more))
	for _, rel := range append(append([]string{}, existing...), more...) {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		merged = append(merged, rel)
	}
	return merged
}

// planUpdateEntriesAtomic 在同一初始索引版本上规划整个批次。
// 每条成功变换后重新解析内存文本，下一条基于前序结果继续规划。
func planUpdateEntriesAtomic(
	root string,
	items []AtomicUpdateItem,
) (*atomicBatchPlan, *Fail) {
	start := time.Now()
	if fail := validateEntryWriteMessages(); fail != nil {
		return nil, fail
	}

	if len(items) == 0 {
		return nil, &Fail{
			Code: errBadArgs,
			Msg:  writeMessage("entry.batch.empty"),
		}
	}

	normalized := make([]normalizedAtomicItem, 0, len(items))
	seen := map[string]int{}
	duplicateFindings := []cognition.RepairFinding{}

	for indexPosition, item := range items {
		objectRef := strings.TrimSpace(item.ObjectRef)
		if objectRef != item.ObjectRef || (item.Path == "") == (objectRef == "") {
			return nil, &Fail{
				Code: errBadArgs,
				Msg: writeMessage(
					"entry.batch.path_invalid",
					indexPosition+1,
					item.Path+objectRef,
					"provide exactly one path or object_ref",
				),
				Hint: writeMessage("entry.batch.hint.paths_relative"),
			}
		}
		rel := ""
		if item.Path != "" {
			var err error
			rel, err = afs.NormalizeRelPath(item.Path)
			if err != nil {
				return nil, &Fail{Code: errPathUnsafe, Msg: writeMessage("entry.batch.path_invalid", indexPosition+1, item.Path, localeSafeWriteDetail(err.Error())), Hint: writeMessage("entry.batch.hint.paths_relative")}
			}
		}
		identity := "path:" + rel
		if objectRef != "" {
			identity = "object:" + objectRef
		}
		if previousIndex := seen[identity]; previousIndex != 0 {
			canonicalIdentity := objectRef
			domain := "database"
			path := ""
			if rel != "" {
				canonicalIdentity = "code:" + rel
				domain = "code"
				path = rel
			}
			duplicateFindings = append(duplicateFindings, cognition.RepairFinding{
				CandidateIndex:          indexPosition + 1,
				Path:                    path,
				CanonicalObjectIdentity: canonicalIdentity,
				Domain:                  domain,
				Field:                   "canonical_object_identity",
				RuleCode:                "impact_candidate_duplicate",
				Expected:                "candidate_count=1",
				Actual:                  "candidate_count=2",
				Cause:                   fmt.Sprintf("candidate target is already claimed by candidate %d", previousIndex),
				Code:                    "impact_candidate_duplicate",
				ObjectRef:               canonicalIdentity,
			})
		} else {
			seen[identity] = indexPosition + 1
		}
		normalized = append(normalized, normalizedAtomicItem{
			rel:                    rel,
			objectRef:              objectRef,
			newEntry:               item.NewEntry,
			sourceSHA256:           strings.ToLower(strings.TrimSpace(item.SourceSHA256)),
			candidateID:            strings.ToLower(strings.TrimSpace(item.CandidateID)),
			batchID:                strings.ToLower(strings.TrimSpace(item.BatchID)),
			originalCandidateIndex: indexPosition + 1,
		})
	}

	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return nil, fail
	}
	if len(duplicateFindings) > 0 {
		for index := range duplicateFindings {
			if duplicateFindings[index].Domain == "database" {
				if asset := loaded.set.Volumes["database"]; asset != nil {
					duplicateFindings[index].Path = asset.Descriptor.Path
				}
			}
		}
		duplicateFindings = LocalizeRepairFindings(duplicateFindings)
		duplicateTarget := duplicateFindings[0].CanonicalObjectIdentity
		if duplicateFindings[0].Domain == "code" {
			duplicateTarget = strings.TrimPrefix(duplicateTarget, "code:")
		}
		return nil, &Fail{
			Code:       errBadArgs,
			Msg:        writeMessage("entry.batch.duplicate_path", duplicateTarget),
			Hint:       writeMessage("entry.batch.hint.unique_paths"),
			Findings:   duplicateFindings,
			Repairable: true,
		}
	}
	var initialContext *repoCtx
	var changeEnvelope *cognitionChangeEnvelope
	if loaded.set.LayoutMode == cognition.LayoutLegacyMonolithic {
		for _, item := range normalized {
			if item.objectRef != "" {
				return nil, &Fail{Code: errBadArgs, Msg: writeMessage("entry.batch.path_invalid", 1, item.objectRef, "Legacy updates require repository-relative paths")}
			}
		}
		initialContext = loaded.legacyRepo()
	} else {
		return planCognitionVolumeUpdates(root, loaded, normalized, start)
	}

	workingText := initialContext.text
	workingDocument := initialContext.doc
	outcomes := make([]*UpdateOutcome, 0, len(normalized))
	rels := make([]string, 0, len(normalized))
	indexSourceMismatch := false
	managedWriteState, targetFail := loadManagedWriteState(root, initialContext)
	if targetFail != nil {
		return nil, targetFail
	}

	for itemIndex, item := range normalized {
		if targetFail := validateManagedEntryTarget(managedWriteState, item.rel); targetFail != nil {
			return nil, targetFail
		}
		if item.sourceSHA256 != "" {
			current, hashErr := baseline.HashFile(filepath.Join(
				root,
				filepath.FromSlash(item.rel),
			))
			if hashErr != nil {
				return nil, &Fail{
					Code: errInternal,
					Msg:  writeMessage("entry.batch.source_hash_failed", item.rel, localeSafeWriteDetail(hashErr.Error())),
				}
			}
			if current.SHA256 != item.sourceSHA256 {
				if item.rel == initialContext.cfg.IndexPath {
					indexSourceMismatch = true
				} else {
					return nil, &Fail{
						Code: errWriteConflict,
						Msg:  writeMessage("entry.batch.source_cas_conflict", item.rel),
						Hint: writeMessage("entry.batch.hint.refresh_binding"),
					}
				}
			}
		}
		workingContext := *initialContext
		workingContext.text = workingText
		workingContext.doc = workingDocument

		outcome, nextText, itemFail := prepareUpdateEntry(
			root,
			item.rel,
			item.newEntry,
			&workingContext,
		)
		if itemFail != nil {
			for findingIndex := range itemFail.Findings {
				if itemFail.Findings[findingIndex].CandidateIndex == 0 {
					itemFail.Findings[findingIndex].CandidateIndex = item.originalCandidateIndex
				}
			}
			itemFail.Msg = writeMessage(
				"entry.batch.item_plan_failed",
				itemIndex+1,
				len(normalized),
				item.rel,
				itemFail.Msg,
			)
			return nil, itemFail
		}

		nextDocument, _ := index.Parse(nextText)
		if nextDocument == nil || len(nextDocument.Sections) == 0 {
			return nil, &Fail{
				Code: errInternal,
				Msg:  writeMessage("entry.batch.reparse_failed", itemIndex+1),
				Hint: writeMessage("entry.batch.hint.inspect_structure"),
			}
		}
		index.ResolveRelPaths(nextDocument, root)
		if changeEnvelope != nil {
			if findings := cognition.ValidateProjectedCodeVolume(
				loaded.set,
				[]byte(nextText),
			); len(findings) > 0 {
				return nil, &Fail{
					Code: errImpactResolutionFailed,
					Msg: writeMessage(
						"entry.volume.projected_invalid",
						findings[0].Code,
					),
					Hint: writeMessage("entry.volume.hint.regenerate_candidate"),
				}
			}
		}

		outcomes = append(outcomes, outcome)
		rels = append(rels, item.rel)
		workingText = nextText
		workingDocument = nextDocument
	}
	if budgetFail := validateProjectedEntryBudget(root, initialContext, workingText); budgetFail != nil {
		return nil, budgetFail
	}

	return &atomicBatchPlan{
		outcomes:            outcomes,
		finalText:           workingText,
		rels:                rels,
		normalizedItems:     normalized,
		rc:                  initialContext,
		indexHash:           indexTextHash(initialContext.text),
		indexSourceMismatch: indexSourceMismatch,
		changeEnvelope:      changeEnvelope,
		batchKey:            atomicBatchKey(normalized),
		start:               start,
	}, nil
}

// commitAtomicBatch 在一个写锁临界区内执行一次 CAS 和一次 AtomicWrite。
func commitAtomicBatch(
	root,
	source string,
	plan *atomicBatchPlan,
	retainRecovery bool,
) (string, bool, *Fail) {
	lock, lockErr := afs.AcquireIndexLock(root)
	if lockErr != nil {
		if errors.Is(lockErr, afs.ErrLockTimeout) {
			return "", false, &Fail{
				Code: errWriteConflict,
				Msg:  writeMessage("entry.batch.lock_timeout", localeSafeWriteDetail(lockErr.Error())),
				Hint: writeMessage("entry.batch.hint.lock_timeout"),
			}
		}
		return "", false, &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.batch.lock_failed", localeSafeWriteDetail(lockErr.Error())),
			Hint: writeMessage("entry.write.hint.lock_failed"),
		}
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			fmt.Fprintln(os.Stderr, writeMessage(
				"entry.batch.lock_release_warning",
				localeSafeWriteDetail(releaseErr.Error()),
			))
		}
	}()
	if transactionFail := pendingHeaderTransactionFail(root); transactionFail != nil {
		return "", false, transactionFail
	}
	if plan.volumePlan != nil {
		return commitCognitionVolumeBatchLocked(root, source, plan, retainRecovery)
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return "", false, &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.volume.guard_stale", guardID),
			Hint: writeMessage("entry.batch.hint.replan"),
		}
	}

	currentText, readErr := os.ReadFile(plan.rc.paths.IndexPath)
	if readErr != nil {
		return "", false, &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.batch.cas_read_failed", localeSafeWriteDetail(readErr.Error())),
		}
	}
	if indexTextHash(string(currentText)) != plan.indexHash {
		return "", false, &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.batch.cas_stale"),
			Hint: writeMessage("entry.batch.hint.replan"),
		}
	}

	// Baseline损坏是需要人工核对的外部状态，不得在索引写入后
	// 才发现并用一份仅含本批目标的新Baseline覆盖它。
	baselineState, baselineExists, baselineErr := baseline.Load(root)
	if baselineErr != nil {
		return "", false, &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.batch.baseline_read_failed", localeSafeWriteDetail(baselineErr.Error())),
			Hint: writeMessage("entry.batch.hint.baseline_read"),
		}
	}
	if !baselineExists || baselineState == nil {
		baselineState = baseline.NewBaseline(nil)
	}
	boundFingerprints := make(map[string]baseline.Fingerprint, len(plan.normalizedItems))
	for _, item := range plan.normalizedItems {
		if item.sourceSHA256 == "" {
			continue
		}
		fingerprint, hashErr := baseline.HashFile(filepath.Join(
			root,
			filepath.FromSlash(item.rel),
		))
		if hashErr != nil || fingerprint.SHA256 != item.sourceSHA256 {
			return "", false, &Fail{
				Code: errWriteConflict,
				Msg:  writeMessage("entry.batch.prewrite_source_conflict", item.rel),
				Hint: writeMessage("entry.batch.hint.refresh_binding"),
			}
		}
		boundFingerprints[item.rel] = fingerprint
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return "", false, &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.volume.guard_stale", guardID),
			Hint: writeMessage("entry.batch.hint.replan"),
		}
	}

	expectedIndexSHA256 := indexTextHash(plan.finalText)
	if recoveryErr := saveAtomicBatchRecovery(root, atomicBatchRecovery{
		Version: 1, BatchKey: plan.batchKey,
		PreIndexSHA256: plan.indexHash, PostIndexSHA256: expectedIndexSHA256,
	}); recoveryErr != nil {
		return "", false, &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.batch.recovery_save_failed", localeSafeWriteDetail(recoveryErr.Error())),
		}
	}
	if writeErr := writeAtomicIndex(
		plan.rc.paths.IndexPath,
		[]byte(plan.finalText),
		plan.indexHash,
	); writeErr != nil {
		current, hashErr := baseline.HashFile(plan.rc.paths.IndexPath)
		if hashErr == nil && current.SHA256 == plan.indexHash {
			if cleanupErr := cleanupAtomicBatchRecovery(root, plan.batchKey); cleanupErr != nil {
				return "", false, &Fail{
					Code: errInternal,
					Msg:  writeMessage("entry.batch.recovery_cleanup_preimage_failed", localeSafeWriteDetail(cleanupErr.Error())),
				}
			}
		}
		if hashErr == nil && current.SHA256 == expectedIndexSHA256 {
			return writeMessage("entry.batch.postimage_recovery_pending"), false, nil
		}
		code := errInternal
		if errors.Is(writeErr, afs.ErrAtomicCASConflict) {
			code = errWriteConflict
		}
		return "", false, &Fail{
			Code: code,
			Msg:  writeMessage("entry.batch.index_write_failed", localeSafeWriteDetail(writeErr.Error())),
			Hint: writeMessage("entry.write.hint.disk_permissions"),
		}
	}
	writtenIndex, writtenIndexErr := baseline.HashFile(plan.rc.paths.IndexPath)
	if writtenIndexErr != nil || writtenIndex.SHA256 != expectedIndexSHA256 {
		note := writeMessage("entry.batch.postimage_unconfirmed")
		if writtenIndexErr != nil {
			note = writeMessage(
				"entry.batch.postimage_unconfirmed_detail",
				localeSafeWriteDetail(writtenIndexErr.Error()),
			)
		}
		ledger.Append(root, plan.rc.cfg.LedgerEnabled, ledger.Event{
			Op: "update_entries_batch", PathsCount: len(plan.rels),
			DurationMs: time.Since(plan.start).Milliseconds(), Source: source,
			Result: ledger.ResultError, AppliedCount: len(plan.rels),
		})
		return note, false, nil
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		note := writeMessage("entry.volume.guard_changed_after_write", guardID)
		ledger.Append(root, plan.rc.cfg.LedgerEnabled, ledger.Event{
			Op: "update_entries_batch", PathsCount: len(plan.rels),
			DurationMs: time.Since(plan.start).Milliseconds(), Source: source,
			Result: ledger.ResultError, AppliedCount: len(plan.rels),
		})
		return note, false, nil
	}

	// 索引锁只保护AOCI资产，不能阻止其他进程修改源码。索引写入后必须
	// 再核对绑定，漂移目标不前移Baseline；最终返回stopped而非伪成功。
	sourceDrift := mismatchedBoundSources(
		root, plan.normalizedItems, plan.rc.cfg.IndexPath,
	)
	drifted := make(map[string]bool, len(sourceDrift))
	for _, rel := range sourceDrift {
		drifted[rel] = true
	}
	targetsMoved := 0
	for _, rel := range plan.rels {
		if drifted[rel] {
			continue
		}
		fingerprint, bound := boundFingerprints[rel]
		if !bound {
			targetPath := filepath.Join(
				root,
				filepath.FromSlash(rel),
			)
			var hashErr error
			fingerprint, hashErr = baseline.HashFile(targetPath)
			if hashErr != nil {
				continue
			}
		}
		fingerprint = asManagedIndexFingerprint(baselineState, fingerprint)
		baseline.UpdateOne(
			baselineState,
			rel,
			fingerprint,
		)
		targetsMoved++
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

	baselineComplete := targetsMoved == len(plan.rels) && indexMoved
	var saveErr error
	if targetsMoved == 0 && !indexMoved {
		baselineComplete = false
	} else if saveErr = saveAtomicBaseline(
		root,
		baselineState,
	); saveErr != nil {
		baselineComplete = false
	}

	// 覆盖“核对后、Baseline保存时”发生的并发变化。此时已保存的仍是
	// 旧绑定，因此漂移会保持可见；这里只把结果降级为未完成。
	sourceDrift = mergeDriftedPaths(
		sourceDrift,
		mismatchedBoundSources(
			root, plan.normalizedItems, plan.rc.cfg.IndexPath,
		)...,
	)
	if len(sourceDrift) > 0 {
		baselineComplete = false
	}
	baselineNote := ""
	confirmedIndex, confirmIndexErr := baseline.HashFile(plan.rc.paths.IndexPath)
	if confirmIndexErr != nil || confirmedIndex.SHA256 != expectedIndexSHA256 {
		baselineComplete = false
		if saveErr == nil {
			baselineNote = writeMessage("entry.batch.baseline_postimage_changed")
		}
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		baselineComplete = false
		baselineNote = writeMessage("entry.volume.guard_changed_after_write", guardID)
	}

	if baselineNote == "" {
		if saveErr != nil {
			baselineNote = writeMessage(
				"entry.batch.baseline_save_failed",
				localeSafeWriteDetail(saveErr.Error()),
			)
		} else if len(sourceDrift) > 0 {
			baselineNote = writeMessage(
				"entry.batch.source_drift",
				strings.Join(sourceDrift, ", "),
			)
		} else if targetsMoved == 0 && !indexMoved {
			baselineNote = writeMessage("entry.batch.baseline_unchanged")
		} else if !baselineComplete {
			baselineNote = writeMessage(
				"entry.batch.baseline_partial",
				targetsMoved,
				len(plan.rels),
				indexMoved,
			)
		} else {
			baselineNote = writeMessage(
				"entry.batch.baseline_advanced",
				targetsMoved,
				len(plan.rels),
				indexMoved,
			)
		}
	}
	if baselineComplete {
		if _, receiptErr := saveCompletedEntriesGovernanceReceipt(root, plan, nil); receiptErr != nil {
			baselineComplete = false
			baselineNote = mcpMessage(
				"entries.governance_receipt.persist_failed",
				localeSafeWriteDetail(receiptErr.Error()),
			)
		}
	}

	ledgerResult := ledger.ResultOK
	if !baselineComplete {
		ledgerResult = ledger.ResultError
	}
	ledger.Append(root, plan.rc.cfg.LedgerEnabled, ledger.Event{
		Op:           "update_entries_batch",
		PathsCount:   len(plan.rels),
		DurationMs:   time.Since(plan.start).Milliseconds(),
		Source:       source,
		Result:       ledgerResult,
		AppliedCount: len(plan.rels),
	})
	if baselineComplete && !retainRecovery {
		if cleanupErr := cleanupAtomicBatchRecovery(root, plan.batchKey); cleanupErr != nil {
			return writeMessage(
				"entry.batch.recovery_cleanup_completed_failed",
				localeSafeWriteDetail(cleanupErr.Error()),
			), false, nil
		}
	}

	return baselineNote, baselineComplete, nil
}

// reconcileAlreadyAppliedBaseline处理“索引已写入，Baseline曾中断”。
// 它在同一索引锁下重验CAS，只补齐不匹配指纹；完整重试为零写入。
func reconcileAlreadyAppliedBaseline(
	root,
	source string,
	plan *atomicBatchPlan,
) (string, bool, *Fail) {
	lock, lockErr := afs.AcquireIndexLock(root)
	if lockErr != nil {
		code := errInternal
		if errors.Is(lockErr, afs.ErrLockTimeout) {
			code = errWriteConflict
		}
		return "", false, &Fail{Code: code, Msg: writeMessage(
			"entry.batch.reconcile_lock_failed", localeSafeWriteDetail(lockErr.Error()),
		)}
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			fmt.Fprintln(os.Stderr, writeMessage(
				"entry.batch.reconcile_lock_release_warning",
				localeSafeWriteDetail(releaseErr.Error()),
			))
		}
	}()
	if transactionFail := pendingHeaderTransactionFail(root); transactionFail != nil {
		return "", false, transactionFail
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return "", false, &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.volume.guard_stale", guardID),
			Hint: writeMessage("entry.batch.hint.replan"),
		}
	}

	currentText, readErr := os.ReadFile(plan.rc.paths.IndexPath)
	if readErr != nil || indexTextHash(string(currentText)) != plan.indexHash {
		return "", false, &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.batch.reconcile_index_changed"),
			Hint: writeMessage("entry.batch.hint.replan"),
		}
	}
	expectedIndexSHA256 := plan.indexHash
	var recovery *atomicBatchRecovery
	if candidate, recoveryErr := loadAtomicBatchRecovery(root, plan.batchKey); recoveryErr == nil {
		if candidate.PostIndexSHA256 != expectedIndexSHA256 {
			return "", false, &Fail{
				Code: errWriteConflict,
				Msg:  mcpMessage("entries.recovery_receipt.postimage_mismatch"),
			}
		}
		recovery = candidate
	} else if !os.IsNotExist(recoveryErr) {
		return "", false, &Fail{Code: errInternal, Msg: mcpMessage(
			"entries.recovery_receipt.invalid", localeSafeWriteDetail(recoveryErr.Error()),
		)}
	}

	baselineState, exists, loadErr := baseline.Load(root)
	if loadErr != nil {
		return "", false, &Fail{Code: errInternal, Msg: writeMessage(
			"entry.batch.reconcile_baseline_read_failed", localeSafeWriteDetail(loadErr.Error()),
		)}
	}
	if !exists || baselineState == nil {
		baselineState = baseline.NewBaseline(nil)
	}

	changed := false
	for _, rel := range append(append([]string{}, plan.rels...), plan.rc.cfg.IndexPath) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		fingerprint, hashErr := baseline.HashFile(path)
		if hashErr != nil {
			return "", false, &Fail{Code: errInternal, Msg: writeMessage(
				"entry.batch.reconcile_hash_failed", rel, localeSafeWriteDetail(hashErr.Error()),
			)}
		}
		if rel == plan.rc.cfg.IndexPath && fingerprint.SHA256 != expectedIndexSHA256 {
			return "", false, &Fail{
				Code: errWriteConflict,
				Msg:  writeMessage("entry.batch.reconcile_postimage_changed"),
				Hint: writeMessage("entry.batch.hint.replan"),
			}
		}
		fingerprint = asManagedIndexFingerprint(baselineState, fingerprint)
		if rel != plan.rc.cfg.IndexPath {
			expectedSHA256 := ""
			for _, item := range plan.normalizedItems {
				if item.rel == rel {
					expectedSHA256 = item.sourceSHA256
					break
				}
			}
			if expectedSHA256 == "" {
				return "", false, &Fail{
					Code: errWriteConflict,
					Msg:  writeMessage("entry.batch.reconcile_missing_binding", rel),
					Hint: writeMessage("entry.batch.hint.rebuild_binding"),
				}
			}
			if fingerprint.SHA256 != expectedSHA256 {
				return "", false, &Fail{
					Code: errWriteConflict,
					Msg:  writeMessage("entry.batch.reconcile_source_changed", rel),
					Hint: writeMessage("entry.batch.hint.regenerate_candidate"),
				}
			}
		}
		if current, ok := baselineState.Files[rel]; ok && current == fingerprint {
			continue
		}
		baseline.UpdateOne(baselineState, rel, fingerprint)
		changed = true
	}
	if !changed {
		if recovery != nil {
			if _, receiptErr := saveCompletedEntriesGovernanceReceipt(root, plan, recovery); receiptErr != nil {
				return "", false, &Fail{Code: errInternal, Msg: mcpMessage(
					"entries.recovery_receipt.governance_persist_failed",
					localeSafeWriteDetail(receiptErr.Error()),
				)}
			}
		}
		ledger.Append(root, plan.rc.cfg.LedgerEnabled, ledger.Event{
			Op: "update_entries_batch_recover", PathsCount: len(plan.rels),
			DurationMs: time.Since(plan.start).Milliseconds(), Source: source,
			Result: ledger.ResultOK, AppliedCount: 0, RecoveredCount: len(plan.rels),
			DuplicateApplies: 1,
		})
		return writeMessage("entry.batch.already_resolved"), true, nil
	}
	if saveErr := saveAtomicBaseline(root, baselineState); saveErr != nil {
		return "", false, &Fail{Code: errInternal, Msg: writeMessage(
			"entry.batch.reconcile_baseline_save_failed", localeSafeWriteDetail(saveErr.Error()),
		)}
	}
	confirmedIndex, confirmIndexErr := baseline.HashFile(plan.rc.paths.IndexPath)
	if confirmIndexErr != nil || confirmedIndex.SHA256 != expectedIndexSHA256 {
		ledger.Append(root, plan.rc.cfg.LedgerEnabled, ledger.Event{
			Op: "update_entries_batch_recover", PathsCount: len(plan.rels),
			DurationMs: time.Since(plan.start).Milliseconds(), Source: source,
			Result: ledger.ResultError, AppliedCount: 0, RecoveredCount: len(plan.rels),
			DuplicateApplies: 1,
		})
		return writeMessage("entry.batch.reconcile_baseline_postimage_changed"), false, nil
	}
	if drifted := mismatchedBoundSources(
		root, plan.normalizedItems, plan.rc.cfg.IndexPath,
	); len(drifted) > 0 {
		ledger.Append(root, plan.rc.cfg.LedgerEnabled, ledger.Event{
			Op:               "update_entries_batch_recover",
			PathsCount:       len(plan.rels),
			DurationMs:       time.Since(plan.start).Milliseconds(),
			Source:           source,
			Result:           ledger.ResultError,
			AppliedCount:     0,
			RecoveredCount:   len(plan.rels),
			DuplicateApplies: 1,
		})
		return writeMessage(
			"entry.batch.reconcile_source_drift",
			strings.Join(drifted, ", "),
		), false, nil
	}
	if recovery != nil {
		if _, receiptErr := saveCompletedEntriesGovernanceReceipt(root, plan, recovery); receiptErr != nil {
			return "", false, &Fail{Code: errInternal, Msg: mcpMessage(
				"entries.recovery_receipt.baseline_governance_persist_failed",
				localeSafeWriteDetail(receiptErr.Error()),
			)}
		}
	}
	ledger.Append(root, plan.rc.cfg.LedgerEnabled, ledger.Event{
		Op:               "update_entries_batch_recover",
		PathsCount:       len(plan.rels),
		DurationMs:       time.Since(plan.start).Milliseconds(),
		Source:           source,
		Result:           ledger.ResultOK,
		AppliedCount:     0,
		RecoveredCount:   len(plan.rels),
		DuplicateApplies: 1,
	})
	return writeMessage("entry.batch.reconcile_completed"), true, nil
}

// ApplyUpdateEntriesAtomic 是整批回写的唯一入口(auto 无人值守与人工
// Entries Apply 共用)。
func ApplyUpdateEntriesAtomic(
	root string,
	items []AtomicUpdateItem,
	source string,
	dryRun bool,
) (*AtomicBatchOutcome, *Fail) {
	return ApplyUpdateEntriesAtomicBound(root, items, source, dryRun, "")
}

// ApplyUpdateEntriesAtomicBound把Host-Agent生成期索引摘要带入最终提交边界。
// 普通MCP批次使用上面的兼容入口；Host-Agent必须传Manifest.IndexSHA256。
func ApplyUpdateEntriesAtomicBound(
	root string,
	items []AtomicUpdateItem,
	source string,
	dryRun bool,
	expectedIndexSHA256 string,
) (*AtomicBatchOutcome, *Fail) {
	return applyUpdateEntriesAtomicBound(
		root, items, source, dryRun, expectedIndexSHA256,
		expectedIndexSHA256 != "",
	)
}

// ApplyUpdateEntriesAtomicBoundRetained keeps the replay-prevention transaction
// after Baseline completion so a caller can durably record its Application
// audit before removing the transaction.
func ApplyUpdateEntriesAtomicBoundRetained(
	root string,
	items []AtomicUpdateItem,
	source string,
	dryRun bool,
	expectedIndexSHA256 string,
) (*AtomicBatchOutcome, *Fail) {
	return applyUpdateEntriesAtomicBound(
		root, items, source, dryRun, expectedIndexSHA256, true,
	)
}

func applyUpdateEntriesAtomicBound(
	root string,
	items []AtomicUpdateItem,
	source string,
	dryRun bool,
	expectedIndexSHA256 string,
	retainRecovery bool,
) (*AtomicBatchOutcome, *Fail) {
	start := time.Now()

	plan, fail := planUpdateEntriesAtomic(root, items)
	if fail != nil {
		if !dryRun {
			appendWriteFailEvent(
				root, "update_entries_batch", source, fail.Code, start,
			)
		}
		return nil, fail
	}
	boundIndexSHA256 := strings.ToLower(strings.TrimSpace(expectedIndexSHA256))
	boundRecovery := false
	if boundIndexSHA256 != "" && plan.indexHash != boundIndexSHA256 &&
		plan.finalText == plan.rc.text {
		if recovery, recoveryErr := loadEntriesRecoveryIncludingArchive(root, plan.batchKey); recoveryErr == nil && recovery.PreIndexSHA256 == boundIndexSHA256 &&
			recovery.PostIndexSHA256 == plan.indexHash {
			boundRecovery = true
		}
	}
	if plan.indexSourceMismatch && !boundRecovery {
		fail := &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.batch.source_cas_conflict", plan.rc.cfg.IndexPath),
			Hint: writeMessage("entry.batch.hint.refresh_binding"),
		}
		if !dryRun {
			appendWriteFailEvent(root, "update_entries_batch", source, fail.Code, start)
		}
		return nil, fail
	}
	if boundIndexSHA256 != "" && plan.indexHash != boundIndexSHA256 && !boundRecovery {
		fail := &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.batch.generation_plan_stale"),
			Hint: writeMessage("entry.batch.hint.generation_plan_stale"),
		}
		if !dryRun {
			appendWriteFailEvent(root, "update_entries_batch", source, fail.Code, start)
		}
		return nil, fail
	}

	if dryRun {
		for _, outcome := range plan.outcomes {
			outcome.DryRun = true
			outcome.BaselineNote = writeMessage("entry.batch.preview_item")
		}
		return &AtomicBatchOutcome{
			Items:            plan.outcomes,
			Volume:           plan.volumeID(),
			Volumes:          plan.volumeIDs(),
			BaselineNote:     writeMessage("entry.batch.preview_complete"),
			BaselineComplete: true,
			DryRun:           true,
		}, nil
	}

	if plan.volumePlan != nil && plan.volumePlan.allPost {
		baselineNote, baselineComplete, reconcileFail := reconcileCognitionVolumeBatch(root, source, plan, retainRecovery)
		if reconcileFail != nil {
			appendWriteFailEvent(root, "update_entries_batch", source, reconcileFail.Code, start)
			return nil, reconcileFail
		}
		return &AtomicBatchOutcome{
			Items: plan.outcomes, Volume: plan.volumeID(), Volumes: plan.volumeIDs(), BaselineNote: baselineNote,
			BaselineComplete: baselineComplete, AlreadyApplied: true, RecoveredCount: len(plan.outcomes),
		}, nil
	}

	if plan.finalText == plan.rc.text {
		baselineNote, baselineComplete, reconcileFail := reconcileAlreadyAppliedBaseline(root, source, plan)
		if reconcileFail != nil {
			appendWriteFailEvent(
				root, "update_entries_batch", source, reconcileFail.Code, start,
			)
			return nil, reconcileFail
		}
		if baselineComplete && !retainRecovery {
			if cleanupErr := cleanupAtomicBatchRecovery(root, plan.batchKey); cleanupErr != nil {
				baselineComplete = false
				baselineNote = writeMessage(
					"entry.batch.recovery_cleanup_failed",
					localeSafeWriteDetail(cleanupErr.Error()),
				)
			}
		}
		return &AtomicBatchOutcome{
			Items:            plan.outcomes,
			Volume:           plan.volumeID(),
			Volumes:          plan.volumeIDs(),
			BaselineNote:     baselineNote,
			BaselineComplete: baselineComplete,
			AlreadyApplied:   true,
			AppliedCount:     0,
			RecoveredCount:   len(plan.outcomes),
		}, nil
	}

	baselineNote, baselineComplete, fail := commitAtomicBatch(
		root, source, plan, retainRecovery,
	)
	if fail != nil {
		appendWriteFailEvent(
			root, "update_entries_batch", source, fail.Code, start,
		)
		return nil, fail
	}

	appliedCount := len(plan.outcomes)
	if plan.volumePlan != nil && !baselineComplete {
		appliedCount = 0
	}
	return &AtomicBatchOutcome{
		Items:            plan.outcomes,
		Volume:           plan.volumeID(),
		Volumes:          plan.volumeIDs(),
		BaselineNote:     baselineNote,
		BaselineComplete: baselineComplete,
		AppliedCount:     appliedCount,
	}, nil
}

// RenderAtomicBatchOutcome 供 auto 与人工 Entries Apply 渲染整批结果。
func RenderAtomicBatchOutcome(outcome *AtomicBatchOutcome) string {
	if outcome == nil {
		return ""
	}

	var builder strings.Builder
	if outcome.AlreadyApplied {
		builder.WriteString(writeMessage("entry.batch.duplicate_heading") + "\n")
	} else {
		for _, item := range outcome.Items {
			builder.WriteString(RenderOutcome(item))
		}
	}
	if outcome.BaselineNote != "" {
		builder.WriteString(outcome.BaselineNote + "\n")
	}
	return builder.String()
}
