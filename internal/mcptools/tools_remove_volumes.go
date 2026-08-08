package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

var writeRemoveBaselineCAS = afs.AtomicWriteCAS

func planVolumeRemoveEntry(root, objectRef string, _ bool) (*removePlan, *Fail) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return nil, fail
	}
	if loaded.set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, &Fail{Code: errBadArgs, Msg: writeMessage("remove.path_invalid", "canonical Volume identity requires a Volumes layout")}
	}
	volumeID := cognition.ScopeDatabase
	if strings.HasPrefix(objectRef, "code:") {
		volumeID = cognition.ScopeCode
		path := strings.TrimPrefix(objectRef, "code:")
		if normalized, err := afs.NormalizeRelPath(path); err != nil || normalized != path {
			return nil, &Fail{Code: errPathUnsafe, Msg: writeMessage("remove.path_invalid", "invalid canonical Code identity")}
		}
	}
	asset := loaded.set.Volumes[volumeID]
	if asset == nil || asset.State != cognition.AssetPresent {
		return nil, &Fail{Code: errBadArgs, Msg: "remove_volume_unavailable"}
	}
	recovery, recoveryErr := loadRemoveRecovery(root, objectRef)
	if recoveryErr != nil && !os.IsNotExist(recoveryErr) {
		return nil, &Fail{Code: errInternal, Msg: writeMessage("remove.recovery_read_failed", localeSafeWriteDetail(recoveryErr.Error()))}
	}
	if recovery != nil {
		if !recovery.VolumeMode || recovery.ObjectRef != objectRef || recovery.VolumeID != volumeID ||
			recovery.VolumePath != asset.Descriptor.Path || recovery.VolumePostimage == "" {
			return nil, &Fail{Code: errWriteConflict, Msg: writeMessage("remove.recovery_invalid")}
		}
		plan := volumeRemovePlanFromRecovery(root, loaded, asset, recovery)
		if guardFail := validateVolumeRemoveGuards(root, plan, true); guardFail != nil {
			return nil, guardFail
		}
		current := asset.SHA256
		object := cognitionObjectByRef(asset, objectRef)
		switch current {
		case recovery.PreIndexSHA256:
			if object == nil || object.CanonicalLine != recovery.RemovedLine {
				return nil, &Fail{Code: errWriteConflict, Msg: writeMessage("remove.recovery_entry_reappeared")}
			}
			facts, orphanFail := proveVolumeOrphan(root, loaded, objectRef, volumeID)
			if orphanFail != nil {
				return nil, orphanFail
			}
			if recovery.OwnershipRepair {
				finding, proofFail := proveVolumeOwnershipRepair(loaded.set, facts, objectRef, volumeID)
				if proofFail != nil || finding == nil || finding.ExpectedOwner != recovery.PreservedOwner {
					if proofFail != nil {
						return nil, proofFail
					}
					return nil, &Fail{Code: errWriteConflict, Msg: "remove_ownership_repair_proof_changed"}
				}
			}
		case recovery.PostIndexSHA256:
			if object != nil {
				return nil, &Fail{Code: errWriteConflict, Msg: writeMessage("remove.recovery_postimage_drift")}
			}
			plan.alreadyApplied = true
		default:
			return nil, &Fail{Code: errWriteConflict, Msg: writeMessage("remove.recovery_postimage_drift")}
		}
		return plan, nil
	}

	object := cognitionObjectByRef(asset, objectRef)
	if object == nil {
		return nil, &Fail{Code: errBadArgs, Msg: writeMessage("remove.entry_missing", objectRef)}
	}
	facts, orphanFail := proveVolumeOrphan(root, loaded, objectRef, volumeID)
	if orphanFail != nil {
		return nil, orphanFail
	}
	ownershipFinding, ownershipFail := proveVolumeOwnershipRepair(loaded.set, facts, objectRef, volumeID)
	if ownershipFail != nil {
		return nil, ownershipFail
	}
	guardSet := []string{}
	if ownershipFinding != nil {
		// A machine-produced ownership finding proves that the R target remains
		// valid through the preserved formal owner. The ordinary orphan path
		// below deliberately retains its ResolveImpact relation rejection.
		guardSet = append(guardSet, cognition.OwnerRoot, cognition.OwnerMeta)
		guardSet = append(guardSet, loaded.set.DeclaredOrder...)
	} else {
		impact, impactErr := cognition.ResolveImpact(loaded.set, []cognition.ImpactCandidate{{
			Change: cognition.ImpactChangeDelete, ObjectRef: objectRef, OriginalCandidateIndex: 1,
		}})
		if impactErr != nil {
			return nil, &Fail{Code: errBadArgs, Msg: "remove_orphan_relation_still_valid", Hint: "remove_or_update_valid_relations_first"}
		}
		guardSet = impact.GuardSet
	}
	newText, removeErr := index.RemoveEntry(string(asset.Raw), object.CanonicalLine)
	if removeErr != nil {
		return nil, &Fail{Code: errWriteConflict, Msg: writeMessage("remove.transform_failed", localeSafeWriteDetail(removeErr.Error()))}
	}
	if findings := cognition.ValidateProjectedObjectVolume(loaded.set, volumeID, []byte(newText)); len(findings) != 0 {
		return nil, &Fail{Code: errCandidateInvalid, Msg: writeMessage("entry.volume.projected_invalid", findings[0].Code)}
	}

	baselineRaw, readErr := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if readErr != nil || loaded.bl == nil {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_volume_baseline_required"}
	}
	postFingerprint := asManagedIndexFingerprint(loaded.bl, baseline.HashBytes(asset.Descriptor.Path, []byte(newText)))
	baseline.UpdateOne(loaded.bl, asset.Descriptor.Path, postFingerprint)
	if volumeID == cognition.ScopeDatabase {
		if err := baseline.RemoveDatabaseCognitionBinding(loaded.bl, objectRef); err != nil {
			return nil, &Fail{Code: errWriteConflict, Msg: "remove_database_binding_required"}
		}
	}
	loaded.bl.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	baselinePost, marshalErr := baseline.MarshalExact(loaded.bl)
	if marshalErr != nil {
		return nil, &Fail{Code: errInternal, Msg: "remove_baseline_postimage_invalid"}
	}
	guardSHA := volumeRemoveGuardSHA(loaded.set, guardSet)
	evidenceIdentity, evidenceErr := savedEvidenceGuardIdentity(root, loaded.cfg, volumeID)
	if evidenceErr != nil {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_evidence_guard_unavailable"}
	}
	cfgCopy := *loaded.cfg
	cfgCopy.IndexPath = asset.Descriptor.Path
	rc := &repoCtx{cfg: &cfgCopy, paths: config.AOCIPaths(root, asset.Descriptor.Path),
		text: string(asset.Raw), doc: asset.Document, bl: loaded.bl}
	outcome := &RemoveOutcome{Rel: objectRef, RemovedLine: object.CanonicalLine}
	if ownershipFinding != nil {
		outcome.OwnershipRepair = true
		outcome.PreservedOwner = ownershipFinding.ExpectedOwner
	}
	return &removePlan{
		out: outcome, newText: newText,
		rc: rc, indexHash: asset.SHA256, orphanOnly: true, volumeMode: true,
		objectRef: objectRef, volumeID: volumeID, guardSHA256: guardSHA, evidenceIdentity: evidenceIdentity,
		baselinePreSHA256: indexTextHash(string(baselineRaw)), baselinePostSHA256: indexTextHash(string(baselinePost)),
		baselinePostimage: string(baselinePost),
	}, nil
}

func volumeRemovePlanFromRecovery(root string, loaded *cognitionRepoCtx, asset *cognition.Asset, recovery *removeRecovery) *removePlan {
	cfgCopy := *loaded.cfg
	cfgCopy.IndexPath = recovery.VolumePath
	return &removePlan{
		out: &RemoveOutcome{Rel: recovery.ObjectRef, RemovedLine: recovery.RemovedLine,
			OwnershipRepair: recovery.OwnershipRepair, PreservedOwner: recovery.PreservedOwner}, newText: recovery.VolumePostimage,
		rc:        &repoCtx{cfg: &cfgCopy, paths: config.AOCIPaths(root, recovery.VolumePath), text: string(asset.Raw), doc: asset.Document, bl: loaded.bl},
		indexHash: recovery.PreIndexSHA256, orphanOnly: true, volumeMode: true,
		objectRef: recovery.ObjectRef, volumeID: recovery.VolumeID, guardSHA256: recovery.GuardSHA256,
		evidenceIdentity: recovery.EvidenceIdentity, baselinePreSHA256: recovery.BaselinePreSHA256,
		baselinePostSHA256: recovery.BaselinePostSHA256, baselinePostimage: recovery.BaselinePostimage,
	}
}

func proveVolumeOrphan(root string, loaded *cognitionRepoCtx, objectRef, volumeID string) (*volumegovernance.Facts, *Fail) {
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_orphan_facts_unavailable"}
	}
	if volumeID == cognition.ScopeCode {
		path := strings.TrimPrefix(objectRef, "code:")
		for _, orphan := range facts.CodeDrift.Orphan {
			if orphan == path {
				return facts, nil
			}
		}
	} else {
		for _, item := range facts.DatabaseCognition.Items {
			if item.ObjectRef == objectRef && item.State == machinecontract.DatabaseCognitionOrphan {
				return facts, nil
			}
		}
	}
	return facts, &Fail{Code: errBadArgs, Msg: "remove_target_not_proven_orphan", Hint: "run_aoci_maintain_and_use_its_exact_orphan_candidate"}
}

// proveVolumeOwnershipRepair recognizes only the exact shared-governance
// finding for a misplaced Code Entry. Callers cannot request this exception:
// expected/actual ownership, the safe action, and the preserved formal owner
// all come from current machine facts and the loaded CognitionSet.
func proveVolumeOwnershipRepair(
	set *cognition.Set,
	facts *volumegovernance.Facts,
	objectRef, volumeID string,
) (*volumegovernance.Finding, *Fail) {
	if set == nil || facts == nil || volumeID != cognition.OwnerCode || !strings.HasPrefix(objectRef, "code:") {
		return nil, nil
	}
	path := strings.TrimPrefix(objectRef, "code:")
	var matched *volumegovernance.Finding
	for index := range facts.Findings {
		finding := &facts.Findings[index]
		if finding.Cause != "volume_ownership_conflict" || finding.Domain != volumeID || finding.Target != path {
			continue
		}
		if matched != nil {
			return nil, &Fail{Code: errWriteConflict, Msg: "remove_ownership_repair_proof_ambiguous"}
		}
		matched = finding
	}
	if matched == nil {
		return nil, nil
	}
	if facts.RecoveryPending || facts.ThirdPartyConflict || !facts.StructureValid ||
		matched.Code != "code_orphan" || matched.AffectedPath != path ||
		matched.SafeRepairAction != "aoci_remove_entry path="+objectRef ||
		matched.ExpectedOwner == "" || matched.ExpectedOwner == matched.ActualOwner ||
		matched.ActualOwner != volumeID || matched.ExpectedOwner != cognition.ExpectedOwner(objectRef) ||
		!formalOwnerPresent(set, matched.ExpectedOwner, path) {
		return nil, &Fail{Code: errWriteConflict, Msg: "remove_ownership_repair_proof_invalid"}
	}
	return matched, nil
}

func formalOwnerPresent(set *cognition.Set, owner, path string) bool {
	var asset *cognition.Asset
	switch owner {
	case cognition.OwnerRoot:
		asset = &set.Root
	case cognition.OwnerMeta:
		asset = &set.Meta
	case cognition.OwnerCode, cognition.OwnerDatabase:
		asset = set.Volumes[owner]
	default:
		return false
	}
	return asset != nil && asset.State == cognition.AssetPresent && filepath.ToSlash(asset.Descriptor.Path) == path
}

func volumeRemoveGuardSHA(set *cognition.Set, guards []string) map[string]string {
	result := map[string]string{}
	for _, guard := range guards {
		switch guard {
		case "root":
			result[guard] = set.Root.SHA256
		case "meta":
			result[guard] = set.Meta.SHA256
		default:
			if asset := set.Volumes[guard]; asset != nil {
				result[guard] = asset.SHA256
			}
		}
	}
	return result
}

func savedEvidenceGuardIdentity(root string, cfg *config.Config, volumeID string) (string, error) {
	if volumeID != cognition.ScopeDatabase {
		return "not_applicable", nil
	}
	accepted, exists, err := dbevidence.LoadBaseline(root)
	if err != nil || !exists {
		return "", fmt.Errorf("accepted evidence unavailable")
	}
	type sourceGuard struct {
		SourceID string `json:"source_id"`
		SHA256   string `json:"snapshot_sha256"`
	}
	sources := []sourceGuard{}
	for _, source := range cfg.DatabaseSources {
		if !source.Enabled {
			continue
		}
		_, snapshot, snapshotExists, snapshotErr := dbevidence.LoadSnapshot(root, source.SourceID)
		if snapshotErr != nil || !snapshotExists {
			return "", fmt.Errorf("saved evidence unavailable")
		}
		sources = append(sources, sourceGuard{SourceID: source.SourceID, SHA256: snapshot.SourceSnapshotSHA256})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].SourceID < sources[j].SourceID })
	data, err := json.Marshal(struct {
		Baseline dbevidence.Baseline `json:"baseline"`
		Sources  []sourceGuard       `json:"sources"`
	}{Baseline: accepted, Sources: sources})
	if err != nil {
		return "", err
	}
	return indexTextHash(string(data)), nil
}

func validateVolumeRemoveGuards(root string, plan *removePlan, allowTargetPostimage bool) *Fail {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return &Fail{Code: errWriteConflict, Msg: "remove_volume_configuration_guard_changed"}
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		return &Fail{Code: errWriteConflict, Msg: "remove_volume_cognition_guard_unavailable"}
	}
	for guard, expected := range plan.guardSHA256 {
		actual := ""
		switch guard {
		case "root":
			actual = set.Root.SHA256
		case "meta":
			actual = set.Meta.SHA256
		default:
			if asset := set.Volumes[guard]; asset != nil {
				actual = asset.SHA256
			}
		}
		if guard == plan.volumeID && allowTargetPostimage && actual == indexTextHash(plan.newText) {
			continue
		}
		if actual != expected {
			return &Fail{Code: errWriteConflict, Msg: "remove_volume_guard_changed: " + guard}
		}
	}
	evidenceIdentity, evidenceErr := savedEvidenceGuardIdentity(root, cfg, plan.volumeID)
	if evidenceErr != nil || evidenceIdentity != plan.evidenceIdentity {
		return &Fail{Code: errWriteConflict, Msg: "remove_evidence_guard_changed"}
	}
	return nil
}

func commitVolumeRemove(root, source string, plan *removePlan) *Fail {
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return &Fail{Code: errWriteConflict, Msg: writeMessage("remove.lock_failed", localeSafeWriteDetail(err.Error()))}
	}
	defer lock.Release()
	if transactionFail := pendingHeaderTransactionFail(root); transactionFail != nil {
		return transactionFail
	}
	if guardFail := validateVolumeRemoveGuards(root, plan, true); guardFail != nil {
		return guardFail
	}
	recovery := removeRecovery{
		Version: 1, Rel: plan.objectRef, RemovedLine: plan.out.RemovedLine,
		PreIndexSHA256: plan.indexHash, PostIndexSHA256: indexTextHash(plan.newText), VolumeMode: true,
		ObjectRef: plan.objectRef, VolumeID: plan.volumeID, VolumePath: plan.rc.cfg.IndexPath,
		VolumePostimage: plan.newText, GuardSHA256: plan.guardSHA256, EvidenceIdentity: plan.evidenceIdentity,
		BaselinePreSHA256: plan.baselinePreSHA256, BaselinePostSHA256: plan.baselinePostSHA256,
		BaselinePostimage: plan.baselinePostimage, OwnershipRepair: plan.out.OwnershipRepair,
		PreservedOwner: plan.out.PreservedOwner,
	}
	if err := saveRemoveRecovery(root, recovery); err != nil {
		return &Fail{Code: errInternal, Msg: writeMessage("remove.recovery_save_failed", localeSafeWriteDetail(err.Error()))}
	}
	volumeRaw, readErr := os.ReadFile(plan.rc.paths.IndexPath)
	if readErr != nil {
		return &Fail{Code: errInternal, Msg: "remove_volume_read_failed"}
	}
	volumeSHA := indexTextHash(string(volumeRaw))
	if volumeSHA == recovery.PreIndexSHA256 {
		if err := writeRemoveIndex(plan.rc.paths.IndexPath, []byte(recovery.VolumePostimage), recovery.PreIndexSHA256); err != nil {
			_ = removeRecoveryFile(removeRecoveryPath(root, plan.objectRef))
			return &Fail{Code: errWriteConflict, Msg: writeMessage("remove.index_write_failed", localeSafeWriteDetail(err.Error()))}
		}
	} else if volumeSHA != recovery.PostIndexSHA256 {
		return &Fail{Code: errWriteConflict, Msg: "remove_volume_third_party_conflict"}
	}
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineRaw, baselineErr := os.ReadFile(baselinePath)
	if baselineErr != nil {
		return &Fail{Code: errInternal, Msg: "remove_baseline_read_failed"}
	}
	baselineSHA := indexTextHash(string(baselineRaw))
	if baselineSHA == recovery.BaselinePreSHA256 {
		if err := writeRemoveBaselineCAS(baselinePath, []byte(recovery.BaselinePostimage), recovery.BaselinePreSHA256); err != nil {
			return &Fail{Code: errWriteConflict, Msg: "remove_baseline_cas_failed"}
		}
	} else if baselineSHA != recovery.BaselinePostSHA256 {
		return &Fail{Code: errWriteConflict, Msg: "remove_baseline_third_party_conflict"}
	}
	set, loadErr := cognition.Load(root, plan.rc.cfg.IndexPath)
	if loadErr != nil || cognitionObjectByRef(set.Volumes[plan.volumeID], plan.objectRef) != nil {
		return &Fail{Code: errInternal, Msg: "remove_volume_postimage_unconfirmed"}
	}
	if plan.out.OwnershipRepair {
		if guardFail := validateVolumeRemoveGuards(root, plan, true); guardFail != nil {
			return guardFail
		}
	}
	transactionID := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(removeRecoveryPath(root, plan.objectRef)), "remove-"), ".json")
	event := ledger.Event{Op: "remove_entry", Source: source, Result: ledger.ResultOK, AppliedCount: 1,
		RecoveryTransactionID: transactionID, PreIndexSHA256: recovery.PreIndexSHA256,
		PostIndexSHA256: recovery.PostIndexSHA256, IndexSHA256: recovery.PostIndexSHA256,
		BaselineSHA256: recovery.BaselinePostSHA256}
	if err := cognitiontxn.EnsureLedger(root, plan.rc.cfg.LedgerEnabled, event); err != nil {
		return &Fail{Code: errInternal, Msg: "remove_ledger_completion_failed"}
	}
	if err := removeRecoveryFile(removeRecoveryPath(root, plan.objectRef)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &Fail{Code: errInternal, Msg: "remove_recovery_cleanup_failed"}
	}
	return nil
}
