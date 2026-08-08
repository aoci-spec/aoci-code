// Entries原子批次恢复收据 —— 绑定候选集合、Generation preimage与确定性postimage，
// 防止Host-Agent把“当前索引碰巧已含候选”误判为本run的中断恢复。
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

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type atomicBatchRecovery struct {
	Version          int                                  `json:"version"`
	BatchKey         string                               `json:"batch_key"`
	PreIndexSHA256   string                               `json:"pre_index_sha256"`
	PostIndexSHA256  string                               `json:"post_index_sha256"`
	BaselinePreState string                               `json:"baseline_pre_state,omitempty"`
	BaselinePreSHA   string                               `json:"baseline_pre_sha256,omitempty"`
	Assets           []atomicBatchRecoveryAsset           `json:"assets,omitempty"`
	Guards           []atomicBatchRecoveryGuard           `json:"guards,omitempty"`
	CodeBatchID      string                               `json:"code_batch_id,omitempty"`
	DatabaseBatchID  string                               `json:"database_batch_id,omitempty"`
	DatabaseBindings []atomicBatchRecoveryDatabaseBinding `json:"database_bindings,omitempty"`
}

type atomicBatchRecoveryAsset struct {
	VolumeID   string `json:"volume_id"`
	Path       string `json:"path"`
	PreSHA256  string `json:"pre_sha256"`
	PostSHA256 string `json:"post_sha256"`
	Preimage   []byte `json:"preimage,omitempty"`
}

type atomicBatchRecoveryGuard struct {
	VolumeID string `json:"volume_id"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
}

// atomicBatchRecoveryDatabaseBinding is the durable governance side effect
// needed to finish a Database cognition transaction after its Volume postimage
// is visible. It extends the existing Entries recovery receipt; it is not a
// second Apply or recovery system.
type atomicBatchRecoveryDatabaseBinding struct {
	ObjectRef           string `json:"object_ref"`
	SourceID            string `json:"source_id"`
	EvidenceVersion     string `json:"evidence_version"`
	TableEvidenceSHA256 string `json:"table_evidence_sha256"`
	EntrySHA256         string `json:"entry_sha256"`
}

var removeAtomicBatchRecoveryFile = os.Remove

func atomicBatchKey(items []normalizedAtomicItem) string {
	volumeItems := false
	for _, item := range items {
		if item.objectRef != "" || item.candidateID != "" || item.batchID != "" {
			volumeItems = true
			break
		}
	}
	if volumeItems {
		ordered := append([]normalizedAtomicItem{}, items...)
		sort.Slice(ordered, func(i, j int) bool {
			left := ordered[i].objectRef + "\x00" + ordered[i].rel
			right := ordered[j].objectRef + "\x00" + ordered[j].rel
			return left < right
		})
		hash := sha256.New()
		_, _ = hash.Write([]byte("cognition-volume-batch/v2\x00"))
		for _, item := range ordered {
			for _, value := range []string{item.objectRef, item.rel, item.newEntry, strings.ToLower(strings.TrimSpace(item.sourceSHA256)), item.candidateID, item.batchID} {
				_, _ = hash.Write([]byte(fmt.Sprintf("%d:", len(value))))
				_, _ = hash.Write([]byte(value))
			}
		}
		return hex.EncodeToString(hash.Sum(nil))
	}
	hash := sha256.New()
	for _, item := range items {
		_, _ = hash.Write([]byte(item.rel))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(item.newEntry))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strings.ToLower(strings.TrimSpace(item.sourceSHA256))))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func atomicBatchRecoveryPath(root, batchKey string) string {
	return filepath.Join(root, ".aoci", "transactions", "entries-"+batchKey+".json")
}

func saveAtomicBatchRecovery(root string, recovery atomicBatchRecovery) error {
	path := atomicBatchRecoveryPath(root, recovery.BatchKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recovery, "", "  ")
	if err != nil {
		return err
	}
	return afs.AtomicWrite(path, append(data, '\n'))
}

func loadAtomicBatchRecovery(root, batchKey string) (*atomicBatchRecovery, error) {
	data, err := os.ReadFile(atomicBatchRecoveryPath(root, batchKey))
	if err != nil {
		return nil, err
	}
	return decodeAtomicBatchRecovery(data, batchKey)
}

func decodeAtomicBatchRecovery(data []byte, batchKey string) (*atomicBatchRecovery, error) {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var recovery atomicBatchRecovery
	if err := decoder.Decode(&recovery); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("entries_recovery_receipt_trailing_json")
	}
	if recovery.BatchKey != batchKey || !validRecoverySHA256(recovery.PreIndexSHA256) || !validRecoverySHA256(recovery.PostIndexSHA256) {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	if recovery.Version == 1 {
		if len(recovery.Assets) != 0 || recovery.CodeBatchID != "" || recovery.DatabaseBatchID != "" || len(recovery.DatabaseBindings) != 0 {
			return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
		}
		return &recovery, nil
	}
	if (recovery.Version != 2 && recovery.Version != 3 && recovery.Version != 4) || len(recovery.Assets) == 0 || len(recovery.Assets) > 2 {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	if recovery.Version < 4 && (recovery.BaselinePreState != "" || recovery.BaselinePreSHA != "" || len(recovery.Guards) != 0) {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	if recovery.Version < 4 && recovery.CodeBatchID != "" {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	if recovery.Version == 4 && recovery.BaselinePreState != "present" && recovery.BaselinePreState != "absent" {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	if recovery.Version == 4 && ((recovery.BaselinePreState == "present" && !validRecoverySHA256(recovery.BaselinePreSHA)) ||
		(recovery.BaselinePreState == "absent" && recovery.BaselinePreSHA != "")) {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	seen := map[string]bool{}
	seenVolumes := map[string]bool{}
	for _, asset := range recovery.Assets {
		normalizedPath, pathErr := afs.NormalizeRelPath(asset.Path)
		if (asset.VolumeID != "code" && asset.VolumeID != "database") || pathErr != nil || normalizedPath != asset.Path ||
			seen[asset.Path] || seenVolumes[asset.VolumeID] ||
			!validRecoverySHA256(asset.PreSHA256) || !validRecoverySHA256(asset.PostSHA256) || asset.PreSHA256 == asset.PostSHA256 {
			return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
		}
		if (recovery.Version < 4 && len(asset.Preimage) != 0) ||
			(recovery.Version == 4 && (len(asset.Preimage) == 0 || governanceBytesSHA256(asset.Preimage) != asset.PreSHA256)) {
			return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
		}
		seen[asset.Path] = true
		seenVolumes[asset.VolumeID] = true
	}
	if recovery.Version == 4 {
		if len(recovery.Guards) < 2 || len(recovery.Guards) > 4 {
			return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
		}
		guardVolumes, guardPaths := map[string]bool{}, map[string]bool{}
		previousVolume := ""
		for _, guard := range recovery.Guards {
			normalizedPath, pathErr := afs.NormalizeRelPath(guard.Path)
			if pathErr != nil || normalizedPath != guard.Path || guard.VolumeID <= previousVolume ||
				(guard.VolumeID != "root" && guard.VolumeID != "meta" && guard.VolumeID != "code" && guard.VolumeID != "database") ||
				guardVolumes[guard.VolumeID] || guardPaths[guard.Path] || !validRecoverySHA256(guard.SHA256) {
				return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
			}
			previousVolume = guard.VolumeID
			guardVolumes[guard.VolumeID], guardPaths[guard.Path] = true, true
		}
		if !guardVolumes["root"] || !guardVolumes["meta"] {
			return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
		}
	}
	if recovery.CodeBatchID != "" && (!validRecoverySHA256(recovery.CodeBatchID) || !seenVolumes["code"]) {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	if recovery.Version == 2 || (recovery.Version == 4 && recovery.DatabaseBatchID == "") {
		if recovery.DatabaseBatchID != "" || len(recovery.DatabaseBindings) != 0 {
			return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
		}
		return &recovery, nil
	}
	if !validRecoverySHA256(recovery.DatabaseBatchID) || !seenVolumes["database"] ||
		len(recovery.DatabaseBindings) == 0 || len(recovery.DatabaseBindings) > machinecontract.EntriesBatchMaxItems {
		return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
	}
	previousRef := ""
	for _, binding := range recovery.DatabaseBindings {
		if binding.ObjectRef <= previousRef || !cognition.IsCanonicalDatabaseRef(binding.ObjectRef) ||
			sourceIDFromRecoveryObjectRef(binding.ObjectRef) != binding.SourceID || binding.EvidenceVersion != dbevidence.EvidenceVersion ||
			!validRecoverySHA256(binding.TableEvidenceSHA256) || !validRecoverySHA256(binding.EntrySHA256) {
			return nil, fmt.Errorf("entries_batch_recovery_receipt_invalid")
		}
		previousRef = binding.ObjectRef
	}
	return &recovery, nil
}

func sourceIDFromRecoveryObjectRef(ref string) string {
	parts := strings.Split(strings.TrimPrefix(ref, "database://"), "/")
	if len(parts) != 3 {
		return ""
	}
	return parts[0]
}

func validRecoverySHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func cleanupAtomicBatchRecovery(root, batchKey string) error {
	err := removeAtomicBatchRecoveryFile(atomicBatchRecoveryPath(root, batchKey))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func normalizeAtomicRecoveryItems(items []AtomicUpdateItem) ([]normalizedAtomicItem, error) {
	normalized := make([]normalizedAtomicItem, 0, len(items))
	for indexPosition, item := range items {
		rel := ""
		objectRef := strings.TrimSpace(item.ObjectRef)
		if (item.Path == "") == (objectRef == "") || objectRef != item.ObjectRef {
			return nil, fmt.Errorf("exactly one path or object_ref is required")
		}
		if item.Path != "" {
			var err error
			rel, err = afs.NormalizeRelPath(item.Path)
			if err != nil {
				return nil, err
			}
		}
		normalized = append(normalized, normalizedAtomicItem{
			rel: rel, objectRef: objectRef, newEntry: item.NewEntry, sourceSHA256: item.SourceSHA256,
			candidateID: item.CandidateID, batchID: item.BatchID, originalCandidateIndex: indexPosition + 1,
		})
	}
	return normalized, nil
}

// UpdateEntriesAtomicRecoveryPending核对当前批次是否仍有完整、可验证的恢复收据。
// 不存在表示正常完成态；存在但损坏必须显式报错，不能被零写重试吞掉。
func UpdateEntriesAtomicRecoveryPending(root string, items []AtomicUpdateItem) (bool, error) {
	normalized, err := normalizeAtomicRecoveryItems(items)
	if err != nil {
		return false, err
	}
	_, err = loadAtomicBatchRecovery(root, atomicBatchKey(normalized))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CompleteUpdateEntriesAtomicRecovery在Application审计成功后清理Host-Agent收据。
// 审计前中断必须保留收据，下一次同run才能证明no-op确属本批postimage。
func CompleteUpdateEntriesAtomicRecovery(root string, items []AtomicUpdateItem) error {
	normalized, err := normalizeAtomicRecoveryItems(items)
	if err != nil {
		return err
	}
	batchKey := atomicBatchKey(normalized)
	if recovery, err := loadAtomicBatchRecovery(root, batchKey); err == nil && (recovery.Version == 2 || recovery.Version == 3 || recovery.Version == 4) {
		_, _, archiveErr := archiveAtomicBatchRecoveryByKey(root, batchKey)
		return archiveErr
	}
	return cleanupAtomicBatchRecovery(root, batchKey)
}

// RollbackUpdateEntriesAtomicRecovery restores exact Volume preimages for one
// active v4 Entries transaction. It is intentionally an internal policy hook,
// not another MCP tool or transaction system. Rollback is allowed only while
// the existing Baseline remains at its exact preimage and every participant is
// a proven preimage or postimage; otherwise the caller must Resume or hard
// block according to the existing recovery policy.
func RollbackUpdateEntriesAtomicRecovery(root string, items []AtomicUpdateItem) error {
	plan, fail := planUpdateEntriesAtomic(root, items)
	if fail != nil {
		return fmt.Errorf("entries_rollback_plan_failed[%s]: %s", fail.Code, fail.Msg)
	}
	if plan == nil || plan.volumePlan == nil || plan.volumePlan.recovery == nil || plan.volumePlan.recovery.Version != 4 {
		return fmt.Errorf("entries_rollback_recovery_unavailable")
	}
	recovery := plan.volumePlan.recovery
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return err
	}
	defer lock.Release()
	if guardID := recoveryGuardMismatch(root, recovery); guardID != "" {
		return fmt.Errorf("entries_rollback_guard_changed: %s", guardID)
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return fmt.Errorf("entries_rollback_guard_changed: %s", guardID)
	}
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	if recovery.BaselinePreState == "present" {
		info, statErr := os.Lstat(baselinePath)
		if statErr != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("entries_rollback_baseline_changed")
		}
		data, readErr := os.ReadFile(baselinePath)
		if readErr != nil || governanceBytesSHA256(data) != recovery.BaselinePreSHA {
			return fmt.Errorf("entries_rollback_baseline_changed")
		}
	} else if _, statErr := os.Lstat(baselinePath); !os.IsNotExist(statErr) {
		return fmt.Errorf("entries_rollback_baseline_changed")
	}
	states := make([]string, len(recovery.Assets))
	for index, asset := range recovery.Assets {
		path := filepath.Join(root, filepath.FromSlash(asset.Path))
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("entries_rollback_participant_changed: %s", asset.VolumeID)
		}
		current, hashErr := baseline.HashFile(path)
		if hashErr != nil {
			return hashErr
		}
		switch current.SHA256 {
		case asset.PreSHA256:
			states[index] = "preimage"
		case asset.PostSHA256:
			states[index] = "postimage"
		default:
			return fmt.Errorf("entries_rollback_participant_changed: %s", asset.VolumeID)
		}
	}
	for index := len(recovery.Assets) - 1; index >= 0; index-- {
		if states[index] == "preimage" {
			continue
		}
		asset := recovery.Assets[index]
		if err := afs.AtomicWriteCAS(filepath.Join(root, filepath.FromSlash(asset.Path)), asset.Preimage, asset.PostSHA256); err != nil {
			return fmt.Errorf("entries_rollback_write_failed: %s: %w", asset.VolumeID, err)
		}
	}
	for _, asset := range recovery.Assets {
		current, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(asset.Path)))
		if hashErr != nil || current.SHA256 != asset.PreSHA256 {
			return fmt.Errorf("entries_rollback_postcondition_failed: %s", asset.VolumeID)
		}
	}
	if _, _, err := archiveAtomicBatchRecoveryByKey(root, recovery.BatchKey); err != nil {
		return fmt.Errorf("entries_rollback_archive_failed: %w", err)
	}
	return nil
}
