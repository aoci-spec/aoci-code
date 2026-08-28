// Explicit closeout for a proven pre-Apply zero-write failure or an Entries
// post-write failure, including terminal supersession by later governance.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/spf13/cobra"
)

type entriesRecoveryResult struct {
	Version               int      `json:"version"`
	RunID                 string   `json:"run_id"`
	Status                string   `json:"status"`
	FailureKinds          string   `json:"failure_kinds,omitempty"`
	Applied               int      `json:"applied"`
	Recovered             int      `json:"recovered"`
	AlreadyResolved       bool     `json:"already_resolved"`
	PreIndexSHA256        string   `json:"pre_index_sha256,omitempty"`
	PostIndexSHA256       string   `json:"post_index_sha256,omitempty"`
	CurrentIndexSHA256    string   `json:"current_index_sha256,omitempty"`
	CurrentBaselineSHA256 string   `json:"current_baseline_sha256,omitempty"`
	RepositorySHA256      string   `json:"repository_sha256,omitempty"`
	GovernanceReceipts    []string `json:"governance_receipts,omitempty"`
	ArchivedRecoveryAsset string   `json:"archived_recovery_asset,omitempty"`
}

type entriesAlignmentProof struct {
	IndexSHA256      string
	BaselineSHA256   string
	RepositorySHA256 string
}

func newEntriesRecoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recover <run_id>",
		Short: cliMessage("cli.short.index_entries_recover"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{Code: ExitConfig, Err: err}
			}
			result, recoverErr := recoverEntriesRun(root, args[0], ledger.SourceHuman)
			if flagJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				if writeErr := encoder.Encode(result); writeErr != nil {
					return &ExitError{Code: ExitInternal, Err: writeErr}
				}
			} else if result != nil {
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage(
					"entries.recover.result", result.RunID, result.Status,
				))
			}
			if recoverErr != nil {
				return &ExitError{Code: ExitInvalid, Err: fmt.Errorf(
					"%s", cliMessage("entries.recover.failed", recoverErr),
				)}
			}
			return nil
		},
	}
}

func recoverEntriesRun(
	root,
	runID,
	source string,
) (*entriesRecoveryResult, error) {
	start := time.Now()
	result := &entriesRecoveryResult{
		Version: 1, RunID: runID, Status: draft.RunResolutionPending,
		GovernanceReceipts: []string{},
	}
	cfg, err := config.Load(root)
	if err != nil {
		return result, err
	}
	if err := requireLegacyWriteLayout(root, cfg, false); err != nil {
		return result, err
	}
	lease, err := afs.AcquireEntriesRunLock(root, runID)
	if err != nil {
		return result, fmt.Errorf("entries_recovery_run_lock_failed: %w", err)
	}
	defer lease.Release()

	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		return result, err
	}
	if manifest.Kind != draft.KindEntries {
		return result, fmt.Errorf("entries_recovery_wrong_manifest_kind: %s", runID)
	}
	if terminal, ok := draft.StoredTerminalRunResolution(root, manifest); ok {
		cfg, cfgErr := config.Load(root)
		if cfgErr != nil {
			return entriesRecoveryResultFromResolution(
				runID, terminal, recoveredApplicationCount(manifest), true,
			), cfgErr
		}
		if auditErr := ensureEntriesRecoveryLedger(
			root, cfg, runID, source, terminal,
			recoveredApplicationCount(manifest), time.Since(start),
		); auditErr != nil {
			return entriesRecoveryResultFromResolution(
				runID, terminal, recoveredApplicationCount(manifest), true,
			), auditErr
		}
		return entriesRecoveryResultFromResolution(
			runID, terminal, recoveredApplicationCount(manifest), true,
		), nil
	}
	if closure, ok := draft.StoredZeroWriteRunClosed(root, manifest); ok {
		if auditErr := ensureEntriesZeroWriteClosureLedger(root, cfg, runID, source, closure); auditErr != nil {
			return entriesZeroWriteRecoveryResult(runID, closure, true), auditErr
		}
		return entriesZeroWriteRecoveryResult(runID, closure, true), nil
	}

	snapshot, err := loadEntryDraftSnapshot(root, runID, manifest)
	if err != nil {
		return result, fmt.Errorf("entries_recovery_draft_evidence_unreadable: %w", err)
	}
	if manifest.GenerationSource == draft.GenerationSourceHostAgent &&
		manifest.AppliedAt == "" && len(manifest.Reviews) == 0 &&
		len(manifest.Applications) == 0 && len(manifest.Resolutions) == 0 &&
		len(manifest.ZeroWriteClosures) == 0 {
		if snapshot.Hash != manifest.GenerationHash {
			return result, fmt.Errorf("entries_recovery_pre_apply_zero_write_unproven")
		}
		if evidenceErr := rejectEntriesApplyEvidence(root, runID); evidenceErr != nil {
			return result, evidenceErr
		}
		indexPath := filepath.Join(root, filepath.FromSlash(cfg.IndexPath))
		lock, lockErr := afs.AcquireIndexLock(root)
		if lockErr != nil {
			return result, fmt.Errorf("entries_recovery_proof_lock_failed: %w", lockErr)
		}
		defer lock.Release()
		currentIndexSHA256, digestErr := rawFileSHA256(indexPath)
		if digestErr != nil {
			return result, digestErr
		}
		closure := draft.ZeroWriteClosure{
			Version: 1, At: time.Now().UTC().Format(time.RFC3339Nano),
			Step:   draft.ZeroWriteStepGenerationPlan,
			Reason: draft.ZeroWriteReasonRecovery, DraftHash: snapshot.Hash,
			PreIndexSHA256: manifest.IndexSHA256, FormalAssetWrites: 0,
		}
		if currentIndexSHA256 != manifest.IndexSHA256 {
			items, itemErr := atomicItemsFromReviewedSnapshot(&entriesCheckResult{
				Manifest: manifest,
				Snapshot: snapshot,
			})
			if itemErr != nil {
				return result, fmt.Errorf("entries_recovery_candidate_evidence_incomplete: %w", itemErr)
			}
			governance, proofErr := mcptools.ProveEntriesZeroWriteGovernance(
				root, indexPath, items, manifest.IndexSHA256, manifest.CreatedAt, closure.At,
			)
			if proofErr != nil {
				return result, fmt.Errorf("entries_recovery_pre_apply_zero_write_unproven: %w", proofErr)
			}
			alignment, alignmentErr := proveCurrentEntriesZeroWriteAlignment(root, indexPath)
			if alignmentErr != nil {
				return result, alignmentErr
			}
			if governance.CurrentIndexSHA256 != alignment.IndexSHA256 {
				return result, fmt.Errorf("entries_recovery_index_changed_during_proof")
			}
			closure.Version = 2
			closure.CurrentIndexSHA256 = alignment.IndexSHA256
			closure.CurrentBaselineSHA256 = alignment.BaselineSHA256
			closure.RepositorySHA256 = alignment.RepositorySHA256
			closure.StagedTransactionID = governance.StagedTransactionID
			closure.GovernanceReceipts = append([]string{}, governance.GovernanceReceipts...)
		}
		if appendErr := draft.AppendZeroWriteClosure(root, runID, closure); appendErr != nil {
			return result, fmt.Errorf("entries_recovery_zero_write_closure_failed: %w", appendErr)
		}
		manifest, err = draft.LoadManifest(root, runID)
		if err != nil {
			return result, err
		}
		closure, ok := draft.StoredZeroWriteRunClosed(root, manifest)
		if !ok {
			return result, fmt.Errorf("entries_recovery_zero_write_closure_unproven")
		}
		if auditErr := ensureEntriesZeroWriteClosureLedger(root, cfg, runID, source, closure); auditErr != nil {
			return entriesZeroWriteRecoveryResult(runID, closure, false), auditErr
		}
		return entriesZeroWriteRecoveryResult(runID, closure, false), nil
	}
	items, err := atomicItemsFromReviewedSnapshot(&entriesCheckResult{
		Manifest: manifest,
		Snapshot: snapshot,
	})
	if err != nil {
		return result, fmt.Errorf("entries_recovery_candidate_evidence_incomplete: %w", err)
	}
	failureKinds, wroteFormalEntries, err := originalEntriesFailureEvidence(root, runID, manifest, snapshot.Hash)
	if err != nil {
		return result, err
	}
	if !wroteFormalEntries {
		return result, fmt.Errorf("entries_recovery_formal_write_unproven")
	}
	result.FailureKinds = failureKinds

	indexPath := filepath.Join(root, filepath.FromSlash(cfg.IndexPath))
	currentIndexSHA256, err := rawFileSHA256(indexPath)
	if err != nil {
		return result, err
	}
	governanceProof, err := mcptools.ProveEntriesGovernanceSupersession(
		root, indexPath, items, currentIndexSHA256,
	)
	if err != nil {
		return result, fmt.Errorf("%s", cliMessage("entries.recover.governance_proof_error", err))
	}
	if manifest.GenerationSource == draft.GenerationSourceHostAgent &&
		governanceProof.PreIndexSHA256 != manifest.IndexSHA256 {
		return result, fmt.Errorf("entries_recovery_generation_preimage_mismatch")
	}

	// When the original postimage is still current, only the existing
	// replay-prevention path runs. It rechecks source_sha256 and completes
	// Baseline without a second formal index write.
	if currentIndexSHA256 == governanceProof.PostIndexSHA256 &&
		!hasSuccessfulRecoveryApplication(manifest, snapshot.Hash, len(items)) {
		outcome, applyFail := mcptools.ApplyUpdateEntriesAtomicBound(
			root, items, source, false, governanceProof.PreIndexSHA256,
		)
		if applyFail != nil {
			return result, fmt.Errorf("entries_original_postimage_recovery_rejected[%s]: %s", applyFail.Code, applyFail.Msg)
		}
		if outcome == nil || !outcome.BaselineComplete || outcome.AppliedCount != 0 ||
			outcome.RecoveredCount != len(items) {
			return result, fmt.Errorf("entries_original_postimage_recovery_incomplete")
		}
		result.Recovered = outcome.RecoveredCount
		if err := config.AdvanceLocaleMigration(root, false, itemPaths(items), nil); err != nil {
			return result, fmt.Errorf("entries_recovery_locale_closeout_failed: %w", err)
		}
	}

	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return result, fmt.Errorf("entries_recovery_proof_lock_failed: %w", err)
	}
	defer lock.Release()

	currentIndexSHA256, err = rawFileSHA256(indexPath)
	if err != nil {
		return result, err
	}
	governanceProof, err = mcptools.ProveEntriesGovernanceSupersession(
		root, indexPath, items, currentIndexSHA256,
	)
	if err != nil {
		return result, fmt.Errorf("%s", cliMessage("entries.recover.governance_proof_error", err))
	}
	alignment, err := proveCurrentEntriesAlignment(root, indexPath)
	if err != nil {
		return result, err
	}
	status := draft.RunResolutionRecovered
	if currentIndexSHA256 != governanceProof.PostIndexSHA256 {
		status = draft.RunResolutionSuperseded
		if len(governanceProof.GovernanceReceipts) == 0 {
			return result, fmt.Errorf("entries_supersession_governance_chain_missing")
		}
	}
	archivedAsset, archivedSHA256, err := mcptools.ArchiveEntriesAtomicRecovery(root, items)
	if err != nil {
		return result, fmt.Errorf("entries_recovery_archive_failed: %w", err)
	}
	record := draft.RunResolutionRecord{
		Status: status, FailureKinds: failureKinds,
		TransactionID:          governanceProof.TransactionID,
		PreIndexSHA256:         governanceProof.PreIndexSHA256,
		PostIndexSHA256:        governanceProof.PostIndexSHA256,
		CurrentIndexSHA256:     alignment.IndexSHA256,
		CurrentBaselineSHA256:  alignment.BaselineSHA256,
		RepositorySHA256:       alignment.RepositorySHA256,
		GovernanceReceipts:     append([]string{}, governanceProof.GovernanceReceipts...),
		ArchivedRecoveryAsset:  archivedAsset,
		ArchivedRecoverySHA256: archivedSHA256,
	}
	if status == draft.RunResolutionRecovered {
		// Baseline recovery already persisted the original transaction receipt.
		// The terminal record links it by transaction ID and archived pre/postimage;
		// it is not misrepresented as a later governance transition.
		record.GovernanceReceipts = []string{}
	}
	var resolutionErr error
	if status == draft.RunResolutionRecovered {
		resolutionErr = draft.AppendRecoveredRunCompletion(
			root,
			runID,
			draft.ApplicationRecord{
				DraftHash: snapshot.Hash, PathsCount: len(items), Recovered: len(items),
			},
			record,
		)
	} else {
		resolutionErr = draft.AppendRunResolution(root, runID, record)
	}
	if resolutionErr != nil {
		return result, fmt.Errorf("entries_recovery_terminal_state_save_failed: %w", resolutionErr)
	}
	finalResult := entriesRecoveryResultFromResolution(
		runID, record, result.Recovered, false,
	)
	if err := ensureEntriesRecoveryLedger(
		root, cfg, runID, source, record, result.Recovered, time.Since(start),
	); err != nil {
		return finalResult, err
	}
	return finalResult, nil
}

func entriesZeroWriteRecoveryResult(
	runID string,
	closure draft.ZeroWriteClosure,
	alreadyResolved bool,
) *entriesRecoveryResult {
	currentIndexSHA256 := closure.PreIndexSHA256
	if closure.CurrentIndexSHA256 != "" {
		currentIndexSHA256 = closure.CurrentIndexSHA256
	}
	return &entriesRecoveryResult{
		Version: 1, RunID: runID, Status: draft.RunResolutionZeroWrite,
		FailureKinds: closure.Reason, AlreadyResolved: alreadyResolved,
		PreIndexSHA256:        closure.PreIndexSHA256,
		CurrentIndexSHA256:    currentIndexSHA256,
		CurrentBaselineSHA256: closure.CurrentBaselineSHA256,
		RepositorySHA256:      closure.RepositorySHA256,
		GovernanceReceipts:    append([]string{}, closure.GovernanceReceipts...),
	}
}

func ensureEntriesZeroWriteClosureLedger(
	root string,
	cfg *config.Config,
	runID,
	source string,
	closure draft.ZeroWriteClosure,
) error {
	if cfg == nil || !cfg.LedgerEnabled {
		return nil
	}
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("entries_recovery_ledger_corrupt_before_zero_write_audit: %d", corrupt)
	}
	for _, event := range events {
		if event.Op != "entries_recover" || event.DraftRunID != runID {
			continue
		}
		if matchesEntriesZeroWriteClosureEvent(event, runID, closure) {
			return nil
		}
		return fmt.Errorf("entries_recovery_ledger_zero_write_event_conflict")
	}
	currentIndexSHA256 := closure.PreIndexSHA256
	if closure.CurrentIndexSHA256 != "" {
		currentIndexSHA256 = closure.CurrentIndexSHA256
	}
	ledger.Append(root, true, ledger.Event{
		Op: "entries_recover", Source: source, Result: ledger.ResultOK,
		DraftRunID: runID, RejectKinds: closure.Reason,
		RecoveryStatus:        draft.RunResolutionZeroWrite,
		RecoveryTransactionID: closure.StagedTransactionID,
		PreIndexSHA256:        closure.PreIndexSHA256,
		IndexSHA256:           currentIndexSHA256,
		BaselineSHA256:        closure.CurrentBaselineSHA256,
		RepositorySHA256:      closure.RepositorySHA256,
		GovernanceReceipts:    append([]string{}, closure.GovernanceReceipts...),
	})
	events, corrupt = ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("entries_recovery_ledger_corrupt_after_zero_write_audit: %d", corrupt)
	}
	for _, event := range events {
		if matchesEntriesZeroWriteClosureEvent(event, runID, closure) {
			return nil
		}
	}
	return fmt.Errorf("entries_recovery_ledger_zero_write_event_missing")
}

func matchesEntriesZeroWriteClosureEvent(
	event ledger.Event,
	runID string,
	closure draft.ZeroWriteClosure,
) bool {
	currentIndexSHA256 := closure.PreIndexSHA256
	if closure.CurrentIndexSHA256 != "" {
		currentIndexSHA256 = closure.CurrentIndexSHA256
	}
	return event.Op == "entries_recover" && event.DraftRunID == runID &&
		event.Result == ledger.ResultOK &&
		event.RecoveryStatus == draft.RunResolutionZeroWrite &&
		event.RecoveryTransactionID == closure.StagedTransactionID &&
		event.PreIndexSHA256 == closure.PreIndexSHA256 &&
		event.IndexSHA256 == currentIndexSHA256 &&
		event.BaselineSHA256 == closure.CurrentBaselineSHA256 &&
		event.RepositorySHA256 == closure.RepositorySHA256 &&
		strings.Join(event.GovernanceReceipts, "\x00") ==
			strings.Join(closure.GovernanceReceipts, "\x00") &&
		event.RejectKinds == closure.Reason
}

func entriesRecoveryResultFromResolution(
	runID string,
	record draft.RunResolutionRecord,
	recovered int,
	alreadyResolved bool,
) *entriesRecoveryResult {
	return &entriesRecoveryResult{
		Version: 1, RunID: runID, Status: record.Status,
		FailureKinds: record.FailureKinds, Recovered: recovered,
		AlreadyResolved:       alreadyResolved,
		PreIndexSHA256:        record.PreIndexSHA256,
		PostIndexSHA256:       record.PostIndexSHA256,
		CurrentIndexSHA256:    record.CurrentIndexSHA256,
		CurrentBaselineSHA256: record.CurrentBaselineSHA256,
		RepositorySHA256:      record.RepositorySHA256,
		GovernanceReceipts:    append([]string{}, record.GovernanceReceipts...),
		ArchivedRecoveryAsset: record.ArchivedRecoveryAsset,
	}
}

func recoveredApplicationCount(manifest *draft.Manifest) int {
	if manifest == nil || manifest.AppliedAt == "" {
		return 0
	}
	for _, application := range manifest.Applications {
		if application.At == manifest.AppliedAt && application.Applied == 0 &&
			application.Recovered > 0 && application.Rejected == 0 &&
			application.RejectKinds == "" {
			return application.Recovered
		}
	}
	return 0
}

func ensureEntriesRecoveryLedger(
	root string,
	cfg *config.Config,
	runID,
	source string,
	record draft.RunResolutionRecord,
	recovered int,
	duration time.Duration,
) error {
	if cfg == nil || !cfg.LedgerEnabled {
		return nil
	}
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("entries_recovery_ledger_corrupt_before_terminal_audit: %d", corrupt)
	}
	for _, event := range events {
		if event.Op != "entries_recover" || event.DraftRunID != runID {
			continue
		}
		if event.Result == ledger.ResultOK &&
			event.RecoveryStatus == record.Status &&
			event.RecoveryTransactionID == record.TransactionID &&
			event.PreIndexSHA256 == record.PreIndexSHA256 &&
			event.PostIndexSHA256 == record.PostIndexSHA256 &&
			event.IndexSHA256 == record.CurrentIndexSHA256 &&
			event.BaselineSHA256 == record.CurrentBaselineSHA256 &&
			event.RepositorySHA256 == record.RepositorySHA256 &&
			strings.Join(event.GovernanceReceipts, "\x00") ==
				strings.Join(record.GovernanceReceipts, "\x00") {
			return nil
		}
		return fmt.Errorf("entries_recovery_ledger_terminal_event_conflict")
	}
	ledger.Append(root, true, ledger.Event{
		Op: "entries_recover", Source: source, Result: ledger.ResultOK,
		DraftRunID: runID, RecoveredCount: recovered,
		RejectKinds: record.FailureKinds, RecoveryStatus: record.Status,
		PreIndexSHA256:        record.PreIndexSHA256,
		PostIndexSHA256:       record.PostIndexSHA256,
		IndexSHA256:           record.CurrentIndexSHA256,
		BaselineSHA256:        record.CurrentBaselineSHA256,
		RepositorySHA256:      record.RepositorySHA256,
		RecoveryTransactionID: record.TransactionID,
		GovernanceReceipts:    append([]string{}, record.GovernanceReceipts...),
		DurationMs:            duration.Milliseconds(),
	})
	events, corrupt = ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("entries_recovery_ledger_corrupt_after_terminal_audit: %d", corrupt)
	}
	for _, event := range events {
		if event.Op == "entries_recover" && event.DraftRunID == runID &&
			event.RecoveryTransactionID == record.TransactionID &&
			event.RecoveryStatus == record.Status {
			return nil
		}
	}
	return fmt.Errorf("entries_recovery_ledger_terminal_event_missing")
}

func originalEntriesFailureEvidence(
	root,
	runID string,
	manifest *draft.Manifest,
	draftHash string,
) (string, bool, error) {
	kinds := map[string]bool{}
	wrote := false
	for _, application := range manifest.Applications {
		if application.DraftHash != "" && application.DraftHash != draftHash {
			continue
		}
		applicationKinds := []string{}
		for _, kind := range strings.Split(application.RejectKinds, ",") {
			if kind == "baseline_incomplete" || kind == "application_audit" {
				applicationKinds = append(applicationKinds, kind)
			}
		}
		if application.Applied+application.Recovered > 0 && len(applicationKinds) > 0 {
			wrote = true
			for _, kind := range applicationKinds {
				kinds[kind] = true
			}
		}
	}
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return "", false, fmt.Errorf("entries_recovery_ledger_corrupt: %d", corrupt)
	}
	for _, event := range events {
		if event.Op != "entries_apply" || event.DraftRunID != runID ||
			event.Result != ledger.ResultError {
			continue
		}
		eventKinds := []string{}
		for _, kind := range strings.Split(event.RejectKinds, ",") {
			if kind == "baseline_incomplete" || kind == "application_audit" {
				eventKinds = append(eventKinds, kind)
			}
		}
		if event.AppliedCount+event.RecoveredCount > 0 && len(eventKinds) > 0 {
			wrote = true
			for _, kind := range eventKinds {
				kinds[kind] = true
			}
		}
	}
	ordered := []string{}
	for _, kind := range []string{"baseline_incomplete", "application_audit"} {
		if kinds[kind] {
			ordered = append(ordered, kind)
		}
	}
	if len(ordered) == 0 {
		return "", wrote, fmt.Errorf("entries_recovery_original_failure_class_missing")
	}
	return strings.Join(ordered, ","), wrote, nil
}

func rejectEntriesApplyEvidence(root, runID string) error {
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("entries_recovery_ledger_corrupt: %d", corrupt)
	}
	for _, event := range events {
		if event.Op == "entries_apply" && event.DraftRunID == runID {
			return fmt.Errorf("entries_recovery_pre_apply_ledger_conflict")
		}
	}
	return nil
}

func hasSuccessfulRecoveryApplication(manifest *draft.Manifest, draftHash string, count int) bool {
	if manifest == nil || manifest.AppliedAt == "" {
		return false
	}
	for _, application := range manifest.Applications {
		if application.At == manifest.AppliedAt && application.DraftHash == draftHash &&
			application.PathsCount == count && application.Applied == 0 &&
			application.Recovered == count && application.Rejected == 0 &&
			application.RejectKinds == "" {
			return true
		}
	}
	return false
}

func itemPaths(items []mcptools.AtomicUpdateItem) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.Path)
	}
	return paths
}

func proveCurrentEntriesAlignment(
	root string,
	indexPath string,
) (*entriesAlignmentProof, error) {
	return proveCurrentEntriesAlignmentWithPolicy(root, indexPath, true)
}

func proveCurrentEntriesZeroWriteAlignment(
	root string,
	indexPath string,
) (*entriesAlignmentProof, error) {
	return proveCurrentEntriesAlignmentWithPolicy(root, indexPath, false)
}

func proveCurrentEntriesAlignmentWithPolicy(
	root string,
	indexPath string,
	requireComplete bool,
) (*entriesAlignmentProof, error) {
	configPath := filepath.Join(root, ".aoci", "config.json")
	configState, err := optionalRecoveryProofFileState(configPath)
	if err != nil {
		return nil, err
	}
	proofConfig, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	paths := config.AOCIPaths(root, proofConfig.IndexPath)
	if paths.IndexPath != indexPath {
		return nil, fmt.Errorf("entries_recovery_index_path_changed_during_proof")
	}
	baselineData, err := os.ReadFile(paths.BaselinePath)
	if err != nil {
		return nil, fmt.Errorf("entries_recovery_current_baseline_missing_or_corrupt: %w", err)
	}
	baselineSHA256 := sha256Bytes(baselineData)
	curationState, err := optionalRecoveryProofFileState(paths.CurationPath)
	if err != nil {
		return nil, err
	}
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	document, parseErrs := index.Parse(string(indexBytes))
	if document == nil || len(parseErrs) > 0 {
		return nil, fmt.Errorf("entries_recovery_current_index_invalid")
	}
	index.ResolveRelPaths(document, root)
	currentBaseline, exists, err := baseline.Load(root)
	if err != nil || !exists || currentBaseline == nil {
		return nil, fmt.Errorf("entries_recovery_current_baseline_missing_or_corrupt: %w", err)
	}
	snapshot, warnings, err := baseline.Snapshot(root, proofConfig.WalkOptions())
	if err != nil || len(warnings) > 0 {
		return nil, fmt.Errorf("entries_recovery_repository_snapshot_incomplete: err=%v warnings=%v", err, warnings)
	}
	detected := baseline.DetectWith(
		root, document, currentBaseline, snapshot,
		proofConfig.WalkOptions(), proofConfig.LineEndingTolerance,
	)
	if len(detected.Orphan) > 0 || len(detected.Stale) > 0 || len(detected.Unbaselined) > 0 {
		return nil, fmt.Errorf(
			"entries_recovery_current_alignment_drift: missing=%v orphan=%v stale=%v unbaselined=%v",
			detected.Missing, detected.Orphan, detected.Stale, detected.Unbaselined,
		)
	}
	score, err := indexgen.BuildScore(root, proofConfig, document)
	if err != nil {
		return nil, err
	}
	if dimByName(score, "format").Bad > 0 || dimByName(score, "dict").Bad > 0 {
		return nil, fmt.Errorf("entries_recovery_current_check_hard_gate_failed")
	}
	if requireComplete {
		if score.Drift.ActionableMissing > 0 || score.Drift.PendingCurationMissing > 0 {
			return nil, fmt.Errorf("entries_recovery_current_check_hard_gate_failed")
		}
		plan, err := buildAgentPlan(root, proofConfig, document, indexPath)
		if err != nil || plan.Stage != agentPlanStageAligned {
			return nil, fmt.Errorf("entries_recovery_current_guide_not_aligned: stage=%v err=%v", func() string {
				if plan == nil {
					return ""
				}
				return plan.Stage
			}(), err)
		}
	}
	repositorySHA256 := calculateRepositorySnapshotHash(snapshot)
	secondSnapshot, secondWarnings, err := baseline.Snapshot(root, proofConfig.WalkOptions())
	if err != nil || len(secondWarnings) > 0 ||
		calculateRepositorySnapshotHash(secondSnapshot) != repositorySHA256 {
		return nil, fmt.Errorf("entries_recovery_repository_changed_during_proof")
	}
	confirmedIndexSHA256, err := rawFileSHA256(indexPath)
	if err != nil || confirmedIndexSHA256 != sha256Bytes(indexBytes) {
		return nil, fmt.Errorf("entries_recovery_index_changed_during_proof")
	}
	confirmedBaselineSHA256, err := rawFileSHA256(paths.BaselinePath)
	if err != nil || confirmedBaselineSHA256 != baselineSHA256 {
		return nil, fmt.Errorf("entries_recovery_baseline_changed_during_proof")
	}
	confirmedConfigState, err := optionalRecoveryProofFileState(paths.ConfigPath)
	if err != nil || confirmedConfigState != configState {
		return nil, fmt.Errorf("entries_recovery_config_changed_during_proof")
	}
	confirmedCurationState, err := optionalRecoveryProofFileState(paths.CurationPath)
	if err != nil || confirmedCurationState != curationState {
		return nil, fmt.Errorf("entries_recovery_curation_changed_during_proof")
	}
	return &entriesAlignmentProof{
		IndexSHA256: confirmedIndexSHA256, BaselineSHA256: baselineSHA256,
		RepositorySHA256: repositorySHA256,
	}, nil
}

func optionalRecoveryProofFileState(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	return "present:" + sha256Bytes(data), nil
}

func rawFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Bytes(data), nil
}

func sha256Bytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
