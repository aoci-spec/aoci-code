// D69-2A atomic batch entry write-back.
//
// Index entry: tools_write_batch.go[MWR9AM]
//
// Differences from the manual entries apply:
//   - the manual pipeline keeps per-entry partial-success semantics;
//   - this pipeline was first designed for unattended auto and now also serves the manual Entries Apply and
//     the Host Agent governance chain (whole-batch reuse): every target is first planned on the same in-memory index,
//     any failing target means zero writes; once all pass, one write lock, one CAS, one AtomicWrite.
//
// The atomic boundary is the formal index file: either the whole batch's new text lands in one write, or the formal index changes nothing.
// The baseline and the ledger are post-write by-products; their failure only forms a hint and never rolls back a successful atomic index write.
//
// Failure-path recording (R60-F.9-A1, 2026-07-18): the same as the single-entry pipeline: planning refusals and
// CAS/lock conflicts are recorded through appendWriteFailEvent (op=update_entries_batch),
// timing is lifted to the top of ApplyUpdateEntriesAtomic; a dry run is pure reading and records nothing.
package mcptools

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// Tests inject interruptions or concurrent changes only at explicit write boundaries; the production path always points at the atomic implementation.
var (
	writeAtomicIndex   = afs.AtomicWriteCAS
	saveAtomicBaseline = baseline.SaveUnderIndexLock
)

// AtomicUpdateItem is one input of the atomic batch write-back.
type AtomicUpdateItem struct {
	Path         string
	ObjectRef    string
	NewEntry     string
	SourceSHA256 string
	CandidateID  string
	BatchID      string
}

// AtomicBatchOutcome is the result of planning or applying a whole batch successfully.
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

// planUpdateEntriesAtomic plans the whole batch on the same initial index version.
// After each successful transformation the in-memory text is re-parsed, and the next item plans on top of the earlier results.
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
				return nil, &Fail{Code: errPathUnsafe, Msg: writeMessage("entry.batch.path_invalid", indexPosition+1, item.Path, localizeRelPathError(err)), Hint: writeMessage("entry.batch.hint.paths_relative")}
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
	// A Legacy single-file layout is read-only since 0.2.0: its authoring
	// surfaces are retired, and the one next step is the migration to Volumes.
	if loaded.set.LayoutMode == cognition.LayoutLegacyMonolithic {
		return nil, legacyReadOnlyFail()
	}
	return planCognitionVolumeUpdates(root, loaded, normalized, start)
}

// commitAtomicBatch performs one CAS and one AtomicWrite inside one write-lock critical section.
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
	// Every plan that reaches commit is a Volumes plan: a Legacy layout is
	// refused before planning since 0.2.0, so its single-index commit is gone.
	if plan.volumePlan == nil {
		return "", false, &Fail{Code: errInternal, Msg: "atomic_batch_plan_without_volume"}
	}
	return commitCognitionVolumeBatchLocked(root, source, plan, retainRecovery)
}

// ApplyUpdateEntriesAtomic is the single entry for whole-batch write-back (shared by unattended auto and the manual
// Entries Apply).
func ApplyUpdateEntriesAtomic(
	root string,
	items []AtomicUpdateItem,
	source string,
	dryRun bool,
) (*AtomicBatchOutcome, *Fail) {
	return ApplyUpdateEntriesAtomicBound(root, items, source, dryRun, "")
}

// ApplyUpdateEntriesAtomicBound carries the Host Agent generation-time index digest into the final commit boundary.
// An ordinary MCP batch uses the compatibility entry above; the Host Agent must pass Manifest.IndexSHA256.
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
				root, "update_entries_batch", source, fail, start,
			)
		}
		return nil, fail
	}
	boundIndexSHA256 := strings.ToLower(strings.TrimSpace(expectedIndexSHA256))
	if plan.indexSourceMismatch {
		fail := &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.batch.source_cas_conflict", plan.rc.cfg.IndexPath),
			Hint: writeMessage("entry.batch.hint.refresh_binding"),
		}
		if !dryRun {
			appendWriteFailEvent(root, "update_entries_batch", source, fail, start)
		}
		return nil, fail
	}
	if boundIndexSHA256 != "" && plan.indexHash != boundIndexSHA256 {
		fail := &Fail{
			Code: errWriteConflict,
			Msg:  writeMessage("entry.batch.generation_plan_stale"),
			Hint: writeMessage("entry.batch.hint.generation_plan_stale"),
		}
		if !dryRun {
			appendWriteFailEvent(root, "update_entries_batch", source, fail, start)
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
			appendWriteFailEvent(root, "update_entries_batch", source, reconcileFail, start)
			return nil, reconcileFail
		}
		return &AtomicBatchOutcome{
			Items: plan.outcomes, Volume: plan.volumeID(), Volumes: plan.volumeIDs(), BaselineNote: baselineNote,
			BaselineComplete: baselineComplete, AlreadyApplied: true, RecoveredCount: len(plan.outcomes),
		}, nil
	}

	baselineNote, baselineComplete, fail := commitAtomicBatch(
		root, source, plan, retainRecovery,
	)
	if fail != nil {
		appendWriteFailEvent(
			root, "update_entries_batch", source, fail, start,
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

// RenderAtomicBatchOutcome renders the whole-batch result for auto and the manual Entries Apply.
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
