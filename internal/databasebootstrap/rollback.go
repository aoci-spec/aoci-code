package databasebootstrap

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Rollback restores the exact Code-only preimage of an incomplete Database
// Bootstrap. Unknown or third-party bytes stop the operation.
func Rollback(root, transactionID string) (*Result, error) {
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
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_incomplete_transaction_required")
	}
	preview := &intent.Preview
	if err := validateGuards(absRoot, preview); err != nil {
		return nil, err
	}
	type targetState struct {
		path  string
		state string
	}
	targets := []targetState{}
	for _, target := range []struct {
		path, pre, post string
		absent          bool
	}{
		{preview.DatabasePath, "", preview.DatabasePostimageSHA256, true},
		{preview.RootPath, preview.RootPreimageSHA256, preview.RootPostimageSHA256, false},
		{preview.BaselinePath, preview.BaselinePreimageSHA256, preview.BaselinePostimageSHA256, false},
	} {
		state, _, err := cognitiontxn.Classify(filepath.Join(absRoot, filepath.FromSlash(target.path)), target.pre, target.post, target.absent)
		if err != nil || (state != cognitiontxn.StatePreimage && state != cognitiontxn.StatePostimage) {
			return nil, fmt.Errorf("database_bootstrap_recovery_conflict: %s", target.path)
		}
		targets = append(targets, targetState{target.path, state})
	}
	stateFor := func(path string) string {
		for _, target := range targets {
			if target.path == path {
				return target.state
			}
		}
		return ""
	}
	if stateFor(preview.BaselinePath) == cognitiontxn.StatePostimage {
		if err := afs.AtomicWriteCAS(filepath.Join(absRoot, filepath.FromSlash(preview.BaselinePath)), []byte(preview.BaselinePreimage), preview.BaselinePostimageSHA256); err != nil {
			return nil, fmt.Errorf("database_bootstrap_rollback_baseline_failed: %w", err)
		}
	}
	if stateFor(preview.RootPath) == cognitiontxn.StatePostimage {
		preimage := rootPreimage(preview)
		if cognitiontxn.SHA256(preimage) != preview.RootPreimageSHA256 {
			return nil, fmt.Errorf("database_bootstrap_rollback_root_preimage_invalid")
		}
		if err := afs.AtomicWriteCAS(filepath.Join(absRoot, filepath.FromSlash(preview.RootPath)), preimage, preview.RootPostimageSHA256); err != nil {
			return nil, fmt.Errorf("database_bootstrap_rollback_root_failed: %w", err)
		}
	}
	if stateFor(preview.DatabasePath) == cognitiontxn.StatePostimage {
		recoveredDir := filepath.Join(transactionDir(absRoot, transactionID), "recovered")
		if err := os.MkdirAll(recoveredDir, 0o700); err != nil {
			return nil, err
		}
		target := filepath.Join(recoveredDir, filepath.Base(preview.DatabasePath))
		if err := afs.AtomicMoveCAS(filepath.Join(absRoot, filepath.FromSlash(preview.DatabasePath)), target, preview.DatabasePostimageSHA256); err != nil {
			return nil, fmt.Errorf("database_bootstrap_rollback_database_failed: %w", err)
		}
	}
	result := &Result{Version: machinecontract.DatabaseCognitionBootstrapResultV1,
		Operation: machinecontract.CognitionOperationDatabaseBootstrap, TransactionID: transactionID,
		Status: StatusRolledBack, DatabaseReady: false, DatabaseEntryCount: 0,
		CodeVolumeSHA256: preview.CodeVolumeSHA256, RootSHA256: preview.RootPreimageSHA256,
		BaselineSHA256: preview.BaselinePreimageSHA256, NetworkAccessed: false, BusinessDataRead: false,
		DDLDMLStatements: 0, NextAction: "database_bootstrap_may_be_replanned"}
	result.ResultDigest, err = resultDigest(result)
	if err != nil {
		return nil, err
	}
	if err := saveResult(absRoot, result); err != nil {
		return nil, err
	}
	if err := archiveIntent(absRoot, intent); err != nil {
		return nil, err
	}
	return result, nil
}

func rootPreimage(preview *Preview) []byte {
	post := []byte(preview.RootPostimage)
	descriptor := []byte(databaseDescriptor + "\n")
	if index := bytes.Index(post, descriptor); index >= 0 {
		return append(append([]byte{}, post[:index]...), post[index+len(descriptor):]...)
	}
	descriptor = []byte(databaseDescriptor + "\r\n")
	if index := bytes.Index(post, descriptor); index >= 0 {
		return append(append([]byte{}, post[:index]...), post[index+len(descriptor):]...)
	}
	return nil
}
