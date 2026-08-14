package codebatch

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

const Version = "code-cognition-batch/v1"

func BuildPlan(root, compositeIdentity, scopePolicyIdentity, codeVolumePath, codeVolumeSHA256 string, candidates []Candidate, limit int) (Plan, error) {
	if limit < 1 {
		return Plan{}, fmt.Errorf("code_candidate_batch_limit_invalid")
	}
	ordered := cloneCandidates(candidates)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ObjectRef < ordered[j].ObjectRef })
	if err := validateCandidates(ordered); err != nil {
		return Plan{}, err
	}
	selected := ordered
	if len(selected) > limit {
		selected = selected[:limit]
	}
	return savePlan(root, compositeIdentity, scopePolicyIdentity, codeVolumePath, codeVolumeSHA256, ordered, selected, nil, limit)
}

// ReplanForRelations issues a replacement current batch that is closed under
// the accumulated model-authored relations. No formal cognition is written.
//
// 计划阶段拿不到关系图,因此这里把本次观察到的关系边合并进收据累积的事实,
// 再在"已知图"上按强连通分量装箱 —— 逆拓扑前缀天然自闭合。最小不可拆成分
// 超过上限时显式失败并带出精确事实,绝不丢弃 R 或改写模型语义。
// issuedBatchIDs 是本计划谱系已签发过的批次身份,用于兜底检测不收敛。
func ReplanForRelations(root string, receipt Receipt, observed []ObservedRelation,
	issuedBatchIDs []string, limit int) (Plan, ClosureDiagnostic, error) {
	if err := validateReceipt(receipt); err != nil {
		return Plan{}, ClosureDiagnostic{}, err
	}
	merged := MergeObservedRelations(receipt.ObservedRelations, observed)
	known := make(map[string][]string, len(merged))
	for _, relation := range merged {
		known[relation.SourceObjectRef] = relation.TargetObjectRefs
	}
	// change != create 的目标在当前卷里已经存在,其关系无需同批解析。
	resolved := map[string]bool{}
	for _, target := range receipt.AllTargets {
		if target.Change != "create" {
			resolved[target.ObjectRef] = true
		}
	}
	// 已被观察过关系的对象 —— 只有它们不会再带来意外的跨批边。
	observedRefs := make(map[string]bool, len(merged))
	for _, relation := range merged {
		observedRefs[relation.SourceObjectRef] = true
	}
	selectedTargets, diagnostic, err := SelectRelationClosedBatch(receipt.AllTargets, known, resolved, observedRefs, limit)
	if err != nil {
		return Plan{}, diagnostic, err
	}
	if len(selectedTargets) == 0 {
		return Plan{}, diagnostic, fmt.Errorf("code_candidate_relation_target_not_in_plan")
	}
	candidateByRef := map[string]Candidate{}
	for _, target := range receipt.AllTargets {
		candidateByRef[target.ObjectRef] = Candidate{Target: target}
	}
	allCandidates := make([]Candidate, 0, len(receipt.AllTargets))
	for _, target := range receipt.AllTargets {
		allCandidates = append(allCandidates, candidateByRef[target.ObjectRef])
	}
	selectedCandidates := make([]Candidate, 0, len(selectedTargets))
	for _, target := range selectedTargets {
		selectedCandidates = append(selectedCandidates, candidateByRef[target.ObjectRef])
	}
	// 兜底: 算法正确时不会重复签发同一批次;真的发生说明存在未预见的边界,
	// 此时立刻停下并报出不收敛,而不是继续消耗宿主的创作时间。
	planned := plannedBatchIdentity(receipt, selectedCandidates)
	for _, issued := range issuedBatchIDs {
		if issued == planned {
			return Plan{}, diagnostic, fmt.Errorf("code_candidate_relation_replan_not_converging")
		}
	}
	plan, err := savePlan(root, receipt.CompositeIdentity, receipt.ScopePolicyIdentity, receipt.CodeVolumePath,
		receipt.CodeVolumeSHA256, allCandidates, selectedCandidates, merged, limit)
	return plan, diagnostic, err
}

// plannedBatchIdentity 预演 savePlan 的批次身份推导,用于落盘前的重复检测。
// PlanID 由全集派生且已被 validateReceipt 核对过,这里直接复用。
func plannedBatchIdentity(receipt Receipt, selected []Candidate) string {
	return receiptHash("code-cognition-batch/v1", receipt.PlanID,
		encodeTargets(targetsWithoutCandidateIDs(selected), false))
}

func ValidateSubmission(root, batchID, compositeIdentity, scopePolicyIdentity, codeVolumePath, codeVolumeSHA256 string, submissions []Submission, allowPostimage bool) (Receipt, error) {
	receipt, err := LoadReceipt(root, batchID)
	if err != nil {
		if expected, ok := resolveExactSubmissionReceipt(root, compositeIdentity, scopePolicyIdentity,
			codeVolumePath, codeVolumeSHA256, submissions, allowPostimage); ok {
			return Receipt{}, &SubmissionError{Code: "code_candidate_batch_id_mismatch",
				ExpectedBatchID: expected.BatchID, ActualBatchID: batchID}
		}
		return Receipt{}, err
	}
	if !allowPostimage && (receipt.CompositeIdentity != compositeIdentity || receipt.ScopePolicyIdentity != scopePolicyIdentity ||
		receipt.CodeVolumePath != codeVolumePath || receipt.CodeVolumeSHA256 != codeVolumeSHA256) {
		return Receipt{}, fmt.Errorf("code_candidate_plan_stale")
	}
	if len(submissions) != len(receipt.Targets) {
		return Receipt{}, fmt.Errorf("code_candidate_batch_incomplete")
	}
	submitted := map[string]Submission{}
	for index, item := range submissions {
		if item.CandidateIndex == 0 {
			item.CandidateIndex = index + 1
		}
		if item.ObjectRef == "" || item.CandidateID == "" || item.SourceSHA256 == "" || submitted[item.ObjectRef].ObjectRef != "" {
			return Receipt{}, fmt.Errorf("code_candidate_duplicate_or_invalid")
		}
		submitted[item.ObjectRef] = item
	}
	issues, matched := submissionIssues(receipt, submissions)
	if !matched {
		return Receipt{}, fmt.Errorf("code_candidate_batch_mismatch")
	}
	if len(issues) > 0 {
		if !receiptSourcesCurrent(root, receipt) {
			return Receipt{}, fmt.Errorf("code_candidate_plan_stale")
		}
		return Receipt{}, &SubmissionError{Code: "code_candidate_binding_mismatch", Issues: issues}
	}
	return receipt, nil
}

// resolveExactSubmissionReceipt recognizes only a unique current receipt for
// which every per-candidate binding is already exact. This lets the caller
// distinguish a copied authoring_batch.batch_identity from the Code domain's
// actual batch id without accepting a guessed or stale receipt.
func resolveExactSubmissionReceipt(root, compositeIdentity, scopePolicyIdentity, codeVolumePath, codeVolumeSHA256 string, submissions []Submission, allowPostimage bool) (Receipt, bool) {
	directory := filepath.Join(root, ".aoci", "drafts", "code-cognition")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return Receipt{}, false
	}
	var matched Receipt
	matchCount := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "candidate-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		candidateBatchID := strings.TrimSuffix(strings.TrimPrefix(name, "candidate-"), ".json")
		receipt, loadErr := LoadReceipt(root, candidateBatchID)
		if loadErr != nil || receipt.CompositeIdentity != compositeIdentity ||
			receipt.ScopePolicyIdentity != scopePolicyIdentity || receipt.CodeVolumePath != codeVolumePath ||
			(!allowPostimage && receipt.CodeVolumeSHA256 != codeVolumeSHA256) || !submissionExactlyMatches(receipt, submissions) ||
			!receiptSourcesCurrent(root, receipt) {
			continue
		}
		matched = receipt
		matchCount++
	}
	return matched, matchCount == 1
}

func receiptSourcesCurrent(root string, receipt Receipt) bool {
	for _, target := range receipt.Targets {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(target.Path)))
		if err != nil {
			return false
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != target.SourceSHA256 {
			return false
		}
	}
	return true
}

func submissionExactlyMatches(receipt Receipt, submissions []Submission) bool {
	if len(submissions) != len(receipt.Targets) {
		return false
	}
	targets := make(map[string]Target, len(receipt.Targets))
	for _, target := range receipt.Targets {
		targets[target.ObjectRef] = target
	}
	for _, item := range submissions {
		target, ok := targets[item.ObjectRef]
		if !ok || item.CandidateID != target.CandidateID || item.SourceSHA256 != target.SourceSHA256 {
			return false
		}
		delete(targets, item.ObjectRef)
	}
	return len(targets) == 0
}

func submissionIssues(receipt Receipt, submissions []Submission) ([]SubmissionIssue, bool) {
	byObject := make(map[string]Target, len(receipt.Targets))
	byCandidate := make(map[string]Target, len(receipt.Targets))
	for _, target := range receipt.Targets {
		byObject[target.ObjectRef] = target
		byCandidate[target.CandidateID] = target
	}
	used := map[string]bool{}
	issues := []SubmissionIssue{}
	for index, item := range submissions {
		candidateIndex := item.CandidateIndex
		if candidateIndex == 0 {
			candidateIndex = index + 1
		}
		pathTarget, pathOK := byObject[item.ObjectRef]
		candidateTarget, candidateOK := byCandidate[item.CandidateID]
		target, ok := pathTarget, pathOK
		if pathOK && candidateOK && pathTarget.ObjectRef != candidateTarget.ObjectRef {
			pathSourceMatches := item.SourceSHA256 == pathTarget.SourceSHA256
			candidateSourceMatches := item.SourceSHA256 == candidateTarget.SourceSHA256
			if pathSourceMatches == candidateSourceMatches {
				return nil, false
			}
			if candidateSourceMatches {
				target = candidateTarget
			}
		} else if !pathOK && candidateOK {
			target, ok = candidateTarget, true
		}
		if !ok || used[target.ObjectRef] {
			return nil, false
		}
		used[target.ObjectRef] = true
		base := SubmissionIssue{CandidateIndex: candidateIndex, Path: target.Path, ObjectRef: target.ObjectRef}
		if item.ObjectRef != target.ObjectRef {
			issue := base
			issue.Field, issue.Expected, issue.Actual, issue.Code = "path", target.Path,
				strings.TrimPrefix(item.ObjectRef, "code:"), "code_candidate_path_mismatch"
			issues = append(issues, issue)
		}
		if item.CandidateID != target.CandidateID {
			issue := base
			issue.Field, issue.Expected, issue.Actual, issue.Code = "candidate_id", target.CandidateID,
				item.CandidateID, "code_candidate_id_mismatch"
			issues = append(issues, issue)
		}
		if item.SourceSHA256 != target.SourceSHA256 {
			issue := base
			issue.Field, issue.Expected, issue.Actual, issue.Code = "source_sha256", target.SourceSHA256,
				item.SourceSHA256, "code_candidate_source_sha256_mismatch"
			issues = append(issues, issue)
		}
	}
	return issues, len(used) == len(receipt.Targets)
}

func LoadReceipt(root, batchID string) (Receipt, error) {
	if !validSHA256(batchID) {
		return Receipt{}, fmt.Errorf("code_candidate_batch_id_invalid")
	}
	data, err := os.ReadFile(receiptPath(root, batchID))
	if err != nil {
		if os.IsNotExist(err) {
			return Receipt{}, fmt.Errorf("code_candidate_receipt_missing")
		}
		return Receipt{}, fmt.Errorf("code_candidate_receipt_unreadable")
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
		return Receipt{}, fmt.Errorf("code_candidate_receipt_trailing_json")
	}
	if err := validateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func savePlan(root, compositeIdentity, scopePolicyIdentity, codeVolumePath, codeVolumeSHA256 string, all, selected []Candidate, observed []ObservedRelation, limit int) (Plan, error) {
	allTargets := targetsWithoutCandidateIDs(all)
	planID := receiptHash("code-cognition-plan/v1", compositeIdentity, scopePolicyIdentity, codeVolumePath, codeVolumeSHA256, encodeTargets(allTargets, false))
	selectedTargets := targetsWithoutCandidateIDs(selected)
	batchID := receiptHash("code-cognition-batch/v1", planID, encodeTargets(selectedTargets, false))
	for index := range selectedTargets {
		selectedTargets[index].CandidateID = receiptHash("code-cognition-object/v1", batchID, encodeTarget(selectedTargets[index], false))
	}
	selectedByRef := map[string]Target{}
	for _, target := range selectedTargets {
		selectedByRef[target.ObjectRef] = target
	}
	planCandidates := make([]Candidate, 0, len(selected))
	for _, candidate := range selected {
		candidate.Target = selectedByRef[candidate.ObjectRef]
		planCandidates = append(planCandidates, candidate)
	}
	receipt := Receipt{Version: Version, PlanID: planID, BatchID: batchID, CompositeIdentity: compositeIdentity,
		ScopePolicyIdentity: scopePolicyIdentity, CodeVolumePath: codeVolumePath, CodeVolumeSHA256: codeVolumeSHA256,
		AllTargets: allTargets, Targets: selectedTargets, ObservedRelations: observed}
	if err := saveReceipt(root, receipt); err != nil {
		return Plan{}, err
	}
	remaining := len(all) - len(selected)
	return Plan{Version: Version, PlanID: planID, BatchID: batchID, CompositeIdentity: compositeIdentity,
		ScopePolicyIdentity: scopePolicyIdentity, CodeVolumePath: codeVolumePath, CodeVolumeSHA256: codeVolumeSHA256,
		TotalTargets: len(all), MaxEntries: limit, Included: len(selected), Remaining: remaining,
		CompleteCandidateSetForBatch: true, ContinuationRequired: remaining > 0, Candidates: planCandidates,
		NextAction: "author_complete_current_machine_batch"}, nil
}

func saveReceipt(root string, receipt Receipt) error {
	if err := validateReceipt(receipt); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	path := receiptPath(root, receipt.BatchID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("code_candidate_receipt_directory_unavailable")
	}
	data = append(data, '\n')
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("code_candidate_receipt_conflict")
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("code_candidate_receipt_unreadable")
	}
	if err := afs.AtomicWrite(path, data); err != nil {
		return fmt.Errorf("code_candidate_receipt_write_failed")
	}
	return nil
}

func validateReceipt(receipt Receipt) error {
	path, pathErr := afs.NormalizeRelPath(receipt.CodeVolumePath)
	if receipt.Version != Version || !validSHA256(receipt.PlanID) || !validSHA256(receipt.BatchID) ||
		!validSHA256(receipt.CompositeIdentity) || pathErr != nil || path != receipt.CodeVolumePath ||
		!validSHA256(receipt.CodeVolumeSHA256) || len(receipt.AllTargets) == 0 || len(receipt.Targets) == 0 {
		return fmt.Errorf("code_candidate_receipt_invalid")
	}
	if receipt.ScopePolicyIdentity != "" && !validSHA256(receipt.ScopePolicyIdentity) {
		return fmt.Errorf("code_candidate_receipt_invalid")
	}
	if err := validateTargets(receipt.AllTargets, false); err != nil {
		return err
	}
	if err := validateTargets(receipt.Targets, true); err != nil {
		return err
	}
	wantPlan := receiptHash("code-cognition-plan/v1", receipt.CompositeIdentity, receipt.ScopePolicyIdentity,
		receipt.CodeVolumePath, receipt.CodeVolumeSHA256, encodeTargets(receipt.AllTargets, false))
	if receipt.PlanID != wantPlan {
		return fmt.Errorf("code_candidate_plan_id_mismatch")
	}
	wantBatch := receiptHash("code-cognition-batch/v1", receipt.PlanID, encodeTargets(receipt.Targets, false))
	if receipt.BatchID != wantBatch {
		return fmt.Errorf("code_candidate_batch_id_mismatch")
	}
	all := map[string]Target{}
	for _, target := range receipt.AllTargets {
		all[target.ObjectRef] = target
	}
	for _, target := range receipt.Targets {
		base, ok := all[target.ObjectRef]
		if !ok || base.Path != target.Path || base.Change != target.Change || base.SourceSHA256 != target.SourceSHA256 ||
			base.ExistingEntry != target.ExistingEntry {
			return fmt.Errorf("code_candidate_batch_target_not_in_plan")
		}
		wantCandidate := receiptHash("code-cognition-object/v1", receipt.BatchID, encodeTarget(target, false))
		if target.CandidateID != wantCandidate {
			return fmt.Errorf("code_candidate_id_mismatch")
		}
	}
	return nil
}

func validateCandidates(candidates []Candidate) error {
	targets := targetsWithoutCandidateIDs(candidates)
	return validateTargets(targets, false)
}

func validateTargets(targets []Target, requireCandidateID bool) error {
	previous := ""
	for _, target := range targets {
		path, err := afs.NormalizeRelPath(target.Path)
		if err != nil || path != target.Path || target.ObjectRef != "code:"+target.Path || target.ObjectRef <= previous ||
			(target.Change != "create" && target.Change != "update") || !validSHA256(target.SourceSHA256) ||
			(requireCandidateID && !validSHA256(target.CandidateID)) || (!requireCandidateID && target.CandidateID != "") {
			return fmt.Errorf("code_candidate_target_invalid")
		}
		previous = target.ObjectRef
	}
	return nil
}

func targetsWithoutCandidateIDs(candidates []Candidate) []Target {
	result := make([]Target, 0, len(candidates))
	for _, candidate := range candidates {
		target := candidate.Target
		target.CandidateID = ""
		result = append(result, target)
	}
	return result
}

func cloneCandidates(values []Candidate) []Candidate {
	return append([]Candidate{}, values...)
}

func receiptPath(root, batchID string) string {
	return filepath.Join(root, ".aoci", "drafts", "code-cognition", "candidate-"+batchID+".json")
}

func encodeTargets(targets []Target, includeCandidateID bool) string {
	var builder strings.Builder
	for _, target := range targets {
		builder.WriteString(encodeTarget(target, includeCandidateID))
	}
	return builder.String()
}

func encodeTarget(target Target, includeCandidateID bool) string {
	values := []string{target.ObjectRef, target.Path, target.Change, target.SourceSHA256, target.ExistingEntry}
	if includeCandidateID {
		values = append(values, target.CandidateID)
	}
	var builder strings.Builder
	for _, value := range values {
		fmt.Fprintf(&builder, "%d:%s", len(value), value)
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
