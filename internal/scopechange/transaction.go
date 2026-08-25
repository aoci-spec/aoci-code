package scopechange

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

var transactionFault = func(string) error { return nil }

func NewApproval(preview *Preview, actor, approvedAt string) (*Approval, error) {
	if preview == nil || !cognitiontxn.ValidAuditActor(actor) {
		return nil, fmt.Errorf("managed_scope_approval_actor_invalid")
	}
	parsed, err := time.Parse(time.RFC3339, approvedAt)
	if err != nil || parsed.Location() != time.UTC {
		return nil, fmt.Errorf("managed_scope_approval_timestamp_invalid")
	}
	approval := &Approval{Version: machinecontract.ManagedScopeChangeApprovalV2,
		EnvelopeDigest: preview.EnvelopeDigest, ApprovedWriteSet: append([]string{}, preview.Plan.WriteSet...),
		ApprovedRecoveryPolicy: preview.Plan.RecoveryDirection, Actor: actor,
		Mechanism: machinecontract.ApprovalMechanismInteractiveDigestConfirmation, ApprovedAt: approvedAt}
	approval.ApprovalDigest, err = approvalIdentity(*approval)
	return approval, err
}

func DecodeApproval(data []byte) (*Approval, error) {
	var value Approval
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Version != machinecontract.ManagedScopeChangeApprovalV2 {
		return nil, fmt.Errorf("managed_scope_approval_invalid")
	}
	want, err := approvalIdentity(value)
	if err != nil || want != value.ApprovalDigest || !cognitiontxn.ValidAuditActor(value.Actor) {
		return nil, fmt.Errorf("managed_scope_approval_identity_invalid")
	}
	return &value, nil
}

// applyAuthorizationBranch selects the mechanism that actually authorizes this
// transaction.
//
// A posture relaxation is planned under the weaker desired mode but has to be
// authorized under the stronger receipted one, or the change would ratify
// itself. plan.InteractionRequired already carries that decision, so an auto
// plan that still demands interaction is routed to the interactive branch
// instead of the policy-bound one. Every other plan keeps its declared mode.
func applyAuthorizationBranch(effectiveMode string, interactionRequired bool) string {
	if effectiveMode == machinecontract.ApplyAuthorizationAuto && interactionRequired {
		return machinecontract.ApplyAuthorizationReview
	}
	return effectiveMode
}

func approvalIdentity(value Approval) (string, error) {
	value.ApprovalDigest = ""
	return digestJSON(value)
}

func validateApproval(preview *Preview, approval *Approval) error {
	if !preview.Plan.InteractionRequired {
		return nil
	}
	if approval == nil || approval.Version != machinecontract.ManagedScopeChangeApprovalV2 ||
		approval.EnvelopeDigest != preview.EnvelopeDigest || approval.Mechanism != machinecontract.ApprovalMechanismInteractiveDigestConfirmation ||
		approval.ApprovedRecoveryPolicy != preview.Plan.RecoveryDirection ||
		!equalStrings(approval.ApprovedWriteSet, preview.Plan.WriteSet) || !cognitiontxn.ValidAuditActor(approval.Actor) {
		return fmt.Errorf("managed_scope_human_approval_required")
	}
	want, err := approvalIdentity(*approval)
	if err != nil || want != approval.ApprovalDigest {
		return fmt.Errorf("managed_scope_approval_digest_mismatch")
	}
	return nil
}

func Apply(repositoryRoot string, preview *Preview, approval *Approval) (*Result, error) {
	return ApplyAuthorized(repositoryRoot, preview, approval, nil)
}

// ApplyAuthorized selects exactly one authorization mechanism from the bound
// team policy, then publishes through the shared CAS/recovery transaction.
func ApplyAuthorized(repositoryRoot string, preview *Preview, approval *Approval, policyBound *PolicyBoundApproval) (*Result, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || preview == nil {
		return nil, fmt.Errorf("managed_scope_apply_input_invalid")
	}
	if err := validatePreview(preview); err != nil {
		return nil, err
	}
	transactionID := preview.EnvelopeDigest[:24]
	if archived, archiveErr := loadIntentAt(archivePath(root, transactionID), transactionID); archiveErr == nil {
		status, inspectErr := inspectIntent(root, archived, false)
		if inspectErr != nil || status.ThirdPartyConflict || status.State != "complete" {
			return nil, fmt.Errorf("managed_scope_archived_transaction_conflict")
		}
		return resultFor(archived, "already_applied", nil), nil
	}
	activeIntentExists := false
	if _, loadErr := loadIntentAt(intentPath(root, transactionID), transactionID); loadErr == nil {
		activeIntentExists = true
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return nil, loadErr
	}
	if activeIntentExists {
		lock, lockErr := afs.AcquireIndexLock(root)
		if lockErr != nil {
			return nil, fmt.Errorf("managed_scope_lock_failed")
		}
		defer lock.Release()
		if err := cognitiontxn.RejectOtherPending(root, intentFilename(transactionID)); err != nil {
			return nil, err
		}
		active, loadErr := loadIntentAt(intentPath(root, transactionID), transactionID)
		if loadErr != nil {
			return nil, fmt.Errorf("managed_scope_recovery_not_found")
		}
		return advance(root, active)
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
	}
	authorizationPolicy, authorizationIdentity, err := resolveApplyAuthorizationPolicy(cfg)
	if err != nil {
		return nil, err
	}
	if err := validateAuthorizationPolicyBinding(preview, authorizationPolicy, authorizationIdentity); err != nil {
		return nil, err
	}
	switch applyAuthorizationBranch(authorizationPolicy.EffectiveMode, preview.Plan.InteractionRequired) {
	case machinecontract.ApplyAuthorizationAuto:
		if approval != nil {
			return nil, fmt.Errorf("managed_scope_human_approval_not_authoritative_in_auto")
		}
		if policyBound == nil {
			policyBound, err = NewPolicyBoundApproval(root, preview, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339))
			if err != nil {
				return nil, err
			}
		}
		if err := validatePolicyBoundApprovalLive(root, preview, policyBound); err != nil {
			return nil, err
		}
	case machinecontract.ApplyAuthorizationReview, machinecontract.ApplyAuthorizationLegacy:
		if policyBound != nil {
			return nil, fmt.Errorf("managed_scope_policy_bound_approval_not_authoritative")
		}
		if err := validateApproval(preview, approval); err != nil {
			return nil, err
		}
	case machinecontract.ApplyAuthorizationOff:
		return nil, fmt.Errorf("managed_scope_apply_authorization_blocked: automation_off")
	default:
		return nil, fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_lock_failed")
	}
	defer lock.Release()
	if archived, archiveErr := loadIntentAt(archivePath(root, transactionID), transactionID); archiveErr == nil {
		status, inspectErr := inspectIntent(root, archived, false)
		if inspectErr != nil || status.ThirdPartyConflict || status.State != "complete" {
			return nil, fmt.Errorf("managed_scope_archived_transaction_conflict")
		}
		return resultFor(archived, "already_applied", nil), nil
	}
	if err := cognitiontxn.RejectOtherPending(root, intentFilename(transactionID)); err != nil {
		return nil, err
	}
	if active, loadErr := loadIntentAt(intentPath(root, transactionID), transactionID); loadErr == nil {
		return advance(root, active)
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return nil, loadErr
	}
	rebuilt, err := Build(root, preview.Plan.PreparedAt, preview.CandidateSet)
	if err != nil || rebuilt.EnvelopeDigest != preview.EnvelopeDigest {
		return nil, fmt.Errorf("managed_scope_replay_mismatch")
	}
	if err := classifyPreviewPreimages(root, preview); err != nil {
		return nil, err
	}
	if policyBound != nil {
		if err := validatePolicyBoundApprovalLive(root, preview, policyBound); err != nil {
			return nil, err
		}
	}
	if err := cognitiontxn.EnsureSafeDirectory(root, ".aoci/transactions/history"); err != nil {
		return nil, fmt.Errorf("managed_scope_runtime_boundary_invalid: %w", err)
	}
	preimages, err := capturePreimages(root, preview)
	if err != nil {
		return nil, err
	}
	posts := []cognitiontxn.Postimage{}
	for _, image := range formalImages(preview) {
		posts = append(posts, cognitiontxn.Postimage{Path: image.Path, SHA: image.PostimageSHA256, Data: image.PostimageBytes})
	}
	staging, err := cognitiontxn.Stage(root, Operation, transactionID, posts, transactionFault)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_staging_failed: %w", err)
	}
	createdAt := preview.Plan.PreparedAt
	if approval != nil {
		createdAt = approval.ApprovedAt
	} else if policyBound != nil {
		createdAt = policyBound.CreatedAt
	}
	intent := &RecoveryIntent{Version: machinecontract.ManagedScopeRecoveryV2, Operation: Operation,
		TransactionID: transactionID, Preview: *preview, Approval: approval, PolicyBoundApproval: policyBound, Staging: staging,
		Preimages: preimages, CreatedAt: createdAt}
	intent.RecoveryDigest, err = recoveryIdentity(*intent)
	if err != nil {
		return nil, err
	}
	intentBytes, err := Encode(intent)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.SaveImmutable(intentPath(root, transactionID), intentBytes); err != nil {
		return nil, fmt.Errorf("managed_scope_recovery_intent_failed: %w", err)
	}
	if err := transactionFault("after_intent"); err != nil {
		return nil, err
	}
	return advance(root, intent)
}

func Resume(repositoryRoot, transactionID string) (*Result, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || !validTransactionID(transactionID) {
		return nil, fmt.Errorf("managed_scope_transaction_invalid")
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_lock_failed")
	}
	defer lock.Release()
	if err := cognitiontxn.RejectOtherPending(root, intentFilename(transactionID)); err != nil {
		return nil, err
	}
	intent, err := loadIntentAt(intentPath(root, transactionID), transactionID)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_recovery_not_found")
	}
	return advance(root, intent)
}

func advance(root string, intent *RecoveryIntent) (*Result, error) {
	status, err := inspectIntent(root, intent, true)
	if err != nil {
		return nil, err
	}
	if status.ThirdPartyConflict {
		return nil, fmt.Errorf("managed_scope_recovery_conflict")
	}
	written := []string{}
	for _, image := range formalImages(&intent.Preview) {
		target := targetStatus(status, image.Path)
		if target.State == cognitiontxn.StatePostimage {
			continue
		}
		if target.State != cognitiontxn.StatePreimage {
			return nil, fmt.Errorf("managed_scope_target_conflict: %s", image.Path)
		}
		if image.Path == ".aoci/baseline.json" {
			if err := validateSourceGuards(root, intent); err != nil {
				return nil, err
			}
		}
		if err := transactionFault("before_publish_" + safeFaultName(image.Path)); err != nil {
			return nil, err
		}
		data, err := cognitiontxn.ReadStaged(root, intent.Staging, image.Path)
		if err != nil {
			return nil, err
		}
		if err := afs.AtomicWriteCAS(filepath.Join(root, filepath.FromSlash(image.Path)), data, image.PreimageSHA256); err != nil {
			return nil, fmt.Errorf("managed_scope_publish_failed[%s]: %w", image.Path, err)
		}
		written = append(written, image.Path)
		if err := transactionFault("after_publish_" + safeFaultName(image.Path)); err != nil {
			return nil, err
		}
		status, err = inspectIntent(root, intent, true)
		if err != nil || status.ThirdPartyConflict {
			return nil, fmt.Errorf("managed_scope_post_publish_conflict: %s", image.Path)
		}
	}
	if err := internalVerify(root, intent); err != nil {
		return nil, fmt.Errorf("managed_scope_internal_verify_failed: %w", err)
	}
	result := resultFor(intent, "applied", written)
	if err := saveResult(root, intent.TransactionID, result); err != nil {
		return nil, err
	}
	if err := transactionFault("before_ledger"); err != nil {
		return nil, err
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.EnsureLedger(root, cfg.LedgerEnabled, ledger.Event{Op: "managed_scope_change_apply",
		Source: authorizationSource(intent), Result: ledger.ResultOK, AppliedCount: formalMutationCount(&intent.Preview),
		RecoveryTransactionID: intent.TransactionID, PreIndexSHA256: intent.Preview.IndexPostimage.PreimageSHA256,
		PostIndexSHA256: intent.Preview.IndexPostimage.PostimageSHA256, IndexSHA256: intent.Preview.IndexPostimage.PostimageSHA256,
		BaselineSHA256: intent.Preview.BaselinePostimage.PostimageSHA256}); err != nil {
		return nil, fmt.Errorf("managed_scope_ledger_failed: %w", err)
	}
	if err := transactionFault("before_archive"); err != nil {
		return nil, err
	}
	intentBytes, _ := Encode(intent)
	if err := cognitiontxn.ArchiveImmutable(intentPath(root, intent.TransactionID), archivePath(root, intent.TransactionID), intentBytes); err != nil {
		return nil, fmt.Errorf("managed_scope_archive_failed: %w", err)
	}
	result.RecoveryAvailable = false
	return result, nil
}

func formalMutationCount(preview *Preview) int {
	count := 0
	for _, image := range formalImages(preview) {
		if image.PreimageSHA256 != image.PostimageSHA256 {
			count++
		}
	}
	return count
}

func authorizationSource(intent *RecoveryIntent) string {
	if intent != nil && intent.PolicyBoundApproval != nil {
		return ledger.SourcePolicy
	}
	return ledger.SourceHuman
}

func Rollback(repositoryRoot, transactionID string) (*Result, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || !validTransactionID(transactionID) {
		return nil, fmt.Errorf("managed_scope_transaction_invalid")
	}
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_lock_failed")
	}
	defer lock.Release()
	if err := cognitiontxn.RejectOtherPending(root, intentFilename(transactionID)); err != nil {
		return nil, err
	}
	intent, err := loadIntentAt(intentPath(root, transactionID), transactionID)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_recovery_not_found")
	}
	status, err := inspectIntent(root, intent, true)
	if err != nil || status.ThirdPartyConflict {
		return nil, fmt.Errorf("managed_scope_rollback_conflict")
	}
	recovered := []string{}
	images := formalImages(&intent.Preview)
	for index := len(images) - 1; index >= 0; index-- {
		image := images[index]
		target := targetStatus(status, image.Path)
		if image.PreimageSHA256 == image.PostimageSHA256 || target.State == cognitiontxn.StatePreimage {
			continue
		}
		if target.State != cognitiontxn.StatePostimage {
			return nil, fmt.Errorf("managed_scope_rollback_conflict: %s", image.Path)
		}
		preimage, ok := preimageFor(intent.Preimages, image.Path)
		if !ok || digestBytes(preimage.PostimageBytes) != image.PreimageSHA256 {
			return nil, fmt.Errorf("managed_scope_rollback_preimage_missing: %s", image.Path)
		}
		if err := afs.AtomicWriteCAS(filepath.Join(root, filepath.FromSlash(image.Path)), preimage.PostimageBytes, image.PostimageSHA256); err != nil {
			return nil, fmt.Errorf("managed_scope_rollback_failed[%s]: %w", image.Path, err)
		}
		recovered = append(recovered, image.Path)
		status, err = inspectIntent(root, intent, true)
		if err != nil || status.ThirdPartyConflict {
			return nil, fmt.Errorf("managed_scope_rollback_postwrite_conflict")
		}
	}
	result := resultFor(intent, "rolled_back", nil)
	result.RecoveredPaths = recovered
	result.IndexSHA256 = intent.Preview.IndexPostimage.PreimageSHA256
	result.BaselineSHA256 = intent.Preview.BaselinePostimage.PreimageSHA256
	result.RecoveryAvailable = false
	if err := saveResult(root, intent.TransactionID, result); err != nil {
		return nil, err
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, err
	}
	if err := cognitiontxn.EnsureLedger(root, cfg.LedgerEnabled, ledger.Event{Op: "managed_scope_change_rollback",
		Source: authorizationSource(intent), Result: ledger.ResultOK, RecoveredCount: len(recovered), RecoveryTransactionID: intent.TransactionID,
		IndexSHA256: intent.Preview.IndexPostimage.PreimageSHA256, BaselineSHA256: intent.Preview.BaselinePostimage.PreimageSHA256}); err != nil {
		return nil, err
	}
	intentBytes, _ := Encode(intent)
	if err := cognitiontxn.ArchiveImmutable(intentPath(root, transactionID), archivePath(root, transactionID), intentBytes); err != nil {
		return nil, err
	}
	return result, nil
}

func Inspect(repositoryRoot, transactionID string) (*Status, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || !validTransactionID(transactionID) {
		return nil, fmt.Errorf("managed_scope_transaction_invalid")
	}
	intent, err := loadIntentAt(intentPath(root, transactionID), transactionID)
	active := true
	if errors.Is(err, os.ErrNotExist) {
		intent, err = loadIntentAt(archivePath(root, transactionID), transactionID)
		active = false
	}
	if err != nil {
		return nil, fmt.Errorf("managed_scope_transaction_not_found")
	}
	return inspectIntent(root, intent, active)
}

func inspectIntent(root string, intent *RecoveryIntent, active bool) (*Status, error) {
	result := &Status{Version: machinecontract.ManagedScopeChangeStatusV2, TransactionID: intent.TransactionID,
		Targets: []TargetStatus{}, RecoveryAvailable: active, RollbackAvailable: active}
	pre, post, unknown := 0, 0, 0
	for _, image := range formalImages(&intent.Preview) {
		state, actual, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(image.Path)), image.PreimageSHA256, image.PostimageSHA256, false)
		if err != nil {
			return nil, err
		}
		result.Targets = append(result.Targets, TargetStatus{Path: image.Path, State: state, ActualSHA256: actual})
		switch state {
		case cognitiontxn.StatePreimage:
			pre++
		case cognitiontxn.StatePostimage:
			post++
		default:
			unknown++
		}
	}
	switch {
	case unknown > 0:
		result.State, result.ThirdPartyConflict = "conflict", true
	case post == len(result.Targets):
		result.State = "complete"
	case pre == len(result.Targets):
		result.State = "preimage"
	default:
		result.State = "partial"
	}
	if !active {
		result.RecoveryAvailable, result.RollbackAvailable = false, false
	}
	return result, nil
}

func validatePreview(preview *Preview) error {
	want, err := previewIdentity(*preview)
	if err != nil || want != preview.PreviewID || want != preview.EnvelopeDigest || preview.Version != machinecontract.ManagedScopeChangePreviewV2 {
		return fmt.Errorf("managed_scope_preview_identity_invalid")
	}
	if preview.EnvelopeVersion != machinecontract.ManagedScopeChangeEnvelopeV2 {
		return fmt.Errorf("managed_scope_envelope_version_invalid")
	}
	for _, image := range formalImages(preview) {
		if digestBytes(image.PostimageBytes) != image.PostimageSHA256 {
			return fmt.Errorf("managed_scope_postimage_identity_invalid: %s", image.Path)
		}
	}
	return nil
}

func classifyPreviewPreimages(root string, preview *Preview) error {
	for _, image := range formalImages(preview) {
		state, _, err := cognitiontxn.Classify(filepath.Join(root, filepath.FromSlash(image.Path)), image.PreimageSHA256, image.PostimageSHA256, false)
		if err != nil || (state != cognitiontxn.StatePreimage && !(image.PreimageSHA256 == image.PostimageSHA256 && state == cognitiontxn.StatePostimage)) {
			return fmt.Errorf("managed_scope_third_party_conflict: %s", image.Path)
		}
	}
	return nil
}

func capturePreimages(root string, preview *Preview) ([]FormalImage, error) {
	result := []FormalImage{}
	for _, image := range formalImages(preview) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(image.Path)))
		if err != nil || digestBytes(data) != image.PreimageSHA256 {
			return nil, fmt.Errorf("managed_scope_preimage_capture_failed: %s", image.Path)
		}
		result = append(result, FormalImage{Path: image.Path, PreimageState: "present", PreimageSHA256: image.PreimageSHA256,
			PostimageSHA256: image.PreimageSHA256, PostimageBytes: data})
	}
	return result, nil
}

func validateSourceGuards(root string, intent *RecoveryIntent) error {
	return validateSourceGuardsWithCurationBasis(root, intent, false)
}

// replayPlanTimeCuration=true 表示调用发生在 postimage 已经发布之后。此时被
// Curation 排除的路径已不在磁盘上的 Baseline 里,按当前 Baseline 重算会得到
// 另一份排除集合与评估身份,把一笔已经完成的事务判成陈旧、且再也无法归档
// (审查修正)。发布后必须回放信封里的计划时事实。
func validateSourceGuardsWithCurationBasis(root string, intent *RecoveryIntent, replayPlanTimeCuration bool) error {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return fmt.Errorf("managed_scope_source_guard_config_invalid")
	}
	curationDocument, _, _, err := curation.Load(root)
	if err != nil {
		return fmt.Errorf("managed_scope_source_guard_curation_invalid")
	}
	activeBaseline, exists, err := baseline.Load(root)
	if err != nil || !exists {
		return fmt.Errorf("managed_scope_source_guard_baseline_invalid")
	}
	// Evaluation identity includes configured future-path Curation exclusions,
	// even when those paths are absent from the current inventory. Reconstruct
	// the exact Build input instead of projecting only exclusions that happened
	// to produce a PathEvaluation.
	curationExclude := activeCurationExclusions(cfg.CurationExclude, curationDocument, activeBaseline)
	if replayPlanTimeCuration && intent.Preview.CurationExclusions != nil {
		curationExclude = append([]string{}, intent.Preview.CurationExclusions...)
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
		WalkOptions: cfg.WalkOptions(), CurationExclude: curationExclude})
	if err != nil || evaluation.PolicyIdentity != intent.Preview.Evaluation.PolicyIdentity {
		return fmt.Errorf("managed_scope_source_guard_inventory_changed")
	}
	currentSnapshot, err := managedscope.Snapshot(root, evaluation, managedscope.SnapshotOptions{
		HighRiskContentApproved: len(cfg.SafeInventoryHighRiskOptIn) == 0 || intent.Preview.CandidateSet.SafetyApproval != nil})
	if err != nil {
		return fmt.Errorf("managed_scope_source_guard_snapshot_changed")
	}
	guardBaseline := activeBaseline
	indexPath := intent.Preview.IndexPostimage.Path
	if current, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(indexPath))); hashErr == nil && current.SHA256 == intent.Preview.IndexPostimage.PostimageSHA256 {
		projected := *activeBaseline
		projected.Files = cloneFingerprints(activeBaseline.Files)
		current.Role = machinecontract.ScopeRoleIndex
		projected.Files[indexPath] = current
		guardBaseline = &projected
	}
	formalVolumeGuards, err := FormalCognitionBaselineGuards(root, cfg.IndexPath, guardBaseline)
	if err != nil {
		return fmt.Errorf("managed_scope_source_guard_snapshot_changed")
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		return fmt.Errorf("managed_scope_source_guard_snapshot_changed")
	}
	formalAssetGuards, _ := formalCognitionLiveGuards(set)
	for path := range formalAssetGuards {
		delete(currentSnapshot, path)
	}
	for path, fingerprint := range formalAssetGuards {
		currentSnapshot[path] = fingerprint
	}
	for path, fingerprint := range formalVolumeGuards {
		currentSnapshot[path] = fingerprint
	}
	expected := cloneFingerprints(intent.Preview.SourceGuard)
	if len(expected) == 0 {
		// Recovery intents written before source_guard was envelope-bound retain
		// their original validation so an upgrade does not strand recovery.
		if isObserveReviewOnly(intent.Preview.CandidateSet) {
			projected, _ := observeOnlyProjection(activeBaseline, currentSnapshot, evaluationRoles(evaluation))
			if !equalFingerprints(projected, intent.Preview.Baseline.Files) {
				return fmt.Errorf("managed_scope_source_guard_snapshot_changed")
			}
			for _, rel := range intent.Preview.CandidateSet.ObserveReview.Paths {
				current, currentExists := currentSnapshot[rel]
				expectedFingerprint, expectedExists := intent.Preview.Baseline.Files[rel]
				if currentExists != expectedExists || (currentExists && current != expectedFingerprint) {
					return fmt.Errorf("managed_scope_source_guard_drift: %s", rel)
				}
			}
			return nil
		}
		expected = cloneFingerprints(intent.Preview.Baseline.Files)
	}
	// Source validation runs immediately before Baseline publication and again
	// after internal Apply verification. The transaction's own Index postimage
	// is therefore authoritative here; every other source remains Plan-preimage-bound.
	if _, guarded := expected[indexPath]; guarded {
		fingerprint := baseline.HashBytes(indexPath, intent.Preview.IndexPostimage.PostimageBytes)
		fingerprint.Role = machinecontract.ScopeRoleIndex
		expected[indexPath] = fingerprint
	}
	if !equalFingerprints(currentSnapshot, expected) {
		return fmt.Errorf("managed_scope_source_guard_snapshot_changed")
	}
	for rel, expectedFingerprint := range expected {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || fingerprint.SHA256 != expectedFingerprint.SHA256 {
			return fmt.Errorf("managed_scope_source_guard_drift: %s", rel)
		}
	}
	return nil
}

func equalFingerprints(left, right map[string]baseline.Fingerprint) bool {
	if len(left) != len(right) {
		return false
	}
	for path, fingerprint := range left {
		if right[path] != fingerprint {
			return false
		}
	}
	return true
}

func internalVerify(root string, intent *RecoveryIntent) error {
	if err := validateSourceGuardsWithCurationBasis(root, intent, true); err != nil {
		return err
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return err
	}
	indexBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(intent.Preview.IndexPostimage.Path)))
	if err != nil || digestBytes(indexBytes) != intent.Preview.IndexPostimage.PostimageSHA256 {
		return fmt.Errorf("index_postimage_invalid")
	}
	validation, err := cognitionbudget.Validate(root, indexBytes, cfg.EffectiveCognitionBudget())
	if err != nil || !validation.Allowed {
		return fmt.Errorf("budget_postimage_invalid")
	}
	value, exists, err := baseline.Load(root)
	if err != nil || !exists || value.ManagedScope == nil ||
		value.ManagedScope.PolicyIdentity != intent.Preview.Plan.NewPolicyIdentity ||
		value.ManagedScope.BudgetPolicyIdentity != intent.Preview.Plan.NewBudgetPolicyIdentity {
		return fmt.Errorf("baseline_postimage_invalid")
	}
	budgetIdentity, _ := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if intent.Preview.Evaluation.PolicyIdentity != value.ManagedScope.PolicyIdentity || budgetIdentity != value.ManagedScope.BudgetPolicyIdentity {
		return fmt.Errorf("policy_activation_invalid")
	}
	return nil
}

func resultFor(intent *RecoveryIntent, status string, written []string) *Result {
	if status == "applied" {
		written = append([]string{}, intent.Preview.Plan.WriteSet...)
	}
	return &Result{Version: machinecontract.ManagedScopeChangeResultV2, TransactionID: intent.TransactionID,
		Status: status, EnvelopeDigest: intent.Preview.EnvelopeDigest, WrittenPaths: append([]string{}, written...),
		AuthorizationMechanism: authorizationMechanism(intent), ApprovalDigest: authorizationDigest(intent),
		RecoveredPaths: []string{}, IndexSHA256: intent.Preview.IndexPostimage.PostimageSHA256,
		BaselineSHA256: intent.Preview.BaselinePostimage.PostimageSHA256,
		PolicyIdentity: intent.Preview.Plan.NewPolicyIdentity, BudgetPolicyIdentity: intent.Preview.Plan.NewBudgetPolicyIdentity,
		RecoveryAvailable: true, NetworkAccessed: false}
}

func recoveryIdentity(value RecoveryIntent) (string, error) {
	value.RecoveryDigest = ""
	return digestJSON(value)
}

func loadIntentAt(path, transactionID string) (*RecoveryIntent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value RecoveryIntent
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Version != machinecontract.ManagedScopeRecoveryV2 ||
		value.Operation != Operation || value.TransactionID != transactionID {
		return nil, fmt.Errorf("managed_scope_recovery_invalid")
	}
	want, err := recoveryIdentity(value)
	if err != nil || want != value.RecoveryDigest || validatePreview(&value.Preview) != nil || validateIntentAuthorization(&value) != nil {
		return nil, fmt.Errorf("managed_scope_recovery_binding_invalid")
	}
	return &value, nil
}

func saveResult(root, transactionID string, result *Result) error {
	if err := cognitiontxn.EnsureSafeDirectory(root, filepath.ToSlash(filepath.Join(".aoci", "transactions", "scope-"+transactionID))); err != nil {
		return err
	}
	data, err := Encode(result)
	if err != nil {
		return err
	}
	return cognitiontxn.SaveImmutable(resultPath(root, transactionID, result.Status), data)
}

func intentFilename(id string) string { return "scope-" + id + ".json" }
func intentPath(root, id string) string {
	return filepath.Join(root, ".aoci", "transactions", intentFilename(id))
}
func archivePath(root, id string) string {
	return filepath.Join(root, ".aoci", "transactions", "history", intentFilename(id))
}
func resultPath(root, id, status string) string {
	return filepath.Join(root, ".aoci", "transactions", "scope-"+id, "result-"+safeFaultName(status)+".json")
}
func validTransactionID(value string) bool {
	return len(value) == 24 && strings.Trim(value, "0123456789abcdef") == ""
}
func safeFaultName(value string) string {
	return strings.NewReplacer("/", "_", ".", "_").Replace(value)
}
func equalStrings(left, right []string) bool {
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
func targetStatus(status *Status, path string) TargetStatus {
	for _, target := range status.Targets {
		if target.Path == path {
			return target
		}
	}
	return TargetStatus{Path: path, State: cognitiontxn.StateUnknown}
}
func preimageFor(values []FormalImage, path string) (FormalImage, bool) {
	for _, value := range values {
		if value.Path == path {
			return value, true
		}
	}
	return FormalImage{}, false
}
