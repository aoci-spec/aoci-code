package dbcognition

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func BuildPlan(root string, assessment Assessment, set *cognition.Set, objectLimit, evidenceByteLimit int) (Plan, error) {
	if assessment.DatabaseVolumeState != cognition.AssetPresent || assessment.DatabaseVolumePath == "" || assessment.DatabaseVolumeSHA256 == "" {
		return Plan{}, errors.New(machinecontract.DatabaseCognitionVolumeAbsent)
	}
	if objectLimit < 1 || evidenceByteLimit < 1 {
		return Plan{}, fmt.Errorf("database_cognition_batch_limit_invalid")
	}
	actionable := make([]Item, 0)
	for _, item := range assessment.Items {
		switch item.State {
		case machinecontract.DatabaseCognitionMissing, machinecontract.DatabaseCognitionStale, machinecontract.DatabaseCognitionUnbaselined:
			actionable = append(actionable, item)
		}
	}
	sort.Slice(actionable, func(i, j int) bool { return actionable[i].ObjectRef < actionable[j].ObjectRef })
	selected := make([]Candidate, 0, objectLimit)
	totalBytes := 0
	for _, item := range actionable {
		if len(selected) >= objectLimit || item.record == nil {
			break
		}
		table, err := dbevidence.LoadTableEvidence(root, *item.record)
		if err != nil {
			return Plan{}, fmt.Errorf("database_evidence_invalid: %s", item.ObjectRef)
		}
		canonical, err := dbevidence.CanonicalJSON(table)
		if err != nil {
			return Plan{}, err
		}
		if len(selected) > 0 && totalBytes+len(canonical) > evidenceByteLimit {
			break
		}
		codeRefs := relatedCodeRefs(set, item.ObjectRef)
		var existing *string
		if item.CurrentEntry != "" {
			value := item.CurrentEntry
			existing = &value
		}
		bundle := dbevidence.BuildEvidenceBundle(table, item.TableEvidenceSHA256, nil, codeRefs, existing)
		selected = append(selected, Candidate{
			ReceiptTarget:         ReceiptTarget{ObjectRef: item.ObjectRef, SourceID: item.SourceID, CognitionState: item.State, EvidenceVersion: item.EvidenceVersion, TableEvidenceSHA256: item.TableEvidenceSHA256, EvidenceRef: item.EvidenceRef},
			ExistingDatabaseEntry: item.CurrentEntry, EvidenceBytes: len(canonical), EvidenceBundle: bundle,
		})
		totalBytes += len(canonical)
	}
	if len(selected) == 0 {
		return Plan{Version: machinecontract.DatabaseCognitionCandidateVersion, DatabaseVolumePath: assessment.DatabaseVolumePath, DatabaseVolumeSHA256: assessment.DatabaseVolumeSHA256, Candidates: []Candidate{}, NextAction: assessment.NextAction}, nil
	}
	targets := make([]ReceiptTarget, len(selected))
	for index := range selected {
		targets[index] = selected[index].ReceiptTarget
	}
	batchID := receiptHash("database-cognition-batch/v1", assessment.DatabaseVolumePath, assessment.DatabaseVolumeSHA256, encodeTargets(targets, false))
	for index := range targets {
		targets[index].CandidateID = receiptHash("database-cognition-object/v1", batchID, targets[index].ObjectRef, targets[index].SourceID, targets[index].CognitionState, targets[index].EvidenceVersion, targets[index].TableEvidenceSHA256, targets[index].EvidenceRef)
		selected[index].CandidateID = targets[index].CandidateID
	}
	receipt := Receipt{Version: machinecontract.DatabaseCognitionCandidateVersion, BatchID: batchID, DatabaseVolumePath: assessment.DatabaseVolumePath, DatabaseVolumeSHA256: assessment.DatabaseVolumeSHA256, Targets: targets}
	if err := SaveReceipt(root, receipt); err != nil {
		return Plan{}, err
	}
	return Plan{Version: receipt.Version, BatchID: batchID, DatabaseVolumePath: receipt.DatabaseVolumePath, DatabaseVolumeSHA256: receipt.DatabaseVolumeSHA256, TargetCount: len(selected), Remaining: len(actionable) - len(selected), EvidenceBytes: totalBytes, Candidates: selected, NextAction: machinecontract.DatabaseCognitionActionAuthorCompleteBatch}, nil
}

func ValidateSubmission(root string, sources []dbevidence.SourceConfig, batchID string, submissions []Submission) (Receipt, error) {
	receipt, err := LoadReceipt(root, batchID)
	if err != nil {
		return Receipt{}, err
	}
	if len(submissions) != len(receipt.Targets) {
		return Receipt{}, fmt.Errorf("database_candidate_batch_incomplete")
	}
	configured := map[string]dbevidence.SourceConfig{}
	for _, source := range sources {
		configured[source.SourceID] = source
	}
	submitted := map[string]string{}
	for _, item := range submissions {
		if item.ObjectRef == "" || item.CandidateID == "" || submitted[item.ObjectRef] != "" {
			return Receipt{}, fmt.Errorf("database_candidate_duplicate_or_invalid")
		}
		submitted[item.ObjectRef] = item.CandidateID
	}
	for _, target := range receipt.Targets {
		if submitted[target.ObjectRef] != target.CandidateID {
			return Receipt{}, fmt.Errorf("database_candidate_batch_mismatch")
		}
		source, ok := configured[target.SourceID]
		if !ok || !source.Enabled {
			return Receipt{}, fmt.Errorf("database_candidate_source_unavailable")
		}
		manifest, snapshot, exists, loadErr := dbevidence.LoadSnapshot(root, target.SourceID)
		if loadErr != nil || !exists {
			return Receipt{}, fmt.Errorf("database_candidate_evidence_invalid_or_unavailable")
		}
		if !dbevidence.SourceConfigMatchesManifest(source, manifest) {
			return Receipt{}, fmt.Errorf("database_candidate_source_selection_changed")
		}
		matched := false
		for _, record := range snapshot.Tables {
			if record.ObjectRef != target.ObjectRef {
				continue
			}
			if snapshot.EvidenceVersion != target.EvidenceVersion || record.TableEvidenceSHA256 != target.TableEvidenceSHA256 || record.EvidenceRef != target.EvidenceRef {
				return Receipt{}, fmt.Errorf("database_candidate_evidence_stale")
			}
			if _, err := dbevidence.LoadTableEvidence(root, record); err != nil {
				return Receipt{}, fmt.Errorf("database_candidate_evidence_invalid")
			}
			matched = true
			break
		}
		if !matched {
			return Receipt{}, fmt.Errorf("database_candidate_object_missing")
		}
	}
	return receipt, nil
}

func SaveReceipt(root string, receipt Receipt) error {
	if err := validateReceipt(receipt); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	path := receiptPath(root, receipt.BatchID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("database_candidate_receipt_directory_unavailable")
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if bytes.Equal(existing, append(data, '\n')) {
			return nil
		}
		return fmt.Errorf("database_candidate_receipt_conflict")
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("database_candidate_receipt_unreadable")
	}
	if err := afs.AtomicWrite(path, append(data, '\n')); err != nil {
		return fmt.Errorf("database_candidate_receipt_write_failed")
	}
	return nil
}

func LoadReceipt(root, batchID string) (Receipt, error) {
	if !validSHA256(batchID) {
		return Receipt{}, fmt.Errorf("database_candidate_batch_id_invalid")
	}
	data, err := os.ReadFile(receiptPath(root, batchID))
	if err != nil {
		if os.IsNotExist(err) {
			return Receipt{}, fmt.Errorf("database_candidate_receipt_missing")
		}
		return Receipt{}, fmt.Errorf("database_candidate_receipt_unreadable")
	}
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return Receipt{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var receipt Receipt
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Receipt{}, fmt.Errorf("database_candidate_receipt_trailing_json")
	}
	if err := validateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func validateReceipt(receipt Receipt) error {
	normalizedPath, pathErr := afs.NormalizeRelPath(receipt.DatabaseVolumePath)
	if receipt.Version != machinecontract.DatabaseCognitionCandidateVersion || !validSHA256(receipt.BatchID) ||
		pathErr != nil || normalizedPath != receipt.DatabaseVolumePath || !validSHA256(receipt.DatabaseVolumeSHA256) || len(receipt.Targets) == 0 {
		return fmt.Errorf("database_candidate_receipt_invalid")
	}
	wantBatch := receiptHash("database-cognition-batch/v1", receipt.DatabaseVolumePath, receipt.DatabaseVolumeSHA256, encodeTargets(receipt.Targets, false))
	if receipt.BatchID != wantBatch {
		return fmt.Errorf("database_candidate_batch_id_mismatch")
	}
	previous := ""
	for _, target := range receipt.Targets {
		actionable := target.CognitionState == machinecontract.DatabaseCognitionMissing ||
			target.CognitionState == machinecontract.DatabaseCognitionStale ||
			target.CognitionState == machinecontract.DatabaseCognitionUnbaselined
		if target.ObjectRef <= previous || !actionable || !cognition.IsCanonicalDatabaseRef(target.ObjectRef) || sourceIDFromObjectRef(target.ObjectRef) != target.SourceID || !validSHA256(target.TableEvidenceSHA256) || target.EvidenceVersion != dbevidence.EvidenceVersion || target.EvidenceRef == "" {
			return fmt.Errorf("database_candidate_target_invalid")
		}
		wantCandidate := receiptHash("database-cognition-object/v1", receipt.BatchID, target.ObjectRef, target.SourceID, target.CognitionState, target.EvidenceVersion, target.TableEvidenceSHA256, target.EvidenceRef)
		if target.CandidateID != wantCandidate {
			return fmt.Errorf("database_candidate_id_mismatch")
		}
		previous = target.ObjectRef
	}
	return nil
}

func relatedCodeRefs(set *cognition.Set, objectRef string) []string {
	if set == nil || set.Volumes["code"] == nil {
		return []string{}
	}
	refs := []string{}
	seen := map[string]bool{}
	knownCode := map[string]bool{}
	for _, object := range set.Volumes["code"].Objects {
		knownCode[object.CanonicalRef] = true
		if object.Entry == nil {
			continue
		}
		for _, relation := range strings.Split(object.Entry.R, ",") {
			if strings.TrimSpace(relation) == objectRef {
				if !seen[object.CanonicalRef] {
					refs = append(refs, object.CanonicalRef)
					seen[object.CanonicalRef] = true
				}
				break
			}
		}
	}
	if database := set.Volumes["database"]; database != nil {
		for _, object := range database.Objects {
			if object.CanonicalRef != objectRef || object.Entry == nil {
				continue
			}
			for _, relation := range strings.Split(object.Entry.R, ",") {
				ref := strings.TrimSpace(relation)
				if knownCode[ref] && !seen[ref] {
					refs = append(refs, ref)
					seen[ref] = true
				}
			}
		}
	}
	sort.Strings(refs)
	return refs
}

func receiptPath(root, batchID string) string {
	return filepath.Join(root, ".aoci", "drafts", "database-cognition", "candidate-"+batchID+".json")
}

func encodeTargets(targets []ReceiptTarget, includeCandidate bool) string {
	var builder strings.Builder
	for _, target := range targets {
		values := []string{target.ObjectRef, target.SourceID, target.CognitionState, target.EvidenceVersion, target.TableEvidenceSHA256, target.EvidenceRef}
		if includeCandidate {
			values = append(values, target.CandidateID)
		}
		for _, value := range values {
			fmt.Fprintf(&builder, "%d:%s", len(value), value)
		}
	}
	return builder.String()
}

func receiptHash(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func validSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
