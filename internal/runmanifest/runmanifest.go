// Package runmanifest owns deterministic decoding and terminal-state proof for
// draft run Manifests. It deliberately has no dependency on the model-facing
// draft orchestration package so CLI and MCP consumers share one machine truth.
package runmanifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

const (
	KindHeader   = "header"
	KindEntries  = "entries"
	KindCuration = "curation"
)

const (
	ResolutionRecovered  = "recovered"
	ResolutionSuperseded = "superseded_recovered"
	ResolutionPending    = "pending"
	ResolutionZeroWrite  = "zero_write_closed"

	ZeroWriteStepGenerationPlan = "generation_plan"
	ZeroWriteReasonPlanGuard    = "generation_plan_guard_failed"
	ZeroWriteReasonRecovery     = "pre_apply_recovery_requested"
)

const manifestFileName = "manifest.json"

var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z(-[0-9]+)?$`)

// EntryStatus captures the immutable generation-time state of one path.
type EntryStatus struct {
	Path         string `json:"path"`
	Status       string `json:"status"`
	Note         string `json:"note,omitempty"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
}

// ReviewRecord records one machine check or content review.
type ReviewRecord struct {
	At         string `json:"at"`
	Action     string `json:"action"`
	DraftHash  string `json:"draft_hash,omitempty"`
	PathsCount int    `json:"paths_count,omitempty"`
	Passed     int    `json:"passed,omitempty"`
	Warned     int    `json:"warned,omitempty"`
	Rejected   int    `json:"rejected,omitempty"`
	Skipped    int    `json:"skipped,omitempty"`
}

// ApplicationRecord records one Apply attempt and its bound draft facts.
type ApplicationRecord struct {
	At          string `json:"at"`
	DraftHash   string `json:"draft_hash,omitempty"`
	PathsCount  int    `json:"paths_count,omitempty"`
	Applied     int    `json:"applied,omitempty"`
	Recovered   int    `json:"recovered,omitempty"`
	Rejected    int    `json:"rejected,omitempty"`
	RejectKinds string `json:"reject_kinds,omitempty"`
}

// ZeroWriteClosure records a machine-proven pre-Apply terminal state. It
// preserves the draft while allowing a fresh Plan without pretending that a
// post-write recovery or supersession occurred.
type ZeroWriteClosure struct {
	Version               int      `json:"version"`
	At                    string   `json:"at"`
	Step                  string   `json:"step"`
	Reason                string   `json:"reason"`
	DraftHash             string   `json:"draft_hash"`
	PreIndexSHA256        string   `json:"pre_index_sha256"`
	CurrentIndexSHA256    string   `json:"current_index_sha256,omitempty"`
	CurrentBaselineSHA256 string   `json:"current_baseline_sha256,omitempty"`
	RepositorySHA256      string   `json:"repository_sha256,omitempty"`
	StagedTransactionID   string   `json:"staged_transaction_id,omitempty"`
	GovernanceReceipts    []string `json:"governance_receipts,omitempty"`
	FormalAssetWrites     int      `json:"formal_asset_writes"`
}

// ResolutionRecord is the append-only terminal proof for a post-write failure.
type ResolutionRecord struct {
	At                     string   `json:"at"`
	Status                 string   `json:"status"`
	FailureKinds           string   `json:"failure_kinds"`
	TransactionID          string   `json:"transaction_id"`
	PreIndexSHA256         string   `json:"pre_index_sha256"`
	PostIndexSHA256        string   `json:"post_index_sha256"`
	CurrentIndexSHA256     string   `json:"current_index_sha256"`
	CurrentBaselineSHA256  string   `json:"current_baseline_sha256"`
	RepositorySHA256       string   `json:"repository_sha256"`
	GovernanceReceipts     []string `json:"governance_receipts,omitempty"`
	ArchivedRecoveryAsset  string   `json:"archived_recovery_asset"`
	ArchivedRecoverySHA256 string   `json:"archived_recovery_sha256"`
}

// Manifest is the complete persisted audit envelope for one draft run.
type Manifest struct {
	RunID     string `json:"run_id"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`

	AppliedAt string `json:"applied_at,omitempty"`

	GenerationSource string `json:"generation_source,omitempty"`
	AgentName        string `json:"agent_name,omitempty"`
	PlanID           string `json:"plan_id,omitempty"`
	// HeaderIntent decodes transitional Feature manifests only. New Header
	// runs bind intent in a hashed draft file and never serialize this field.
	HeaderIntent   string `json:"header_intent,omitempty"`
	IndexSHA256    string `json:"index_sha256,omitempty"`
	HeaderSHA256   string `json:"header_sha256,omitempty"`
	CurationSHA256 string `json:"curation_sha256,omitempty"`
	GenerationHash string `json:"generation_hash,omitempty"`

	Model        string   `json:"model,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	EndpointHash string   `json:"endpoint_hash,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	PromptHash   string   `json:"prompt_hash,omitempty"`

	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	TokenSource  string `json:"token_source,omitempty"`

	Entries  []EntryStatus `json:"entries,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
	Files    []string      `json:"files,omitempty"`

	Reviews           []ReviewRecord      `json:"reviews,omitempty"`
	Applications      []ApplicationRecord `json:"applications,omitempty"`
	Resolutions       []ResolutionRecord  `json:"resolutions,omitempty"`
	ZeroWriteClosures []ZeroWriteClosure  `json:"zero_write_closures,omitempty"`
}

func manifestPath(root, runID string) (string, error) {
	if !runIDPattern.MatchString(runID) {
		return "", fmt.Errorf("run_id shape invalid: %q", runID)
	}
	return filepath.Join(root, ".aoci", "drafts", runID, manifestFileName), nil
}

// Load reads and strictly validates one run Manifest.
func Load(root, runID string) (*Manifest, error) {
	path, err := manifestPath(root, runID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 manifest 失败(run %s): %w", runID, err)
	}
	return Decode(data, runID)
}

// Decode validates JSON shape and identity while retaining legacy optionality.
func Decode(data []byte, expectedRunID string) (*Manifest, error) {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return nil, fmt.Errorf("manifest 损坏(run %s): %w", expectedRunID, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("manifest 损坏(run %s): JSON解析失败: %w", expectedRunID, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("manifest 损坏(run %s): JSON只能包含一个顶层对象", expectedRunID)
		}
		return nil, fmt.Errorf("manifest 损坏(run %s): JSON存在尾随内容: %w", expectedRunID, err)
	}
	if manifest.RunID == "" {
		return nil, fmt.Errorf("manifest 损坏(run %s): manifest.run_id为空", expectedRunID)
	}
	if !runIDPattern.MatchString(manifest.RunID) {
		return nil, fmt.Errorf("manifest 损坏(run %s): manifest.run_id形态非法: %q", expectedRunID, manifest.RunID)
	}
	if manifest.RunID != expectedRunID {
		return nil, fmt.Errorf(
			"manifest 损坏(run %s): manifest.run_id=%q与目录run_id不一致",
			expectedRunID, manifest.RunID,
		)
	}
	if manifest.Kind != KindHeader && manifest.Kind != KindEntries && manifest.Kind != KindCuration {
		return nil, fmt.Errorf("manifest 损坏(run %s): manifest.kind非法: %q", expectedRunID, manifest.Kind)
	}
	if manifest.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339, manifest.CreatedAt); err != nil {
			return nil, fmt.Errorf("manifest 损坏(run %s): created_at不是RFC3339: %w", expectedRunID, err)
		}
	}
	if manifest.AppliedAt != "" {
		if _, err := time.Parse(time.RFC3339, manifest.AppliedAt); err != nil {
			return nil, fmt.Errorf("manifest 损坏(run %s): applied_at不是RFC3339: %w", expectedRunID, err)
		}
	}
	for position, record := range manifest.Resolutions {
		if err := ValidateResolution(record); err != nil {
			return nil, fmt.Errorf(
				"manifest corrupt(run %s): resolutions[%d] invalid: %w",
				expectedRunID, position, err,
			)
		}
	}
	for position, record := range manifest.ZeroWriteClosures {
		if err := ValidateZeroWriteClosure(record); err != nil {
			return nil, fmt.Errorf(
				"manifest corrupt(run %s): zero_write_closures[%d] invalid: %w",
				expectedRunID, position, err,
			)
		}
	}
	return &manifest, nil
}

// ValidateZeroWriteClosure validates the self-contained proof fields. The
// surrounding Manifest supplies the generation bindings checked by
// ZeroWriteClosed.
func ValidateZeroWriteClosure(record ZeroWriteClosure) error {
	if (record.Version != 1 && record.Version != 2) ||
		record.Step != ZeroWriteStepGenerationPlan ||
		(record.Reason != ZeroWriteReasonPlanGuard && record.Reason != ZeroWriteReasonRecovery) ||
		record.FormalAssetWrites != 0 ||
		!validSHA256(record.DraftHash) || !validSHA256(record.PreIndexSHA256) {
		return fmt.Errorf("entries_zero_write_closure_invalid")
	}
	if record.Version == 1 && (record.CurrentIndexSHA256 != "" ||
		record.CurrentBaselineSHA256 != "" || record.RepositorySHA256 != "" ||
		record.StagedTransactionID != "" || len(record.GovernanceReceipts) != 0) {
		return fmt.Errorf("entries_zero_write_closure_invalid")
	}
	if record.Version == 2 && (record.Reason != ZeroWriteReasonRecovery ||
		!validSHA256(record.CurrentIndexSHA256) ||
		record.CurrentIndexSHA256 == record.PreIndexSHA256 ||
		!validSHA256(record.CurrentBaselineSHA256) ||
		!validSHA256(record.RepositorySHA256) ||
		!validSHA256(record.StagedTransactionID) || len(record.GovernanceReceipts) == 0) {
		return fmt.Errorf("entries_zero_write_closure_invalid")
	}
	for _, receiptID := range record.GovernanceReceipts {
		if !validSHA256(receiptID) {
			return fmt.Errorf("entries_zero_write_closure_invalid")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, record.At); err != nil {
		return fmt.Errorf("entries_zero_write_closure_timestamp_invalid: %w", err)
	}
	return nil
}

// ZeroWriteClosed accepts exactly one bound pre-Apply closure. Any Review,
// Application, Apply timestamp, or post-write resolution makes this proof
// inapplicable and keeps pending-run arbitration fail closed.
func ZeroWriteClosed(manifest *Manifest) (ZeroWriteClosure, bool) {
	if manifest == nil || manifest.Kind != KindEntries || manifest.AppliedAt != "" ||
		len(manifest.Reviews) != 0 || len(manifest.Applications) != 0 ||
		len(manifest.Resolutions) != 0 || len(manifest.ZeroWriteClosures) != 1 {
		return ZeroWriteClosure{}, false
	}
	record := manifest.ZeroWriteClosures[0]
	if ValidateZeroWriteClosure(record) != nil ||
		record.DraftHash != manifest.GenerationHash ||
		record.PreIndexSHA256 != manifest.IndexSHA256 {
		return ZeroWriteClosure{}, false
	}
	return record, true
}

// StoredZeroWriteClosed verifies the completed governance chain carried by a
// v2 recovery closure. Legacy v1 closures remain self-contained.
func StoredZeroWriteClosed(root string, manifest *Manifest) (ZeroWriteClosure, bool) {
	record, ok := ZeroWriteClosed(manifest)
	if !ok || record.Version == 1 {
		return record, ok
	}
	createdAt, err := time.Parse(time.RFC3339, manifest.CreatedAt)
	if err != nil {
		return ZeroWriteClosure{}, false
	}
	closedAt, err := time.Parse(time.RFC3339Nano, record.At)
	if err != nil {
		return ZeroWriteClosure{}, false
	}
	current := record.PreIndexSHA256
	previousAt := createdAt
	seen := map[string]bool{}
	for position, receiptID := range record.GovernanceReceipts {
		if seen[receiptID] {
			return ZeroWriteClosure{}, false
		}
		seen[receiptID] = true
		receipt, completedAt, err := loadStoredGovernanceReceipt(root, receiptID)
		if err != nil || receipt.PreIndexSHA256 != current ||
			(position == 0 && receipt.TransactionID == record.StagedTransactionID) ||
			!completedAt.After(previousAt) || completedAt.After(closedAt) {
			return ZeroWriteClosure{}, false
		}
		current = receipt.PostIndexSHA256
		previousAt = completedAt
	}
	if current != record.CurrentIndexSHA256 {
		return ZeroWriteClosure{}, false
	}
	return record, true
}

// ValidateResolution validates the self-contained terminal record fields.
func ValidateResolution(record ResolutionRecord) error {
	if record.Status != ResolutionRecovered && record.Status != ResolutionSuperseded {
		return fmt.Errorf("entries_run_resolution_status_invalid: %q", record.Status)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.At); err != nil {
		return fmt.Errorf("entries_run_resolution_timestamp_invalid: %w", err)
	}
	if strings.TrimSpace(record.FailureKinds) == "" ||
		strings.TrimSpace(record.TransactionID) == "" ||
		!validSHA256(record.PreIndexSHA256) || !validSHA256(record.PostIndexSHA256) ||
		!validSHA256(record.CurrentIndexSHA256) || !validSHA256(record.CurrentBaselineSHA256) ||
		!validSHA256(record.RepositorySHA256) ||
		strings.TrimSpace(record.ArchivedRecoveryAsset) == "" ||
		!validSHA256(record.ArchivedRecoverySHA256) {
		return fmt.Errorf("entries_run_resolution_proof_incomplete")
	}
	if record.Status == ResolutionRecovered &&
		(record.CurrentIndexSHA256 != record.PostIndexSHA256 || len(record.GovernanceReceipts) != 0) {
		return fmt.Errorf("entries_recovered_current_index_mismatch")
	}
	if record.Status == ResolutionSuperseded &&
		(record.CurrentIndexSHA256 == record.PostIndexSHA256 || len(record.GovernanceReceipts) == 0) {
		return fmt.Errorf("entries_superseded_governance_proof_missing")
	}
	return nil
}

// TerminalResolution accepts exactly one structurally valid terminal claim.
func TerminalResolution(manifest *Manifest) (ResolutionRecord, bool) {
	if manifest == nil || len(manifest.Resolutions) != 1 {
		return ResolutionRecord{}, false
	}
	record := manifest.Resolutions[0]
	if ValidateResolution(record) != nil {
		return ResolutionRecord{}, false
	}
	return record, true
}

// StoredTerminalResolution verifies archived transaction bytes and the entire
// completed governance receipt chain, not merely the Manifest claim.
func StoredTerminalResolution(root string, manifest *Manifest) (ResolutionRecord, bool) {
	record, ok := TerminalResolution(manifest)
	if !ok {
		return ResolutionRecord{}, false
	}
	expectedArchive := filepath.ToSlash(filepath.Join(
		".aoci", "transactions", "history", "entries-"+record.TransactionID+".json",
	))
	if record.ArchivedRecoveryAsset != expectedArchive {
		return ResolutionRecord{}, false
	}
	archiveData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(expectedArchive)))
	if err != nil {
		return ResolutionRecord{}, false
	}
	archiveDigest := sha256.Sum256(archiveData)
	if hex.EncodeToString(archiveDigest[:]) != record.ArchivedRecoverySHA256 {
		return ResolutionRecord{}, false
	}
	var recovery struct {
		Version         int    `json:"version"`
		BatchKey        string `json:"batch_key"`
		PreIndexSHA256  string `json:"pre_index_sha256"`
		PostIndexSHA256 string `json:"post_index_sha256"`
	}
	if decodeStrictEvidence(archiveData, &recovery) != nil || recovery.Version != 1 ||
		recovery.BatchKey != record.TransactionID ||
		recovery.PreIndexSHA256 != record.PreIndexSHA256 ||
		recovery.PostIndexSHA256 != record.PostIndexSHA256 ||
		recovery.PreIndexSHA256 == recovery.PostIndexSHA256 {
		return ResolutionRecord{}, false
	}
	current := record.PostIndexSHA256
	for _, receiptID := range record.GovernanceReceipts {
		receipt, _, err := loadStoredGovernanceReceipt(root, receiptID)
		if err != nil || receipt.PreIndexSHA256 != current {
			return ResolutionRecord{}, false
		}
		current = receipt.PostIndexSHA256
	}
	if current != record.CurrentIndexSHA256 {
		return ResolutionRecord{}, false
	}
	return record, true
}

type storedGovernanceReceipt struct {
	Version         int      `json:"version"`
	ReceiptID       string   `json:"receipt_id"`
	Kind            string   `json:"kind"`
	TransactionID   string   `json:"transaction_id"`
	PreIndexSHA256  string   `json:"pre_index_sha256"`
	PostIndexSHA256 string   `json:"post_index_sha256"`
	Paths           []string `json:"paths"`
	CompletedAt     string   `json:"completed_at"`
}

func loadStoredGovernanceReceipt(
	root,
	receiptID string,
) (*storedGovernanceReceipt, time.Time, error) {
	if !validSHA256(receiptID) {
		return nil, time.Time{}, fmt.Errorf("entries_governance_receipt_id_invalid")
	}
	data, err := os.ReadFile(filepath.Join(root, ".aoci", "governance", "entries-"+receiptID+".json"))
	if err != nil {
		return nil, time.Time{}, err
	}
	var receipt storedGovernanceReceipt
	if decodeStrictEvidence(data, &receipt) != nil || receipt.Version != 1 ||
		receipt.Kind != "entries" || receipt.ReceiptID != receiptID ||
		!validSHA256(receipt.TransactionID) || len(receipt.Paths) == 0 ||
		!validSHA256(receipt.PreIndexSHA256) || !validSHA256(receipt.PostIndexSHA256) ||
		receipt.PreIndexSHA256 == receipt.PostIndexSHA256 ||
		receipt.ReceiptID != governanceReceiptID(
			receipt.Version, receipt.Kind, receipt.TransactionID,
			receipt.PreIndexSHA256, receipt.PostIndexSHA256,
			receipt.Paths, receipt.CompletedAt,
		) {
		return nil, time.Time{}, fmt.Errorf("entries_governance_receipt_invalid")
	}
	completedAt, err := time.Parse(time.RFC3339Nano, receipt.CompletedAt)
	if err != nil {
		return nil, time.Time{}, err
	}
	seenPaths := map[string]bool{}
	for _, rel := range receipt.Paths {
		normalized, err := afs.NormalizeRelPath(rel)
		if err != nil || normalized != rel || seenPaths[rel] {
			return nil, time.Time{}, fmt.Errorf("entries_governance_receipt_path_invalid")
		}
		seenPaths[rel] = true
	}
	return &receipt, completedAt, nil
}

func decodeStrictEvidence(data []byte, target any) error {
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
		return fmt.Errorf("terminal proof contains trailing JSON")
	}
	return nil
}

func governanceReceiptID(
	version int,
	kind, transactionID, preIndexSHA256, postIndexSHA256 string,
	paths []string,
	completedAt string,
) string {
	payload, _ := json.Marshal(struct {
		Version         int      `json:"version"`
		Kind            string   `json:"kind"`
		TransactionID   string   `json:"transaction_id"`
		PreIndexSHA256  string   `json:"pre_index_sha256"`
		PostIndexSHA256 string   `json:"post_index_sha256"`
		Paths           []string `json:"paths"`
		CompletedAt     string   `json:"completed_at"`
	}{
		Version: version, Kind: kind, TransactionID: transactionID,
		PreIndexSHA256: preIndexSHA256, PostIndexSHA256: postIndexSHA256,
		Paths: append([]string{}, paths...), CompletedAt: completedAt,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func listRunIDs(root string) ([]string, error) {
	directory := filepath.Join(root, ".aoci", "drafts")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	runIDs := []string{}
	for _, entry := range entries {
		if entry.IsDir() && runIDPattern.MatchString(entry.Name()) {
			runIDs = append(runIDs, entry.Name())
		}
	}
	sort.Strings(runIDs)
	return runIDs, nil
}

// LatestPending returns the newest unresolved run of the requested kind.
func LatestPending(root, kind string) (string, error) {
	runIDs, err := listRunIDs(root)
	if err != nil {
		return "", err
	}
	for position := len(runIDs) - 1; position >= 0; position-- {
		manifest, err := Load(root, runIDs[position])
		if err != nil {
			return runIDs[position], err
		}
		if manifest.Kind != kind {
			continue
		}
		if len(manifest.Resolutions) > 0 {
			if _, terminal := StoredTerminalResolution(root, manifest); terminal {
				continue
			}
			return runIDs[position], nil
		}
		if safeZeroWriteRejection(root, manifest) {
			continue
		}
		if manifest.AppliedAt != "" {
			continue
		}
		return runIDs[position], nil
	}
	return "", nil
}

// safeZeroWriteRejection recognizes repairable runs that reached a persisted
// machine rejection before any formal asset write. Such a run is terminal for
// pending-run arbitration: a restarted host must be able to obtain a fresh
// Guide and submit a corrected full batch instead of being directed to the
// post-write recovery command, which deliberately accepts only proven writes.
//
// A Check rejection is safe only when it is the newest Review and no Apply was
// attempted. An Apply rejection is safe only when every Application proves a
// whole-batch zero-write rejection and none carries a post-write failure kind.
func safeZeroWriteRejection(root string, manifest *Manifest) bool {
	if manifest == nil || manifest.Kind != KindEntries || manifest.AppliedAt != "" ||
		len(manifest.Resolutions) != 0 {
		return false
	}
	if _, closed := StoredZeroWriteClosed(root, manifest); closed {
		return true
	}
	if len(manifest.ZeroWriteClosures) != 0 {
		return false
	}
	if len(manifest.Applications) == 0 {
		if len(manifest.Reviews) == 0 {
			return false
		}
		latest := manifest.Reviews[len(manifest.Reviews)-1]
		return latest.Action == "check" && latest.DraftHash != "" && latest.PathsCount > 0 &&
			(latest.Rejected > 0 || latest.Skipped > 0)
	}
	for _, application := range manifest.Applications {
		if application.DraftHash == "" || application.PathsCount <= 0 ||
			application.Applied != 0 || application.Recovered != 0 ||
			application.Rejected != application.PathsCount || application.Rejected <= 0 ||
			strings.Contains(application.RejectKinds, "baseline_incomplete") ||
			strings.Contains(application.RejectKinds, "application_audit") {
			return false
		}
	}
	return true
}
