package cognitionplan

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// ValidateCandidate replays discovery, rejects superseded facts, validates all
// candidate bytes in memory, and creates Preview/Diff/Digest artifacts. It has
// no mechanism to accept approval or apply any file.
func ValidateCandidate(repositoryRoot string, suppliedPlan *Plan, candidate *LayoutCandidate) (*Preview, error) {
	before, err := snapshotFormalAssets(repositoryRoot)
	if err != nil {
		return nil, err
	}
	current, err := rebuildPlan(repositoryRoot, suppliedPlan)
	if err != nil {
		return nil, err
	}
	if suppliedPlan.PlanID != current.PlanID || candidate.PlanID != current.PlanID {
		after, snapshotErr := snapshotFormalAssets(repositoryRoot)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		return &Preview{
			Version: machinecontract.CognitionLayoutPreviewV1, Operation: suppliedPlan.Operation,
			Status: machinecontract.CognitionPlannerSuperseded, PlanID: suppliedPlan.PlanID,
			PhysicalDiff: emptyPhysicalDiff(), LogicalDiff: emptyLogicalDiff(),
			Sets: emptyReviewSets(), Risks: []Risk{{Code: "plan_input_drift"}},
			Recovery: emptyRecovery(), FormalAssetProof: compareFormalAssets(before, after),
			NetworkAccessed: false, NextAction: machinecontract.CognitionPlannerSuperseded,
		}, nil
	}
	if current.Status != machinecontract.CognitionPlannerAuthoringRequired {
		return nil, fmt.Errorf("planner_candidate_not_applicable: %s", current.Status)
	}
	preview := &Preview{
		Version: machinecontract.CognitionLayoutPreviewV1, Operation: current.Operation,
		Status: machinecontract.CognitionPlannerInvalid, PlanID: current.PlanID,
		PhysicalDiff: emptyPhysicalDiff(), LogicalDiff: emptyLogicalDiff(), Sets: emptyReviewSets(), Risks: []Risk{},
		Recovery: emptyRecovery(), NetworkAccessed: false, NextAction: machinecontract.CognitionPlannerAuthoringRequired,
	}
	if current.Operation == OperationBootstrap {
		preview.SemanticAuthoringRequirement = SemanticAuthoringRequirementForPlan(current, candidate)
	}

	assets, assetIdentities, assetRisks := validateCandidateEnvelope(current, candidate)
	preview.Risks = append(preview.Risks, assetRisks...)
	provenance, provenanceRisks := validateSemanticAuthoringProvenance(current, candidate)
	preview.SemanticAuthoringProvenance = provenance
	preview.Risks = append(preview.Risks, provenanceRisks...)
	var descriptors []cognition.Descriptor
	var projected *cognition.Set
	if len(preview.Risks) == 0 {
		rootRaw := []byte(assets["root"].Content)
		var findings []cognition.Finding
		descriptors, findings = cognition.ParseProjectedRoot(rootRaw)
		for _, finding := range findings {
			preview.Risks = append(preview.Risks, Risk{Code: finding.Code, Target: finding.AssetID})
		}
		if !descriptorsMatchPlan(descriptors, current.TargetKinds) {
			preview.Risks = append(preview.Risks, Risk{Code: "candidate_topology_mismatch", Target: "root"})
		}
		rootLocale, explicitLocale, localeErr := index.DetectLocale(string(rootRaw))
		if localeErr != nil || !explicitLocale || rootLocale != current.Locale {
			preview.Risks = append(preview.Risks, Risk{Code: "candidate_locale_mismatch", Target: "root"})
		}
		for _, finding := range cognition.ValidateProjectedTargetPaths(repositoryRoot, descriptors) {
			preview.Risks = append(preview.Risks, Risk{Code: finding.Code, Target: finding.AssetID})
		}
		volumeRaw := map[string][]byte{}
		for _, descriptor := range descriptors {
			if asset, exists := assets[descriptor.ID]; exists {
				volumeRaw[descriptor.ID] = []byte(asset.Content)
			}
		}
		if len(preview.Risks) == 0 {
			var projectedFindings []cognition.Finding
			projected, projectedFindings = cognition.BuildProjectedSet(repositoryRoot, rootRaw, volumeRaw)
			for _, finding := range projectedFindings {
				preview.Risks = append(preview.Risks, Risk{Code: finding.Code, Target: finding.AssetID})
			}
		}
	}
	preview.ProjectedDescriptors = append([]cognition.Descriptor{}, descriptors...)
	if projected != nil {
		preview.ProjectedCompositeIdentity = projected.CompositeIdentity
		for _, conflict := range cognition.OwnershipConflicts(projected) {
			preview.Risks = append(preview.Risks, Risk{Code: "volume_ownership_conflict", Target: conflict.ObjectRef})
		}
		preview.Risks = append(preview.Risks, validateObjectCoverage(current, projected, candidate.MappingResolutions)...)
		cfg, configErr := config.LoadReadOnly(repositoryRoot)
		if configErr != nil {
			preview.Risks = append(preview.Risks, Risk{Code: "cognition_budget_policy_invalid"})
		} else {
			budget := volumegovernance.AssessProjectedBudget(cfg, projected)
			if budget.Mode == machinecontract.BudgetModeEnforce {
				for _, violation := range budget.Violations {
					preview.Risks = append(preview.Risks, Risk{Code: violation.Code, Target: violation.Path})
				}
			}
		}
	}

	mapping := cloneMapping(current.Mapping)
	if current.Operation == OperationMigration && mapping != nil && projected != nil {
		mappingRisks := reconcileMigrationMapping(mapping, projected, candidate.MappingResolutions)
		preview.Risks = append(preview.Risks, mappingRisks...)
		preview.SemanticMapping = mapping
	}
	if current.Operation == OperationBootstrap && len(candidate.MappingResolutions) > 0 {
		preview.Risks = append(preview.Risks, Risk{Code: "mapping_resolution_not_applicable"})
	}

	preview.CandidateIdentity = candidateIdentity(candidate)
	preview.PlanID = candidateBoundPlanIdentity(current.PlanID, preview.CandidateIdentity)
	preview.PhysicalDiff = buildPhysicalDiff(current, assets)
	preview.LogicalDiff = buildLogicalDiff(current, projected, mapping)
	if projected != nil {
		sets, setRisks := resolveReviewSets(projected, current)
		preview.Sets = sets
		preview.Risks = append(preview.Risks, setRisks...)
	} else {
		preview.Sets = plannedReviewSets(current)
	}
	preview.Recovery = buildRecoverySummary(current, assets)
	sortRisks(preview.Risks)

	complete := len(preview.Risks) == 0 && projected != nil
	if mapping != nil {
		complete = complete && mappingComplete(mapping)
	}
	if complete {
		preview.Status = machinecontract.CognitionPlannerPreviewReady
		preview.NextAction = "approval_digest_ready_no_apply"
		preview.ApprovalDigest = buildApprovalDigest(current, assetIdentities, mapping, preview)
	}
	after, err := snapshotFormalAssets(repositoryRoot)
	if err != nil {
		return nil, err
	}
	preview.FormalAssetProof = compareFormalAssets(before, after)
	if !preview.FormalAssetProof.FormalAssetsUnchanged {
		return nil, fmt.Errorf("formal_assets_changed_during_candidate_validation")
	}
	return preview, nil
}

func rebuildPlan(repositoryRoot string, plan *Plan) (*Plan, error) {
	options := Options{RepositoryRoot: repositoryRoot, Locale: plan.Locale, TargetKinds: plan.TargetKinds}
	if plan.Operation == OperationBootstrap {
		return BootstrapPlan(options)
	}
	return MigrationPlan(options)
}

func validateCandidateEnvelope(plan *Plan, candidate *LayoutCandidate) (map[string]CandidateAsset, []CandidateAssetIdentity, []Risk) {
	expected := []struct{ id, path string }{{"root", "aoci.txt"}, {"meta", "aoci.meta.txt"}}
	for _, kind := range plan.TargetKinds {
		path := "aoci." + kind + ".txt"
		expected = append(expected, struct{ id, path string }{kind, path})
	}
	assets := make(map[string]CandidateAsset, len(candidate.Assets))
	identities := make([]CandidateAssetIdentity, 0, len(candidate.Assets))
	risks := make([]Risk, 0)
	formalBefore := map[string]FormalAssetState{}
	for _, state := range plan.FormalAssetProof.Before {
		formalBefore[state.Path] = state
	}
	if len(candidate.Assets) != len(expected) {
		risks = append(risks, Risk{Code: "candidate_asset_set_incomplete"})
	}
	for index, asset := range candidate.Assets {
		if _, duplicate := assets[asset.AssetID]; duplicate {
			risks = append(risks, Risk{Code: "candidate_asset_duplicate", Target: asset.AssetID})
			continue
		}
		assets[asset.AssetID] = asset
		if index >= len(expected) || asset.AssetID != expected[index].id || asset.Path != expected[index].path {
			risks = append(risks, Risk{Code: "candidate_asset_order_or_path_invalid", Target: asset.AssetID})
		}
		if asset.Path != "aoci.txt" && formalBefore[asset.Path].Exists {
			risks = append(risks, Risk{Code: "candidate_target_already_exists", Target: asset.Path})
		}
		content := []byte(asset.Content)
		if !utf8.Valid(content) || strings.HasPrefix(asset.Content, "\ufeff") {
			risks = append(risks, Risk{Code: "candidate_asset_encoding_invalid", Target: asset.AssetID})
		}
		if strings.Contains(asset.Content, "MODEL_AUTHORING_REQUIRED") {
			risks = append(risks, Risk{Code: "candidate_authoring_incomplete", Target: asset.AssetID})
		}
		identities = append(identities, CandidateAssetIdentity{AssetID: asset.AssetID, Path: asset.Path, SHA256: hashBytes(content), Bytes: len(content)})
	}
	if root, exists := assets["root"]; exists && (!hasAuthoredRootField(root.Content, "#Project:") || !hasAuthoredRootField(root.Content, "#Global-Invariants:")) {
		risks = append(risks, Risk{Code: "root_semantics_incomplete", Target: "root"})
	}
	return assets, identities, risks
}

func hasAuthoredRootField(content, prefix string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix))
		return value != "" && value != "-" && value != "MODEL_AUTHORING_REQUIRED"
	}
	return false
}

func descriptorsMatchPlan(descriptors []cognition.Descriptor, kinds []string) bool {
	expected := append([]string{"meta"}, kinds...)
	if len(descriptors) != len(expected) {
		return false
	}
	for index := range expected {
		if descriptors[index].ID != expected[index] || descriptors[index].Kind != expected[index] || descriptors[index].State != machinecontract.CognitionVolumeEnabled {
			return false
		}
	}
	return true
}

func validateObjectCoverage(plan *Plan, projected *cognition.Set, resolutions []MappingResolution) []Risk {
	risks := make([]Risk, 0)
	resolutionByUnit := map[string]MappingResolution{}
	for _, resolution := range resolutions {
		resolutionByUnit[resolution.UnitID] = resolution
	}
	expectedCode := map[string]bool{}
	for _, object := range plan.Inventory {
		if object.Eligible {
			expectedCode["code:"+object.Path] = true
		}
	}
	if plan.Mapping != nil {
		for _, record := range plan.Mapping.Records {
			asset, target := record.TargetAsset, record.TargetRef
			if resolution, exists := resolutionByUnit[record.UnitID]; exists && resolution.SemanticReviewed && strings.TrimSpace(resolution.Reviewer) != "" {
				asset, target = resolution.TargetAsset, resolution.TargetRef
			}
			if asset == "code" && target != "" {
				expectedCode[target] = true
			}
		}
	}
	if asset := projected.Volumes["code"]; asset != nil {
		actual := map[string]bool{}
		for _, object := range asset.Objects {
			actual[object.CanonicalRef] = true
		}
		for ref := range expectedCode {
			if !actual[ref] {
				risks = append(risks, Risk{Code: "code_authoring_missing", Target: ref})
			}
		}
		for ref := range actual {
			if !expectedCode[ref] {
				risks = append(risks, Risk{Code: "code_candidate_target_unbound", Target: ref})
			}
		}
	} else if len(expectedCode) > 0 && containsString(plan.TargetKinds, "code") {
		risks = append(risks, Risk{Code: "code_volume_missing"})
	}
	expectedDatabase := map[string]bool{}
	for _, evidence := range plan.Evidence {
		if !strings.HasSuffix(evidence.ObjectRef, "/-") {
			expectedDatabase[evidence.ObjectRef] = true
		}
	}
	if asset := projected.Volumes["database"]; asset != nil {
		actual := map[string]bool{}
		for _, object := range asset.Objects {
			actual[object.CanonicalRef] = true
		}
		for ref := range expectedDatabase {
			if !actual[ref] {
				risks = append(risks, Risk{Code: "database_authoring_missing", Target: ref})
			}
		}
		for ref := range actual {
			if !expectedDatabase[ref] {
				risks = append(risks, Risk{Code: "database_candidate_target_unbound", Target: ref})
			}
		}
	} else if len(expectedDatabase) > 0 && containsString(plan.TargetKinds, "database") {
		risks = append(risks, Risk{Code: "database_volume_missing"})
	}
	return risks
}

func reconcileMigrationMapping(mapping *SemanticMapping, projected *cognition.Set, resolutions []MappingResolution) []Risk {
	risks := make([]Risk, 0)
	resolutionByUnit := map[string]MappingResolution{}
	lastUnit := ""
	for _, resolution := range resolutions {
		if resolution.UnitID <= lastUnit {
			risks = append(risks, Risk{Code: "mapping_resolution_order_invalid", Target: resolution.UnitID})
		}
		lastUnit = resolution.UnitID
		if _, duplicate := resolutionByUnit[resolution.UnitID]; duplicate {
			risks = append(risks, Risk{Code: "mapping_resolution_duplicate", Target: resolution.UnitID})
		}
		resolutionByUnit[resolution.UnitID] = resolution
	}
	projectedLines := map[string]string{}
	for _, assetID := range []string{"code", "database"} {
		if asset := projected.Volumes[assetID]; asset != nil {
			for _, object := range asset.Objects {
				projectedLines[assetID+"\x00"+object.CanonicalRef] = object.CanonicalLine
			}
		}
	}
	knownUnits := map[string]bool{}
	targetCounts := map[string]int{}
	unresolvedSemantic := 0
	validResolution := func(value MappingResolution) bool {
		return value.SemanticReviewed && strings.TrimSpace(value.Reviewer) != "" &&
			(value.TargetAsset == "root" || value.TargetAsset == "meta" || value.TargetAsset == "code" || value.TargetAsset == "database")
	}
	for _, record := range mapping.Records {
		if record.UnitKind != "entry" {
			continue
		}
		resolution, exists := resolutionByUnit[record.UnitID]
		if !exists || !validResolution(resolution) {
			continue
		}
		asset, target := resolution.TargetAsset, resolution.TargetRef
		if target != "" {
			key := strings.ToLower(asset + "\x00" + target)
			targetCounts[key]++
		}
	}
	for index := range mapping.Records {
		record := &mapping.Records[index]
		knownUnits[record.UnitID] = true
		if record.Mode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		resolution, exists := resolutionByUnit[record.UnitID]
		if record.UnitKind == "entry" {
			if !exists || !validResolution(resolution) {
				risks = append(risks, Risk{Code: "mapping_authoring_incomplete", Target: record.UnitID})
				unresolvedSemantic++
				continue
			}
			asset, target := resolution.TargetAsset, resolution.TargetRef
			if !containsString(record.AllowedTargets, asset) {
				risks = append(risks, Risk{Code: "mapping_disposition_target_invalid", Target: record.UnitID})
				continue
			}
			record.TargetAsset, record.TargetRef = asset, target
			if record.LegacySelfEntry && asset == cognition.OwnerRoot {
				if target != "" {
					risks = append(risks, Risk{Code: "mapping_root_target_ref_invalid", Target: record.UnitID})
					continue
				}
				record.Mode = machinecontract.CognitionMappingModelRegenerationRequired
				record.ReasonCode = "model_reviewed_root_ownership_mapping"
				continue
			}
			if asset != cognition.OwnerCode && asset != cognition.OwnerDatabase {
				risks = append(risks, Risk{Code: "mapping_entry_target_kind_invalid", Target: record.UnitID})
				continue
			}
			if cognition.ExpectedOwner(target) != asset {
				risks = append(risks, Risk{Code: "volume_ownership_conflict", Target: target})
				continue
			}
			key := strings.ToLower(asset + "\x00" + target)
			if target == "" || projectedLines[asset+"\x00"+target] == "" {
				risks = append(risks, Risk{Code: "mapping_target_missing", Target: target})
				continue
			}
			if targetCounts[key] == 1 && projectedLines[asset+"\x00"+target] == record.SourceText {
				record.Mode = machinecontract.CognitionMappingPreserved
				record.ReasonCode = "entry_bytes_preserved"
				continue
			}
			record.Mode = machinecontract.CognitionMappingModelRegenerationRequired
			record.ReasonCode = "model_reviewed_mapping"
			continue
		}
		if !exists || !validResolution(resolution) {
			risks = append(risks, Risk{Code: "mapping_authoring_incomplete", Target: record.UnitID})
			unresolvedSemantic++
			continue
		}
		record.TargetAsset = resolution.TargetAsset
		record.TargetRef = resolution.TargetRef
		record.ReasonCode = "model_reviewed_mapping"
	}
	for unitID := range resolutionByUnit {
		if !knownUnits[unitID] {
			risks = append(risks, Risk{Code: "mapping_resolution_unknown", Target: unitID})
		}
	}
	duplicateCount := 0
	for _, count := range targetCounts {
		if count > 1 {
			duplicateCount += count - 1
		}
	}
	mapping.Coverage.DuplicateTargetCount = duplicateCount
	mapping.Coverage.AmbiguousMappingCount = unresolvedSemantic
	mapping.Coverage.ProjectedCognitionValid = len(risks) == 0
	mapping.Coverage.SemanticReviewStatus = machinecontract.CognitionSemanticEquivalenceUnverified
	data, err := mappingBytesForIdentity(mapping)
	if err != nil {
		risks = append(risks, Risk{Code: "semantic_mapping_encode_failed"})
	} else {
		mapping.MappingSHA256 = hashBytes(data)
	}
	return risks
}

func resolveReviewSets(projected *cognition.Set, plan *Plan) (ReviewSets, []Risk) {
	candidates := make([]cognition.ImpactCandidate, 0)
	for _, id := range []string{"code", "database"} {
		asset := projected.Volumes[id]
		if asset == nil {
			continue
		}
		for _, object := range asset.Objects {
			candidates = append(candidates, cognition.ImpactCandidate{Change: cognition.ImpactChangeUpdate, ObjectRef: object.CanonicalRef,
				CanonicalLine: object.CanonicalLine, OriginalCandidateIndex: len(candidates) + 1})
		}
	}
	if len(candidates) == 0 {
		candidates = append(candidates, cognition.ImpactCandidate{Change: cognition.ImpactChangeAsset, VolumeID: "meta", OriginalCandidateIndex: 1})
	}
	impact, err := cognition.ResolveImpact(projected, candidates)
	if err != nil {
		risks := make([]Risk, 0, len(impact.Findings))
		for _, finding := range impact.Findings {
			risks = append(risks, Risk{Code: finding.Code, Target: finding.ObjectRef})
		}
		return plannedReviewSets(plan), risks
	}
	sets := ReviewSets{Review: []string{}, Write: []string{}, Guard: []string{}}
	for _, review := range impact.ReviewSet {
		sets.Review = append(sets.Review, review.Object)
	}
	sets.Write = append(sets.Write, impact.WriteSet...)
	sets.Guard = append(sets.Guard, impact.GuardSet...)
	sets.Write = appendUnique(sets.Write, "root", "baseline")
	sets.Guard = appendUnique(sets.Guard, "layout_identity", "baseline_identity", "inventory_identity", "source_evidence_identity", "curation_identity", "volume_registry_identity")
	sort.Strings(sets.Review)
	sort.Strings(sets.Write)
	sort.Strings(sets.Guard)
	return sets, nil
}

func plannedReviewSets(plan *Plan) ReviewSets {
	sets := emptyReviewSets()
	for _, task := range plan.AuthoringTasks {
		if task.ObjectRef != "" {
			sets.Review = append(sets.Review, task.ObjectRef)
		}
	}
	sets.Write = []string{"baseline", "meta", "root"}
	sets.Write = append(sets.Write, plan.TargetKinds...)
	sets.Guard = []string{"baseline_identity", "curation_identity", "inventory_identity", "layout_identity", "source_evidence_identity", "volume_registry_identity"}
	sort.Strings(sets.Review)
	sort.Strings(sets.Write)
	return sets
}

func buildPhysicalDiff(plan *Plan, assets map[string]CandidateAsset) PhysicalDiff {
	before := map[string]FormalAssetState{}
	for _, state := range plan.FormalAssetProof.Before {
		before[state.Path] = state
	}
	files := make([]FileDiff, 0, len(assets))
	for _, asset := range assets {
		state := before[asset.Path]
		change := "add"
		if state.Exists {
			change = "replace"
		}
		files = append(files, FileDiff{Path: asset.Path, Change: change, BeforeSHA256: state.SHA256, AfterSHA256: hashBytes([]byte(asset.Content)), BeforeBytes: state.SizeBytes, AfterBytes: int64(len([]byte(asset.Content)))})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	diff := PhysicalDiff{Files: files, BaselineDelta: "planned_existing_governance_pipeline_update"}
	data, _ := canonicalJSON(struct {
		Files         []FileDiff `json:"files"`
		BaselineDelta string     `json:"baseline_delta"`
	}{diff.Files, diff.BaselineDelta})
	diff.PhysicalDiffSHA256 = hashBytes(data)
	return diff
}

func buildLogicalDiff(plan *Plan, projected *cognition.Set, mapping *SemanticMapping) LogicalDiff {
	changes := []LogicalChange{{Kind: "layout_topology", SourceRef: plan.Layout, TargetRef: "root+volumes", Mode: plan.Operation}}
	for _, kind := range append([]string{"meta"}, plan.TargetKinds...) {
		changes = append(changes, LogicalChange{Kind: "volume_scope", TargetRef: kind, Mode: "enabled"})
	}
	if mapping != nil {
		for _, record := range mapping.Records {
			if record.Mode == machinecontract.CognitionMappingStructuralOnly {
				continue
			}
			changes = append(changes, LogicalChange{Kind: "semantic_mapping", SourceRef: record.UnitID, TargetRef: canonicalMappingTarget(record.TargetAsset, record.TargetRef), Mode: record.Mode})
		}
	}
	if projected != nil {
		changes = append(changes, LogicalChange{Kind: "composite_identity", TargetRef: projected.CompositeIdentity, Mode: "projected"})
	}
	sort.SliceStable(changes, func(i, j int) bool {
		left := changes[i].Kind + "\x00" + changes[i].SourceRef + "\x00" + changes[i].TargetRef
		right := changes[j].Kind + "\x00" + changes[j].SourceRef + "\x00" + changes[j].TargetRef
		return left < right
	})
	diff := LogicalDiff{Changes: changes}
	data, _ := canonicalJSON(changes)
	diff.LogicalDiffSHA256 = hashBytes(data)
	return diff
}

func canonicalMappingTarget(asset, ref string) string {
	if ref == "" {
		return asset
	}
	if strings.HasPrefix(ref, asset+":") || (asset == "database" && strings.HasPrefix(ref, "database://")) {
		return ref
	}
	return asset + ":" + ref
}

func buildRecoverySummary(plan *Plan, assets map[string]CandidateAsset) RecoverySummary {
	preimage := make([]string, 0, len(assets)+1)
	postimage := make([]string, 0, len(assets)+1)
	for _, asset := range assets {
		preimage = append(preimage, asset.Path)
		postimage = append(postimage, asset.Path)
	}
	preimage = append(preimage, ".aoci/baseline.json")
	postimage = append(postimage, ".aoci/baseline.json")
	sort.Strings(preimage)
	sort.Strings(postimage)
	return RecoverySummary{CommitPoint: "root_last", PreimageSet: preimage, PostimageSet: postimage, RollbackCondition: "guard_identities_unchanged", Direction: "postimage_to_preimage"}
}

func buildApprovalDigest(plan *Plan, assets []CandidateAssetIdentity, mapping *SemanticMapping, preview *Preview) *ApprovalDigest {
	mappingSHA := hashBytes(nil)
	if mapping != nil {
		mappingSHA = mapping.MappingSHA256
	}
	digest := &ApprovalDigest{
		Version: machinecontract.CognitionApprovalDigestV1, Operation: plan.Operation, PlanID: preview.PlanID,
		ProtocolVersion: plan.Version, RepositoryIdentity: plan.RepositoryIdentity, LayoutIdentity: plan.LayoutIdentity,
		BaselineIdentity: plan.BaselineIdentity, InventoryIdentity: plan.InventoryIdentity, SourceEvidenceIdentity: plan.SourceEvidenceIdentity,
		CandidateAssets: append([]CandidateAssetIdentity{}, assets...), MappingSHA256: mappingSHA,
		PhysicalDiffSHA256: preview.PhysicalDiff.PhysicalDiffSHA256, LogicalDiffSHA256: preview.LogicalDiff.LogicalDiffSHA256,
		Sets: preview.Sets, RecoveryDirection: preview.Recovery.Direction,
	}
	data, _ := canonicalJSON(*digest)
	digest.Digest = hashBytes(data)
	return digest
}

func candidateBoundPlanIdentity(discoveryPlanID, candidateIdentity string) string {
	identity := newIdentity("candidate-bound-cognition-plan")
	identity.field("discovery_plan_id", discoveryPlanID)
	identity.field("candidate_identity", candidateIdentity)
	return identity.sum()
}

func mappingComplete(mapping *SemanticMapping) bool {
	coverage := mapping.Coverage
	if !coverage.ByteReversible || coverage.LegacyEntryCoverage != "100.00%" || coverage.LegacySemanticAtomCoverage != "100.00%" ||
		coverage.LegacyEntryDispositionTotal != coverage.LegacyEntryTotal || coverage.LegacyEntryDispositionComplete != coverage.LegacyEntryTotal ||
		coverage.DuplicateTargetCount != 0 || coverage.UnexplainedDropCount != 0 || coverage.AmbiguousMappingCount != 0 || !coverage.ProjectedCognitionValid {
		return false
	}
	return true
}

func cloneMapping(source *SemanticMapping) *SemanticMapping {
	if source == nil {
		return nil
	}
	copyValue := *source
	copyValue.Records = append([]MappingRecord{}, source.Records...)
	return &copyValue
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func appendUnique(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, addition := range additions {
		if !seen[addition] {
			values = append(values, addition)
			seen[addition] = true
		}
	}
	return values
}

func sortRisks(risks []Risk) {
	sort.SliceStable(risks, func(i, j int) bool {
		if risks[i].Code != risks[j].Code {
			return risks[i].Code < risks[j].Code
		}
		return risks[i].Target < risks[j].Target
	})
}

func emptyPhysicalDiff() PhysicalDiff {
	diff := PhysicalDiff{Files: []FileDiff{}, BaselineDelta: "none"}
	data, _ := canonicalJSON(diff.Files)
	diff.PhysicalDiffSHA256 = hashBytes(data)
	return diff
}

func emptyLogicalDiff() LogicalDiff {
	diff := LogicalDiff{Changes: []LogicalChange{}}
	data, _ := canonicalJSON(diff.Changes)
	diff.LogicalDiffSHA256 = hashBytes(data)
	return diff
}

func emptyReviewSets() ReviewSets {
	return ReviewSets{Review: []string{}, Write: []string{}, Guard: []string{}}
}

func emptyRecovery() RecoverySummary {
	return RecoverySummary{CommitPoint: "none", PreimageSet: []string{}, PostimageSet: []string{}, RollbackCondition: "not_applicable", Direction: "none"}
}
