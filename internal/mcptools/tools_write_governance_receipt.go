// Persistent proof chain for completed Entries governance.
//
// An active transaction proves one write's pre/postimage and recovery ownership.
// A separate append-only receipt proves every completed transition after it.
// Receipts never authorize candidate replay or replace current Baseline/repository
// alignment checks.
package mcptools

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

const entriesGovernanceReceiptVersion = 1

type entriesGovernanceReceipt struct {
	Version         int      `json:"version"`
	ReceiptID       string   `json:"receipt_id"`
	Kind            string   `json:"kind"`
	TransactionID   string   `json:"transaction_id"`
	PreIndexSHA256  string   `json:"pre_index_sha256"`
	PostIndexSHA256 string   `json:"post_index_sha256"`
	Paths           []string `json:"paths"`
	CompletedAt     string   `json:"completed_at"`
}

// EntriesGovernanceProof links an old transaction to the current index through
// completed Entries governance receipts.
type EntriesGovernanceProof struct {
	TransactionID      string
	PreIndexSHA256     string
	PostIndexSHA256    string
	CurrentIndexSHA256 string
	GovernanceReceipts []string
}

func governanceReceiptID(receipt entriesGovernanceReceipt) string {
	payload, _ := json.Marshal(struct {
		Version         int      `json:"version"`
		Kind            string   `json:"kind"`
		TransactionID   string   `json:"transaction_id"`
		PreIndexSHA256  string   `json:"pre_index_sha256"`
		PostIndexSHA256 string   `json:"post_index_sha256"`
		Paths           []string `json:"paths"`
		CompletedAt     string   `json:"completed_at"`
	}{
		Version: receipt.Version, Kind: receipt.Kind,
		TransactionID:  receipt.TransactionID,
		PreIndexSHA256: receipt.PreIndexSHA256, PostIndexSHA256: receipt.PostIndexSHA256,
		Paths: append([]string{}, receipt.Paths...), CompletedAt: receipt.CompletedAt,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func governanceBytesSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func governanceReceiptDirectory(root string) string {
	return filepath.Join(root, ".aoci", "governance")
}

func governanceReceiptPath(root, receiptID string) string {
	return filepath.Join(governanceReceiptDirectory(root), "entries-"+receiptID+".json")
}

func validateGovernanceReceipt(receipt entriesGovernanceReceipt) error {
	if receipt.Version != entriesGovernanceReceiptVersion || receipt.Kind != "entries" ||
		!validRecoverySHA256(receipt.ReceiptID) ||
		!validRecoverySHA256(receipt.TransactionID) ||
		!validRecoverySHA256(receipt.PreIndexSHA256) ||
		!validRecoverySHA256(receipt.PostIndexSHA256) ||
		receipt.PreIndexSHA256 == receipt.PostIndexSHA256 || len(receipt.Paths) == 0 {
		return fmt.Errorf("entries_governance_receipt_invalid")
	}
	if receipt.ReceiptID != governanceReceiptID(receipt) {
		return fmt.Errorf("entries_governance_receipt_id_mismatch")
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt.CompletedAt); err != nil {
		return fmt.Errorf("entries_governance_receipt_timestamp_invalid: %w", err)
	}
	seen := map[string]bool{}
	for _, rel := range receipt.Paths {
		normalized, err := afs.NormalizeRelPath(rel)
		if err != nil || normalized != rel || seen[rel] {
			return fmt.Errorf("entries_governance_receipt_path_invalid_or_duplicate: %q", rel)
		}
		seen[rel] = true
	}
	return nil
}

func marshalGovernanceReceipt(receipt entriesGovernanceReceipt) ([]byte, error) {
	if err := validateGovernanceReceipt(receipt); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeGovernanceReceipt(data []byte) (*entriesGovernanceReceipt, error) {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var receipt entriesGovernanceReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("entries_governance_receipt_trailing_json")
	}
	if err := validateGovernanceReceipt(receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func saveCompletedEntriesGovernanceReceipt(
	root string,
	plan *atomicBatchPlan,
	recovery *atomicBatchRecovery,
) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("entries_governance_receipt_missing_plan")
	}
	preIndexSHA256 := plan.indexHash
	postIndexSHA256 := indexTextHash(plan.finalText)
	if recovery != nil {
		if recovery.BatchKey != plan.batchKey || (plan.volumePlan == nil && recovery.PostIndexSHA256 != plan.indexHash) {
			return "", fmt.Errorf("entries_governance_recovery_plan_mismatch")
		}
		preIndexSHA256 = recovery.PreIndexSHA256
		postIndexSHA256 = recovery.PostIndexSHA256
	}
	paths := append([]string{}, plan.rels...)
	if plan.volumePlan != nil {
		paths = append([]string{}, plan.volumePlan.volumePaths...)
	}
	receipt := entriesGovernanceReceipt{
		Version:         entriesGovernanceReceiptVersion,
		Kind:            "entries",
		TransactionID:   plan.batchKey,
		PreIndexSHA256:  preIndexSHA256,
		PostIndexSHA256: postIndexSHA256,
		Paths:           paths,
		CompletedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	existingReceipts, err := loadAllEntriesGovernanceReceipts(root)
	if err != nil {
		return "", err
	}
	matchingReceiptID := ""
	for _, existing := range existingReceipts {
		if existing.TransactionID != receipt.TransactionID ||
			existing.PreIndexSHA256 != receipt.PreIndexSHA256 ||
			existing.PostIndexSHA256 != receipt.PostIndexSHA256 ||
			strings.Join(existing.Paths, "\x00") != strings.Join(receipt.Paths, "\x00") {
			continue
		}
		if matchingReceiptID != "" {
			return "", fmt.Errorf("entries_governance_receipt_duplicate_transition")
		}
		matchingReceiptID = existing.ReceiptID
	}
	if matchingReceiptID != "" {
		return matchingReceiptID, nil
	}
	receipt.ReceiptID = governanceReceiptID(receipt)
	data, err := marshalGovernanceReceipt(receipt)
	if err != nil {
		return "", err
	}
	path := governanceReceiptPath(root, receipt.ReceiptID)
	if existing, readErr := os.ReadFile(path); readErr == nil {
		existingReceipt, decodeErr := decodeGovernanceReceipt(existing)
		if decodeErr != nil || existingReceipt.ReceiptID != receipt.ReceiptID ||
			existingReceipt.TransactionID != receipt.TransactionID ||
			existingReceipt.PreIndexSHA256 != receipt.PreIndexSHA256 ||
			existingReceipt.PostIndexSHA256 != receipt.PostIndexSHA256 ||
			strings.Join(existingReceipt.Paths, "\x00") != strings.Join(receipt.Paths, "\x00") {
			return "", fmt.Errorf("entries_governance_receipt_conflict_or_corrupt")
		}
		return receipt.ReceiptID, nil
	} else if !os.IsNotExist(readErr) {
		return "", readErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := afs.AtomicWrite(path, data); err != nil {
		return "", err
	}
	return receipt.ReceiptID, nil
}

func loadAllEntriesGovernanceReceipts(root string) ([]entriesGovernanceReceipt, error) {
	entries, err := os.ReadDir(governanceReceiptDirectory(root))
	if os.IsNotExist(err) {
		return []entriesGovernanceReceipt{}, nil
	}
	if err != nil {
		return nil, err
	}
	receipts := make([]entriesGovernanceReceipt, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "entries-") ||
			!strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(governanceReceiptDirectory(root), entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		receipt, decodeErr := decodeGovernanceReceipt(data)
		if decodeErr != nil || entry.Name() != "entries-"+receipt.ReceiptID+".json" {
			if decodeErr == nil {
				decodeErr = fmt.Errorf("entries_governance_receipt_filename_mismatch")
			}
			return nil, decodeErr
		}
		receipts = append(receipts, *receipt)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ReceiptID < receipts[j].ReceiptID })
	return receipts, nil
}

func archivedAtomicBatchRecoveryPath(root, batchKey string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", "entries-"+batchKey+".json")
}

func loadEntriesRecoveryIncludingArchive(root, batchKey string) (*atomicBatchRecovery, error) {
	recovery, err := loadAtomicBatchRecovery(root, batchKey)
	if err == nil || !os.IsNotExist(err) {
		return recovery, err
	}
	data, readErr := os.ReadFile(archivedAtomicBatchRecoveryPath(root, batchKey))
	if readErr != nil {
		return nil, readErr
	}
	return decodeAtomicBatchRecovery(data, batchKey)
}

// ProveEntriesGovernanceSupersession requires one unambiguous completed receipt
// chain from the old postimage to the current index. Gaps, forks, corrupt
// receipts, and other pending transactions fail closed.
func ProveEntriesGovernanceSupersession(
	root string,
	indexPath string,
	items []AtomicUpdateItem,
	currentIndexSHA256 string,
) (*EntriesGovernanceProof, error) {
	normalized, err := normalizeAtomicRecoveryItems(items)
	if err != nil {
		return nil, err
	}
	batchKey := atomicBatchKey(normalized)
	recovery, err := loadEntriesRecoveryIncludingArchive(root, batchKey)
	if err != nil {
		return nil, fmt.Errorf("entries_original_recovery_receipt_unreadable: %w", err)
	}
	if currentIndexSHA256 == recovery.PostIndexSHA256 {
		return &EntriesGovernanceProof{
			TransactionID:      recovery.BatchKey,
			PreIndexSHA256:     recovery.PreIndexSHA256,
			PostIndexSHA256:    recovery.PostIndexSHA256,
			CurrentIndexSHA256: currentIndexSHA256,
			GovernanceReceipts: []string{},
		}, nil
	}
	if err := rejectOtherPendingGovernanceAssets(root, indexPath, batchKey); err != nil {
		return nil, err
	}
	receipts, err := loadAllEntriesGovernanceReceipts(root)
	if err != nil {
		return nil, fmt.Errorf("entries_later_governance_receipts_unreadable: %w", err)
	}
	current := recovery.PostIndexSHA256
	chain := []string{}
	seen := map[string]bool{}
	for current != currentIndexSHA256 {
		matches := []entriesGovernanceReceipt{}
		for _, receipt := range receipts {
			if receipt.PreIndexSHA256 == current {
				matches = append(matches, receipt)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf(
				"entries_governance_chain_gap_or_fork: sha=%s matches=%d",
				current,
				len(matches),
			)
		}
		receipt := matches[0]
		if seen[receipt.ReceiptID] {
			return nil, fmt.Errorf("entries_governance_chain_cycle")
		}
		seen[receipt.ReceiptID] = true
		chain = append(chain, receipt.ReceiptID)
		current = receipt.PostIndexSHA256
	}
	return &EntriesGovernanceProof{
		TransactionID:      recovery.BatchKey,
		PreIndexSHA256:     recovery.PreIndexSHA256,
		PostIndexSHA256:    recovery.PostIndexSHA256,
		CurrentIndexSHA256: currentIndexSHA256,
		GovernanceReceipts: chain,
	}, nil
}

func rejectOtherPendingGovernanceAssets(root, indexPath, allowedBatchKey string) error {
	directory := filepath.Join(root, ".aoci", "transactions")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		entries = nil
		err = nil
	}
	if err != nil {
		return err
	}
	allowed := "entries-" + allowedBatchKey + ".json"
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Name() == allowed {
			continue
		}
		return fmt.Errorf("other_pending_aoci_transaction: %s", entry.Name())
	}
	for _, suffix := range []string{".aoci-cas.intent", ".aoci-cas.swap"} {
		if _, err := os.Lstat(indexPath + suffix); err == nil {
			return fmt.Errorf("pending_atomicwrite_cas_asset: %s", suffix)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ArchiveEntriesAtomicRecovery preserves the exact original transaction bytes
// under history, then removes the active path. Identical archives are accepted
// idempotently; conflicting evidence is never overwritten.
func ArchiveEntriesAtomicRecovery(root string, items []AtomicUpdateItem) (string, string, error) {
	normalized, err := normalizeAtomicRecoveryItems(items)
	if err != nil {
		return "", "", err
	}
	batchKey := atomicBatchKey(normalized)
	return archiveAtomicBatchRecoveryByKey(root, batchKey)
}

func archiveAtomicBatchRecoveryByKey(root, batchKey string) (string, string, error) {
	activePath := atomicBatchRecoveryPath(root, batchKey)
	archivePath := archivedAtomicBatchRecoveryPath(root, batchKey)
	data, readErr := os.ReadFile(activePath)
	if os.IsNotExist(readErr) {
		data, readErr = os.ReadFile(archivePath)
		if readErr != nil {
			return "", "", readErr
		}
		if _, err := loadEntriesRecoveryIncludingArchive(root, batchKey); err != nil {
			return "", "", err
		}
		rel, err := filepath.Rel(root, archivePath)
		if err != nil {
			return "", "", err
		}
		return filepath.ToSlash(rel), governanceBytesSHA256(data), nil
	}
	if readErr != nil {
		return "", "", readErr
	}
	if _, err := loadAtomicBatchRecovery(root, batchKey); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return "", "", err
	}
	if existing, err := os.ReadFile(archivePath); err == nil {
		if !bytes.Equal(existing, data) {
			return "", "", fmt.Errorf("entries_recovery_archive_conflict")
		}
	} else if os.IsNotExist(err) {
		if err := afs.AtomicWrite(archivePath, data); err != nil {
			return "", "", err
		}
	} else {
		return "", "", err
	}
	if err := os.Remove(activePath); err != nil && !os.IsNotExist(err) {
		return "", "", err
	}
	rel, err := filepath.Rel(root, archivePath)
	if err != nil {
		return "", "", err
	}
	return filepath.ToSlash(rel), governanceBytesSHA256(data), nil
}
