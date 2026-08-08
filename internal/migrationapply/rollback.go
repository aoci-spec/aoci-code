package migrationapply

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Rollback restores the exact Legacy and Baseline preimages of an incomplete
// Migration. It never operates on an archived, completed transaction.
func Rollback(root, transactionID string) (*ApplyResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lock, err := afs.AcquireIndexLock(absRoot)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	intent, err := loadRecoveryAt(intentPath(absRoot, transactionID), transactionID)
	if err != nil {
		if archived, archiveErr := loadRecoveryAt(archivePath(absRoot, transactionID), transactionID); archiveErr == nil {
			if result, resultErr := loadResultAt(rollbackPath(absRoot, transactionID), transactionID); resultErr == nil && result.Status == machinecontract.CognitionMigrationStatusRolledBack && archived.TransactionID == transactionID {
				return result, nil
			}
		}
		return nil, fmt.Errorf("migration_incomplete_transaction_required")
	}
	if err := cognitiontxn.RejectOtherPending(absRoot, "migration-"+transactionID+".json"); err != nil {
		return nil, err
	}
	if err := cognitionplan.ValidateExternalGuards(absRoot, &intent.Envelope.Plan); err != nil {
		return nil, fmt.Errorf("migration_guard_drift: %w", err)
	}
	status, err := inspectEnvelopeState(absRoot, transactionID, &intent.Envelope, true)
	if err != nil {
		return nil, err
	}
	if status.Status == machinecontract.CognitionMigrationStatusRecoveryConflict {
		return nil, fmt.Errorf("migration_recovery_conflict")
	}
	legacyBytes, err := decodeSnapshotContent(intent.Envelope.Snapshot.LegacyContentBase64, intent.Envelope.Snapshot.LegacySHA256)
	if err != nil {
		return nil, err
	}
	baselineBytes, err := decodeSnapshotContent(intent.Envelope.Snapshot.BaselineContentBase64, intent.Envelope.Snapshot.BaselineSHA256)
	if err != nil {
		return nil, err
	}

	rootState, _ := statusTarget(status, intent.Envelope.Root.Path)
	baselineState, _ := statusTarget(status, intent.Envelope.Baseline.Path)
	if rootState.DiskState == cognitiontxn.StatePostimage {
		if err := migrationFault("before_rollback_root"); err != nil {
			return nil, err
		}
		if err := afs.AtomicWriteCAS(filepath.Join(absRoot, "aoci.txt"), legacyBytes, intent.Envelope.Root.PostSHA256); err != nil {
			return nil, fmt.Errorf("migration_rollback_root_cas_failed: %w", err)
		}
		if err := migrationFault("after_rollback_root"); err != nil {
			return nil, err
		}
	} else if rootState.DiskState != cognitiontxn.StatePreimage {
		return nil, fmt.Errorf("migration_recovery_conflict: aoci.txt")
	}
	if baselineState.DiskState == cognitiontxn.StatePostimage {
		if err := migrationFault("before_rollback_baseline"); err != nil {
			return nil, err
		}
		if err := afs.AtomicWriteCAS(filepath.Join(absRoot, ".aoci", "baseline.json"), baselineBytes, intent.Envelope.Baseline.PostSHA256); err != nil {
			return nil, fmt.Errorf("migration_rollback_baseline_cas_failed: %w", err)
		}
		if err := migrationFault("after_rollback_baseline"); err != nil {
			return nil, err
		}
	} else if baselineState.DiskState != cognitiontxn.StatePreimage {
		return nil, fmt.Errorf("migration_recovery_conflict: .aoci/baseline.json")
	}

	if err := cognitiontxn.EnsureSafeDirectory(absRoot, filepath.ToSlash(filepath.Join(".aoci", "transactions", "migration-"+transactionID, "recovered"))); err != nil {
		return nil, err
	}
	for index, path := range []string{"aoci.database.txt", "aoci.code.txt", "aoci.meta.txt"} {
		target, declared := statusTarget(status, path)
		if !declared || target.DiskState == cognitiontxn.StatePreimage {
			continue
		}
		if target.DiskState != cognitiontxn.StatePostimage {
			return nil, fmt.Errorf("migration_recovery_conflict: %s", path)
		}
		recoveryTarget := filepath.Join(absRoot, ".aoci", "transactions", "migration-"+transactionID, "recovered", fmt.Sprintf("%02d-%s", index, filepath.Base(path)))
		if err := migrationFault("before_rollback_" + target.Kind); err != nil {
			return nil, err
		}
		if err := afs.AtomicMoveCAS(filepath.Join(absRoot, filepath.FromSlash(path)), recoveryTarget, target.ActualSHA256); err != nil {
			return nil, fmt.Errorf("migration_rollback_move_failed[%s]: %w", path, err)
		}
		if err := migrationFault("after_rollback_" + target.Kind); err != nil {
			return nil, err
		}
	}
	recovered := recoveredMigrationPaths(absRoot, transactionID, &intent.Envelope)
	if err := verifyLegacyPreimage(absRoot, &intent.Envelope.Snapshot); err != nil {
		return nil, err
	}
	result := &ApplyResult{
		Version: machinecontract.CognitionMigrationApplyResultV2, Operation: OperationMigration,
		TransactionID: transactionID, Status: machinecontract.CognitionMigrationStatusRolledBack,
		ActiveLayout: "legacy", FormalComplete: false, WrittenPaths: []string{}, RecoveredPaths: recovered,
		BaselineSHA256: intent.Envelope.Snapshot.BaselineSHA256, CompositeIdentity: "",
		NetworkAccessed: false, NextAction: "migration_may_be_replanned",
	}
	result.ResultDigest, _ = applyResultDigest(result)
	data, _ := prettyJSON(result)
	if err := migrationFault("before_rollback_receipt"); err != nil {
		return nil, err
	}
	if err := cognitiontxn.SaveImmutable(rollbackPath(absRoot, transactionID), data); err != nil {
		return nil, err
	}
	if err := migrationFault("after_rollback_receipt"); err != nil {
		return nil, err
	}
	if err := migrationFault("before_rollback_archive"); err != nil {
		return nil, err
	}
	if err := archiveRecovery(absRoot, intent); err != nil {
		return nil, err
	}
	if err := migrationFault("after_rollback_archive"); err != nil {
		return nil, err
	}
	return result, nil
}

func recoveredMigrationPaths(root, transactionID string, envelope *ApplyEnvelope) []string {
	result := []string{}
	declared := map[string]bool{}
	for _, target := range envelope.VolumeTargets {
		declared[target.Path] = true
	}
	for index, path := range []string{"aoci.database.txt", "aoci.code.txt", "aoci.meta.txt"} {
		if !declared[path] {
			continue
		}
		if info, err := os.Lstat(filepath.Join(root, ".aoci", "transactions", "migration-"+transactionID, "recovered", fmt.Sprintf("%02d-%s", index, filepath.Base(path)))); err == nil && info.Mode().IsRegular() {
			result = append(result, path)
		}
	}
	return result
}

func decodeSnapshotContent(encoded, digest string) ([]byte, error) {
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || sha256Hex(data) != digest {
		return nil, fmt.Errorf("migration_snapshot_content_invalid")
	}
	return data, nil
}

func verifyLegacyPreimage(root string, snapshot *LegacySnapshot) error {
	legacy, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil || sha256Hex(legacy) != snapshot.LegacySHA256 {
		return fmt.Errorf("migration_legacy_restore_invalid")
	}
	baselineBytes, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil || sha256Hex(baselineBytes) != snapshot.BaselineSHA256 {
		return fmt.Errorf("migration_legacy_baseline_restore_invalid")
	}
	set, loadErr := cognition.Load(root, "aoci.txt")
	if loadErr != nil || set == nil || set.LayoutMode != cognition.LayoutLegacyMonolithic || !bytes.Equal(set.Root.Raw, legacy) {
		return fmt.Errorf("migration_legacy_internal_verify_failed")
	}
	return nil
}
