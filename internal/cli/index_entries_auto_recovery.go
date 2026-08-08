// Entries Auto完成态重试只验证既有Application并收口恢复收据，不重跑Apply。
package cli

import (
	"fmt"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

var completeEntriesAutoRecovery = mcptools.CompleteUpdateEntriesAtomicRecovery
var entriesAutoRecoveryPending = mcptools.UpdateEntriesAtomicRecoveryPending

func finishCompletedEntriesAuto(
	repoRoot,
	runID,
	source string,
	cfg *config.Config,
	manifest *draft.Manifest,
	result *entriesAutoFinalizeResult,
) (*entriesAutoFinalizeResult, error) {
	items, application, recoveryErr := completedEntriesAutoEvidence(repoRoot, runID, manifest)
	if recoveryErr != nil {
		result.FailedStep = entriesAutoStepAudit
		result.Recovery = cliMessage("entries.auto.recovery.completed_evidence")
		return result, &ExitError{Code: ExitInvalid, Err: recoveryErr}
	}
	pending, recoveryErr := entriesAutoRecoveryPending(repoRoot, items)
	if recoveryErr != nil {
		result.FailedStep = entriesAutoStepAudit
		result.Recovery = cliMessage("entries.auto.recovery.receipt_invalid")
		return result, &ExitError{Code: ExitInternal, Err: recoveryErr}
	}
	recoveryStart := time.Now()
	if pending {
		if recoveryErr := completeEntriesAutoRecovery(repoRoot, items); recoveryErr != nil {
			result.FailedStep = entriesAutoStepAudit
			result.Recovery = cliMessage("entries.recovery_receipt.cleanup_retry")
			return result, &ExitError{Code: ExitInternal, Err: recoveryErr}
		}
		appendEntriesAutoApplyLedger(
			repoRoot, cfg, runID, source, len(items), 0, len(items), 0, "",
			time.Since(recoveryStart),
		)
	}
	result.Status = entriesAutoStatusApplied
	result.Checked = len(manifest.Entries)
	result.Passed = len(manifest.Entries)
	result.DiffReviewed = len(manifest.Entries)
	result.Recovered = len(items)
	result.AuditRecorded = true
	result.DraftHash = application.DraftHash
	if pending {
		result.Recovery = cliMessage("entries.auto.recovery.cleaned")
	} else {
		result.Recovery = cliMessage("entries.auto.recovery.already_complete")
	}
	return result, nil
}

// completedEntriesAutoEvidence把兼容AppliedAt重新绑定到成功Application、
// 同一草稿快照和完整候选批次。AppliedAt本身不是完成证据。
func completedEntriesAutoEvidence(
	repoRoot,
	runID string,
	manifest *draft.Manifest,
) ([]mcptools.AtomicUpdateItem, draft.ApplicationRecord, error) {
	if manifest == nil || manifest.AppliedAt == "" {
		return nil, draft.ApplicationRecord{}, fmt.Errorf("%s", cliMessage("entries.auto.completed_marker_empty"))
	}
	var application *draft.ApplicationRecord
	for position := len(manifest.Applications) - 1; position >= 0; position-- {
		candidate := &manifest.Applications[position]
		if candidate.At == manifest.AppliedAt {
			application = candidate
			break
		}
	}
	if application == nil || application.DraftHash == "" ||
		application.PathsCount != len(manifest.Entries) ||
		application.Applied+application.Recovered != len(manifest.Entries) ||
		application.Rejected != 0 || application.RejectKinds != "" {
		return nil, draft.ApplicationRecord{}, fmt.Errorf("%s", cliMessage("entries.auto.completed_application_invalid"))
	}
	snapshot, err := loadEntryDraftSnapshot(repoRoot, runID, manifest)
	if err != nil {
		return nil, draft.ApplicationRecord{}, err
	}
	if snapshot.Hash != application.DraftHash {
		return nil, draft.ApplicationRecord{}, fmt.Errorf("%s", cliMessage("entries.auto.completed_snapshot_mismatch"))
	}
	items, err := atomicItemsFromReviewedSnapshot(&entriesCheckResult{
		Manifest: manifest,
		Snapshot: snapshot,
	})
	if err != nil {
		return nil, draft.ApplicationRecord{}, err
	}
	return items, *application, nil
}
