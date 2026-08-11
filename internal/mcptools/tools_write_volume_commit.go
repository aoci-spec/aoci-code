package mcptools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

var (
	saveVolumeGovernanceReceipt = saveCompletedEntriesGovernanceReceipt
	ensureVolumeLedger          = func(root string, enabled bool, event ledger.Event) error {
		if !enabled {
			return nil
		}
		return cognitiontxn.EnsureLedger(root, true, event)
	}
)

type cognitionVolumeWriteTarget struct {
	VolumeID   string
	Path       string
	CurrentSHA string
	PreSHA     string
	PostSHA    string
	PostRaw    []byte
}

type cognitionVolumeBatchPlan struct {
	targets            []cognitionVolumeWriteTarget
	volumePaths        []string
	sourceFingerprints map[string]baseline.Fingerprint
	recovery           *atomicBatchRecovery
	allPost            bool
	databaseBindings   []baseline.DatabaseCognitionBinding
	databaseReceipt    *dbcognition.Receipt
	codeReceipt        *codebatch.Receipt
}

func planCognitionVolumeUpdates(
	root string,
	loaded *cognitionRepoCtx,
	normalized []normalizedAtomicItem,
	start time.Time,
) (*atomicBatchPlan, *Fail) {
	if len(normalized) == 0 {
		return nil, &Fail{Code: errCrossVolumeWriteNotSupported, Msg: writeMessage("entry.volume.cross_write_not_supported")}
	}
	receiptMode, codeBatchID, databaseBatchID := false, "", ""
	for _, item := range normalized {
		if item.candidateID == "" && item.batchID == "" {
			continue
		}
		if item.candidateID == "" || item.batchID == "" {
			return nil, &Fail{Code: errBadArgs, Msg: "candidate_batch_fields_invalid"}
		}
		switch {
		case item.rel != "":
			if codeBatchID != "" && codeBatchID != item.batchID {
				return nil, &Fail{Code: errBadArgs, Msg: "code_candidate_batch_fields_invalid"}
			}
			codeBatchID = item.batchID
		case cognition.IsCanonicalDatabaseRef(item.objectRef):
			if databaseBatchID != "" && databaseBatchID != item.batchID {
				return nil, &Fail{Code: errBadArgs, Msg: "database_candidate_batch_fields_invalid"}
			}
			databaseBatchID = item.batchID
		default:
			return nil, &Fail{Code: errBadArgs, Msg: "candidate_batch_fields_invalid"}
		}
		receiptMode = true
	}
	ordered := append([]normalizedAtomicItem{}, normalized...)
	sort.Slice(ordered, func(i, j int) bool {
		return volumeItemIdentity(ordered[i]) < volumeItemIdentity(ordered[j])
	})
	batchKey := atomicBatchKey(ordered)
	recovery, recoveryErr := loadEntriesRecoveryIncludingArchive(root, batchKey)
	if recoveryErr != nil && !os.IsNotExist(recoveryErr) {
		return nil, &Fail{Code: errInternal, Msg: mcpMessage("entries.recovery_receipt.invalid", localeSafeWriteDetail(recoveryErr.Error()))}
	}
	var databaseReceipt *dbcognition.Receipt
	var codeReceipt *codebatch.Receipt
	if receiptMode {
		if len(normalized) > machinecontract.EntriesBatchMaxItems {
			return nil, &Fail{Code: errBadArgs, Msg: "cognition_candidate_batch_too_large"}
		}
		if codeBatchID != "" {
			submissions := make([]codebatch.Submission, 0, len(normalized))
			for _, item := range normalized {
				if item.rel == "" {
					continue
				}
				if item.candidateID == "" || item.batchID != codeBatchID || !validRecoverySHA256(item.sourceSHA256) {
					return nil, &Fail{Code: errBadArgs, Msg: "code_candidate_batch_fields_invalid"}
				}
				submissions = append(submissions, codebatch.Submission{CandidateIndex: item.originalCandidateIndex,
					ObjectRef: "code:" + item.rel, CandidateID: item.candidateID, SourceSHA256: item.sourceSHA256})
			}
			facts, factsErr := volumegovernance.Assess(root, loaded.cfg, loaded.set)
			if factsErr != nil {
				return nil, &Fail{Code: errInternal, Msg: "code_candidate_governance_unavailable"}
			}
			allowPostimage := recovery != nil && recovery.Version == 4 && recovery.CodeBatchID == codeBatchID &&
				inspectVolumeTargetStates(root, recoveryVolumeTargets(recovery)) == "postimage"
			receipt, err := codebatch.ValidateSubmission(root, codeBatchID, facts.CompositeIdentity,
				facts.ManagedScope.PolicyIdentity, facts.Code.Path, facts.Code.SHA256, submissions, allowPostimage)
			if err != nil {
				var submissionErr *codebatch.SubmissionError
				if errors.As(err, &submissionErr) {
					findings := codeSubmissionRepairFindings(root, submissionErr)
					return nil, &Fail{Code: errCandidateInvalid, Msg: submissionErr.Error(),
						Findings: findings, Repairable: len(findings) > 0}
				}
				return nil, &Fail{Code: errWriteConflict, Msg: localeSafeWriteDetail(err.Error()), Hint: writeMessage("entry.batch.hint.replan")}
			}
			codeReceipt = &receipt
			if replan, needed, relationErr := replanCodeRelationClosure(root, receipt, normalized); relationErr != nil {
				return nil, &Fail{Code: errCandidateInvalid, Msg: relationErr.Error(), Repairable: true}
			} else if needed {
				return nil, &Fail{Code: errWriteConflict, Msg: "code_candidate_relation_replan_required", CodePlan: &replan}
			}
		}
		if databaseBatchID != "" {
			submissions := make([]dbcognition.Submission, 0, len(normalized))
			for _, item := range normalized {
				if item.rel != "" {
					continue
				}
				if !cognition.IsCanonicalDatabaseRef(item.objectRef) || item.sourceSHA256 != "" || item.candidateID == "" || item.batchID != databaseBatchID {
					return nil, &Fail{Code: errBadArgs, Msg: "database_candidate_batch_fields_invalid"}
				}
				submissions = append(submissions, dbcognition.Submission{ObjectRef: item.objectRef, CandidateID: item.candidateID})
			}
			receipt, err := dbcognition.ValidateSubmission(root, loaded.cfg.DatabaseSources, databaseBatchID, submissions)
			if err != nil {
				if recovery == nil || (recovery.Version != 3 && recovery.Version != 4) || recovery.DatabaseBatchID != databaseBatchID ||
					!databaseRecoveryPostimage(root, recovery) || !databaseRecoveryEvidenceCurrent(root, loaded.cfg.DatabaseSources, recovery) {
					return nil, &Fail{Code: errWriteConflict, Msg: localeSafeWriteDetail(err.Error()), Hint: writeMessage("entry.batch.hint.replan")}
				}
			} else {
				databaseReceipt = &receipt
			}
		}
	} else if len(normalized) > machinecontract.EntriesBatchMaxItems {
		return nil, &Fail{Code: errBadArgs, Msg: "cognition_candidate_batch_too_large"}
	}
	candidates := make([]cognition.ImpactCandidate, 0, len(ordered))
	volumes := map[string]bool{}
	for _, item := range ordered {
		if !receiptMode && !validRecoverySHA256(item.sourceSHA256) {
			return nil, &Fail{Code: errBadArgs, Msg: writeMessage("entry.write.mcp.source_binding_required", volumeItemIdentity(item)), Hint: writeMessage("entry.write.mcp.hint.source_binding")}
		}
		candidate := cognition.ImpactCandidate{
			Change:                 cognition.ImpactChangeUpdate,
			OriginalCandidateIndex: item.originalCandidateIndex,
		}
		switch {
		case item.rel != "":
			candidate.ObjectRef = "code:" + item.rel
			candidate.VolumeID = "code"
			candidate.Path = item.rel
			candidate.CanonicalLine = canonicalVolumeCandidateLine(item.rel, item.newEntry)
			if cognitionObjectByRef(loaded.set.Volumes["code"], candidate.ObjectRef) == nil {
				candidate.Change = cognition.ImpactChangeCreate
			}
			volumes["code"] = true
		case cognition.IsCanonicalDatabaseRef(item.objectRef):
			candidate.ObjectRef = item.objectRef
			candidate.VolumeID = "database"
			if databaseAsset := loaded.set.Volumes["database"]; databaseAsset != nil {
				candidate.Path = databaseAsset.Descriptor.Path
			}
			candidate.CanonicalLine = index.StripFences(item.newEntry)
			volumes["database"] = true
			if cognitionObjectByRef(loaded.set.Volumes["database"], item.objectRef) == nil {
				if !receiptMode {
					return nil, &Fail{Code: errBadArgs, Msg: "database_create_requires_candidate_receipt", Hint: writeMessage("entry.batch.hint.replan")}
				}
				candidate.Change = cognition.ImpactChangeCreate
			}
		default:
			return nil, &Fail{Code: errBadArgs, Msg: writeMessage("entry.batch.path_invalid", 1, item.objectRef, "object_ref must be a canonical Database table identity")}
		}
		candidates = append(candidates, candidate)
	}
	envelope, fail := resolveCognitionChangeEnvelope(loaded.set, candidates)
	if fail != nil {
		if receiptMode && fail.Code == errImpactResolutionFailed && strings.Contains(fail.Msg, "impact_candidate_") {
			fail.Code = errCandidateInvalid
		}
		return nil, fail
	}

	projected := map[string][]byte{}
	outcomes := make([]*UpdateOutcome, 0, len(ordered))
	rels := make([]string, 0, len(ordered))
	sourceFingerprints := map[string]baseline.Fingerprint{}
	databaseProjected, databaseActions, databaseFail := projectDatabaseCandidates(loaded.set.Volumes["database"], candidates)
	if databaseFail != nil {
		return nil, databaseFail
	}
	if databaseProjected != nil {
		projected["database"] = databaseProjected
	}
	var codeContext *repoCtx
	if volumes[cognition.ScopeCode] {
		codeContext = volumeCodeRepoContext(root, loaded)
	}
	for itemIndex, item := range ordered {
		candidate := candidates[itemIndex]
		if item.rel != "" {
			fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(item.rel)))
			if err != nil || fingerprint.SHA256 != item.sourceSHA256 {
				return nil, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.source_conflict", "code:"+item.rel), Hint: writeMessage("entry.batch.hint.refresh_binding")}
			}
			sourceFingerprints[item.rel] = fingerprint
			outcome, nextText, itemFail := prepareUpdateEntry(root, item.rel, item.newEntry, codeContext)
			if itemFail != nil {
				return nil, itemFail
			}
			projected["code"] = []byte(nextText)
			codeContext.text = nextText
			codeContext.doc, _ = index.Parse(nextText)
			index.ResolveRelPaths(codeContext.doc, root)
			outcomes = append(outcomes, outcome)
			rels = append(rels, item.rel)
			continue
		}
		object := cognitionObjectByRef(loaded.set.Volumes["database"], candidate.ObjectRef)
		before := ""
		if object != nil {
			before = object.CanonicalLine
		}
		outcomes = append(outcomes, &UpdateOutcome{
			Action: databaseActions[candidate.ObjectRef], Rel: candidate.ObjectRef,
			Diff: renderEntryWriteDiff(before, candidate.CanonicalLine),
		})
		rels = append(rels, candidate.ObjectRef)
	}

	for _, volumeID := range envelope.WriteSet {
		if projected[volumeID] == nil {
			projected[volumeID] = append([]byte{}, loaded.set.Volumes[volumeID].Raw...)
		}
		if findings := cognition.ValidateProjectedObjectVolume(loaded.set, volumeID, projected[volumeID]); len(findings) > 0 {
			code := errImpactResolutionFailed
			if receiptMode && volumeID == "database" {
				code = errCandidateInvalid
			}
			return nil, &Fail{Code: code, Msg: writeMessage("entry.volume.projected_invalid", findings[0].Code), Hint: writeMessage("entry.volume.hint.regenerate_candidate")}
		}
	}
	projectedVolumeRaw := map[string][]byte{cognition.ScopeMeta: append([]byte{}, loaded.set.Meta.Raw...)}
	for _, volumeID := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		if asset := loaded.set.Volumes[volumeID]; asset != nil {
			projectedVolumeRaw[volumeID] = append([]byte{}, asset.Raw...)
		}
		if next := projected[volumeID]; next != nil {
			projectedVolumeRaw[volumeID] = append([]byte{}, next...)
		}
	}
	projectedSet, projectedFindings := cognition.BuildProjectedSet(root, canonicalProjectedRoot(loaded.set.Root.Raw), projectedVolumeRaw)
	if projectedSet == nil || len(projectedFindings) != 0 {
		detail := "projected_cognition_set_invalid"
		if len(projectedFindings) != 0 {
			detail = projectedFindings[0].Code
		}
		return nil, &Fail{Code: errImpactResolutionFailed, Msg: writeMessage("entry.volume.projected_invalid", detail), Hint: writeMessage("entry.volume.hint.regenerate_candidate")}
	}
	projectedBudget := volumegovernance.AssessProjectedBudget(loaded.cfg, projectedSet)
	if projectedBudget.Mode == machinecontract.BudgetModeEnforce && len(projectedBudget.Violations) != 0 {
		return nil, &Fail{Code: errCandidateInvalid, Msg: writeMessage("entry.write.budget_failed", projectedBudget.Violations[0].Code, projectedBudget.WholeIndexTokens), Hint: writeMessage("entry.write.hint.budget_reauthor")}
	}

	participants := make([]cognitionVolumeWriteTarget, 0, len(envelope.WriteSet))
	volumePaths := make([]string, 0, len(envelope.WriteSet))
	for _, volumeID := range envelope.WriteSet {
		asset := loaded.set.Volumes[volumeID]
		participants = append(participants, cognitionVolumeWriteTarget{
			VolumeID: volumeID, Path: asset.Descriptor.Path, CurrentSHA: asset.SHA256,
			PreSHA: asset.SHA256, PostSHA: indexTextHash(string(projected[volumeID])), PostRaw: projected[volumeID],
		})
		volumePaths = append(volumePaths, asset.Descriptor.Path)
	}
	if recovery != nil {
		if recovery.Version != 2 && recovery.Version != 3 && recovery.Version != 4 {
			return nil, &Fail{Code: errWriteConflict, Msg: mcpMessage("entries.recovery_receipt.postimage_mismatch")}
		}
		if fail := bindVolumeRecovery(participants, recovery); fail != nil {
			return nil, fail
		}
	}
	if databaseReceipt != nil {
		database := targetByVolume(participants, "database")
		if database == nil || database.Path != databaseReceipt.DatabaseVolumePath || database.PreSHA != databaseReceipt.DatabaseVolumeSHA256 {
			return nil, &Fail{Code: errWriteConflict, Msg: "database_candidate_volume_preimage_stale", Hint: writeMessage("entry.batch.hint.replan")}
		}
	}
	if codeReceipt != nil {
		code := targetByVolume(participants, cognition.ScopeCode)
		if code == nil || code.Path != codeReceipt.CodeVolumePath || code.PreSHA != codeReceipt.CodeVolumeSHA256 {
			return nil, &Fail{Code: errWriteConflict, Msg: "code_candidate_volume_preimage_stale", Hint: writeMessage("entry.batch.hint.replan")}
		}
	}
	for itemIndex, item := range ordered {
		if item.objectRef == "" {
			continue
		}
		if item.candidateID != "" {
			continue
		}
		database := targetByVolume(participants, "database")
		expected := database.CurrentSHA
		if recovery != nil {
			expected = database.PreSHA
		}
		if item.sourceSHA256 != expected {
			return nil, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.source_conflict", candidates[itemIndex].ObjectRef), Hint: writeMessage("entry.batch.hint.refresh_binding")}
		}
	}
	targets := make([]cognitionVolumeWriteTarget, 0, len(participants))
	allPost := true
	for _, participant := range participants {
		if recovery == nil && participant.PreSHA == participant.PostSHA {
			continue
		}
		if participant.CurrentSHA != participant.PostSHA {
			allPost = false
		}
		targets = append(targets, participant)
	}
	if recovery == nil && len(targets) > 0 {
		allPost = false
	}

	paths := loaded.paths
	primary := participants[0]
	paths.IndexPath = filepath.Join(root, filepath.FromSlash(primary.Path))
	rc := &repoCtx{cfg: loaded.cfg, paths: paths, text: string(loaded.set.Volumes[primary.VolumeID].Raw), bl: loaded.bl}
	preIdentity := cognitionVolumeTransitionIdentity(participants, false)
	postIdentity := cognitionVolumeTransitionIdentity(participants, true)
	if recovery != nil {
		preIdentity, postIdentity = recovery.PreIndexSHA256, recovery.PostIndexSHA256
	}
	databaseBindings := []baseline.DatabaseCognitionBinding{}
	if databaseReceipt != nil {
		targetByRef := map[string]dbcognition.ReceiptTarget{}
		for _, target := range databaseReceipt.Targets {
			targetByRef[target.ObjectRef] = target
		}
		for _, candidate := range candidates {
			if !cognition.IsCanonicalDatabaseRef(candidate.ObjectRef) {
				continue
			}
			target := targetByRef[candidate.ObjectRef]
			databaseBindings = append(databaseBindings, baseline.DatabaseCognitionBinding{
				ObjectRef: candidate.ObjectRef, SourceID: target.SourceID, EvidenceVersion: target.EvidenceVersion,
				TableEvidenceSHA256: target.TableEvidenceSHA256, EntrySHA256: dbcognition.EntrySHA256(candidate.CanonicalLine),
			})
		}
	}
	if recovery != nil && (recovery.Version == 3 || (recovery.Version == 4 && recovery.DatabaseBatchID != "")) {
		recoveredBindings, bindErr := databaseBindingsFromRecovery(recovery, recovery.DatabaseBatchID, databaseCandidatesOnly(candidates))
		if bindErr != nil || (len(databaseBindings) > 0 && !sameDatabaseBindings(databaseBindings, recoveredBindings)) {
			return nil, &Fail{Code: errWriteConflict, Msg: mcpMessage("entries.recovery_receipt.postimage_mismatch")}
		}
		databaseBindings = recoveredBindings
	}
	if databaseReceipt != nil && len(databaseBindings) == 0 {
		return nil, &Fail{Code: errWriteConflict, Msg: "database_candidate_binding_unavailable", Hint: writeMessage("entry.batch.hint.replan")}
	}
	if len(targets) == 0 && len(databaseBindings) > 0 {
		allPost = databaseBindingsMatch(loaded.bl, databaseBindings)
	}
	return &atomicBatchPlan{
		outcomes: outcomes, rels: rels, normalizedItems: ordered, rc: rc,
		indexHash: preIdentity, finalText: postIdentity, changeEnvelope: envelope,
		volumePlan: &cognitionVolumeBatchPlan{targets: targets, volumePaths: volumePaths, sourceFingerprints: sourceFingerprints,
			recovery: recovery, allPost: allPost, databaseBindings: databaseBindings, databaseReceipt: databaseReceipt, codeReceipt: codeReceipt},
		batchKey: batchKey, start: start,
	}, nil
}

func codeSubmissionRepairFindings(root string, mismatch *codebatch.SubmissionError) []cognition.RepairFinding {
	if mismatch == nil {
		return nil
	}
	result := make([]cognition.RepairFinding, 0, len(mismatch.Issues)+1)
	for _, issue := range mismatch.Issues {
		if issue.CandidateIndex < 1 || issue.Path == "" || issue.ObjectRef == "" || issue.Field == "" ||
			issue.Expected == "" || issue.Actual == "" || issue.Code == "" {
			continue
		}
		result = append(result, cognition.RepairFinding{CandidateIndex: issue.CandidateIndex,
			Path: issue.Path, CanonicalObjectIdentity: issue.ObjectRef, Domain: cognition.ScopeCode,
			Field: issue.Field, RuleCode: issue.Code, Expected: issue.Expected, Actual: issue.Actual,
			Code: issue.Code, ObjectRef: issue.ObjectRef})
	}
	if mismatch.Code == "code_candidate_batch_id_mismatch" && mismatch.ExpectedBatchID != "" && mismatch.ActualBatchID != "" {
		if receipt, err := codebatch.LoadReceipt(root, mismatch.ExpectedBatchID); err == nil && len(receipt.Targets) > 0 {
			target := receipt.Targets[0]
			result = append(result, cognition.RepairFinding{CandidateIndex: 1, Path: target.Path,
				CanonicalObjectIdentity: target.ObjectRef, Domain: cognition.ScopeCode, Field: "code_batch_id",
				RuleCode: mismatch.Code,
				Expected: "code_plan.batch_id=" + mismatch.ExpectedBatchID + "; candidates[].batch_id=" + mismatch.ExpectedBatchID,
				Actual:   "code_batch_id=" + mismatch.ActualBatchID + "; authoring_batch.batch_identity is not a Code batch id",
				Code:     mismatch.Code, ObjectRef: target.ObjectRef})
		}
	}
	return result
}

func recoveryVolumeTargets(recovery *atomicBatchRecovery) []cognitionVolumeWriteTarget {
	if recovery == nil {
		return nil
	}
	targets := make([]cognitionVolumeWriteTarget, 0, len(recovery.Assets))
	for _, asset := range recovery.Assets {
		targets = append(targets, cognitionVolumeWriteTarget{VolumeID: asset.VolumeID, Path: asset.Path,
			PreSHA: asset.PreSHA256, PostSHA: asset.PostSHA256})
	}
	return targets
}

func replanCodeRelationClosure(root string, receipt codebatch.Receipt, normalized []normalizedAtomicItem) (codebatch.Plan, bool, error) {
	all, current := map[string]bool{}, map[string]bool{}
	for _, target := range receipt.AllTargets {
		all[target.ObjectRef] = true
	}
	for _, target := range receipt.Targets {
		current[target.ObjectRef] = true
	}
	required := map[string]bool{}
	for _, item := range normalized {
		if item.rel == "" || item.batchID != receipt.BatchID {
			continue
		}
		entry, ok := index.ParseEntryLine(canonicalVolumeCandidateLine(item.rel, item.newEntry), 1)
		if !ok {
			continue
		}
		for _, relation := range strings.Split(entry.R, ",") {
			ref := strings.TrimSpace(relation)
			if !strings.HasPrefix(ref, "code:") || !all[ref] || current[ref] {
				continue
			}
			required["code:"+item.rel] = true
			required[ref] = true
		}
	}
	if len(required) == 0 {
		return codebatch.Plan{}, false, nil
	}
	refs := make([]string, 0, len(required))
	for ref := range required {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	plan, err := codebatch.ReplanForRelations(root, receipt, refs, machinecontract.EntriesBatchMaxItems)
	return plan, err == nil, err
}

// canonicalProjectedRoot upgrades only the legacy five-field descriptor form
// in memory so the strict projected-set validator can evaluate an active
// Volumes v1 layout. Root remains a Guard and is never written by this path.
func canonicalProjectedRoot(raw []byte) []byte {
	newline := "\n"
	text := string(raw)
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	trailing := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for index, line := range lines {
		if !strings.HasPrefix(line, "#Volume:") {
			continue
		}
		if fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#Volume:"))); len(fields) == 5 {
			lines[index] = line + " state=enabled"
		}
	}
	result := strings.Join(lines, newline)
	if trailing {
		result += newline
	}
	return []byte(result)
}

func databaseCandidatesOnly(candidates []cognition.ImpactCandidate) []cognition.ImpactCandidate {
	result := make([]cognition.ImpactCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if cognition.IsCanonicalDatabaseRef(candidate.ObjectRef) {
			result = append(result, candidate)
		}
	}
	return result
}

func databaseBindingsFromRecovery(recovery *atomicBatchRecovery, batchID string, candidates []cognition.ImpactCandidate) ([]baseline.DatabaseCognitionBinding, error) {
	if recovery == nil || (recovery.Version != 3 && recovery.Version != 4) || recovery.DatabaseBatchID != batchID || len(recovery.DatabaseBindings) != len(candidates) {
		return nil, fmt.Errorf("database_recovery_binding_mismatch")
	}
	byRef := make(map[string]cognition.ImpactCandidate, len(candidates))
	for _, candidate := range candidates {
		if !cognition.IsCanonicalDatabaseRef(candidate.ObjectRef) {
			return nil, fmt.Errorf("database_recovery_binding_mismatch")
		}
		byRef[candidate.ObjectRef] = candidate
	}
	result := make([]baseline.DatabaseCognitionBinding, 0, len(recovery.DatabaseBindings))
	for _, binding := range recovery.DatabaseBindings {
		candidate, exists := byRef[binding.ObjectRef]
		if !exists || dbcognition.EntrySHA256(candidate.CanonicalLine) != binding.EntrySHA256 {
			return nil, fmt.Errorf("database_recovery_binding_mismatch")
		}
		result = append(result, baseline.DatabaseCognitionBinding{
			ObjectRef: binding.ObjectRef, SourceID: binding.SourceID, EvidenceVersion: binding.EvidenceVersion,
			TableEvidenceSHA256: binding.TableEvidenceSHA256, EntrySHA256: binding.EntrySHA256,
		})
	}
	return result, nil
}

func sameDatabaseBindings(left, right []baseline.DatabaseCognitionBinding) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func databaseBindingsMatch(state *baseline.Baseline, expected []baseline.DatabaseCognitionBinding) bool {
	if len(expected) == 0 {
		return false
	}
	for _, binding := range expected {
		current, exists := baseline.FindDatabaseCognitionBinding(state, binding.ObjectRef)
		if !exists || current != binding {
			return false
		}
	}
	return true
}

func volumeItemIdentity(item normalizedAtomicItem) string {
	if item.objectRef != "" {
		return item.objectRef
	}
	return "code:" + item.rel
}

func cognitionObjectByRef(asset *cognition.Asset, ref string) *cognition.Object {
	if asset == nil {
		return nil
	}
	for index := range asset.Objects {
		if asset.Objects[index].CanonicalRef == ref {
			return &asset.Objects[index]
		}
	}
	return nil
}

func projectDatabaseCandidates(asset *cognition.Asset, candidates []cognition.ImpactCandidate) ([]byte, map[string]string, *Fail) {
	actions := map[string]string{}
	databaseCandidates := []cognition.ImpactCandidate{}
	for _, candidate := range candidates {
		if cognition.IsCanonicalDatabaseRef(candidate.ObjectRef) {
			databaseCandidates = append(databaseCandidates, candidate)
		}
	}
	if len(databaseCandidates) == 0 {
		return nil, actions, nil
	}
	if asset == nil || asset.State != cognition.AssetPresent {
		return nil, nil, &Fail{Code: errVolumeReadOnly, Msg: "database_volume_absent"}
	}
	sort.Slice(databaseCandidates, func(i, j int) bool { return databaseCandidates[i].ObjectRef < databaseCandidates[j].ObjectRef })
	newline := "\n"
	if strings.Contains(string(asset.Raw), "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(string(asset.Raw), "\n")
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	creates := map[string][]string{}
	for _, candidate := range databaseCandidates {
		object := cognitionObjectByRef(asset, candidate.ObjectRef)
		if object != nil {
			lineIndex := object.SourceLineNumber - 1
			if lineIndex < 0 || lineIndex >= len(lines) || lines[lineIndex] != object.CanonicalLine {
				return nil, nil, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.impact_failed", "projected_object_preimage_changed")}
			}
			lines[lineIndex] = candidate.CanonicalLine
			actions[candidate.ObjectRef] = writeMessage("entry.write.action.replace")
			continue
		}
		namespace := strings.TrimSuffix(candidate.ObjectRef, "/"+databaseObjectName(candidate.ObjectRef))
		creates[namespace] = append(creates[namespace], candidate.CanonicalLine)
		actions[candidate.ObjectRef] = writeMessage("entry.write.action.insert")
	}
	namespaces := make([]string, 0, len(creates))
	for namespace := range creates {
		namespaces = append(namespaces, namespace)
		sort.Strings(creates[namespace])
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		headerIndex := -1
		for index, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "/"+namespace+"/===") {
				headerIndex = index
				break
			}
		}
		if headerIndex < 0 {
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			description := strings.TrimPrefix(namespace, "database://")
			lines = append(lines, "==="+description+"/"+namespace+"/===")
			lines = append(lines, creates[namespace]...)
			lines = append(lines, "")
			continue
		}
		insertAt := len(lines)
		for index := headerIndex + 1; index < len(lines); index++ {
			if strings.HasPrefix(strings.TrimSpace(lines[index]), "===") {
				insertAt = index
				break
			}
		}
		for insertAt > headerIndex+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
			insertAt--
		}
		addition := append([]string{}, creates[namespace]...)
		lines = append(lines[:insertAt], append(addition, lines[insertAt:]...)...)
	}
	return []byte(strings.Join(lines, newline)), actions, nil
}

func databaseObjectName(ref string) string {
	if index := strings.LastIndexByte(ref, '/'); index >= 0 {
		return ref[index+1:]
	}
	return ref
}

func bindVolumeRecovery(participants []cognitionVolumeWriteTarget, recovery *atomicBatchRecovery) *Fail {
	if len(recovery.Assets) == 0 || len(recovery.Assets) > len(participants) {
		return &Fail{Code: errWriteConflict, Msg: mcpMessage("entries.recovery_receipt.postimage_mismatch")}
	}
	seen := map[string]bool{}
	for _, recoveryAsset := range recovery.Assets {
		participant := targetByPath(participants, recoveryAsset.Path)
		if participant == nil || participant.VolumeID != recoveryAsset.VolumeID || participant.PostSHA != recoveryAsset.PostSHA256 ||
			(participant.CurrentSHA != recoveryAsset.PreSHA256 && participant.CurrentSHA != recoveryAsset.PostSHA256) {
			return &Fail{Code: errWriteConflict, Msg: mcpMessage("entries.recovery_receipt.postimage_mismatch")}
		}
		participant.PreSHA = recoveryAsset.PreSHA256
		participant.PostSHA = recoveryAsset.PostSHA256
		seen[participant.Path] = true
	}
	for index := range participants {
		participant := &participants[index]
		if seen[participant.Path] {
			continue
		}
		if participant.CurrentSHA != participant.PostSHA {
			return &Fail{Code: errWriteConflict, Msg: mcpMessage("entries.recovery_receipt.postimage_mismatch")}
		}
	}
	return nil
}

func targetByVolume(targets []cognitionVolumeWriteTarget, volumeID string) *cognitionVolumeWriteTarget {
	for index := range targets {
		if targets[index].VolumeID == volumeID {
			return &targets[index]
		}
	}
	return nil
}

func targetByPath(targets []cognitionVolumeWriteTarget, path string) *cognitionVolumeWriteTarget {
	for index := range targets {
		if targets[index].Path == path {
			return &targets[index]
		}
	}
	return nil
}

func databaseRecoveryPostimage(root string, recovery *atomicBatchRecovery) bool {
	if recovery == nil || (recovery.Version != 3 && recovery.Version != 4) {
		return false
	}
	for _, asset := range recovery.Assets {
		if asset.VolumeID != "database" {
			continue
		}
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(asset.Path)))
		return err == nil && current.SHA256 == asset.PostSHA256
	}
	return false
}

func databaseRecoveryEvidenceCurrent(root string, sources []dbevidence.SourceConfig, recovery *atomicBatchRecovery) bool {
	if recovery == nil || len(recovery.DatabaseBindings) == 0 {
		return false
	}
	evidenceBaseline, exists, err := dbevidence.LoadBaseline(root)
	if err != nil || !exists {
		return false
	}
	accepted := map[string]string{}
	for _, source := range evidenceBaseline.Sources {
		accepted[source.SourceID] = source.SourceSnapshotSHA256
	}
	configured := map[string]dbevidence.SourceConfig{}
	for _, source := range sources {
		if source.Enabled {
			configured[source.SourceID] = source
		}
	}
	type sourceEvidence struct {
		snapshot dbevidence.Snapshot
		valid    bool
	}
	loaded := map[string]sourceEvidence{}
	for _, binding := range recovery.DatabaseBindings {
		current, seen := loaded[binding.SourceID]
		if !seen {
			source, configuredSource := configured[binding.SourceID]
			manifest, snapshot, snapshotExists, loadErr := dbevidence.LoadSnapshot(root, binding.SourceID)
			current = sourceEvidence{snapshot: snapshot, valid: configuredSource && loadErr == nil && snapshotExists &&
				dbevidence.SourceConfigMatchesManifest(source, manifest) && accepted[binding.SourceID] == snapshot.SourceSnapshotSHA256}
			loaded[binding.SourceID] = current
		}
		if !current.valid || current.snapshot.EvidenceVersion != binding.EvidenceVersion {
			return false
		}
		matched := false
		for _, table := range current.snapshot.Tables {
			if table.ObjectRef == binding.ObjectRef && table.TableEvidenceSHA256 == binding.TableEvidenceSHA256 {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func cognitionVolumeTransitionIdentity(targets []cognitionVolumeWriteTarget, post bool) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("cognition-volume-transition/v1"))
	for _, target := range targets {
		value := target.PreSHA
		if post {
			value = target.PostSHA
		}
		for _, field := range []string{target.VolumeID, target.Path, value} {
			var length [8]byte
			binary.BigEndian.PutUint64(length[:], uint64(len(field)))
			_, _ = hash.Write(length[:])
			_, _ = hash.Write([]byte(field))
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func commitCognitionVolumeBatchLocked(root, source string, plan *atomicBatchPlan, retainRecovery bool) (string, bool, *Fail) {
	volumePlan := plan.volumePlan
	if guardID := recoveryGuardMismatch(root, volumePlan.recovery); guardID != "" {
		return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.guard_stale", guardID), Hint: writeMessage("entry.batch.hint.replan")}
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.guard_stale", guardID), Hint: writeMessage("entry.batch.hint.replan")}
	}
	baselineState, exists, err := baseline.Load(root)
	if err != nil {
		return "", false, &Fail{Code: errInternal, Msg: writeMessage("entry.batch.baseline_read_failed", localeSafeWriteDetail(err.Error())), Hint: writeMessage("entry.batch.hint.baseline_read")}
	}
	if !exists || baselineState == nil {
		baselineState = baseline.NewBaseline(nil)
	}
	if fail := validateVolumePlanPrewrite(root, volumePlan); fail != nil {
		return "", false, fail
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.guard_stale", guardID), Hint: writeMessage("entry.batch.hint.replan")}
	}

	if volumePlan.recovery == nil && len(volumePlan.targets) > 0 {
		baselineStateName, baselineSHA, baselineErr := volumeRecoveryBaselinePreimage(root)
		if baselineErr != nil {
			return "", false, &Fail{Code: errInternal, Msg: writeMessage("entry.batch.baseline_read_failed", localeSafeWriteDetail(baselineErr.Error()))}
		}
		recovery := atomicBatchRecovery{Version: 4, BatchKey: plan.batchKey, PreIndexSHA256: plan.indexHash, PostIndexSHA256: plan.finalText,
			BaselinePreState: baselineStateName, BaselinePreSHA: baselineSHA}
		for _, target := range volumePlan.targets {
			preimage, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(target.Path)))
			if readErr != nil || governanceBytesSHA256(preimage) != target.PreSHA {
				return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.target_conflict", target.VolumeID), Hint: writeMessage("entry.batch.hint.replan")}
			}
			recovery.Assets = append(recovery.Assets, atomicBatchRecoveryAsset{VolumeID: target.VolumeID, Path: target.Path,
				PreSHA256: target.PreSHA, PostSHA256: target.PostSHA, Preimage: preimage})
		}
		guardIDs := make([]string, 0, len(plan.changeEnvelope.guards))
		for guardID := range plan.changeEnvelope.guards {
			guardIDs = append(guardIDs, guardID)
		}
		sort.Strings(guardIDs)
		for _, guardID := range guardIDs {
			guard := plan.changeEnvelope.guards[guardID]
			recovery.Guards = append(recovery.Guards, atomicBatchRecoveryGuard{VolumeID: guardID, Path: guard.Path, SHA256: guard.SHA256})
		}
		if len(volumePlan.databaseBindings) > 0 && volumePlan.databaseReceipt != nil {
			recovery.DatabaseBatchID = volumePlan.databaseReceipt.BatchID
			for _, binding := range volumePlan.databaseBindings {
				recovery.DatabaseBindings = append(recovery.DatabaseBindings, atomicBatchRecoveryDatabaseBinding{
					ObjectRef: binding.ObjectRef, SourceID: binding.SourceID, EvidenceVersion: binding.EvidenceVersion,
					TableEvidenceSHA256: binding.TableEvidenceSHA256, EntrySHA256: binding.EntrySHA256,
				})
			}
		}
		if volumePlan.codeReceipt != nil {
			recovery.CodeBatchID = volumePlan.codeReceipt.BatchID
		}
		if err := saveAtomicBatchRecovery(root, recovery); err != nil {
			return "", false, &Fail{Code: errInternal, Msg: writeMessage("entry.batch.recovery_save_failed", localeSafeWriteDetail(err.Error()))}
		}
		volumePlan.recovery = &recovery
	}
	for _, target := range volumePlan.targets {
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(target.Path)))
		if err == nil && current.SHA256 == target.PostSHA {
			continue
		}
		writeErr := writeAtomicIndex(filepath.Join(root, filepath.FromSlash(target.Path)), target.PostRaw, target.PreSHA)
		if writeErr != nil {
			states := inspectVolumeTargetStates(root, volumePlan.targets)
			if states == "preimage" {
				_ = cleanupAtomicBatchRecovery(root, plan.batchKey)
				code := errInternal
				if errors.Is(writeErr, afs.ErrAtomicCASConflict) {
					code = errWriteConflict
				}
				return "", false, &Fail{Code: code, Msg: writeMessage("entry.batch.index_write_failed", localeSafeWriteDetail(writeErr.Error())), Hint: writeMessage("entry.write.hint.disk_permissions")}
			}
			return writeMessage("entry.volume.recovery_required"), false, nil
		}
	}
	if inspectVolumeTargetStates(root, volumePlan.targets) != "postimage" {
		return writeMessage("entry.volume.recovery_required"), false, nil
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return writeMessage("entry.volume.guard_changed_after_write", guardID), false, nil
	}

	complete, note := advanceCognitionVolumeBaseline(root, baselineState, plan)
	if !complete {
		return note, false, nil
	}
	if volumePlan.recovery != nil {
		if _, err := saveVolumeGovernanceReceipt(root, plan, volumePlan.recovery); err != nil {
			return mcpMessage("entries.governance_receipt.persist_failed", localeSafeWriteDetail(err.Error())), false, nil
		}
	}
	ledgerEvent := ledger.Event{Op: "update_entries_batch", PathsCount: len(plan.rels), DurationMs: time.Since(plan.start).Milliseconds(), Source: source,
		Result: ledger.ResultOK, AppliedCount: len(plan.rels), RecoveryTransactionID: plan.batchKey}
	if err := ensureVolumeLedger(root, plan.rc.cfg.LedgerEnabled, ledgerEvent); err != nil {
		return "entries_ledger_persist_failed", false, nil
	}
	if !retainRecovery && volumePlan.recovery != nil {
		if _, _, err := archiveAtomicBatchRecoveryByKey(root, plan.batchKey); err != nil {
			return writeMessage("entry.batch.recovery_cleanup_completed_failed", localeSafeWriteDetail(err.Error())), false, nil
		}
	}
	return note, true, nil
}

func validateVolumePlanPrewrite(root string, volumePlan *cognitionVolumeBatchPlan) *Fail {
	if volumePlan.codeReceipt != nil {
		recoveredPostimage := volumePlan.recovery != nil && volumePlan.recovery.Version == 4 &&
			volumePlan.recovery.CodeBatchID == volumePlan.codeReceipt.BatchID &&
			inspectVolumeTargetStates(root, volumePlan.targets) == "postimage"
		if !recoveredPostimage {
			cfg, err := config.Load(root)
			if err != nil {
				return &Fail{Code: errWriteConflict, Msg: "code_candidate_configuration_changed"}
			}
			set, loadErr := cognition.Load(root, cfg.IndexPath)
			if loadErr != nil {
				return &Fail{Code: errWriteConflict, Msg: "code_candidate_cognition_changed"}
			}
			facts, factsErr := volumegovernance.Assess(root, cfg, set)
			if factsErr != nil || facts.CompositeIdentity != volumePlan.codeReceipt.CompositeIdentity ||
				facts.ManagedScope.PolicyIdentity != volumePlan.codeReceipt.ScopePolicyIdentity ||
				facts.Code.Path != volumePlan.codeReceipt.CodeVolumePath || facts.Code.SHA256 != volumePlan.codeReceipt.CodeVolumeSHA256 {
				return &Fail{Code: errWriteConflict, Msg: "code_candidate_receipt_stale", Hint: writeMessage("entry.batch.hint.replan")}
			}
		}
	}
	if volumePlan.databaseReceipt != nil {
		cfg, err := config.Load(root)
		if err != nil {
			return &Fail{Code: errWriteConflict, Msg: "database_candidate_configuration_changed"}
		}
		submissions := make([]dbcognition.Submission, 0, len(volumePlan.databaseReceipt.Targets))
		for _, target := range volumePlan.databaseReceipt.Targets {
			submissions = append(submissions, dbcognition.Submission{ObjectRef: target.ObjectRef, CandidateID: target.CandidateID})
		}
		current, validateErr := dbcognition.ValidateSubmission(root, cfg.DatabaseSources, volumePlan.databaseReceipt.BatchID, submissions)
		if validateErr != nil || current.DatabaseVolumePath != volumePlan.databaseReceipt.DatabaseVolumePath || current.DatabaseVolumeSHA256 != volumePlan.databaseReceipt.DatabaseVolumeSHA256 {
			return &Fail{Code: errWriteConflict, Msg: "database_candidate_receipt_stale", Hint: writeMessage("entry.batch.hint.replan")}
		}
	}
	for rel, expected := range volumePlan.sourceFingerprints {
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || current.SHA256 != expected.SHA256 {
			return &Fail{Code: errWriteConflict, Msg: writeMessage("entry.batch.prewrite_source_conflict", rel), Hint: writeMessage("entry.batch.hint.refresh_binding")}
		}
	}
	for _, target := range volumePlan.targets {
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(target.Path)))
		allowProvenPostimage := volumePlan.recovery != nil && current.SHA256 == target.PostSHA
		if err != nil || (current.SHA256 != target.PreSHA && !allowProvenPostimage) {
			return &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.target_conflict", target.VolumeID), Hint: writeMessage("entry.batch.hint.replan")}
		}
	}
	return nil
}

func inspectVolumeTargetStates(root string, targets []cognitionVolumeWriteTarget) string {
	pre, post := true, true
	for _, target := range targets {
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(target.Path)))
		if err != nil {
			return "unknown"
		}
		pre = pre && current.SHA256 == target.PreSHA
		post = post && current.SHA256 == target.PostSHA
		if current.SHA256 != target.PreSHA && current.SHA256 != target.PostSHA {
			return "unknown"
		}
	}
	if post {
		return "postimage"
	}
	if pre {
		return "preimage"
	}
	return "partial"
}

func volumeRecoveryBaselinePreimage(root string) (string, string, error) {
	path := filepath.Join(root, ".aoci", "baseline.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "absent", "", nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("baseline_preimage_unsafe")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	return "present", governanceBytesSHA256(data), nil
}

func recoveryGuardMismatch(root string, recovery *atomicBatchRecovery) string {
	if recovery == nil || recovery.Version != 4 {
		return ""
	}
	assets := map[string]atomicBatchRecoveryAsset{}
	for _, asset := range recovery.Assets {
		assets[asset.Path] = asset
	}
	for _, guard := range recovery.Guards {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(guard.Path)))
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return guard.VolumeID
		}
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(guard.Path)))
		if err != nil {
			return guard.VolumeID
		}
		if asset, written := assets[guard.Path]; written {
			if current.SHA256 != asset.PreSHA256 && current.SHA256 != asset.PostSHA256 {
				return guard.VolumeID
			}
			continue
		}
		if current.SHA256 != guard.SHA256 {
			return guard.VolumeID
		}
	}
	return ""
}

func advanceCognitionVolumeBaseline(root string, state *baseline.Baseline, plan *atomicBatchPlan) (bool, string) {
	for rel, fingerprint := range plan.volumePlan.sourceFingerprints {
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || current.SHA256 != fingerprint.SHA256 {
			return false, writeMessage("entry.batch.source_drift", rel)
		}
		baseline.UpdateOne(state, rel, fingerprint)
	}
	for _, rel := range plan.volumePlan.volumePaths {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return false, writeMessage("entry.batch.postimage_unconfirmed")
		}
		baseline.UpdateOne(state, rel, fingerprint)
	}
	for _, binding := range plan.volumePlan.databaseBindings {
		if err := baseline.UpdateDatabaseCognitionBinding(state, binding); err != nil {
			return false, "database_cognition_binding_invalid"
		}
	}
	if err := saveAtomicBaseline(root, state); err != nil {
		return false, writeMessage("entry.batch.baseline_save_failed", localeSafeWriteDetail(err.Error()))
	}
	if fail := validateVolumePlanPrewrite(root, plan.volumePlan); fail != nil {
		return false, writeMessage("entry.volume.recovery_required")
	}
	for _, target := range plan.volumePlan.targets {
		current, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(target.Path)))
		if err != nil || current.SHA256 != target.PostSHA {
			return false, writeMessage("entry.volume.recovery_required")
		}
	}
	return true, writeMessage("entry.volume.baseline_advanced", len(plan.rels), strings.Join(plan.changeEnvelope.WriteSet, ","))
}

func reconcileCognitionVolumeBatch(root, source string, plan *atomicBatchPlan, retainRecovery bool) (string, bool, *Fail) {
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.batch.reconcile_lock_failed", localeSafeWriteDetail(err.Error()))}
	}
	defer lock.Release()
	if guardID := recoveryGuardMismatch(root, plan.volumePlan.recovery); guardID != "" {
		return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.guard_stale", guardID)}
	}
	if guardID, _ := externalGuardMismatch(root, plan.changeEnvelope); guardID != "" {
		return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.volume.guard_stale", guardID)}
	}
	if inspectVolumeTargetStates(root, plan.volumePlan.targets) != "postimage" {
		return "", false, &Fail{Code: errWriteConflict, Msg: writeMessage("entry.batch.reconcile_postimage_changed")}
	}
	state, exists, err := baseline.Load(root)
	if err != nil {
		return "", false, &Fail{Code: errInternal, Msg: writeMessage("entry.batch.reconcile_baseline_read_failed", localeSafeWriteDetail(err.Error()))}
	}
	if !exists || state == nil {
		state = baseline.NewBaseline(nil)
	}
	complete, note := advanceCognitionVolumeBaseline(root, state, plan)
	if !complete {
		return note, false, nil
	}
	if plan.volumePlan.recovery != nil {
		if _, err := saveVolumeGovernanceReceipt(root, plan, plan.volumePlan.recovery); err != nil {
			return "", false, &Fail{Code: errInternal, Msg: mcpMessage("entries.recovery_receipt.governance_persist_failed", localeSafeWriteDetail(err.Error()))}
		}
	}
	ledgerEvent := ledger.Event{Op: "update_entries_batch_recover", PathsCount: len(plan.rels), DurationMs: time.Since(plan.start).Milliseconds(), Source: source,
		Result: ledger.ResultOK, RecoveredCount: len(plan.rels), DuplicateApplies: 1, RecoveryTransactionID: plan.batchKey}
	if err := ensureVolumeLedger(root, plan.rc.cfg.LedgerEnabled, ledgerEvent); err != nil {
		return "entries_ledger_persist_failed", false, nil
	}
	if plan.volumePlan.recovery != nil && !retainRecovery {
		if _, _, err := archiveAtomicBatchRecoveryByKey(root, plan.batchKey); err != nil {
			return writeMessage("entry.batch.recovery_cleanup_failed", localeSafeWriteDetail(err.Error())), false, nil
		}
	}
	return note, true, nil
}
