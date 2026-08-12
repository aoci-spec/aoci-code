package databasebootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

var transactionFault = func(string) error { return nil }

func Pending(root string) ([]string, error) {
	return cognitiontxn.PendingForOperation(root, Operation)
}

func Apply(root string, preview *Preview) (*Result, error) {
	if err := validatePreview(preview); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	transactionID := preview.PreviewDigest[:32]
	lock, err := afs.AcquireIndexLock(absRoot)
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_write_lock_failed: %w", err)
	}
	defer lock.Release()
	if err := cognitiontxn.EnsureSafeDirectory(absRoot, ".aoci/transactions/history"); err != nil {
		return nil, err
	}
	if archived, archiveErr := loadIntent(archivePath(absRoot, transactionID), transactionID); archiveErr == nil {
		if !samePreview(&archived.Preview, preview) {
			return nil, fmt.Errorf("database_bootstrap_completed_binding_conflict")
		}
		result, err := loadResult(absRoot, transactionID)
		if err != nil {
			return nil, err
		}
		copyResult := *result
		copyResult.Status = StatusAlreadyApplied
		return &copyResult, nil
	} else if !errors.Is(archiveErr, os.ErrNotExist) {
		return nil, archiveErr
	}
	activePath := intentPath(absRoot, transactionID)
	if err := cognitiontxn.RejectOtherPending(absRoot, filepath.Base(activePath)); err != nil {
		return nil, err
	}
	intent, loadErr := loadIntent(activePath, transactionID)
	if errors.Is(loadErr, os.ErrNotExist) {
		preparedAt, err := time.Parse(time.RFC3339, preview.PreparedAt)
		if err != nil {
			return nil, fmt.Errorf("database_bootstrap_prepared_at_invalid")
		}
		replayed, err := prepare(absRoot, preparedAt)
		if err != nil || !samePreview(replayed, preview) {
			return nil, fmt.Errorf("database_bootstrap_preview_replay_mismatch")
		}
		staging, err := cognitiontxn.Stage(absRoot, Operation, transactionID, []cognitiontxn.Postimage{
			{Path: preview.DatabasePath, SHA: preview.DatabasePostimageSHA256, Data: []byte(preview.DatabasePostimage)},
			{Path: preview.RootPath, SHA: preview.RootPostimageSHA256, Data: []byte(preview.RootPostimage)},
			{Path: preview.BaselinePath, SHA: preview.BaselinePostimageSHA256, Data: []byte(preview.BaselinePostimage)},
		}, transactionFault)
		if err != nil {
			return nil, err
		}
		intent = &RecoveryIntent{Version: machinecontract.DatabaseCognitionBootstrapRecoveryV1,
			TransactionID: transactionID, Preview: *preview, Staging: staging, CreatedAt: preview.PreparedAt}
		intent.RecoveryDigest, err = recoveryDigest(intent)
		if err != nil {
			return nil, err
		}
		if err := saveIntent(activePath, intent); err != nil {
			return nil, err
		}
		if err := transactionFault("after_recovery_intent"); err != nil {
			return nil, err
		}
	} else if loadErr != nil {
		return nil, loadErr
	} else if !samePreview(&intent.Preview, preview) {
		return nil, fmt.Errorf("database_bootstrap_pending_binding_conflict")
	}
	return advance(absRoot, intent)
}

func Resume(root, transactionID string) (*Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lock, err := afs.AcquireIndexLock(absRoot)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	if err := cognitiontxn.EnsureSafeDirectory(absRoot, ".aoci/transactions/history"); err != nil {
		return nil, err
	}
	if transactionID == "" {
		pending, err := Pending(absRoot)
		if err != nil || len(pending) != 1 {
			return nil, fmt.Errorf("database_bootstrap_transaction_id_required")
		}
		transactionID = pending[0]
	}
	intent, err := loadIntent(intentPath(absRoot, transactionID), transactionID)
	if errors.Is(err, os.ErrNotExist) {
		result, resultErr := loadResult(absRoot, transactionID)
		if resultErr != nil {
			return nil, fmt.Errorf("database_bootstrap_transaction_not_found")
		}
		copyResult := *result
		copyResult.Status = StatusAlreadyApplied
		return &copyResult, nil
	}
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.RejectOtherPending(absRoot, filepath.Base(intentPath(absRoot, transactionID))); err != nil {
		return nil, err
	}
	return advance(absRoot, intent)
}

func advance(root string, intent *RecoveryIntent) (*Result, error) {
	preview := &intent.Preview
	if err := validateGuards(root, preview); err != nil {
		return nil, err
	}
	databaseState, _, err := cognitiontxn.Classify(filepath.Join(root, preview.DatabasePath), "", preview.DatabasePostimageSHA256, true)
	if err != nil || (databaseState != cognitiontxn.StatePreimage && databaseState != cognitiontxn.StatePostimage) {
		return nil, fmt.Errorf("database_bootstrap_database_conflict")
	}
	rootState, _, err := cognitiontxn.Classify(filepath.Join(root, preview.RootPath), preview.RootPreimageSHA256, preview.RootPostimageSHA256, false)
	if err != nil || (rootState != cognitiontxn.StatePreimage && rootState != cognitiontxn.StatePostimage) {
		return nil, fmt.Errorf("database_bootstrap_root_conflict")
	}
	baselineState, _, err := cognitiontxn.Classify(filepath.Join(root, preview.BaselinePath), preview.BaselinePreimageSHA256, preview.BaselinePostimageSHA256, false)
	if err != nil || (baselineState != cognitiontxn.StatePreimage && baselineState != cognitiontxn.StatePostimage) {
		return nil, fmt.Errorf("database_bootstrap_baseline_conflict")
	}
	if rootState == cognitiontxn.StatePostimage && databaseState != cognitiontxn.StatePostimage {
		return nil, fmt.Errorf("database_bootstrap_write_order_conflict")
	}
	if baselineState == cognitiontxn.StatePostimage && rootState != cognitiontxn.StatePostimage {
		return nil, fmt.Errorf("database_bootstrap_write_order_conflict")
	}
	if databaseState == cognitiontxn.StatePreimage {
		if err := publishCreate(root, intent, preview.DatabasePath, preview.DatabasePostimageSHA256); err != nil {
			return nil, err
		}
	}
	if rootState == cognitiontxn.StatePreimage {
		if err := publishReplace(root, intent, preview.RootPath, preview.RootPreimageSHA256, preview.RootPostimageSHA256); err != nil {
			return nil, err
		}
	}
	if baselineState == cognitiontxn.StatePreimage {
		if err := publishReplace(root, intent, preview.BaselinePath, preview.BaselinePreimageSHA256, preview.BaselinePostimageSHA256); err != nil {
			return nil, err
		}
	}
	set, err := verifyPostimage(root, preview)
	if err != nil {
		return nil, err
	}
	result := &Result{Version: machinecontract.DatabaseCognitionBootstrapResultV1,
		Operation: machinecontract.CognitionOperationDatabaseBootstrap, TransactionID: intent.TransactionID,
		Status: StatusApplied, DatabaseReady: true, DatabaseEntryCount: 0,
		CodeVolumeSHA256: preview.CodeVolumeSHA256, RootSHA256: preview.RootPostimageSHA256,
		BaselineSHA256: preview.BaselinePostimageSHA256, NetworkAccessed: false, BusinessDataRead: false,
		DDLDMLStatements: 0, NextAction: machinecontract.DatabaseCognitionActionMaintain}
	result.ResultDigest, err = resultDigest(result)
	if err != nil {
		return nil, err
	}
	if err := saveResult(root, result); err != nil {
		return nil, err
	}
	cfg, _ := config.LoadReadOnly(root)
	ledgerEnabled := cfg != nil && cfg.LedgerEnabled
	if ledgerEnabled {
		if err := cognitiontxn.EnsureLedger(root, true, ledger.Event{Op: "database_cognition_bootstrap", Source: ledger.SourceAgent,
			Result: ledger.ResultOK, AppliedCount: 3, RecoveryTransactionID: intent.TransactionID,
			BaselineSHA256: preview.BaselinePostimageSHA256, IndexSHA256: set.Root.SHA256,
			PreIndexSHA256: preview.RootPreimageSHA256, PostIndexSHA256: preview.RootPostimageSHA256}); err != nil {
			return nil, err
		}
	}
	if err := archiveIntent(root, intent); err != nil {
		return nil, err
	}
	return result, nil
}

func publishCreate(root string, intent *RecoveryIntent, path, postSHA string) error {
	if err := transactionFault("before_publish_" + filepath.Base(path)); err != nil {
		return err
	}
	data, err := cognitiontxn.ReadStaged(root, intent.Staging, path)
	if err != nil || cognitiontxn.SHA256(data) != postSHA {
		return fmt.Errorf("database_bootstrap_staging_invalid: %s", path)
	}
	if err := afs.AtomicCreateCASMode(filepath.Join(root, filepath.FromSlash(path)), data, 0o644); err != nil {
		return fmt.Errorf("database_bootstrap_atomic_create_failed[%s]: %w", path, err)
	}
	return transactionFault("after_publish_" + filepath.Base(path))
}

func publishReplace(root string, intent *RecoveryIntent, path, preSHA, postSHA string) error {
	if err := transactionFault("before_publish_" + filepath.Base(path)); err != nil {
		return err
	}
	data, err := cognitiontxn.ReadStaged(root, intent.Staging, path)
	if err != nil || cognitiontxn.SHA256(data) != postSHA {
		return fmt.Errorf("database_bootstrap_staging_invalid: %s", path)
	}
	if err := afs.AtomicWriteCAS(filepath.Join(root, filepath.FromSlash(path)), data, preSHA); err != nil {
		return fmt.Errorf("database_bootstrap_atomic_replace_failed[%s]: %w", path, err)
	}
	return transactionFault("after_publish_" + filepath.Base(path))
}

func validateGuards(root string, preview *Preview) error {
	for _, guard := range []struct{ path, sha string }{
		{"aoci.meta.txt", preview.MetaSHA256}, {"aoci.code.txt", preview.CodeVolumeSHA256},
		{".aoci/database-baseline.json", preview.EvidenceBaselineSHA256},
	} {
		data, err := readRegular(filepath.Join(root, filepath.FromSlash(guard.path)))
		if err != nil || cognitiontxn.SHA256(data) != guard.sha {
			return fmt.Errorf("database_bootstrap_guard_changed: %s", guard.path)
		}
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return fmt.Errorf("database_bootstrap_guard_changed: database_sources")
	}
	configured := map[string]dbevidence.SourceConfig{}
	for _, source := range cfg.DatabaseSources {
		configured[source.SourceID] = source
	}
	for _, expected := range preview.EvidenceSources {
		source, exists := configured[expected.SourceID]
		manifest, snapshot, snapshotExists, loadErr := dbevidence.LoadSnapshot(root, expected.SourceID)
		if !exists || !source.Enabled || loadErr != nil || !snapshotExists ||
			!dbevidence.SourceConfigMatchesManifest(source, manifest) ||
			snapshot.SourceSnapshotSHA256 != expected.SourceSnapshotSHA256 ||
			snapshot.EvidenceVersion != expected.EvidenceVersion || len(snapshot.Tables) != expected.TableCount {
			return fmt.Errorf("database_bootstrap_guard_changed: database_evidence[%s]", expected.SourceID)
		}
	}
	return nil
}

func verifyPostimage(root string, preview *Preview) (*cognition.Set, error) {
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set == nil || set.CompositeIdentity != preview.ProjectedCompositeIdentity ||
		set.Volumes[cognition.ScopeDatabase] == nil || set.Volumes[cognition.ScopeDatabase].State != cognition.AssetPresent ||
		len(set.Volumes[cognition.ScopeDatabase].Objects) != 0 || set.Volumes[cognition.ScopeCode].SHA256 != preview.CodeVolumeSHA256 {
		return nil, fmt.Errorf("database_bootstrap_internal_verify_failed")
	}
	baselineRaw, err := readRegular(filepath.Join(root, filepath.FromSlash(preview.BaselinePath)))
	if err != nil || cognitiontxn.SHA256(baselineRaw) != preview.BaselinePostimageSHA256 {
		return nil, fmt.Errorf("database_bootstrap_baseline_verify_failed")
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.Files[preview.DatabasePath].SHA256 != preview.DatabasePostimageSHA256 {
		return nil, fmt.Errorf("database_bootstrap_baseline_binding_failed")
	}
	rootBinding, err := classifyRootBaselinePostimage(preview)
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_baseline_binding_failed")
	}
	if rootBinding.required {
		actual, managed := state.Files[preview.RootPath]
		if !managed || actual != rootBinding.expected {
			return nil, fmt.Errorf("database_bootstrap_baseline_binding_failed")
		}
	}
	evidenceRaw, err := readRegular(dbevidence.BaselinePath(root))
	if err != nil || cognitiontxn.SHA256(evidenceRaw) != preview.EvidenceBaselineSHA256 {
		return nil, fmt.Errorf("database_bootstrap_evidence_guard_changed")
	}
	return set, nil
}

type rootBaselinePostimage struct {
	required bool
	expected baseline.Fingerprint
}

// classifyRootBaselinePostimage derives the Root binding contract from the
// frozen Preview instead of introducing a new recovery-schema field. This
// keeps pending v1 transactions created by an older binary resumable: their
// Baseline postimage retained the exact Root preimage fingerprint. New
// previews bind an already-managed Root to its postimage, while repositories
// that did not manage Root remain unenrolled.
func classifyRootBaselinePostimage(preview *Preview) (rootBaselinePostimage, error) {
	var before, after baseline.Baseline
	if err := json.Unmarshal([]byte(preview.BaselinePreimage), &before); err != nil || before.Files == nil {
		return rootBaselinePostimage{}, errors.New("invalid Baseline preimage")
	}
	if err := json.Unmarshal([]byte(preview.BaselinePostimage), &after); err != nil || after.Files == nil {
		return rootBaselinePostimage{}, errors.New("invalid Baseline postimage")
	}
	preFingerprint, preManaged := before.Files[preview.RootPath]
	postFingerprint, postManaged := after.Files[preview.RootPath]
	if !preManaged && !postManaged {
		return rootBaselinePostimage{}, nil
	}
	if !preManaged || !postManaged || preFingerprint.SHA256 != preview.RootPreimageSHA256 ||
		preFingerprint.Role != postFingerprint.Role {
		return rootBaselinePostimage{}, errors.New("invalid Root Baseline transition")
	}

	expected := baseline.HashBytes(preview.RootPath, []byte(preview.RootPostimage))
	expected.Role = preFingerprint.Role
	if postFingerprint == expected {
		return rootBaselinePostimage{required: true, expected: expected}, nil
	}
	// Compatibility for an already-pending recovery created before Root and
	// Baseline were advanced together. Its immutable postimage is honored,
	// never silently rewritten during Resume or Rollback.
	if postFingerprint == preFingerprint {
		return rootBaselinePostimage{}, nil
	}
	return rootBaselinePostimage{}, errors.New("invalid Root Baseline transition")
}

func recoveryDigest(intent *RecoveryIntent) (string, error) {
	copyValue := *intent
	copyValue.RecoveryDigest = ""
	data, err := json.Marshal(copyValue)
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(data), nil
}

func resultDigest(result *Result) (string, error) {
	copyValue := *result
	copyValue.ResultDigest = ""
	data, err := json.Marshal(copyValue)
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(data), nil
}

func saveIntent(path string, intent *RecoveryIntent) error {
	data, err := prettyJSON(intent)
	if err != nil {
		return err
	}
	return cognitiontxn.SaveImmutable(path, data)
}

func loadIntent(path, transactionID string) (*RecoveryIntent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var intent RecoveryIntent
	if err := decodeStrict(data, &intent); err != nil || intent.Version != machinecontract.DatabaseCognitionBootstrapRecoveryV1 ||
		intent.TransactionID != transactionID || validatePreview(&intent.Preview) != nil {
		return nil, fmt.Errorf("database_bootstrap_recovery_intent_invalid")
	}
	digest, err := recoveryDigest(&intent)
	if err != nil || digest != intent.RecoveryDigest {
		return nil, fmt.Errorf("database_bootstrap_recovery_digest_invalid")
	}
	return &intent, nil
}

func saveResult(root string, result *Result) error {
	data, err := prettyJSON(result)
	if err != nil {
		return err
	}
	return cognitiontxn.SaveImmutable(resultPath(root, result.TransactionID), data)
}

func loadResult(root, transactionID string) (*Result, error) {
	data, err := os.ReadFile(resultPath(root, transactionID))
	if err != nil {
		return nil, err
	}
	var result Result
	if err := decodeStrict(data, &result); err != nil || result.Version != machinecontract.DatabaseCognitionBootstrapResultV1 ||
		result.TransactionID != transactionID {
		return nil, fmt.Errorf("database_bootstrap_result_invalid")
	}
	digest, err := resultDigest(&result)
	if err != nil || digest != result.ResultDigest {
		return nil, fmt.Errorf("database_bootstrap_result_digest_invalid")
	}
	return &result, nil
}

func archiveIntent(root string, intent *RecoveryIntent) error {
	data, err := prettyJSON(intent)
	if err != nil {
		return err
	}
	return cognitiontxn.ArchiveImmutable(intentPath(root, intent.TransactionID), archivePath(root, intent.TransactionID), data)
}

func prettyJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeStrict(data []byte, target any) error {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("database_bootstrap_trailing_json")
	}
	return nil
}

func transactionDir(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", Operation+"-"+transactionID)
}

func intentPath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", Operation+"-"+transactionID+".json")
}

func archivePath(root, transactionID string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", Operation+"-"+transactionID+".json")
}

func resultPath(root, transactionID string) string {
	return filepath.Join(transactionDir(root, transactionID), "result.json")
}
