package scopechange

import (
	"bytes"
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

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func Build(repositoryRoot, preparedAt string, candidates CandidateSet) (*Preview, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_repository_invalid")
	}
	parsedTime, err := time.Parse(time.RFC3339, preparedAt)
	if err != nil || parsedTime.Location() != time.UTC {
		return nil, fmt.Errorf("managed_scope_prepared_at_invalid")
	}
	pending, err := cognitiontxn.Pending(root)
	if err != nil || len(pending) != 0 {
		return nil, fmt.Errorf("managed_scope_other_transaction_pending")
	}
	candidates, err = normalizeCandidateSet(candidates)
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_configuration_invalid: %w", err)
	}
	authorizationPolicy, authorizationPolicyIdentity, err := resolveApplyAuthorizationPolicy(cfg)
	if err != nil {
		return nil, err
	}
	configBytes, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		return nil, fmt.Errorf("managed_scope_configuration_preimage_unavailable")
	}
	oldBaseline, exists, err := baseline.Load(root)
	if err != nil || !exists {
		return nil, fmt.Errorf("managed_scope_baseline_preimage_unavailable")
	}
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineBytes, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_baseline_preimage_unavailable")
	}
	curationDocument, curationExists, _, err := curation.Load(root)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_curation_invalid")
	}
	curationBytes := []byte(nil)
	if curationExists {
		curationBytes, err = os.ReadFile(curation.FilePath(root))
		if err != nil {
			return nil, fmt.Errorf("managed_scope_curation_preimage_unavailable")
		}
	}
	curationPostBytes := append([]byte{}, curationBytes...)
	curationPostDocument := curationDocument
	if candidates.Curation != nil {
		if !curationExists {
			return nil, fmt.Errorf("managed_scope_curation_preimage_unavailable")
		}
		curationPostDocument, err = curation.NormalizeDocument(candidates.Curation, true)
		if err != nil {
			return nil, fmt.Errorf("managed_scope_curation_candidate_invalid: %w", err)
		}
		curationPostBytes, err = json.MarshalIndent(curationPostDocument, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("managed_scope_curation_candidate_invalid")
		}
		curationPostBytes = append(curationPostBytes, '\n')
	}
	curationExclude := activeCurationExclusions(cfg.CurationExclude, curationPostDocument, oldBaseline)
	policy := cfg.EffectiveManagedScope()
	budgetPolicy := cfg.EffectiveCognitionBudget()
	budgetIdentity, err := cognitionbudget.Identity(budgetPolicy)
	if err != nil {
		return nil, err
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{
		WalkOptions: cfg.WalkOptions(), CurationExclude: curationExclude,
	})
	if err != nil {
		return nil, fmt.Errorf("managed_scope_inventory_failed: %w", err)
	}
	evaluation = transactionRelevantEvaluation(evaluation, oldBaseline)
	policyIdentity := evaluation.PolicyIdentity
	if err := validateSafetyApproval(policyIdentity, cfg.SafeInventoryHighRiskOptIn, candidates.SafetyApproval); err != nil {
		return nil, err
	}
	desiredSnapshot, err := managedscope.Snapshot(root, evaluation, managedscope.SnapshotOptions{
		HighRiskContentApproved: len(cfg.SafeInventoryHighRiskOptIn) == 0 || candidates.SafetyApproval != nil,
	})
	if err != nil {
		return nil, err
	}
	// The source guard binds the live Plan preimage, independently from the
	// projected Baseline. Observe-only acknowledgement intentionally preserves
	// indexed cognition debt in that Baseline, so it is not a source snapshot.
	sourceGuard := cloneFingerprints(desiredSnapshot)
	formalVolumeGuards, err := FormalCognitionBaselineGuards(root, cfg.IndexPath, oldBaseline)
	if err != nil {
		return nil, err
	}
	// Managed Scope owns business-source fingerprints. Enabled Cognition
	// Volumes are formal assets owned by their lifecycle transactions, even
	// though both are carried by the compatible Baseline v1 Files map. Preserve
	// those formal fingerprints and guard their live bytes without treating the
	// assets as business scope members.
	for path, fingerprint := range formalVolumeGuards {
		desiredSnapshot[path] = fingerprint
		sourceGuard[path] = fingerprint
	}
	indexPath := filepath.Join(root, filepath.FromSlash(cfg.IndexPath))
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_index_preimage_unavailable")
	}
	document, warnings := index.Parse(string(indexBytes))
	index.ResolveRelPaths(document, root)
	if len(warnings) != 0 {
		return nil, fmt.Errorf("managed_scope_index_parse_warnings")
	}
	entries, err := entriesByPath(document)
	if err != nil {
		return nil, err
	}
	desiredRoles := evaluationRoles(evaluation)
	for path := range formalVolumeGuards {
		delete(desiredRoles, path)
	}
	if err := validateSourcesPresent(root, oldBaseline, desiredRoles); err != nil {
		return nil, err
	}
	oldRoles := baselineRolesExcept(oldBaseline, formalVolumeGuards)
	if policy.ObserveChangePolicy == machinecontract.ObserveChangeReviewRequired {
		changes := observedEvidenceChanges(oldBaseline, desiredSnapshot, desiredRoles, activePolicyIdentity(oldBaseline) == policyIdentity)
		if len(changes) != 0 {
			if candidates.ObserveReview == nil || candidates.ObserveReview.ReviewStatus != ReviewStatusReviewed ||
				candidates.ObserveReview.Reviewer == "" || !equalSortedPaths(candidates.ObserveReview.Paths, changes) {
				return nil, fmt.Errorf("observed_evidence_review_required")
			}
		}
	}
	observeReviewOnly := isObserveReviewOnly(candidates) && activePolicyIdentity(oldBaseline) == policyIdentity &&
		(activeBudgetIdentity(oldBaseline) == "" || activeBudgetIdentity(oldBaseline) == budgetIdentity)
	if observeReviewOnly {
		// Acknowledge only the reviewed Observe evidence. Concurrent Index drift
		// and new authoring targets remain bound to their previous Baseline state
		// so the ordinary Entry maintenance path must still resolve them.
		desiredSnapshot, desiredRoles = observeOnlyProjection(oldBaseline, desiredSnapshot, desiredRoles)
		for path := range formalVolumeGuards {
			delete(desiredRoles, path)
		}
	}
	plan := Plan{Version: machinecontract.ManagedScopeChangePlanV2,
		RepositoryRootIdentity: repositoryIdentity(root), OldPolicyIdentity: activePolicyIdentity(oldBaseline),
		NewPolicyIdentity: policyIdentity, OldBudgetPolicyIdentity: activeBudgetIdentity(oldBaseline),
		NewBudgetPolicyIdentity: budgetIdentity, IndexAdded: []ScopeObject{}, IndexRemoved: []ScopeObject{},
		ObserveAdded: []ScopeObject{}, ObserveRemoved: []ScopeObject{}, ExcludeAdded: []ScopeObject{},
		ExcludeRemoved: []ScopeObject{}, Preserved: []ScopeObject{}, RoleChanges: []RoleChange{},
		CoverageReductions: []CoverageReduction{},
		EntryCreates:       []EntryChange{}, EntryRemoves: []EntryChange{}, EntryUpdates: []EntryChange{},
		RetentionReview: []EntryDisposition{}, BaselineAdded: []ScopeObject{}, BaselineRemoved: []ScopeObject{},
		ObserveFingerprintAdded: []ScopeObject{}, ObserveFingerprintRemoved: []ScopeObject{},
		AuthorizationPolicy: authorizationPolicy, AuthorizationPolicyIdentity: authorizationPolicyIdentity,
		WriteSet:          []string{cfg.IndexPath, ".aoci/config.json", ".aoci/baseline.json", ".aoci/ledger.jsonl"},
		GuardSet:          []string{cfg.IndexPath, ".aoci/config.json", ".aoci/baseline.json", ".aoci/curation.json", "safe_inventory", "source_sha256"},
		RecoveryDirection: "preimage_or_partial_to_complete_postimage_or_exact_rollback", PreparedAt: preparedAt,
		NetworkAccessed: false}
	if curationExists && !bytes.Equal(curationBytes, curationPostBytes) {
		plan.WriteSet = append(plan.WriteSet, ".aoci/curation.json")
	}
	evaluationsByPath := managedScopeEvaluationsByPath(evaluation)
	allPaths := unionRolePaths(oldRoles, desiredRoles)
	for _, rel := range allPaths {
		oldRole, hadOld := oldRoles[rel]
		newRole, hasNew := desiredRoles[rel]
		if !hasNew {
			newRole = machinecontract.ScopeRoleExclude
		}
		if !hadOld {
			oldRole = machinecontract.ScopeRoleExclude
		}
		sha := ""
		if fp, ok := desiredSnapshot[rel]; ok {
			sha = fp.SHA256
		} else if fp, ok := oldBaseline.Files[rel]; ok {
			sha = fp.SHA256
		}
		object := ScopeObject{Path: rel, Role: newRole, SourceSHA256: sha}
		if oldRole == newRole {
			plan.Preserved = append(plan.Preserved, object)
			continue
		}
		plan.RoleChanges = append(plan.RoleChanges, RoleChange{Path: rel, OldRole: oldRole, NewRole: newRole, SourceSHA256: sha})
		if oldRole == machinecontract.ScopeRoleIndex && newRole != machinecontract.ScopeRoleIndex {
			basis, ruleID := machinecontract.ScopeDecisionUnspecified, ""
			if item := evaluationsByPath[rel]; item != nil && item.MatchedRule != nil {
				ruleID = item.MatchedRule.RuleID
				if item.MatchedRule.DecisionBasis != "" {
					basis = item.MatchedRule.DecisionBasis
				}
			}
			state := coverageAuthoringState(rel, entries, oldBaseline, desiredSnapshot)
			if state != "current" || basis == machinecontract.ScopeDecisionTransportConstraint {
				plan.CoverageReductions = append(plan.CoverageReductions, CoverageReduction{Path: rel, OldRole: oldRole,
					NewRole: newRole, AuthoringState: state, DecisionBasis: basis, RuleID: ruleID})
			}
		}
		appendRoleDelta(&plan, rel, oldRole, newRole, sha)
	}
	entryCandidateByPath, err := candidateMap(candidates.Entries)
	if err != nil {
		return nil, err
	}
	dispositionByPath, err := dispositionMap(candidates.Dispositions)
	if err != nil {
		return nil, err
	}
	projected := string(indexBytes)
	layout, layoutErr := cognition.DetectLayout(indexBytes)
	if layoutErr != nil {
		return nil, fmt.Errorf("managed_scope_index_layout_invalid")
	}
	for _, rel := range sortedFingerprintPaths(desiredSnapshot) {
		fingerprint := desiredSnapshot[rel]
		if desiredRoles[rel] != machinecontract.ScopeRoleIndex {
			continue
		}
		oldFingerprint, existed := oldBaseline.Files[rel]
		oldRole := ""
		if existed {
			oldRole = baseline.EffectiveRole(oldFingerprint)
		}
		_, authored := entryCandidateByPath[rel]
		if oldRole != machinecontract.ScopeRoleIndex && !authored && !(layout == cognition.LayoutVolumesV1 && entries[rel] == nil) {
			return nil, fmt.Errorf("managed_scope_authoring_candidate_required: %s", rel)
		}
		if oldRole == machinecontract.ScopeRoleIndex && oldFingerprint.SHA256 != fingerprint.SHA256 && !authored {
			return nil, fmt.Errorf("managed_scope_index_source_stale: %s", rel)
		}
	}
	removalPaths := []string{}
	for _, rel := range sortedEntryPaths(entries) {
		entry := entries[rel]
		if desiredRoles[rel] == machinecontract.ScopeRoleIndex {
			continue
		}
		removalPaths = append(removalPaths, rel)
		disposition, ok := dispositionByPath[rel]
		if !ok {
			return nil, fmt.Errorf("scope_entry_disposition_required: %s", rel)
		}
		if err := validateDisposition(disposition, entry, desiredRoles[rel], entries, entryCandidateByPath, candidates.Header); err != nil {
			return nil, err
		}
		plan.RetentionReview = append(plan.RetentionReview, disposition)
		plan.EntryRemoves = append(plan.EntryRemoves, EntryChange{Path: rel, Action: "remove", BeforeSHA256: entrySHA(entry.FullLine)})
	}
	sort.Strings(removalPaths)
	for _, rel := range removalPaths {
		entry := entries[rel]
		projected, err = index.RemoveEntryForPath(projected, root, rel, entry.FullLine)
		if err != nil {
			return nil, fmt.Errorf("managed_scope_entry_remove_failed: %s", rel)
		}
	}
	for _, rel := range sortedCandidatePaths(entryCandidateByPath) {
		candidate := entryCandidateByPath[rel]
		if desiredRoles[rel] != machinecontract.ScopeRoleIndex {
			return nil, fmt.Errorf("managed_scope_candidate_target_not_index: %s", rel)
		}
		fingerprint, ok := desiredSnapshot[rel]
		if !ok || fingerprint.SHA256 != candidate.SourceSHA256 {
			return nil, fmt.Errorf("managed_scope_candidate_source_digest_mismatch: %s", rel)
		}
		if err := validateEntryCandidate(root, candidate, budgetPolicy); err != nil {
			return nil, err
		}
		current, exists := entries[rel]
		if exists {
			if candidate.CurrentEntrySHA256 == "" || candidate.CurrentEntrySHA256 != entrySHA(current.FullLine) {
				return nil, fmt.Errorf("managed_scope_candidate_entry_preimage_mismatch: %s", rel)
			}
			projected, err = index.ReplaceEntryForPath(projected, root, rel, current.FullLine, candidate.NewEntry)
			if err != nil {
				return nil, fmt.Errorf("managed_scope_entry_update_failed: %s", rel)
			}
			plan.EntryUpdates = append(plan.EntryUpdates, EntryChange{Path: rel, Action: "update",
				BeforeSHA256: entrySHA(current.FullLine), AfterSHA256: entrySHA(candidate.NewEntry)})
		} else {
			if candidate.CurrentEntrySHA256 != "" {
				return nil, fmt.Errorf("managed_scope_candidate_unexpected_entry_preimage: %s", rel)
			}
			projected, err = index.InsertEntry(projected, rel, candidate.NewEntry, root)
			if err != nil {
				return nil, fmt.Errorf("managed_scope_entry_create_failed: %s", rel)
			}
			plan.EntryCreates = append(plan.EntryCreates, EntryChange{Path: rel, Action: "create", AfterSHA256: entrySHA(candidate.NewEntry)})
		}
	}
	for _, rel := range sortedRolePaths(desiredRoles) {
		if desiredRoles[rel] != machinecontract.ScopeRoleIndex {
			continue
		}
		if _, existed := entries[rel]; existed {
			continue
		}
		// A freshly scanned onboarding skeleton can already govern a stable
		// source as index before the model authors its Entry. An observe-only
		// acknowledgement must preserve that existing authoring debt instead
		// of widening its write set. Role additions and stale index sources are
		// rejected by the source-bound checks above and still require candidates.
		if oldRoles[rel] == machinecontract.ScopeRoleIndex {
			continue
		}
		if _, supplied := entryCandidateByPath[rel]; !supplied && layout != cognition.LayoutVolumesV1 {
			return nil, fmt.Errorf("managed_scope_authoring_candidate_required: %s", rel)
		}
	}
	if candidates.Header != nil {
		currentHeader, _ := index.ExtractHeader(projected)
		if digestBytes([]byte(currentHeader)) != candidates.Header.CurrentHeaderSHA256 || candidates.Header.ReviewStatus != ReviewStatusReviewed {
			return nil, fmt.Errorf("managed_scope_header_candidate_preimage_mismatch")
		}
		projected, err = index.ReplaceHeader(projected, candidates.Header.NewHeader)
		if err != nil {
			return nil, fmt.Errorf("managed_scope_header_candidate_invalid")
		}
	}
	projectedBytes := []byte(projected)
	beforeReport, err := cognitionbudget.Build(root, indexBytes, budgetPolicy)
	if err != nil {
		return nil, err
	}
	projection, err := cognitionbudget.ValidateProjected(root, indexBytes, projectedBytes, budgetPolicy)
	if err != nil {
		return nil, err
	}
	if len(projection.Violations) > 0 {
		return nil, fmt.Errorf("whole_index_budget_exceeded: current=%d projected=%d target=%d warning=%d max=%d delta=%d violations=%d suggested=%v",
			projection.CurrentTokens, projection.ProjectedWholeIndexTokens, projection.TargetTokens,
			projection.WarningTokens, projection.MaxTokens, projection.BatchDeltaTokens, len(projection.Violations), projection.SuggestedCompression)
	}
	plan.WholeIndexBefore = *beforeReport
	afterReport, err := cognitionbudget.Build(root, projectedBytes, budgetPolicy)
	if err != nil {
		return nil, err
	}
	plan.WholeIndexAfter = *afterReport
	highRiskApprovalDigest := safetyApprovalDigest(candidates.SafetyApproval)
	if observeReviewOnly && oldBaseline.ManagedScope != nil {
		highRiskApprovalDigest = oldBaseline.ManagedScope.HighRiskApprovalDigest
	}
	postBaseline := baseline.Baseline{Version: oldBaseline.Version, CreatedAt: oldBaseline.CreatedAt,
		UpdatedAt: preparedAt, Files: cloneFingerprints(desiredSnapshot), ManagedScope: &baseline.ManagedScopeState{
			Version: machinecontract.ManagedScopeBaselineV1, PolicyIdentity: policyIdentity,
			ObserveChangePolicy: policy.ObserveChangePolicy, BudgetPolicyIdentity: budgetIdentity,
			BudgetPolicy:           cloneBudgetPolicy(budgetPolicy),
			ApplyAuthorizationMode: authorizationPolicy.EffectiveMode,
			HighRiskApprovalDigest: highRiskApprovalDigest},
		DatabaseCognition: cloneDatabaseBindings(oldBaseline.DatabaseCognition)}
	if _, indexed := postBaseline.Files[cfg.IndexPath]; indexed {
		fingerprint := baseline.HashBytes(cfg.IndexPath, projectedBytes)
		fingerprint.Role = machinecontract.ScopeRoleIndex
		postBaseline.Files[cfg.IndexPath] = fingerprint
	}
	baselinePostBytes, err := baseline.MarshalExact(&postBaseline)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_baseline_postimage_invalid: %w", err)
	}
	plan.BaselineAdded, plan.BaselineRemoved, plan.ObserveFingerprintAdded, plan.ObserveFingerprintRemoved = baselineDeltas(oldBaseline.Files, postBaseline.Files)
	plan.Risk = buildRisk(plan, cfg, oldBaseline)
	switch plan.AuthorizationPolicy.EffectiveMode {
	case machinecontract.ApplyAuthorizationReview:
		plan.InteractionRequired = true
	case machinecontract.ApplyAuthorizationLegacy:
		plan.InteractionRequired = len(plan.EntryRemoves) > 0 || plan.Risk.LargeReduction || plan.Risk.HighRiskOptIn ||
			plan.Risk.BudgetPolicyChange || plan.Risk.BudgetRelaxation || plan.Risk.ApprovalPolicyRelaxation
	case machinecontract.ApplyAuthorizationAuto, machinecontract.ApplyAuthorizationOff:
		plan.InteractionRequired = false
	default:
		return nil, fmt.Errorf("managed_scope_apply_authorization_policy_invalid")
	}
	plan.PlanID, err = planIdentity(plan)
	if err != nil {
		return nil, err
	}
	if plan.InteractionRequired {
		plan.ConfirmationPhrase = "APPLY MANAGED SCOPE " + plan.PlanID
	}
	preview := &Preview{Version: machinecontract.ManagedScopeChangePreviewV2,
		EnvelopeVersion: machinecontract.ManagedScopeChangeEnvelopeV2, Plan: plan, CandidateSet: candidates,
		Evaluation: *evaluation, SourceGuard: sourceGuard, IndexPostimage: formalImage(cfg.IndexPath, indexBytes, projectedBytes),
		ConfigPostimage:   formalImage(".aoci/config.json", configBytes, configBytes),
		BaselinePostimage: formalImage(".aoci/baseline.json", baselineBytes, baselinePostBytes), Baseline: postBaseline,
		CurationExclusions: append([]string{}, curationExclude...), NetworkAccessed: false}
	if curationExists {
		image := formalImage(".aoci/curation.json", curationBytes, curationPostBytes)
		preview.CurationPostimage = &image
	}
	physicalIdentities := []string{}
	for _, image := range formalImages(preview) {
		physicalIdentities = append(physicalIdentities, image.Path, image.PreimageSHA256, image.PostimageSHA256)
	}
	preview.PhysicalDiffSHA256 = digestStrings(physicalIdentities)
	preview.SemanticDiffSHA256, _ = digestJSON(candidates)
	preview.RiskDiffSHA256, _ = digestJSON(plan.Risk)
	preview.PreviewID, err = previewIdentity(*preview)
	if err != nil {
		return nil, err
	}
	preview.EnvelopeDigest = preview.PreviewID
	return preview, nil
}

func normalizeCandidateSet(value CandidateSet) (CandidateSet, error) {
	if value.Version == "" {
		value.Version = machinecontract.ManagedScopeCandidateSetV1
	}
	if value.Version != machinecontract.ManagedScopeCandidateSetV1 {
		return CandidateSet{}, fmt.Errorf("managed_scope_candidate_set_version_unsupported")
	}
	if value.Entries == nil {
		value.Entries = []EntryCandidate{}
	}
	if value.Dispositions == nil {
		value.Dispositions = []EntryDisposition{}
	}
	if value.Curation != nil {
		normalized, err := curation.NormalizeDocument(value.Curation, true)
		if err != nil {
			return CandidateSet{}, fmt.Errorf("managed_scope_curation_candidate_invalid: %w", err)
		}
		value.Curation = normalized
	}
	if value.ObserveReview != nil {
		value.ObserveReview.Paths = append([]string{}, value.ObserveReview.Paths...)
		sort.Strings(value.ObserveReview.Paths)
		value.ObserveReview.Paths = deduplicate(value.ObserveReview.Paths)
	}
	if value.SafetyApproval != nil {
		value.SafetyApproval.ExactPaths = append([]string{}, value.SafetyApproval.ExactPaths...)
		sort.Strings(value.SafetyApproval.ExactPaths)
		value.SafetyApproval.ExactPaths = deduplicate(value.SafetyApproval.ExactPaths)
	}
	sort.Slice(value.Entries, func(i, j int) bool { return value.Entries[i].Path < value.Entries[j].Path })
	sort.Slice(value.Dispositions, func(i, j int) bool { return value.Dispositions[i].SourcePath < value.Dispositions[j].SourcePath })
	return value, nil
}

func activeCurationExclusions(configured []string, document *curation.Document, active *baseline.Baseline) []string {
	result := append([]string{}, configured...)
	if document != nil && active != nil {
		for _, decision := range document.Decisions {
			fingerprint, exists := active.Files[decision.Path]
			if decision.Decision == curation.DecisionExclude && exists && fingerprint.SHA256 == decision.SourceSHA256 {
				result = append(result, decision.Path)
			}
		}
	}
	sort.Strings(result)
	return deduplicate(result)
}

func entriesByPath(document *index.Document) (map[string]*index.Entry, error) {
	result := map[string]*index.Entry{}
	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			if entry.RelPath == "" {
				return nil, fmt.Errorf("managed_scope_entry_path_unresolved")
			}
			if _, exists := result[entry.RelPath]; exists {
				return nil, fmt.Errorf("managed_scope_entry_path_duplicate: %s", entry.RelPath)
			}
			result[entry.RelPath] = entry
		}
	}
	return result, nil
}

func evaluationRoles(evaluation *managedscope.Evaluation) map[string]string {
	result := map[string]string{}
	for _, group := range [][]managedscope.PathEvaluation{evaluation.Index, evaluation.Observe, evaluation.Exclude} {
		for _, item := range group {
			result[item.Path] = item.Role
		}
	}
	return result
}

// transactionRelevantEvaluation removes new untracked hard-excluded runtime
// names from the formal envelope. Exclude means no drift: lock files, logs, or
// caches appearing during Apply cannot invalidate a Scope transaction. A
// safety-excluded path remains present when it was governed before or is Git
// tracked, because removing an existing Entry/fingerprint or explaining a new
// tracked sensitive path is a real formal change.
func transactionRelevantEvaluation(value *managedscope.Evaluation, active *baseline.Baseline) *managedscope.Evaluation {
	if value == nil {
		return nil
	}
	result := *value
	result.Index = append([]managedscope.PathEvaluation{}, value.Index...)
	result.Observe = append([]managedscope.PathEvaluation{}, value.Observe...)
	result.Exclude = make([]managedscope.PathEvaluation, 0, len(value.Exclude))
	for _, item := range value.Exclude {
		previouslyGoverned := false
		if active != nil {
			_, previouslyGoverned = active.Files[item.Path]
		}
		if item.RuleSource == machinecontract.ScopeRuleSafety && !previouslyGoverned && item.GitStatus != "tracked" {
			continue
		}
		result.Exclude = append(result.Exclude, item)
	}
	result.IndexCount, result.ObserveCount, result.ExcludeCount = len(result.Index), len(result.Observe), len(result.Exclude)
	result.SafetyExcluded = 0
	summary := value.SafeInventory
	summary.Ignored = 0
	summary.NonignoredUntracked = 0
	summary.BuiltinSensitiveExcluded, summary.RuntimeExcluded, summary.GeneratedExcluded = 0, 0, 0
	summary.ConfiguredExcluded, summary.CurationExcluded, summary.UnsafeFilesystemExcluded = 0, 0, 0
	for _, item := range result.Exclude {
		if item.RuleSource != machinecontract.ScopeRuleSafety {
			continue
		}
		result.SafetyExcluded++
		switch item.SafetyStatus {
		case afs.SafetySensitive:
			summary.BuiltinSensitiveExcluded++
		case afs.SafetyRuntime:
			summary.RuntimeExcluded++
		case afs.SafetyGenerated:
			summary.GeneratedExcluded++
		case afs.SafetyConfigured:
			summary.ConfiguredExcluded++
		case afs.SafetyUnsafe:
			summary.UnsafeFilesystemExcluded++
		}
	}
	result.SafeInventory = summary
	return &result
}

func baselineRoles(value *baseline.Baseline) map[string]string {
	result := map[string]string{}
	if value != nil {
		for path, fingerprint := range value.Files {
			result[path] = baseline.EffectiveRole(fingerprint)
		}
	}
	return result
}

func baselineRolesExcept(value *baseline.Baseline, excluded map[string]baseline.Fingerprint) map[string]string {
	result := baselineRoles(value)
	for path := range excluded {
		delete(result, path)
	}
	return result
}

// FormalCognitionBaselineGuards returns only formal Cognition fingerprints that
// already belong to the active Baseline. It never enrolls a new formal asset:
// Bootstrap, Migration, or Database Bootstrap remain the sole lifecycle
// owners. A stored fingerprint must still match the live asset so Scope Change
// cannot preserve stale formal state or hide a concurrent Volume update.
//
// Database Bootstrap versions before the Root/Baseline binding fix advanced the
// live Root by one canonical Database descriptor while leaving the old Root
// fingerprint in Baseline. That historical state is accepted only when removing
// that exact descriptor reconstructs the stored Root bytes and the declared
// Database Volume itself is already present and Baseline-current. The Scope
// transaction then advances only the Root fingerprint; arbitrary Root drift
// still fails closed.
func FormalCognitionBaselineGuards(root, indexPath string, active *baseline.Baseline) (map[string]baseline.Fingerprint, error) {
	result := map[string]baseline.Fingerprint{}
	if active == nil {
		return result, nil
	}
	set, err := cognition.Load(root, indexPath)
	if err != nil {
		return nil, fmt.Errorf("managed_scope_cognition_layout_invalid: %w", err)
	}
	if set.LayoutMode != cognition.LayoutVolumesV1 {
		return result, nil
	}
	for _, id := range set.DeclaredOrder {
		asset := set.Volumes[id]
		if asset == nil || asset.State != cognition.AssetPresent {
			continue
		}
		stored, exists := active.Files[asset.Descriptor.Path]
		if !exists {
			continue
		}
		current, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(asset.Descriptor.Path)))
		if hashErr != nil || current.SHA256 != stored.SHA256 {
			return nil, fmt.Errorf("managed_scope_formal_volume_baseline_drift: %s", asset.Descriptor.Path)
		}
		result[asset.Descriptor.Path] = stored
	}
	storedRoot, rootManaged := active.Files[indexPath]
	if !rootManaged {
		return result, nil
	}
	currentRoot, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(indexPath)))
	if hashErr != nil {
		return nil, fmt.Errorf("managed_scope_formal_volume_baseline_drift: %s", indexPath)
	}
	if currentRoot.SHA256 == storedRoot.SHA256 {
		storedRoot.Role = machinecontract.ScopeRoleIndex
		result[indexPath] = storedRoot
		return result, nil
	}
	if !legacyDatabaseBootstrapRootAdvance(set, active, indexPath, storedRoot) {
		return nil, fmt.Errorf("managed_scope_formal_volume_baseline_drift: %s", indexPath)
	}
	currentRoot.Role = machinecontract.ScopeRoleIndex
	result[indexPath] = currentRoot
	return result, nil
}

func legacyDatabaseBootstrapRootAdvance(set *cognition.Set, active *baseline.Baseline, indexPath string, storedRoot baseline.Fingerprint) bool {
	if set == nil || active == nil || set.LayoutMode != cognition.LayoutVolumesV1 || set.Root.State != cognition.AssetPresent {
		return false
	}
	database := set.Volumes[cognition.ScopeDatabase]
	if database == nil || database.State != cognition.AssetPresent || database.Descriptor.ID != cognition.ScopeDatabase ||
		database.Descriptor.Kind != cognition.ScopeDatabase || database.Descriptor.Path != "aoci.database.txt" ||
		database.Descriptor.FormatVersion != "table-fras-v2" || database.Descriptor.State != machinecontract.CognitionVolumeEnabled ||
		len(database.Descriptor.DependsOn) != 1 || database.Descriptor.DependsOn[0] != cognition.ScopeMeta {
		return false
	}
	databaseBaseline, exists := active.Files[database.Descriptor.Path]
	if !exists || databaseBaseline.SHA256 != database.SHA256 {
		return false
	}
	separator := "\n"
	if bytes.Contains(set.Root.Raw, []byte("\r\n")) {
		separator = "\r\n"
	}
	descriptor := "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled"
	parts := bytes.Split(set.Root.Raw, []byte(separator))
	descriptorIndex := -1
	for index, part := range parts {
		if string(part) != descriptor {
			continue
		}
		if descriptorIndex >= 0 {
			return false
		}
		descriptorIndex = index
	}
	if descriptorIndex < 0 {
		return false
	}
	preimageParts := append([][]byte{}, parts[:descriptorIndex]...)
	preimageParts = append(preimageParts, parts[descriptorIndex+1:]...)
	preimage := bytes.Join(preimageParts, []byte(separator))
	if baseline.HashBytes(indexPath, preimage).SHA256 != storedRoot.SHA256 {
		return false
	}
	replayed, err := replayLegacyDatabaseDescriptor(preimage)
	return err == nil && bytes.Equal(replayed, set.Root.Raw)
}

// replayLegacyDatabaseDescriptor intentionally mirrors the historical
// Database Bootstrap insertion algorithm. Compatibility is granted only when
// that exact transition reproduces the live Root bytes; moving the canonical
// line elsewhere is not treated as an old transaction postimage.
func replayLegacyDatabaseDescriptor(root []byte) ([]byte, error) {
	text := string(root)
	const descriptor = "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled"
	if strings.Contains(text, "id=database") || strings.Contains(text, descriptor) {
		return nil, errors.New("database descriptor conflict")
	}
	separator := "\n"
	if strings.Contains(text, "\r\n") {
		separator = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	insert := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "#Volume:") {
			insert = index + 1
		}
	}
	if insert < 0 {
		return nil, errors.New("root descriptors missing")
	}
	lines = append(lines, "")
	copy(lines[insert+1:], lines[insert:])
	lines[insert] = descriptor
	return []byte(strings.Join(lines, separator)), nil
}

func validateSourcesPresent(root string, active *baseline.Baseline, desired map[string]string) error {
	for rel := range active.Files {
		if _, exists := desired[rel]; exists {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); errors.Is(err, os.ErrNotExist) {
			// A missing previously indexed source is a valid index-to-exclude
			// transition. The mandatory Entry disposition and human-approved
			// transaction below preserve its semantic and governance boundary.
			continue
		} else if err != nil {
			return fmt.Errorf("managed_scope_source_unreadable: %s", rel)
		}
	}
	return nil
}

func observedEvidenceChanges(active *baseline.Baseline, desired map[string]baseline.Fingerprint, roles map[string]string, policyAligned bool) []string {
	changes := []string{}
	for path, fingerprint := range desired {
		if roles[path] != machinecontract.ScopeRoleObserve {
			continue
		}
		old, exists := active.Files[path]
		if exists && baseline.EffectiveRole(old) == machinecontract.ScopeRoleObserve && old.SHA256 != fingerprint.SHA256 {
			changes = append(changes, path)
			continue
		}
		// A new observe path is evidence drift only while the active policy is
		// unchanged. During a policy transition it is an explicit Scope Add and
		// its initial fingerprint is reviewed as part of that Plan.
		if policyAligned && (!exists || baseline.EffectiveRole(old) != machinecontract.ScopeRoleObserve) {
			changes = append(changes, path)
		}
	}
	for path, old := range active.Files {
		if baseline.EffectiveRole(old) != machinecontract.ScopeRoleObserve {
			continue
		}
		if _, exists := roles[path]; !exists {
			changes = append(changes, path)
		}
	}
	sort.Strings(changes)
	return deduplicate(changes)
}

func observeOnlyProjection(
	active *baseline.Baseline,
	desired map[string]baseline.Fingerprint,
	roles map[string]string,
) (map[string]baseline.Fingerprint, map[string]string) {
	projected := cloneFingerprints(active.Files)
	projectedRoles := baselineRoles(active)
	for path, fingerprint := range desired {
		if roles[path] != machinecontract.ScopeRoleObserve {
			continue
		}
		projected[path] = fingerprint
		projectedRoles[path] = machinecontract.ScopeRoleObserve
	}
	for path, fingerprint := range active.Files {
		if baseline.EffectiveRole(fingerprint) == machinecontract.ScopeRoleObserve &&
			roles[path] != machinecontract.ScopeRoleObserve {
			delete(projected, path)
			delete(projectedRoles, path)
		}
	}
	return projected, projectedRoles
}

func isObserveReviewOnly(candidates CandidateSet) bool {
	return candidates.ObserveReview != nil && len(candidates.Entries) == 0 &&
		len(candidates.Dispositions) == 0 && candidates.Header == nil && candidates.Curation == nil &&
		candidates.SafetyApproval == nil
}

func equalSortedPaths(left, right []string) bool {
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

func unionRolePaths(left, right map[string]string) []string {
	seen := map[string]bool{}
	for path := range left {
		seen[path] = true
	}
	for path := range right {
		seen[path] = true
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func appendRoleDelta(plan *Plan, rel, oldRole, newRole, sha string) {
	oldObject := ScopeObject{Path: rel, Role: oldRole, SourceSHA256: sha}
	newObject := ScopeObject{Path: rel, Role: newRole, SourceSHA256: sha}
	switch oldRole {
	case machinecontract.ScopeRoleIndex:
		plan.IndexRemoved = append(plan.IndexRemoved, oldObject)
	case machinecontract.ScopeRoleObserve:
		plan.ObserveRemoved = append(plan.ObserveRemoved, oldObject)
	case machinecontract.ScopeRoleExclude:
		plan.ExcludeRemoved = append(plan.ExcludeRemoved, oldObject)
	}
	switch newRole {
	case machinecontract.ScopeRoleIndex:
		plan.IndexAdded = append(plan.IndexAdded, newObject)
	case machinecontract.ScopeRoleObserve:
		plan.ObserveAdded = append(plan.ObserveAdded, newObject)
	case machinecontract.ScopeRoleExclude:
		plan.ExcludeAdded = append(plan.ExcludeAdded, newObject)
	}
}

func candidateMap(values []EntryCandidate) (map[string]EntryCandidate, error) {
	result := map[string]EntryCandidate{}
	ids := map[string]bool{}
	for _, value := range values {
		rel, err := afs.NormalizeRelPath(value.Path)
		if err != nil || rel != value.Path ||
			value.CandidateID == "" || ids[value.CandidateID] || value.ReviewStatus != ReviewStatusReviewed {
			return nil, fmt.Errorf("managed_scope_entry_candidate_invalid: %s", value.Path)
		}
		if _, exists := result[value.Path]; exists {
			return nil, fmt.Errorf("managed_scope_entry_candidate_duplicate: %s", value.Path)
		}
		ids[value.CandidateID] = true
		result[value.Path] = value
	}
	return result, nil
}

func dispositionMap(values []EntryDisposition) (map[string]EntryDisposition, error) {
	result := map[string]EntryDisposition{}
	for _, value := range values {
		if value.Version != machinecontract.ScopeEntryDispositionV1 || value.SourcePath == "" ||
			value.CurrentEntrySHA256 == "" || value.UniqueSemantics == nil || value.ReviewStatus != ReviewStatusReviewed || value.Reviewer == "" {
			return nil, fmt.Errorf("scope_entry_disposition_invalid: %s", value.SourcePath)
		}
		if _, exists := result[value.SourcePath]; exists {
			return nil, fmt.Errorf("scope_entry_disposition_duplicate: %s", value.SourcePath)
		}
		result[value.SourcePath] = value
	}
	return result, nil
}

func validateDisposition(value EntryDisposition, current *index.Entry, targetRole string,
	entries map[string]*index.Entry, candidates map[string]EntryCandidate, header *HeaderCandidate) error {
	if current == nil || value.CurrentEntrySHA256 != entrySHA(current.FullLine) || value.TargetRole != targetRole {
		return fmt.Errorf("scope_entry_disposition_binding_invalid: %s", value.SourcePath)
	}
	switch value.Disposition {
	case DispositionNoUniqueSemantics:
		if len(value.UniqueSemantics) != 0 || value.TargetEntry != "" {
			return fmt.Errorf("scope_entry_disposition_invalid: %s", value.SourcePath)
		}
	case DispositionTransferEntry, DispositionTransferSpec:
		if value.TargetEntry == "" || entries[value.TargetEntry] == nil {
			return fmt.Errorf("scope_entry_disposition_target_missing: %s", value.SourcePath)
		}
		if _, updated := candidates[value.TargetEntry]; !updated || len(value.UniqueSemantics) == 0 {
			return fmt.Errorf("scope_entry_disposition_transfer_candidate_required: %s", value.SourcePath)
		}
	case DispositionTransferHeader:
		if header == nil || header.ReviewStatus != ReviewStatusReviewed || len(value.UniqueSemantics) == 0 {
			return fmt.Errorf("scope_entry_disposition_header_candidate_required: %s", value.SourcePath)
		}
	case DispositionExplicitDrop:
		if len(value.UniqueSemantics) == 0 {
			return fmt.Errorf("scope_entry_disposition_drop_reason_required: %s", value.SourcePath)
		}
	case DispositionRetainIndex:
		return fmt.Errorf("scope_entry_disposition_requires_policy_revision: %s", value.SourcePath)
	default:
		return fmt.Errorf("scope_entry_disposition_kind_invalid: %s", value.SourcePath)
	}
	return nil
}

func validateEntryCandidate(root string, value EntryCandidate, policy cognitionbudget.Policy) error {
	line := index.StripFences(value.NewEntry)
	if line != value.NewEntry || index.HasError(index.ValidateEntryLine(value.Path, value.NewEntry)) {
		return fmt.Errorf("managed_scope_entry_candidate_format_invalid: %s", value.Path)
	}
	violations := cognitionbudget.ValidateEntry(value.NewEntry, policy)
	if len(violations) != 0 {
		return fmt.Errorf("entry_field_budget_exceeded: %s", value.Path)
	}
	for _, violation := range index.ValidateEntryRelations(root, value.Path, value.NewEntry) {
		if violation.Level == index.LevelError {
			return fmt.Errorf("managed_scope_entry_candidate_relation_invalid: %s", value.Path)
		}
	}
	return nil
}

func sortedCandidatePaths(values map[string]EntryCandidate) []string {
	result := make([]string, 0, len(values))
	for path := range values {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func sortedEntryPaths(values map[string]*index.Entry) []string {
	result := make([]string, 0, len(values))
	for path := range values {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func sortedFingerprintPaths(values map[string]baseline.Fingerprint) []string {
	result := make([]string, 0, len(values))
	for path := range values {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func sortedRolePaths(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for path := range values {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func baselineDeltas(before, after map[string]baseline.Fingerprint) (added, removed, observeAdded, observeRemoved []ScopeObject) {
	added, removed, observeAdded, observeRemoved = []ScopeObject{}, []ScopeObject{}, []ScopeObject{}, []ScopeObject{}
	for _, path := range unionFingerprintPaths(before, after) {
		old, hadOld := before[path]
		newValue, hasNew := after[path]
		if !hadOld && hasNew {
			added = append(added, ScopeObject{Path: path, Role: baseline.EffectiveRole(newValue), SourceSHA256: newValue.SHA256})
		}
		if hadOld && !hasNew {
			removed = append(removed, ScopeObject{Path: path, Role: baseline.EffectiveRole(old), SourceSHA256: old.SHA256})
		}
		oldObserve := hadOld && baseline.EffectiveRole(old) == machinecontract.ScopeRoleObserve
		newObserve := hasNew && baseline.EffectiveRole(newValue) == machinecontract.ScopeRoleObserve
		if !oldObserve && newObserve {
			observeAdded = append(observeAdded, ScopeObject{Path: path, Role: machinecontract.ScopeRoleObserve, SourceSHA256: newValue.SHA256})
		}
		if oldObserve && !newObserve {
			observeRemoved = append(observeRemoved, ScopeObject{Path: path, Role: machinecontract.ScopeRoleObserve, SourceSHA256: old.SHA256})
		}
	}
	return
}

func unionFingerprintPaths(left, right map[string]baseline.Fingerprint) []string {
	seen := map[string]bool{}
	for path := range left {
		seen[path] = true
	}
	for path := range right {
		seen[path] = true
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func managedScopeEvaluationsByPath(evaluation *managedscope.Evaluation) map[string]*managedscope.PathEvaluation {
	result := map[string]*managedscope.PathEvaluation{}
	if evaluation == nil {
		return result
	}
	for _, group := range [][]managedscope.PathEvaluation{evaluation.Index, evaluation.Observe, evaluation.Exclude} {
		for index := range group {
			item := group[index]
			copyItem := item
			result[item.Path] = &copyItem
		}
	}
	return result
}

func coverageAuthoringState(path string, entries map[string]*index.Entry, old *baseline.Baseline, desired map[string]baseline.Fingerprint) string {
	if entries[path] == nil {
		return "missing"
	}
	if old == nil {
		return "unbaselined"
	}
	previous, exists := old.Files[path]
	if !exists {
		return "unbaselined"
	}
	if current, exists := desired[path]; exists && current.SHA256 != previous.SHA256 {
		return "stale"
	}
	return "current"
}

func buildRisk(plan Plan, cfg *config.Config, old *baseline.Baseline) Risk {
	thresholds := cfg.EffectiveManagedScope().ApprovalThresholds
	largeByPercent := plan.WholeIndexBefore.EntryCount > 0 &&
		len(plan.EntryRemoves)*100 >= plan.WholeIndexBefore.EntryCount*thresholds.EntryRemovalPercent
	risk := Risk{Level: "low", EntryRemovalCount: len(plan.EntryRemoves),
		EntryRemovalThreshold: thresholds.EntryRemovalCount, EntryRemovalPercentThreshold: thresholds.EntryRemovalPercent,
		LargeReduction: len(plan.EntryRemoves) >= thresholds.EntryRemovalCount || largeByPercent,
		HighRiskOptIn:  len(cfg.SafeInventoryHighRiskOptIn) > 0}
	if len(plan.CoverageReductions) > 0 {
		risk.CognitionCoverageReduction = true
		risk.CoverageReductionCount = len(plan.CoverageReductions)
		risk.P1++
		for _, reduction := range plan.CoverageReductions {
			if reduction.DecisionBasis == machinecontract.ScopeDecisionTransportConstraint {
				risk.TransportConstraintNotAllowed = true
				break
			}
		}
	}
	for _, item := range plan.EntryRemoves {
		if !strings.Contains(item.Path, "/") || strings.HasPrefix(item.Path, "cmd/") || strings.HasPrefix(item.Path, "internal/") {
			risk.RootOrPrimaryReduction = true
			break
		}
	}
	if old != nil && old.ManagedScope == nil {
		legacy := cognitionbudget.LegacyPolicy()
		legacyIdentity, _ := cognitionbudget.Identity(legacy)
		risk.BudgetPolicyChange = legacyIdentity != plan.NewBudgetPolicyIdentity
		risk.BudgetRelaxation = budgetRelaxed(legacy, cfg.EffectiveCognitionBudget())
	} else if old != nil && old.ManagedScope != nil && old.ManagedScope.BudgetPolicyIdentity != plan.NewBudgetPolicyIdentity {
		risk.BudgetPolicyChange = true
		if old.ManagedScope.BudgetPolicy == nil {
			// Older receipts carry only an identity, so direction cannot be proven.
			// Auto authorization fails closed until a reviewed migration records the
			// full applied policy.
			risk.BudgetRelaxation = true
		} else {
			risk.BudgetRelaxation = budgetRelaxed(*old.ManagedScope.BudgetPolicy, cfg.EffectiveCognitionBudget())
		}
	}
	risk.ApprovalPolicyRelaxation = approvalRelaxed(old, plan.AuthorizationPolicy.EffectiveMode)
	if risk.EntryRemovalCount > 0 || risk.LargeReduction || risk.RootOrPrimaryReduction || risk.HighRiskOptIn ||
		risk.BudgetPolicyChange || risk.BudgetRelaxation || risk.ApprovalPolicyRelaxation ||
		risk.CognitionCoverageReduction {
		risk.Level = "high"
	}
	return risk
}

// applyAuthorizationStrength 给生效授权模式排序,用于证明模式跃迁的方向。
// off 完全禁止 Apply,review 要求真人确认,legacy 只在高风险面确认,auto 不确认。
func applyAuthorizationStrength(mode string) int {
	switch mode {
	case machinecontract.ApplyAuthorizationOff:
		return 3
	case machinecontract.ApplyAuthorizationReview:
		return 2
	case machinecontract.ApplyAuthorizationLegacy:
		return 1
	case machinecontract.ApplyAuthorizationAuto:
		return 0
	}
	return -1
}

// approvalRelaxed 判定本次事务是否在放松治理姿态本身。
//
// 授权是在生效模式下做出的,所以"改模式"这件事必须按上一份收据记录的模式来
// 判定,否则 review→auto 的跃迁会在新的 auto 模式下自我批准 —— 无人参与即可
// 解除团队的复核姿态(审查修正)。
//
// 迁移边界(明示取舍): 尚未建立受管收据、或收据早于本字段(留空)时,上一次的
// 姿态不可知,此处不追溯阻断 —— 治理是流程控制而非操作系统级边界,若为不可知
// 的历史收据一律失败关闭,每个存量仓库升级后的第一笔事务都会突然要求真人批准,
// 代价真实而收益有限。保护从第一份记录了模式的收据开始严格生效,而当前事务
// 本身就会写入该字段。反过来,收据里出现无法识别的模式属篡改或未来格式,
// 方向不可证,失败关闭。
func approvalRelaxed(old *baseline.Baseline, effectiveMode string) bool {
	if old == nil || old.ManagedScope == nil {
		return false
	}
	previous := old.ManagedScope.ApplyAuthorizationMode
	if previous == "" {
		return false
	}
	previousStrength := applyAuthorizationStrength(previous)
	currentStrength := applyAuthorizationStrength(effectiveMode)
	if previousStrength < 0 || currentStrength < 0 {
		return true
	}
	return currentStrength < previousStrength
}

func budgetRelaxed(oldPolicy, newPolicy cognitionbudget.Policy) bool {
	if oldPolicy.Mode == machinecontract.BudgetModeEnforce && newPolicy.Mode == machinecontract.BudgetModeObserve {
		return true
	}
	if newPolicy.WholeIndex.TargetTokens > oldPolicy.WholeIndex.TargetTokens ||
		newPolicy.WholeIndex.WarningTokens > oldPolicy.WholeIndex.WarningTokens ||
		newPolicy.WholeIndex.MaxTokens > oldPolicy.WholeIndex.MaxTokens {
		return true
	}
	for importance := 1; importance <= 9; importance++ {
		oldR, oldROK := cognitionbudget.LimitFor(oldPolicy.R, importance)
		newR, newROK := cognitionbudget.LimitFor(newPolicy.R, importance)
		oldS, oldSOK := cognitionbudget.LimitFor(oldPolicy.S, importance)
		newS, newSOK := cognitionbudget.LimitFor(newPolicy.S, importance)
		if !oldROK || !newROK || !oldSOK || !newSOK || newR.MaxTokens > oldR.MaxTokens || newS.MaxTokens > oldS.MaxTokens {
			return true
		}
	}
	return false
}

func formalImage(path string, before, after []byte) FormalImage {
	return FormalImage{Path: path, PreimageState: "present", PreimageSHA256: digestBytes(before),
		PostimageSHA256: digestBytes(after), PostimageBytes: append([]byte{}, after...)}
}

func cloneFingerprints(source map[string]baseline.Fingerprint) map[string]baseline.Fingerprint {
	result := make(map[string]baseline.Fingerprint, len(source))
	for path, value := range source {
		result[path] = value
	}
	return result
}

func cloneBudgetPolicy(source cognitionbudget.Policy) *cognitionbudget.Policy {
	result := source
	result.R = append([]cognitionbudget.FieldBand{}, source.R...)
	result.S = append([]cognitionbudget.FieldBand{}, source.S...)
	return &result
}

func cloneDatabaseBindings(source *baseline.DatabaseCognitionBindings) *baseline.DatabaseCognitionBindings {
	if source == nil {
		return nil
	}
	result := *source
	result.Entries = append([]baseline.DatabaseCognitionBinding{}, source.Entries...)
	return &result
}

func activePolicyIdentity(value *baseline.Baseline) string {
	if value != nil && value.ManagedScope != nil {
		return value.ManagedScope.PolicyIdentity
	}
	identity, _ := managedscope.Identity(managedscope.LegacyPolicy())
	return identity
}

func safetyApprovalDigest(value *SafetyApproval) string {
	if value == nil {
		return ""
	}
	return value.ApprovalDigest
}

func validateSafetyApproval(policyIdentity string, configured []string, approval *SafetyApproval) error {
	paths := append([]string{}, configured...)
	sort.Strings(paths)
	paths = deduplicate(paths)
	if len(paths) == 0 {
		return nil
	}
	if approval == nil || approval.Version != machinecontract.ManagedScopeSafetyApprovalV1 ||
		approval.PolicyIdentity != policyIdentity || approval.Mechanism != "human_tty_exact_path_confirmation" ||
		!cognitiontxn.ValidAuditActor(approval.Actor) || !equalSortedPaths(approval.ExactPaths, paths) {
		return fmt.Errorf("managed_scope_high_risk_read_approval_required")
	}
	want, err := safetyApprovalIdentity(*approval)
	if err != nil || want != approval.ApprovalDigest {
		return fmt.Errorf("managed_scope_high_risk_read_approval_invalid")
	}
	return nil
}

func NewSafetyApproval(policyIdentity string, exactPaths []string, actor, approvedAt string) (*SafetyApproval, error) {
	parsed, err := time.Parse(time.RFC3339, approvedAt)
	paths := append([]string{}, exactPaths...)
	sort.Strings(paths)
	paths = deduplicate(paths)
	if err != nil || parsed.Location() != time.UTC || !cognitiontxn.ValidAuditActor(actor) || len(paths) == 0 || len(policyIdentity) != 64 {
		return nil, fmt.Errorf("managed_scope_safety_approval_input_invalid")
	}
	for _, path := range paths {
		if normalized, normalizeErr := afs.NormalizeRelPath(path); normalizeErr != nil || normalized != path {
			return nil, fmt.Errorf("managed_scope_safety_approval_path_invalid")
		}
	}
	approval := &SafetyApproval{Version: machinecontract.ManagedScopeSafetyApprovalV1, PolicyIdentity: policyIdentity,
		ExactPaths: paths, Actor: actor, Mechanism: "human_tty_exact_path_confirmation", ApprovedAt: approvedAt}
	approval.ApprovalDigest, err = safetyApprovalIdentity(*approval)
	return approval, err
}

func safetyApprovalIdentity(value SafetyApproval) (string, error) {
	value.ApprovalDigest = ""
	return digestJSON(value)
}

func activeBudgetIdentity(value *baseline.Baseline) string {
	if value != nil && value.ManagedScope != nil {
		return value.ManagedScope.BudgetPolicyIdentity
	}
	return ""
}

func repositoryIdentity(root string) string {
	head, err := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if err != nil {
		return digestStrings([]string{"non-git", filepath.Clean(root)})
	}
	value := strings.TrimSpace(string(head))
	if strings.HasPrefix(value, "ref: ") {
		ref := strings.TrimSpace(strings.TrimPrefix(value, "ref: "))
		if resolved, readErr := os.ReadFile(filepath.Join(root, ".git", filepath.FromSlash(ref))); readErr == nil {
			value = strings.TrimSpace(string(resolved))
		} else if packed, packedErr := os.ReadFile(filepath.Join(root, ".git", "packed-refs")); packedErr == nil {
			for _, line := range strings.Split(string(packed), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[1] == ref {
					value = fields[0]
					break
				}
			}
		}
	}
	return digestStrings([]string{"git", value})
}

func entrySHA(line string) string    { return digestBytes([]byte(strings.TrimSpace(line))) }
func digestBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func digestStrings(values []string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
func digestJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return digestBytes(data), nil
}
func planIdentity(value Plan) (string, error) {
	value.PlanID, value.ConfirmationPhrase, value.PreparedAt = "", "", ""
	return digestJSON(value)
}
func previewIdentity(value Preview) (string, error) {
	value.PreviewID, value.EnvelopeDigest = "", ""
	value.Plan.PreparedAt = ""
	return digestJSON(value)
}
func deduplicate(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func Encode(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func DecodeCandidateSet(data []byte) (*CandidateSet, error) {
	var value CandidateSet
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("managed_scope_candidate_set_invalid")
	}
	normalized, err := normalizeCandidateSet(value)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func DecodeSafetyApproval(data []byte) (*SafetyApproval, error) {
	var value SafetyApproval
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Version != machinecontract.ManagedScopeSafetyApprovalV1 {
		return nil, fmt.Errorf("managed_scope_safety_approval_invalid")
	}
	want, err := safetyApprovalIdentity(value)
	if err != nil || want != value.ApprovalDigest || !cognitiontxn.ValidAuditActor(value.Actor) {
		return nil, fmt.Errorf("managed_scope_safety_approval_identity_invalid")
	}
	return &value, nil
}

func DecodePreview(data []byte) (*Preview, error) {
	var value Preview
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Version != machinecontract.ManagedScopeChangePreviewV2 ||
		value.EnvelopeVersion != machinecontract.ManagedScopeChangeEnvelopeV2 {
		return nil, fmt.Errorf("managed_scope_preview_invalid")
	}
	want, err := previewIdentity(value)
	if err != nil || value.PreviewID != want || value.EnvelopeDigest != want || value.Plan.Version != machinecontract.ManagedScopeChangePlanV2 {
		return nil, fmt.Errorf("managed_scope_preview_identity_invalid")
	}
	for _, image := range formalImages(&value) {
		if image.PostimageSHA256 != digestBytes(image.PostimageBytes) {
			return nil, fmt.Errorf("managed_scope_postimage_identity_invalid: %s", image.Path)
		}
	}
	return &value, nil
}

func formalImages(value *Preview) []FormalImage {
	result := []FormalImage{value.IndexPostimage, value.ConfigPostimage}
	if value.CurationPostimage != nil {
		result = append(result, *value.CurationPostimage)
	}
	result = append(result, value.BaselinePostimage)
	return result
}
