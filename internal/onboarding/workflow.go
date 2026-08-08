package onboarding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

func Start(root, locale string, now time.Time) (*Session, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("onboarding_repository_root_invalid")
	}
	if existing, exists, loadErr := Load(absRoot); loadErr != nil {
		return nil, loadErr
	} else if exists {
		return existing, nil
	}
	if pending, err := cognitiontxn.Pending(absRoot); err != nil {
		return nil, fmt.Errorf("onboarding_recovery_state_unavailable")
	} else if len(pending) != 0 {
		return nil, fmt.Errorf("onboarding_recovery_pending")
	}
	discovery, err := cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: absRoot, Locale: locale})
	if err != nil {
		return nil, err
	}
	kinds := append([]string{}, discovery.RecommendedKinds...)
	operation := cognitionplan.OperationBootstrap
	plan := discovery
	switch discovery.Layout {
	case machinecontract.CognitionPlannerUninitialized:
		plan, err = cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: absRoot, Locale: locale, TargetKinds: kinds})
	case machinecontract.CognitionPlannerLegacy:
		operation = cognitionplan.OperationMigration
		plan, err = cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: absRoot, Locale: locale, TargetKinds: kinds})
	case machinecontract.CognitionPlannerVolumes:
		return nil, fmt.Errorf("onboarding_already_volumes")
	default:
		return nil, fmt.Errorf("onboarding_layout_invalid_or_mixed")
	}
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadReadOnly(absRoot)
	if err != nil {
		return nil, fmt.Errorf("onboarding_automation_policy_invalid")
	}
	policy := cfg.ResolveOnboardingAutomation(operation == cognitionplan.OperationBootstrap)
	automationPolicy := cloneAutomationPolicy(policy)
	frozen := now.UTC().Truncate(time.Second).Format(time.RFC3339)
	sessionVersion := LegacySessionVersion
	if operation == cognitionplan.OperationBootstrap {
		sessionVersion = SessionVersion
	}
	id := sessionIdentity(sessionVersion, operation, plan.RepositoryIdentity, plan.PlanID, automationPolicy)
	if err := ensureRuntimeBoundary(absRoot, locale); err != nil {
		return nil, err
	}
	pendingTargets := make([]string, 0, len(plan.AuthoringTasks))
	for _, task := range plan.AuthoringTasks {
		pendingTargets = append(pendingTargets, task.TaskID)
	}
	sort.Strings(pendingTargets)
	warnings := make([]string, 0, len(plan.Warnings))
	for _, finding := range plan.Warnings {
		warnings = append(warnings, finding.Code)
	}
	sort.Strings(warnings)
	session := &Session{
		Version: sessionVersion, Revision: 1, Status: "in_progress", OnboardingSessionID: id,
		Operation: operation, AutomationPolicy: automationPolicy,
		RepositoryIdentity: plan.RepositoryIdentity, CurrentLayout: plan.Layout,
		SafeInventoryIdentity: plan.SafeInventory.RulesIdentity, BusinessSourceManifest: plan.BusinessSourceManifest,
		DatabaseSourceProposal: buildDatabaseSourceProposal(plan), EvidenceIdentity: plan.SourceEvidenceIdentity,
		CompletedAuthoringTargets: []string{}, PendingAuthoringTargets: pendingTargets,
		CandidateAssetIdentities: map[string]string{}, ApprovalState: "not_prepared", TransactionState: "not_started",
		LastSuccessPoint: "safe_inventory_and_plan", PendingWarnings: warnings, NextAction: "authoring_next",
		RecoveryDirection: "resume_from_last_success_point", PlanID: plan.PlanID,
		FrozenBaselineTimestamp: frozen, CreatedAt: frozen, UpdatedAt: frozen,
		BusinessRowsRead: 0, DDLDMLStatements: 0, NetworkAccessed: false, Plan: plan,
	}
	planRel, err := saveArtifact(absRoot, session, "plan-"+plan.PlanID+".json", plan)
	if err != nil {
		return nil, err
	}
	session.PlanArtifact = planRel
	if operation == cognitionplan.OperationMigration {
		snapshot, err := migrationapply.CaptureSnapshot(absRoot, locale, kinds, frozen)
		if err != nil {
			return nil, err
		}
		snapshotRel, err := saveArtifact(absRoot, session, "legacy-snapshot.json", snapshot)
		if err != nil {
			return nil, err
		}
		session.SnapshotArtifact = snapshotRel
	}
	if session.DatabaseSourceProposal != nil && len(plan.Evidence) == 0 {
		session.PendingWarnings = append(session.PendingWarnings, "database_source_confirmation_required")
		sort.Strings(session.PendingWarnings)
	}
	if err := save(absRoot, session); err != nil {
		return nil, err
	}
	return session, nil
}

type snapshotCollector interface {
	Snapshot(context.Context, dbevidence.SourceConfig) (dbevidence.SourceManifest, dbevidence.Snapshot, map[string][]byte, error)
}

// CollectDatabaseEvidence performs the two canonical catalog snapshots required
// by onboarding. It stores no credential values and advances only the existing
// Database Evidence and Database Baseline pipelines after byte determinism is
// proven.
func CollectDatabaseEvidence(ctx context.Context, root string, collector snapshotCollector) (*Session, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	if EffectiveAutomationPolicy(session).Mode == config.AutomationModeOff {
		return nil, fmt.Errorf("onboarding_automation_off_evidence_forbidden")
	}
	if collector == nil {
		collector = dbevidence.NewCollector()
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("onboarding_database_configuration_invalid")
	}
	if len(cfg.DatabaseSources) == 0 {
		return nil, fmt.Errorf("onboarding_database_source_confirmation_required")
	}
	for _, source := range cfg.DatabaseSources {
		if !source.Enabled {
			continue
		}
		firstManifest, first, firstFiles, err := collector.Snapshot(ctx, source)
		if err != nil {
			return nil, err
		}
		secondManifest, second, secondFiles, err := collector.Snapshot(ctx, source)
		if err != nil {
			return nil, err
		}
		if !equalCanonicalSnapshots(firstManifest, first, firstFiles, secondManifest, second, secondFiles) {
			return nil, fmt.Errorf("onboarding_database_snapshot_nondeterministic: %s", source.SourceID)
		}
		if err := dbevidence.WriteSnapshot(root, firstManifest, first, firstFiles); err != nil {
			return nil, fmt.Errorf("onboarding_database_evidence_write_failed")
		}
		if err := dbevidence.AcceptSnapshot(root, first, first.SourceSnapshotSHA256); err != nil {
			return nil, fmt.Errorf("onboarding_database_baseline_accept_failed")
		}
	}
	oldPlan, err := loadPlan(root, session)
	if err != nil {
		return nil, err
	}
	locale := oldPlan.Locale
	discovery, err := cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: root, Locale: locale})
	if err != nil {
		return nil, err
	}
	if discovery.BusinessSourceManifest.AggregateSHA256 != session.BusinessSourceManifest.AggregateSHA256 {
		return nil, fmt.Errorf("onboarding_business_source_drift_during_database_evidence")
	}
	kinds := append([]string{}, discovery.RecommendedKinds...)
	var refreshed *cognitionplan.Plan
	if session.Operation == cognitionplan.OperationMigration {
		refreshed, err = cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: locale, TargetKinds: kinds})
	} else {
		refreshed, err = cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: root, Locale: locale, TargetKinds: kinds})
	}
	if err != nil {
		return nil, err
	}
	if session.AutomationPolicy != nil {
		resolved := cfg.ResolveOnboardingAutomation(session.Operation == cognitionplan.OperationBootstrap)
		if resolved != *session.AutomationPolicy {
			return nil, fmt.Errorf("onboarding_automation_policy_drift")
		}
	}
	completed := map[string]bool{}
	if session.Version == LegacySessionVersion {
		for _, id := range session.CompletedAuthoringTargets {
			completed[id] = true
		}
	}
	pending := map[string]bool{}
	for _, task := range refreshed.AuthoringTasks {
		if !completed[task.TaskID] {
			pending[task.TaskID] = true
		}
	}
	planRel, err := saveArtifact(root, session, "plan-"+refreshed.PlanID+".json", refreshed)
	if err != nil {
		return nil, err
	}
	session.PlanID = refreshed.PlanID
	session.PlanArtifact = planRel
	if session.Operation == cognitionplan.OperationMigration {
		snapshot, err := migrationapply.CaptureSnapshot(root, locale, kinds, session.FrozenBaselineTimestamp)
		if err != nil {
			return nil, err
		}
		snapshotRel, err := saveArtifact(root, session, "legacy-snapshot-"+snapshot.SnapshotIdentity+".json", snapshot)
		if err != nil {
			return nil, err
		}
		session.SnapshotArtifact = snapshotRel
	}
	session.EvidenceIdentity = refreshed.SourceEvidenceIdentity
	if session.Version == SessionVersion {
		session.SemanticAuthoringDeclaration = nil
		session.ActiveAuthoringBatch = nil
	}
	session.PendingAuthoringTargets = mapKeys(pending)
	session.CompletedAuthoringTargets = mapKeys(completed)
	session.PendingWarnings = removeString(session.PendingWarnings, "database_source_confirmation_required")
	session.LastSuccessPoint = "database_evidence_deterministic_and_accepted"
	session.NextAction = "authoring_next"
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := save(root, session); err != nil {
		return nil, err
	}
	return session, nil
}

func equalCanonicalSnapshots(leftManifest dbevidence.SourceManifest, left dbevidence.Snapshot, leftFiles map[string][]byte, rightManifest dbevidence.SourceManifest, right dbevidence.Snapshot, rightFiles map[string][]byte) bool {
	leftManifestData, _ := json.Marshal(leftManifest)
	rightManifestData, _ := json.Marshal(rightManifest)
	leftData, _ := json.Marshal(left)
	rightData, _ := json.Marshal(right)
	if string(leftManifestData) != string(rightManifestData) || string(leftData) != string(rightData) || len(leftFiles) != len(rightFiles) {
		return false
	}
	for path, data := range leftFiles {
		if string(data) != string(rightFiles[path]) {
			return false
		}
	}
	return true
}

func Resume(root string) (*Session, error) {
	session, exists, err := Load(root)
	if err != nil || !exists {
		if err == nil {
			err = fmt.Errorf("onboarding_session_missing")
		}
		return nil, err
	}
	policy := EffectiveAutomationPolicy(session)
	if session.Operation == cognitionplan.OperationBootstrap &&
		policy.Mode == config.AutomationModeAuto && session.NextAction == "prepare" {
		if _, err := Prepare(root); err != nil {
			return nil, err
		}
		session, err = LoadRequired(root)
		if err != nil {
			return nil, err
		}
	}
	if session.TransactionState == "apply_pending" && session.ApprovalArtifact != "" {
		if _, err := Apply(root, session.ApprovalArtifact); err != nil {
			return nil, err
		}
		return LoadRequired(root)
	}
	return session, nil
}

func LoadRequired(root string) (*Session, error) {
	session, exists, err := Load(root)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("onboarding_session_missing")
	}
	return session, nil
}

func Next(root string, maxObjects int, maxEvidenceBytes int64) (*AuthoringBatch, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	if EffectiveAutomationPolicy(session).Mode == config.AutomationModeOff {
		return nil, fmt.Errorf("onboarding_automation_off_authoring_forbidden")
	}
	plan, err := loadPlan(root, session)
	if err != nil {
		return nil, err
	}
	if maxObjects <= 0 {
		maxObjects = 25
	}
	if maxEvidenceBytes <= 0 {
		maxEvidenceBytes = 256 * 1024
	}
	pending := make(map[string]bool, len(session.PendingAuthoringTargets))
	for _, id := range session.PendingAuthoringTargets {
		pending[id] = true
	}
	tasks := make([]cognitionplan.AuthoringTask, 0, maxObjects)
	var evidenceBytes int64
	if session.Version == SessionVersion && session.ActiveAuthoringBatch != nil {
		active := map[string]bool{}
		for _, id := range session.ActiveAuthoringBatch.TaskIDs {
			active[id] = true
		}
		for _, task := range plan.AuthoringTasks {
			if active[task.TaskID] && pending[task.TaskID] {
				tasks = append(tasks, task)
			}
		}
		if len(tasks) != len(active) {
			return nil, fmt.Errorf("onboarding_active_authoring_batch_invalid")
		}
		evidenceBytes = session.ActiveAuthoringBatch.EvidenceBytes
	} else {
		for _, task := range plan.AuthoringTasks {
			if !pending[task.TaskID] {
				continue
			}
			size := authoringTaskEvidenceBytes(plan, task)
			if len(tasks) > 0 && (len(tasks) >= maxObjects || evidenceBytes+size > maxEvidenceBytes) {
				break
			}
			tasks = append(tasks, task)
			evidenceBytes += size
		}
	}
	next := "submit_authoring_completion"
	if len(tasks) == 0 {
		next = "preview"
	}
	data, _ := json.Marshal(tasks)
	batchVersion := LegacyBatchVersion
	digestInput := append([]byte(session.OnboardingSessionID+"\x00"), data...)
	var requirement *cognitionplan.SemanticAuthoringRequirement
	if session.Version == SessionVersion {
		batchVersion = BatchVersion
		requirement = cognitionplan.SemanticAuthoringRequirementForPlan(plan, nil)
		digestInput = append([]byte(batchVersion+"\x00"+session.OnboardingSessionID+"\x00"+plan.PlanID+"\x00"+requirement.EvidenceBindingSHA256+"\x00"), data...)
	}
	digest := sha256.Sum256(digestInput)
	batchID := hex.EncodeToString(digest[:])
	if session.Version == SessionVersion && len(tasks) != 0 {
		if session.ActiveAuthoringBatch != nil {
			if session.ActiveAuthoringBatch.BatchID != batchID {
				return nil, fmt.Errorf("onboarding_active_authoring_batch_identity_mismatch")
			}
		} else {
			taskIDs := make([]string, 0, len(tasks))
			for _, task := range tasks {
				taskIDs = append(taskIDs, task.TaskID)
			}
			sort.Strings(taskIDs)
			session.ActiveAuthoringBatch = &ActiveAuthoringBatch{BatchID: batchID, TaskIDs: taskIDs, EvidenceBytes: evidenceBytes}
			session.Revision++
			session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if err := save(root, session); err != nil {
				return nil, err
			}
		}
	}
	return &AuthoringBatch{
		Version: batchVersion, OnboardingSessionID: session.OnboardingSessionID,
		BatchID: batchID, Tasks: tasks, ObjectCount: len(tasks), EvidenceBytes: evidenceBytes,
		CompletedCount: len(session.CompletedAuthoringTargets), PendingCount: len(session.PendingAuthoringTargets),
		NextAction: next, SemanticGenerated: false, SemanticAuthoringRequirement: requirement,
	}, nil
}

func CompleteTasks(root string, completion Completion) (*Session, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	if EffectiveAutomationPolicy(session).Mode == config.AutomationModeOff {
		return nil, fmt.Errorf("onboarding_automation_off_authoring_forbidden")
	}
	if completion.SessionID != session.OnboardingSessionID {
		return nil, fmt.Errorf("onboarding_completion_identity_invalid")
	}
	plan, err := loadPlan(root, session)
	if err != nil {
		return nil, err
	}
	if session.Version == SessionVersion {
		if completion.Version != CompletionVersion || session.ActiveAuthoringBatch == nil ||
			completion.BatchID != session.ActiveAuthoringBatch.BatchID || !sameStringSet(completion.CompletedTasks, session.ActiveAuthoringBatch.TaskIDs) {
			return nil, fmt.Errorf("onboarding_completion_contract_mismatch")
		}
		if err := cognitionplan.ValidateSemanticAuthoringDeclaration(plan, completion.SemanticAuthoringDeclaration); err != nil {
			return nil, fmt.Errorf("onboarding_%w", err)
		}
		if session.SemanticAuthoringDeclaration != nil && !sameSemanticAuthoringDeclaration(session.SemanticAuthoringDeclaration, completion.SemanticAuthoringDeclaration) {
			return nil, fmt.Errorf("onboarding_semantic_authoring_run_mismatch")
		}
	} else if completion.Version != LegacyCompletionVersion || completion.BatchID != "" || completion.SemanticAuthoringDeclaration != nil {
		return nil, fmt.Errorf("onboarding_completion_contract_mismatch")
	}
	pending := map[string]bool{}
	for _, id := range session.PendingAuthoringTargets {
		pending[id] = true
	}
	completed := map[string]bool{}
	for _, id := range session.CompletedAuthoringTargets {
		completed[id] = true
	}
	for _, id := range completion.CompletedTasks {
		if !pending[id] {
			return nil, fmt.Errorf("onboarding_completion_target_invalid")
		}
		delete(pending, id)
		completed[id] = true
	}
	if session.Version == SessionVersion {
		if session.SemanticAuthoringDeclaration == nil {
			declaration := *completion.SemanticAuthoringDeclaration
			session.SemanticAuthoringDeclaration = &declaration
		}
		session.ActiveAuthoringBatch = nil
	}
	session.CompletedAuthoringTargets = mapKeys(completed)
	session.PendingAuthoringTargets = mapKeys(pending)
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	session.LastSuccessPoint = "authoring_progress_saved"
	if len(session.PendingAuthoringTargets) == 0 {
		session.NextAction = "preview"
	}
	if err := save(root, session); err != nil {
		return nil, err
	}
	return session, nil
}

func RecordHostDelivery(root string, receipt HostDeliveryReceipt) (*Session, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	_, digestErr := hex.DecodeString(receipt.BodySHA256)
	if receipt.Version != machinecontract.OverviewDeliveryReceiptV1 || len(receipt.BodySHA256) != 64 || digestErr != nil || receipt.BodyBytes <= 0 ||
		!receipt.EndMarkerObserved || (!receipt.BodySHA256Verified && !receipt.BodyBytesVerified) {
		return nil, fmt.Errorf("onboarding_host_delivery_receipt_invalid")
	}
	receipt.Confirmed = true
	session.HostDeliveryReceipt = &receipt
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := save(root, session); err != nil {
		return nil, err
	}
	return session, nil
}

func Preview(root string, candidateData, mappingData []byte) (*cognitionplan.Preview, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	if EffectiveAutomationPolicy(session).Mode == config.AutomationModeOff {
		return nil, fmt.Errorf("onboarding_automation_off_authoring_forbidden")
	}
	if len(session.PendingAuthoringTargets) != 0 {
		return nil, fmt.Errorf("onboarding_authoring_incomplete")
	}
	plan, err := loadPlan(root, session)
	if err != nil {
		return nil, err
	}
	candidate, err := cognitionplan.DecodeCandidate(candidateData)
	if err != nil {
		return nil, err
	}
	if session.Version == SessionVersion && !cognitionplan.SemanticAuthoringDeclarationMatchesReceipt(session.SemanticAuthoringDeclaration, candidate.SemanticAuthoringProvenance) {
		return nil, fmt.Errorf("onboarding_semantic_authoring_candidate_declaration_mismatch")
	}
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil {
		return nil, err
	}
	if preview.Status != machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest == nil {
		return nil, fmt.Errorf("onboarding_preview_not_ready")
	}
	if session.Operation == cognitionplan.OperationMigration {
		if len(mappingData) == 0 {
			return nil, fmt.Errorf("onboarding_mapping_required")
		}
		snapshot, err := loadMigrationSnapshot(root, session)
		if err != nil {
			return nil, err
		}
		mapping, err := migrationapply.DecodeMapping(mappingData)
		if err != nil {
			return nil, err
		}
		validated, err := migrationapply.ValidateMapping(root, snapshot, plan, candidate, mapping)
		if err != nil {
			return nil, err
		}
		mappingRel, err := saveArtifact(root, session, "mapping.json", validated)
		if err != nil {
			return nil, err
		}
		session.MappingArtifact = mappingRel
		session.MappingIdentity = validated.MappingSHA256
	}
	candidateRel, err := saveArtifactBytes(root, session, "candidate.json", candidateData)
	if err != nil {
		return nil, err
	}
	previewRel, err := saveArtifact(root, session, "preview.json", preview)
	if err != nil {
		return nil, err
	}
	session.CandidateArtifact = candidateRel
	session.PreviewArtifact = previewRel
	session.CandidateIdentity = preview.CandidateIdentity
	session.PreviewIdentity = preview.ApprovalDigest.Digest
	session.CandidateAssetIdentities = candidateAssetIdentities(candidate)
	session.LastSuccessPoint = "preview_ready"
	session.NextAction = "prepare"
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := save(root, session); err != nil {
		return nil, err
	}
	return preview, nil
}

func Prepare(root string) (any, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	policy := EffectiveAutomationPolicy(session)
	if policy.Mode == config.AutomationModeOff {
		return nil, fmt.Errorf("onboarding_automation_off_apply_forbidden")
	}
	if session.Operation == cognitionplan.OperationBootstrap && session.AutomationPolicy != nil {
		cfg, policyErr := config.LoadReadOnly(root)
		if policyErr != nil || cfg.ResolveOnboardingAutomation(true) != policy {
			return nil, fmt.Errorf("onboarding_automation_policy_drift")
		}
	}
	plan, candidate, preview, err := loadPreparedInputs(root, session)
	if err != nil {
		return nil, err
	}
	var envelope any
	if session.Operation == cognitionplan.OperationBootstrap {
		envelope, err = bootstrapapply.Prepare(root, &bootstrapapply.ApplyRequest{
			Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *plan,
			Candidate: *candidate, Preview: *preview, BaselineTimestamp: session.FrozenBaselineTimestamp,
		})
	} else {
		snapshot, loadErr := loadMigrationSnapshot(root, session)
		if loadErr != nil {
			return nil, loadErr
		}
		mappingData, loadErr := readArtifact(root, session.MappingArtifact)
		if loadErr != nil {
			return nil, loadErr
		}
		mapping, loadErr := migrationapply.DecodeMapping(mappingData)
		if loadErr != nil {
			return nil, loadErr
		}
		envelope, err = migrationapply.Prepare(root, &migrationapply.ApplyRequest{
			Version: machinecontract.CognitionMigrationApplyRequestV2, Snapshot: *snapshot, Plan: *plan,
			Mapping: *mapping, Candidate: *candidate, Preview: *preview, BaselineTimestamp: session.FrozenBaselineTimestamp,
		})
	}
	if err != nil {
		return nil, err
	}
	rel, err := saveArtifact(root, session, "apply-envelope.json", envelope)
	if err != nil {
		return nil, err
	}
	session.EnvelopeArtifact = rel
	session.LastSuccessPoint = "envelope_prepared"
	if session.Operation == cognitionplan.OperationBootstrap {
		if policy.Mode == config.AutomationModeAuto {
			bootstrapEnvelope, ok := envelope.(*bootstrapapply.ApplyEnvelope)
			if !ok {
				return nil, fmt.Errorf("onboarding_bootstrap_envelope_invalid")
			}
			projection, eligibilityErr := bootstrapapply.EvaluateAutoEligibility(root, bootstrapEnvelope)
			if eligibilityErr != nil {
				return nil, eligibilityErr
			}
			session.AuthorizationProjection = projection
			if !projection.AutoReady {
				session.ApprovalState = "auto_blocked"
				session.TransactionState = "not_started"
				session.NextAction = "resolve_auto_eligibility_blockers"
				session.Revision++
				session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
				if saveErr := save(root, session); saveErr != nil {
					return nil, saveErr
				}
				return nil, fmt.Errorf("onboarding_auto_eligibility_blocked: %v", projection.Blockers)
			}
			approval, approvalErr := bootstrapapply.RecordPolicyBoundAutoApproval(
				root,
				bootstrapEnvelope,
				time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
			)
			if approvalErr != nil {
				return nil, approvalErr
			}
			approvalRel, saveErr := saveArtifact(root, session, "policy-bound-auto-approval.json", approval)
			if saveErr != nil {
				return nil, saveErr
			}
			session.ApprovalArtifact = approvalRel
			session.ApprovalState = "policy_bound_auto"
			session.TransactionState = "apply_pending"
			session.NextAction = "auto_apply"
		} else {
			session.AuthorizationProjection = nil
			session.ApprovalState = "interaction_required"
			session.NextAction = "human_tty_digest_confirmation"
		}
	} else {
		session.ApprovalState = "interaction_required"
		session.NextAction = "human_tty_digest_confirmation"
	}
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := save(root, session); err != nil {
		return nil, err
	}
	return envelope, nil
}

func Apply(root, approvalRelativeOrAbsolute string) (any, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	if EffectiveAutomationPolicy(session).Mode == config.AutomationModeOff {
		return nil, fmt.Errorf("onboarding_automation_off_apply_forbidden")
	}
	approvalData, err := readExternalOrArtifact(root, approvalRelativeOrAbsolute)
	if err != nil {
		return nil, err
	}
	envelopeData, err := readArtifact(root, session.EnvelopeArtifact)
	if err != nil {
		return nil, err
	}
	var bootstrapEnvelope *bootstrapapply.ApplyEnvelope
	var bootstrapApproval *bootstrapapply.Approval
	var migrationEnvelope *migrationapply.ApplyEnvelope
	var migrationApproval *migrationapply.Approval
	if session.Operation == cognitionplan.OperationBootstrap {
		bootstrapEnvelope, err = bootstrapapply.DecodeApplyEnvelope(envelopeData)
		if err == nil {
			bootstrapApproval, err = bootstrapapply.DecodeApproval(approvalData)
		}
		if err == nil {
			err = bootstrapapply.ValidateApproval(bootstrapEnvelope, bootstrapApproval)
		}
	} else {
		migrationEnvelope, err = migrationapply.DecodeApplyEnvelope(envelopeData)
		if err == nil {
			migrationApproval, err = migrationapply.DecodeApproval(approvalData)
		}
		if err == nil {
			err = migrationapply.ValidateApproval(migrationEnvelope, migrationApproval)
		}
	}
	if err != nil {
		return nil, err
	}
	approvalDigest := sha256.Sum256(approvalData)
	approvalRel, err := saveArtifactBytes(root, session, "approval-"+hex.EncodeToString(approvalDigest[:])+".json", approvalData)
	if err != nil {
		return nil, err
	}
	session.ApprovalArtifact = approvalRel
	session.ApprovalState = "approved_envelope_provided"
	session.TransactionState = "apply_pending"
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := save(root, session); err != nil {
		return nil, err
	}
	var result any
	var transactionID string
	if session.Operation == cognitionplan.OperationBootstrap {
		applied, err := bootstrapapply.Apply(root, bootstrapEnvelope, bootstrapApproval)
		if err != nil {
			return nil, err
		}
		result, transactionID = applied, applied.TransactionID
	} else {
		applied, err := migrationapply.Apply(root, migrationEnvelope, migrationApproval)
		if err != nil {
			return nil, err
		}
		result, transactionID = applied, applied.TransactionID
	}
	resultRel, err := saveArtifact(root, session, "apply-result.json", result)
	if err != nil {
		return nil, err
	}
	session.ResultArtifact = resultRel
	session.TransactionID = transactionID
	session.TransactionState = "applied"
	session.ApprovalState = "consumed"
	finalizeOnboardingGovernance(root, session)
	session.Revision++
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := save(root, session); err != nil {
		return nil, err
	}
	return result, nil
}

func finalizeOnboardingGovernance(root string, session *Session) {
	session.Status = "governance_pending"
	session.LastSuccessPoint = "formal_assets_complete"
	session.GovernanceResult = volumegovernance.ResultBlocked
	session.GuideStage = volumegovernance.ResultBlocked
	session.NextAction = volumegovernance.ResultBlocked
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		session.PendingWarnings = uniqueSorted(append(session.PendingWarnings, "post_apply_configuration_invalid"))
		return
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		session.PendingWarnings = uniqueSorted(append(session.PendingWarnings, "post_apply_structure_invalid"))
		return
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		session.PendingWarnings = uniqueSorted(append(session.PendingWarnings, "post_apply_governance_assessment_failed"))
		return
	}
	session.StructureValid = facts.StructureValid
	session.GovernanceAligned = facts.GovernanceAligned
	session.CheckOK = facts.StructureValid && facts.GovernanceAligned
	session.GovernanceResult = facts.Result
	session.GuideStage = facts.Result
	session.NextAction = facts.NextRequiredAction
	if session.CheckOK && facts.Result == volumegovernance.ResultAligned {
		session.Status = "completed"
		session.LastSuccessPoint = "aligned"
		session.NextAction = "none"
	}
}

func Abort(root string) (*Session, error) {
	session, err := LoadRequired(root)
	if err != nil {
		return nil, err
	}
	if session.TransactionState != "not_started" || session.ApprovalArtifact != "" {
		return nil, fmt.Errorf("onboarding_abort_after_approval_forbidden")
	}
	abortDir := filepath.Join(root, ".aoci", "onboarding", "aborted")
	if err := os.MkdirAll(abortDir, 0o700); err != nil {
		return nil, err
	}
	target := filepath.Join(abortDir, session.OnboardingSessionID+".json")
	if err := os.Rename(SessionPath(root), target); err != nil {
		return nil, err
	}
	session.NextAction = "aborted"
	return session, nil
}

func loadPlan(root string, session *Session) (*cognitionplan.Plan, error) {
	data, err := readArtifact(root, session.PlanArtifact)
	if err != nil {
		return nil, err
	}
	return cognitionplan.DecodePlan(data)
}

func loadMigrationSnapshot(root string, session *Session) (*migrationapply.LegacySnapshot, error) {
	data, err := readArtifact(root, session.SnapshotArtifact)
	if err != nil {
		return nil, err
	}
	return migrationapply.DecodeLegacySnapshot(data)
}

func loadPreparedInputs(root string, session *Session) (*cognitionplan.Plan, *cognitionplan.LayoutCandidate, *cognitionplan.Preview, error) {
	plan, err := loadPlan(root, session)
	if err != nil {
		return nil, nil, nil, err
	}
	candidateData, err := readArtifact(root, session.CandidateArtifact)
	if err != nil {
		return nil, nil, nil, err
	}
	candidate, err := cognitionplan.DecodeCandidate(candidateData)
	if err != nil {
		return nil, nil, nil, err
	}
	previewData, err := readArtifact(root, session.PreviewArtifact)
	if err != nil {
		return nil, nil, nil, err
	}
	preview, err := cognitionplan.DecodePreview(previewData)
	return plan, candidate, preview, err
}

func candidateAssetIdentities(candidate *cognitionplan.LayoutCandidate) map[string]string {
	result := map[string]string{}
	for _, asset := range candidate.Assets {
		digest := sha256.Sum256([]byte(asset.Content))
		result[asset.AssetID] = hex.EncodeToString(digest[:])
	}
	return result
}

func cloneAutomationPolicy(policy config.AutomationPolicy) *config.AutomationPolicy {
	copyValue := policy
	return &copyValue
}

func buildDatabaseSourceProposal(plan *cognitionplan.Plan) *DatabaseSourceProposal {
	paths := []string{}
	engines := map[string]bool{}
	for _, file := range plan.BusinessSourceManifest.Files {
		lower := strings.ToLower(file.Path)
		switch {
		case strings.Contains(lower, "mysql"):
			engines["mysql"] = true
		case strings.Contains(lower, "postgres") || strings.Contains(lower, "pgsql"):
			engines["postgresql"] = true
		case strings.Contains(lower, "prisma") || strings.Contains(lower, "sequelize") || strings.Contains(lower, "typeorm") || strings.Contains(lower, "migration"):
			paths = append(paths, file.Path)
		}
		if strings.Contains(lower, "schema") || strings.Contains(lower, "migration") || strings.HasSuffix(lower, "ormconfig.json") {
			paths = append(paths, file.Path)
		}
	}
	paths = uniqueSorted(paths)
	if len(paths) == 0 && len(engines) == 0 {
		return nil
	}
	engineValues := mapKeys(engines)
	if len(engineValues) == 0 {
		engineValues = []string{"mysql", "postgresql"}
	}
	return &DatabaseSourceProposal{Version: "database-source-proposal/v1", EvidencePaths: paths,
		EngineCandidates: engineValues, SourceIDRequired: true, DatabaseRequired: true,
		CredentialEnvRequired: true, CredentialValueStored: false}
}

func authoringTaskEvidenceBytes(plan *cognitionplan.Plan, task cognitionplan.AuthoringTask) int64 {
	if strings.HasPrefix(task.ObjectRef, "code:") {
		path := strings.TrimPrefix(task.ObjectRef, "code:")
		for _, file := range plan.BusinessSourceManifest.Files {
			if file.Path == path {
				return file.SizeBytes
			}
		}
	}
	return int64(len(strings.Join(task.EvidenceRefs, "\n")))
}

func sessionIdentity(version, operation, repositoryIdentity, planID string, policy *config.AutomationPolicy) string {
	material := version + "\x00" + operation + "\x00" + repositoryIdentity + "\x00" + planID
	if version == SessionVersion && policy != nil {
		material += "\x00" + policy.Mode + "\x00" + policy.Source
	}
	digest := sha256.Sum256([]byte(material))
	return "onboard-" + hex.EncodeToString(digest[:16])
}

func mapKeys[T ~bool](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string{}, left...)
	rightCopy := append([]string{}, right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func sameSemanticAuthoringDeclaration(left, right *cognitionplan.SemanticAuthoringDeclaration) bool {
	return left != nil && right != nil && left.Version == right.Version && left.Origin == right.Origin &&
		left.AuthoringRunID == right.AuthoringRunID && left.DiscoveryPlanID == right.DiscoveryPlanID &&
		left.EvidenceBindingSHA256 == right.EvidenceBindingSHA256
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	return mapKeys(set)
}

func removeString(values []string, unwanted string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != unwanted {
			result = append(result, value)
		}
	}
	return result
}

func readExternalOrArtifact(root, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("onboarding_approval_file_required")
	}
	if !filepath.IsAbs(path) {
		if data, err := readArtifact(root, path); err == nil {
			return data, nil
		}
		path = filepath.Join(root, path)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("onboarding_approval_file_invalid")
	}
	return os.ReadFile(path)
}

func ensureRuntimeBoundary(root, locale string) error {
	content, err := textassets.Load(locale, textassets.TemplateAOCIGitignore)
	if err != nil {
		return err
	}
	path := filepath.Join(root, ".aoci", ".gitignore")
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == content {
			return nil
		}
		manifest, manifestErr := textassets.ReadManifest()
		if manifestErr != nil {
			return manifestErr
		}
		for _, officialLocale := range manifest.OfficialLocales {
			officialContent, loadErr := textassets.Load(officialLocale, textassets.TemplateAOCIGitignore)
			if loadErr != nil {
				return loadErr
			}
			if string(existing) == officialContent {
				return nil
			}
		}
		return fmt.Errorf("onboarding_runtime_boundary_conflict")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return afs.AtomicCreateCAS(path, []byte(content))
}

var _ = errors.Is
