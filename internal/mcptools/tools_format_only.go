// 受治理的gofmt-only基线快速前移。
package mcptools

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

var saveFormatOnlyBaseline = baseline.SaveUnderIndexLock

func formatOnlyCandidates(
	baselineValue *baseline.Baseline,
	snapshot map[string]baseline.Fingerprint,
	stale []string,
	tolerateLineEndings bool,
) []string {
	if baselineValue == nil {
		return []string{}
	}
	result := []string{}
	for _, rel := range stale {
		before, exists := baselineValue.Files[rel]
		after, currentExists := snapshot[rel]
		lineEndingOnly := before.NormalizedSHA256 != "" &&
			before.NormalizedSHA256 == after.NormalizedSHA256
		if lineEndingOnly && !tolerateLineEndings {
			continue
		}
		if exists && currentExists && baseline.IsFormatOnlyChange(before, after) {
			result = append(result, rel)
		}
	}
	return result
}

// applyFormatOnlyBatch在一次锁和一次Baseline原子保存中前移全部已证明目标。
func applyFormatOnlyBatch(
	root string,
	repository *repoCtx,
	snapshot map[string]baseline.Fingerprint,
	paths []string,
) *Fail {
	if len(paths) == 0 {
		return nil
	}
	if repository == nil || repository.bl == nil {
		return &Fail{Code: errInternal, Msg: writeMessage("maintain.format_only.baseline_missing")}
	}
	start := time.Now()
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		code := errInternal
		if errors.Is(err, afs.ErrLockTimeout) {
			code = errWriteConflict
		}
		return &Fail{Code: code, Msg: writeMessage(
			"maintain.format_only.lock_failed",
			localeSafeWriteDetail(err.Error()),
		)}
	}
	defer lock.Release()

	currentBaseline, exists, err := baseline.Load(root)
	if err != nil || !exists || currentBaseline == nil {
		return &Fail{Code: errInternal, Msg: writeMessage("maintain.format_only.baseline_read_failed")}
	}
	for _, rel := range paths {
		expectedBefore, hadBefore := repository.bl.Files[rel]
		lockedBefore, stillPresent := currentBaseline.Files[rel]
		if !hadBefore || !stillPresent || expectedBefore != lockedBefore {
			return &Fail{Code: errWriteConflict, Msg: writeMessage("maintain.format_only.cas_conflict", rel)}
		}
		current, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if hashErr != nil {
			return &Fail{Code: errInternal, Msg: writeMessage(
				"maintain.format_only.source_read_failed",
				rel,
				localeSafeWriteDetail(hashErr.Error()),
			)}
		}
		expectedCurrent, snapshotted := snapshot[rel]
		current.Role = expectedCurrent.Role
		if !snapshotted || current != expectedCurrent ||
			!baseline.IsFormatOnlyChange(expectedBefore, current) {
			return &Fail{Code: errWriteConflict, Msg: writeMessage("maintain.format_only.source_changed", rel)}
		}
		baseline.UpdateOne(currentBaseline, rel, current)
	}
	if err := saveFormatOnlyBaseline(root, currentBaseline); err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage(
			"maintain.format_only.baseline_save_failed",
			localeSafeWriteDetail(err.Error()),
		)}
	}
	for _, rel := range paths {
		current, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		expected := snapshot[rel]
		current.Role = expected.Role
		if hashErr != nil || current != expected {
			ledger.Append(root, repository.cfg.LedgerEnabled, ledger.Event{
				Op: "format_only", PathsCount: len(paths), AppliedCount: 0,
				DurationMs: elapsedMilliseconds(start), Source: ledger.SourceAgent,
				Result: ledger.ResultConflict, FailCode: errWriteConflict,
			})
			return &Fail{Code: errWriteConflict,
				Msg: writeMessage("maintain.format_only.source_changed_during_save", rel)}
		}
	}
	ledger.Append(root, repository.cfg.LedgerEnabled, ledger.Event{
		Op:           "format_only",
		PathsCount:   len(paths),
		AppliedCount: len(paths),
		DurationMs:   elapsedMilliseconds(start),
		Source:       ledger.SourceAgent,
		Result:       ledger.ResultOK,
	})
	return nil
}
