package migrationapply

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// BuildMappingTemplate provides only lexical source/target coordinates and
// pending authoring tasks. It deliberately leaves every semantic role,
// destination, split/merge decision, and review decision to the model/human
// control plane.
func BuildMappingTemplate(repositoryRoot string, snapshot *LegacySnapshot, plan *cognitionplan.Plan, candidate *cognitionplan.LayoutCandidate) (*MigrationMapping, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	if plan == nil || plan.Version != machinecontract.CognitionMigrationPlanV2 || plan.Operation != cognitionplan.OperationMigration || plan.Mapping == nil {
		return nil, fmt.Errorf("migration_mapping_plan_invalid")
	}
	projected, targetRanges, err := candidateTargetRanges(repositoryRoot, plan, candidate)
	if err != nil || projected == nil {
		return nil, fmt.Errorf("migration_mapping_candidate_invalid: %v", err)
	}
	records := make([]MappingRecord, 0, len(snapshot.Ranges))
	tasks := []MappingAuthoringTask{}
	proposals := buildEntryPreservationProposals(repositoryRoot, snapshot, targetRanges)
	for _, source := range snapshot.Ranges {
		record := sourceRecord(source)
		if source.Kind == "structure" || source.Kind == "section" {
			record.SemanticRole = "structure"
			record.TargetAsset = "none"
			record.MappingMode = machinecontract.CognitionMappingStructuralOnly
			record.ReviewStatus = machinecontract.CognitionMigrationSemanticReviewed
			record.Reviewer = "aoci-governance"
		} else {
			taskID := "author:" + source.Identity
			record.AuthoringTaskID = taskID
			record.ReviewStatus = machinecontract.CognitionMigrationSemanticPending
			tasks = append(tasks, MappingAuthoringTask{
				TaskID: taskID, SourceIdentities: []string{source.Identity},
				SourceEvidenceRefs: []string{"legacy:" + source.SHA256}, SourceEvidenceIdentity: plan.SourceEvidenceIdentity,
				CandidateRangeIdentities: []string{}, Status: machinecontract.CognitionMigrationSemanticPending,
				EntryPreservationProposal: proposals[source.Identity],
			})
		}
		records = append(records, record)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	return &MigrationMapping{
		Version: machinecontract.CognitionMigrationMappingV2, SnapshotIdentity: snapshot.SnapshotIdentity,
		PlannerMappingSHA256: plan.Mapping.MappingSHA256, TargetRanges: targetRanges,
		Records: records, MappingGroups: []MappingGroup{}, AuthoringTasks: tasks,
		Coverage: MappingCoverage{SemanticReviewStatus: machinecontract.CognitionMigrationSemanticPending, SemanticEquivalence: machinecontract.CognitionSemanticEquivalenceUnverified},
	}, nil
}

// ValidateMapping verifies model-owned decisions against exact source and
// candidate bytes, then computes coverage and the canonical mapping digest.
// It does not invent or repair any semantic field.
func ValidateMapping(repositoryRoot string, snapshot *LegacySnapshot, plan *cognitionplan.Plan, candidate *cognitionplan.LayoutCandidate, submitted *MigrationMapping) (*MigrationMapping, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	if submitted == nil || submitted.Version != machinecontract.CognitionMigrationMappingV2 || submitted.SnapshotIdentity != snapshot.SnapshotIdentity ||
		plan == nil || plan.Mapping == nil || submitted.PlannerMappingSHA256 != plan.Mapping.MappingSHA256 {
		return nil, fmt.Errorf("migration_mapping_binding_invalid")
	}
	projected, targetRanges, err := candidateTargetRanges(repositoryRoot, plan, candidate)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(targetRanges, submitted.TargetRanges) {
		return nil, fmt.Errorf("migration_mapping_target_catalog_drift")
	}
	if len(submitted.Records) != len(snapshot.Ranges) {
		return nil, fmt.Errorf("migration_mapping_source_coverage_incomplete")
	}

	legacyRaw, _ := base64.StdEncoding.Strict().DecodeString(snapshot.LegacyContentBase64)
	preservedIndex := buildPreservedIndex(projected, plan)
	targetByID := make(map[string]TargetRange, len(targetRanges))
	for _, target := range targetRanges {
		targetByID[target.Identity] = target
	}
	sourceByID := make(map[string]ByteRange, len(snapshot.Ranges))
	recordByID := make(map[string]MappingRecord, len(submitted.Records))
	for _, source := range snapshot.Ranges {
		sourceByID[source.Identity] = source
	}
	taskByID, err := validateAuthoringTasks(submitted.AuthoringTasks, sourceByID, targetByID, plan.SourceEvidenceIdentity)
	if err != nil {
		return nil, err
	}
	groupByID, err := validateMappingGroups(submitted.MappingGroups, taskByID, targetByID)
	if err != nil {
		return nil, err
	}

	entryTotal, entryMapped, semanticTotal, semanticMapped := 0, 0, 0, 0
	preservedFields, regeneratedFields, fieldPreservedEntries, fullRegeneratedEntries, canonicalizedEntries := 0, 0, 0, 0, 0
	unexplained, ambiguous, duplicates := 0, 0, 0
	targetOwners := map[string]string{}
	for indexValue, source := range snapshot.Ranges {
		record := submitted.Records[indexValue]
		legacySelfEntry := isLegacySelfEntrySource(plan, source)
		if !recordMatchesSource(record, source) {
			return nil, fmt.Errorf("migration_mapping_source_record_invalid: %s", source.Identity)
		}
		if _, duplicate := recordByID[record.SourceIdentity]; duplicate {
			return nil, fmt.Errorf("migration_mapping_source_duplicate: %s", record.SourceIdentity)
		}
		recordByID[record.SourceIdentity] = record
		structural := source.Kind == "structure" || source.Kind == "section"
		if source.Kind == "entry" {
			entryTotal++
		}
		if !structural {
			semanticTotal++
		}
		if structural {
			if record.MappingMode != machinecontract.CognitionMappingStructuralOnly || record.SemanticRole != "structure" || record.TargetAsset != "none" ||
				record.TargetObject != "" || record.TargetSemanticRangeIdentity != "" || record.ReviewStatus != machinecontract.CognitionMigrationSemanticReviewed {
				return nil, fmt.Errorf("migration_mapping_structure_invalid: %s", source.Identity)
			}
			continue
		}
		if strings.TrimSpace(record.SemanticRole) == "" || strings.TrimSpace(record.Reviewer) == "" || record.ReviewStatus != machinecontract.CognitionMigrationSemanticReviewed {
			ambiguous++
			continue
		}
		if record.MappingMode != machinecontract.CognitionMappingPreserved && record.MappingMode != machinecontract.CognitionMappingFieldPreserved && record.MappingMode != machinecontract.CognitionMigrationModelRegenerated {
			ambiguous++
			continue
		}
		target, exists := targetByID[record.TargetSemanticRangeIdentity]
		if !exists || target.Asset != record.TargetAsset || target.Object != record.TargetObject || !validTargetAsset(record.TargetAsset) {
			unexplained++
			continue
		}
		if source.Kind == "header_atom" && record.MappingMode != machinecontract.CognitionMigrationModelRegenerated {
			return nil, fmt.Errorf("migration_header_requires_model_regeneration: %s", source.Identity)
		}
		if source.Kind == "entry" {
			if legacySelfEntry {
				if target.Asset != cognition.OwnerRoot || target.Object != "" || target.Kind != "root_semantic" ||
					record.MappingMode != machinecontract.CognitionMigrationModelRegenerated {
					return nil, fmt.Errorf("migration_self_entry_owner_invalid: %s", source.Identity)
				}
			} else if target.Kind != "entry" || target.Object == "" ||
				(target.Asset != cognition.OwnerCode && target.Asset != cognition.OwnerDatabase) ||
				cognition.ExpectedOwner(target.Object) != target.Asset {
				return nil, fmt.Errorf("migration_entry_target_invalid: %s", source.Identity)
			}
		}
		if record.MappingMode == machinecontract.CognitionMappingPreserved {
			if source.SHA256 != target.SHA256 {
				return nil, fmt.Errorf("migration_preserved_bytes_changed: %s", source.Identity)
			}
			if source.Kind == "entry" {
				if err := validatePreservedEntry(legacyRaw, source, target, preservedIndex); err != nil {
					return nil, err
				}
			}
		} else {
			task, exists := taskByID[record.AuthoringTaskID]
			if !exists || task.Status != machinecontract.CognitionMigrationSemanticReviewed || !contains(task.SourceIdentities, source.Identity) ||
				!contains(task.CandidateRangeIdentities, target.Identity) {
				return nil, fmt.Errorf("migration_authoring_task_incomplete: %s", source.Identity)
			}
			if source.Kind == "entry" && record.MappingGroupID == "" {
				return nil, fmt.Errorf("migration_entry_regeneration_group_required: %s", source.Identity)
			}
			if record.MappingGroupID != "" {
				group, exists := groupByID[record.MappingGroupID]
				if !exists || !contains(group.SourceIdentities, source.Identity) || !contains(group.TargetRangeIdentities, target.Identity) || group.AuthoringTaskID != record.AuthoringTaskID {
					return nil, fmt.Errorf("migration_mapping_group_binding_invalid: %s", source.Identity)
				}
			}
			if source.Kind == "entry" {
				switch record.MappingMode {
				case machinecontract.CognitionMappingFieldPreserved:
					preserved, regenerated, canonicalized, preservationErr := validateEntryPreservation(source, target, record.EntryPreservation, snapshot.Ranges, targetRanges)
					if preservationErr != nil {
						return nil, preservationErr
					}
					preservedFields += preserved
					regeneratedFields += regenerated
					fieldPreservedEntries++
					if canonicalized {
						canonicalizedEntries++
					}
				case machinecontract.CognitionMigrationModelRegenerated:
					if record.EntryPreservation != nil {
						return nil, fmt.Errorf("migration_full_regeneration_has_preservation: %s", source.Identity)
					}
					regeneratedFields += 5
					fullRegeneratedEntries++
				}
			}
		}
		owner := record.SourceIdentity
		if record.MappingGroupID != "" {
			owner = "group:" + record.MappingGroupID
		}
		if previous, exists := targetOwners[target.Identity]; exists && previous != owner {
			duplicates++
		} else {
			targetOwners[target.Identity] = owner
		}
		semanticMapped++
		if source.Kind == "entry" {
			entryMapped++
		}
	}

	for _, record := range submitted.Records {
		if record.ParentSourceIdentity == "" {
			continue
		}
		parent, exists := sourceByID[record.ParentSourceIdentity]
		if !exists || record.SourceByteStart < parent.ByteStart || record.SourceByteEnd > parent.ByteEnd {
			return nil, fmt.Errorf("migration_mapping_parent_range_invalid: %s", record.SourceIdentity)
		}
		parentRecord := recordByID[record.ParentSourceIdentity]
		sameReviewedGroup := parentRecord.MappingGroupID != "" && parentRecord.MappingGroupID == record.MappingGroupID
		if parent.Kind == "entry" && parentRecord.TargetAsset != "" && !sameReviewedGroup &&
			(record.TargetAsset != parentRecord.TargetAsset || record.TargetObject != parentRecord.TargetObject) {
			return nil, fmt.Errorf("migration_entry_atom_target_mismatch: %s", record.SourceIdentity)
		}
	}

	allTasksComplete := true
	for _, task := range submitted.AuthoringTasks {
		if task.Status != machinecontract.CognitionMigrationSemanticReviewed || strings.TrimSpace(task.Reviewer) == "" {
			allTasksComplete = false
		}
	}
	coverage := MappingCoverage{
		ByteReversible:   true,
		LegacyEntryTotal: entryTotal, LegacyEntryMapped: entryMapped, LegacyEntryCoverage: percentage(entryMapped, entryTotal),
		LegacySemanticAtomTotal: semanticTotal, LegacySemanticAtomMapped: semanticMapped, LegacySemanticAtomCoverage: percentage(semanticMapped, semanticTotal),
		DuplicateTargetCount: duplicates, UnexplainedDropCount: unexplained, AmbiguousMappingCount: ambiguous,
		ProjectedCognitionValid: projected != nil, AllModelAuthoringTasksComplete: allTasksComplete,
		SemanticReviewStatus: machinecontract.CognitionMigrationSemanticReviewed,
		SemanticEquivalence:  machinecontract.CognitionMigrationSemanticReviewed,
		PreservedFieldCount:  preservedFields, RegeneratedFieldCount: regeneratedFields,
		FieldPreservedEntryCount: fieldPreservedEntries, FullRegeneratedEntryCount: fullRegeneratedEntries,
		IdentityCanonicalizationCount: canonicalizedEntries,
	}
	if entryMapped != entryTotal || semanticMapped != semanticTotal || duplicates != 0 || unexplained != 0 || ambiguous != 0 || !allTasksComplete {
		coverage.SemanticReviewStatus = machinecontract.CognitionMigrationSemanticPending
		coverage.SemanticEquivalence = machinecontract.CognitionSemanticEquivalenceUnverified
	}
	validated := *submitted
	validated.Coverage = coverage
	validated.MappingSHA256 = ""
	digest, err := mappingDigest(&validated)
	if err != nil {
		return nil, err
	}
	validated.MappingSHA256 = digest
	return &validated, nil
}

func VerifyMapping(repositoryRoot string, snapshot *LegacySnapshot, plan *cognitionplan.Plan, candidate *cognitionplan.LayoutCandidate, mapping *MigrationMapping) error {
	validated, err := ValidateMapping(repositoryRoot, snapshot, plan, candidate, mapping)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(*validated, *mapping) {
		return fmt.Errorf("migration_mapping_not_apply_grade")
	}
	coverage := mapping.Coverage
	if !coverage.ByteReversible || coverage.LegacyEntryCoverage != "100.00%" || coverage.LegacySemanticAtomCoverage != "100.00%" ||
		coverage.DuplicateTargetCount != 0 || coverage.UnexplainedDropCount != 0 || coverage.AmbiguousMappingCount != 0 ||
		!coverage.ProjectedCognitionValid || !coverage.AllModelAuthoringTasksComplete ||
		coverage.SemanticReviewStatus != machinecontract.CognitionMigrationSemanticReviewed || coverage.SemanticEquivalence != machinecontract.CognitionMigrationSemanticReviewed {
		return fmt.Errorf("migration_mapping_final_approval_gate_not_met")
	}
	return nil
}

func sourceRecord(source ByteRange) MappingRecord {
	return MappingRecord{
		SourceIdentity: source.Identity, ParentSourceIdentity: source.ParentID,
		SourceByteStart: source.ByteStart, SourceByteEnd: source.ByteEnd,
		SourceLineStart: source.LineStart, SourceLineEnd: source.LineEnd,
		SourceSHA256: source.SHA256, SourceKind: source.Kind,
	}
}

func isLegacySelfEntrySource(plan *cognitionplan.Plan, source ByteRange) bool {
	if plan == nil || plan.Mapping == nil || source.Kind != "entry" {
		return false
	}
	for _, record := range plan.Mapping.Records {
		if record.LegacySelfEntry && record.SourceLine == source.LineStart && record.SourceSHA256 == source.SHA256 {
			return true
		}
	}
	return false
}

func recordMatchesSource(record MappingRecord, source ByteRange) bool {
	return record.SourceIdentity == source.Identity && record.ParentSourceIdentity == source.ParentID &&
		record.SourceByteStart == source.ByteStart && record.SourceByteEnd == source.ByteEnd &&
		record.SourceLineStart == source.LineStart && record.SourceLineEnd == source.LineEnd &&
		record.SourceSHA256 == source.SHA256 && record.SourceKind == source.Kind
}

func validateAuthoringTasks(tasks []MappingAuthoringTask, sources map[string]ByteRange, targets map[string]TargetRange, sourceEvidenceIdentity string) (map[string]MappingAuthoringTask, error) {
	result := map[string]MappingAuthoringTask{}
	last := ""
	for _, task := range tasks {
		if task.TaskID <= last || strings.TrimSpace(task.TaskID) == "" {
			return nil, fmt.Errorf("migration_authoring_task_order_invalid")
		}
		last = task.TaskID
		if _, exists := result[task.TaskID]; exists {
			return nil, fmt.Errorf("migration_authoring_task_duplicate")
		}
		if !sortedUnique(task.SourceIdentities) || !sortedUnique(task.SourceEvidenceRefs) || !sortedUnique(task.CandidateRangeIdentities) ||
			task.SourceEvidenceIdentity != sourceEvidenceIdentity {
			return nil, fmt.Errorf("migration_authoring_task_set_invalid: %s", task.TaskID)
		}
		for _, source := range task.SourceIdentities {
			if _, exists := sources[source]; !exists {
				return nil, fmt.Errorf("migration_authoring_task_source_unknown: %s", source)
			}
		}
		if task.Status != machinecontract.CognitionMigrationSemanticPending && task.Status != machinecontract.CognitionMigrationSemanticReviewed {
			return nil, fmt.Errorf("migration_authoring_task_status_invalid: %s", task.TaskID)
		}
		if task.Status == machinecontract.CognitionMigrationSemanticReviewed {
			if strings.TrimSpace(task.Reviewer) == "" || len(task.CandidateRangeIdentities) == 0 ||
				(!validTargetAsset(task.TargetAsset) && task.TargetAsset != "multiple") ||
				((task.TargetAsset == "code" || task.TargetAsset == "database") && strings.TrimSpace(task.TargetObject) == "") {
				return nil, fmt.Errorf("migration_authoring_task_review_invalid: %s", task.TaskID)
			}
		}
		for _, targetID := range task.CandidateRangeIdentities {
			target, exists := targets[targetID]
			if !exists {
				return nil, fmt.Errorf("migration_authoring_task_target_unknown: %s", targetID)
			}
			if task.TargetAsset != "multiple" && task.TargetAsset != target.Asset {
				return nil, fmt.Errorf("migration_authoring_task_target_asset_mismatch: %s", targetID)
			}
			if task.TargetObject != "" && task.TargetObject != target.Object {
				return nil, fmt.Errorf("migration_authoring_task_target_object_mismatch: %s", targetID)
			}
		}
		result[task.TaskID] = task
	}
	return result, nil
}

func validateMappingGroups(groups []MappingGroup, tasks map[string]MappingAuthoringTask, targets map[string]TargetRange) (map[string]MappingGroup, error) {
	result := map[string]MappingGroup{}
	last := ""
	for _, group := range groups {
		if group.MappingGroupID <= last || strings.TrimSpace(group.MappingGroupID) == "" || !sortedUnique(group.SourceIdentities) || !sortedUnique(group.TargetRangeIdentities) {
			return nil, fmt.Errorf("migration_mapping_group_order_invalid")
		}
		last = group.MappingGroupID
		if group.ReviewStatus != machinecontract.CognitionMigrationSemanticReviewed || strings.TrimSpace(group.Reviewer) == "" {
			return nil, fmt.Errorf("migration_mapping_group_unreviewed: %s", group.MappingGroupID)
		}
		if _, exists := tasks[group.AuthoringTaskID]; !exists {
			return nil, fmt.Errorf("migration_mapping_group_task_unknown: %s", group.MappingGroupID)
		}
		for _, target := range group.TargetRangeIdentities {
			if _, exists := targets[target]; !exists {
				return nil, fmt.Errorf("migration_mapping_group_target_unknown: %s", target)
			}
		}
		result[group.MappingGroupID] = group
	}
	return result, nil
}

type preservedObject struct {
	canonicalLine string
	count         int
}

type preservedValidationIndex struct {
	objects  map[string]map[string]preservedObject
	evidence map[string]bool
}

func buildPreservedIndex(projected *cognition.Set, plan *cognitionplan.Plan) preservedValidationIndex {
	result := preservedValidationIndex{objects: map[string]map[string]preservedObject{}, evidence: map[string]bool{}}
	for _, assetID := range []string{"code", "database"} {
		result.objects[assetID] = map[string]preservedObject{}
		asset := projected.Volumes[assetID]
		if asset == nil {
			continue
		}
		for _, object := range asset.Objects {
			current := result.objects[assetID][object.CanonicalRef]
			if current.count == 0 {
				current.canonicalLine = object.CanonicalLine
			}
			current.count++
			result.objects[assetID][object.CanonicalRef] = current
		}
	}
	for _, item := range plan.Evidence {
		if item.ObjectRef != "" && item.TableEvidenceSHA256 != "" {
			result.evidence[item.ObjectRef] = true
		}
	}
	return result
}

func validatePreservedEntry(legacyRaw []byte, source ByteRange, target TargetRange, validation preservedValidationIndex) error {
	if target.Kind != "entry" || (target.Asset != "code" && target.Asset != "database") || target.Object == "" {
		return fmt.Errorf("migration_preserved_entry_target_invalid: %s", source.Identity)
	}
	line := string(legacyRaw[source.ByteStart:source.ByteEnd])
	entry, ok := index.ParseEntryLine(line, source.LineStart)
	if !ok || len(entry.TagsParsed) == 0 || strings.TrimSpace(entry.F) == "" || strings.TrimSpace(entry.R) == "" || strings.TrimSpace(entry.Api) == "" || strings.TrimSpace(entry.S) == "" {
		return fmt.Errorf("migration_preserved_entry_fras_invalid: %s", source.Identity)
	}
	objects, exists := validation.objects[target.Asset]
	if !exists {
		return fmt.Errorf("migration_preserved_entry_volume_missing: %s", target.Asset)
	}
	object, exists := objects[target.Object]
	if !exists || object.count != 1 || object.canonicalLine != line {
		return fmt.Errorf("migration_preserved_entry_identity_ambiguous: %s", target.Object)
	}
	if target.Asset == "database" {
		if !validation.evidence[target.Object] {
			return fmt.Errorf("migration_preserved_database_evidence_missing: %s", target.Object)
		}
	}
	return nil
}

func buildEntryPreservationProposals(repositoryRoot string, snapshot *LegacySnapshot, targets []TargetRange) map[string]*EntryPreservation {
	result := map[string]*EntryPreservation{}
	legacyRaw, err := base64.StdEncoding.Strict().DecodeString(snapshot.LegacyContentBase64)
	if err != nil {
		return result
	}
	document, _ := index.Parse(string(legacyRaw))
	index.ResolveRelPaths(document, repositoryRoot)
	pathByLine := map[int]string{}
	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			pathByLine[entry.LineNo] = filepath.ToSlash(entry.RelPath)
		}
	}
	targetByObject := map[string][]TargetRange{}
	for _, target := range targets {
		if target.Kind == "entry" && target.Object != "" {
			targetByObject[target.Object] = append(targetByObject[target.Object], target)
		}
	}
	for _, source := range snapshot.Ranges {
		if source.Kind != "entry" {
			continue
		}
		legacyIdentity := pathByLine[source.LineStart]
		if legacyIdentity == "" {
			continue
		}
		canonical := "code:" + legacyIdentity
		matches := targetByObject[canonical]
		if len(matches) != 1 {
			continue
		}
		preserved, regenerated := compareEntryFields(source, matches[0], snapshot.Ranges, targets)
		result[source.Identity] = &EntryPreservation{
			Version: machinecontract.CognitionEntryPreservationV1, PreservedFields: preserved, RegeneratedFields: regenerated,
			IdentityCanonicalizationProposal: &IdentityCanonicalizationProposal{
				SourceObjectIdentity: legacyIdentity, TargetObjectIdentity: canonical, OneToOne: true, TargetExists: true,
				RepresentationOnly: true, ReviewStatus: machinecontract.CognitionMigrationSemanticPending,
			},
			ReviewStatus: machinecontract.CognitionMigrationSemanticPending,
		}
	}
	return result
}

func compareEntryFields(source ByteRange, target TargetRange, sourceRanges []ByteRange, targetRanges []TargetRange) ([]string, []string) {
	sourceAtoms := sourceEntryAtoms(source.Identity, sourceRanges)
	targetAtoms := targetEntryAtoms(target.Identity, targetRanges)
	preserved := []string{}
	regenerated := []string{}
	for _, field := range []string{"tags", "F", "R", "A", "S"} {
		kind := fieldAtomKind(field)
		if sourceAtoms[kind] != "" && sourceAtoms[kind] == targetAtoms[kind] {
			preserved = append(preserved, field)
		} else {
			regenerated = append(regenerated, field)
		}
	}
	return preserved, regenerated
}

func sourceEntryAtoms(parent string, ranges []ByteRange) map[string]string {
	result := map[string]string{}
	for _, item := range ranges {
		if item.ParentID == parent {
			result[item.Kind] = item.SHA256
		}
	}
	return result
}

func targetEntryAtoms(parent string, ranges []TargetRange) map[string]string {
	result := map[string]string{}
	for _, item := range ranges {
		if targetParentIdentity(item, ranges) == parent {
			result[item.Kind] = item.SHA256
		}
	}
	return result
}

func targetParentIdentity(item TargetRange, ranges []TargetRange) string {
	if item.Kind == "entry" {
		return ""
	}
	for _, possible := range ranges {
		if possible.Kind == "entry" && possible.Asset == item.Asset && possible.Object == item.Object &&
			item.ByteStart >= possible.ByteStart && item.ByteEnd <= possible.ByteEnd {
			return possible.Identity
		}
	}
	return ""
}

func fieldAtomKind(field string) string {
	return map[string]string{"tags": "entry_tag", "F": "entry_f", "R": "entry_r", "A": "entry_a", "S": "entry_s"}[field]
}

func validateEntryPreservation(source ByteRange, target TargetRange, preservation *EntryPreservation, sourceRanges []ByteRange, targetRanges []TargetRange) (int, int, bool, error) {
	if preservation == nil || preservation.Version != machinecontract.CognitionEntryPreservationV1 ||
		preservation.ReviewStatus != machinecontract.CognitionMigrationSemanticReviewed || strings.TrimSpace(preservation.Reviewer) == "" {
		return 0, 0, false, fmt.Errorf("migration_entry_preservation_review_incomplete: %s", source.Identity)
	}
	classification := map[string]string{}
	for _, field := range preservation.PreservedFields {
		if fieldAtomKind(field) == "" || classification[field] != "" {
			return 0, 0, false, fmt.Errorf("migration_entry_preservation_fields_invalid: %s", source.Identity)
		}
		classification[field] = "preserved"
	}
	for _, field := range preservation.RegeneratedFields {
		if fieldAtomKind(field) == "" || classification[field] != "" {
			return 0, 0, false, fmt.Errorf("migration_entry_preservation_fields_invalid: %s", source.Identity)
		}
		classification[field] = "regenerated"
	}
	sourceAtoms := sourceEntryAtoms(source.Identity, sourceRanges)
	targetAtoms := targetEntryAtoms(target.Identity, targetRanges)
	for _, field := range []string{"tags", "F", "R", "A", "S"} {
		if classification[field] == "" {
			return 0, 0, false, fmt.Errorf("migration_entry_preservation_fields_incomplete: %s", source.Identity)
		}
		if classification[field] == "preserved" {
			kind := fieldAtomKind(field)
			if sourceAtoms[kind] == "" || sourceAtoms[kind] != targetAtoms[kind] {
				return 0, 0, false, fmt.Errorf("migration_preserved_field_bytes_changed: %s:%s", source.Identity, field)
			}
		}
	}
	canonicalized := false
	if proposal := preservation.IdentityCanonicalizationProposal; proposal != nil {
		if !proposal.OneToOne || !proposal.TargetExists || !proposal.RepresentationOnly ||
			proposal.ReviewStatus != machinecontract.CognitionMigrationSemanticReviewed || strings.TrimSpace(proposal.Reviewer) == "" ||
			proposal.TargetObjectIdentity != target.Object || !canonicalIdentityRepresentation(proposal.SourceObjectIdentity, proposal.TargetObjectIdentity, target.Asset) ||
			countTargetObject(targetRanges, target.Asset, target.Object) != 1 {
			return 0, 0, false, fmt.Errorf("migration_identity_canonicalization_invalid: %s", source.Identity)
		}
		canonicalized = true
	}
	return len(preservation.PreservedFields), len(preservation.RegeneratedFields), canonicalized, nil
}

func canonicalIdentityRepresentation(source, target, asset string) bool {
	source = filepath.ToSlash(strings.TrimSpace(source))
	target = strings.TrimSpace(target)
	if asset == "code" {
		return target == "code:"+source
	}
	if asset == "database" {
		return strings.HasPrefix(source, "database://") && target == source
	}
	return false
}

func countTargetObject(ranges []TargetRange, asset, object string) int {
	count := 0
	for _, item := range ranges {
		if item.Kind == "entry" && item.Asset == asset && item.Object == object {
			count++
		}
	}
	return count
}

func candidateTargetRanges(repositoryRoot string, plan *cognitionplan.Plan, candidate *cognitionplan.LayoutCandidate) (*cognition.Set, []TargetRange, error) {
	if candidate == nil || candidate.Version != machinecontract.CognitionLayoutCandidateV1 || candidate.PlanID != plan.PlanID {
		return nil, nil, fmt.Errorf("migration_candidate_binding_invalid")
	}
	assets := map[string]cognitionplan.CandidateAsset{}
	volumeRaw := map[string][]byte{}
	for _, asset := range candidate.Assets {
		assets[asset.AssetID] = asset
		if asset.AssetID != "root" {
			volumeRaw[asset.AssetID] = []byte(asset.Content)
		}
	}
	rootAsset, exists := assets["root"]
	if !exists {
		return nil, nil, fmt.Errorf("migration_root_candidate_missing")
	}
	projected, findings := cognition.BuildProjectedSet(repositoryRoot, []byte(rootAsset.Content), volumeRaw)
	if len(findings) != 0 || projected == nil {
		return nil, nil, fmt.Errorf("migration_projected_cognition_invalid")
	}
	ranges := []TargetRange{}
	for _, assetID := range []string{"root", "meta", "code", "database"} {
		asset, exists := assets[assetID]
		if !exists {
			continue
		}
		objects := map[int]string{}
		if projected.Volumes[assetID] != nil {
			for _, object := range projected.Volumes[assetID].Objects {
				objects[object.SourceLineNumber] = object.CanonicalRef
			}
		}
		ranges = append(ranges, enumerateTargetAsset(assetID, []byte(asset.Content), objects)...)
	}
	return projected, ranges, nil
}

func enumerateTargetAsset(asset string, raw []byte, objects map[int]string) []TargetRange {
	ranges := []TargetRange{}
	for _, line := range splitRawLines(raw) {
		trimmed := strings.TrimSpace(line.text)
		object := objects[line.number]
		if object != "" {
			parent := newTargetRange(asset, object, "entry", line.start, line.contentEnd, line.number, raw)
			ranges = append(ranges, parent)
			for _, child := range enumerateEntryAtoms(raw, line, parent.Identity) {
				ranges = append(ranges, targetFromSourceRange(asset, object, child))
			}
			continue
		}
		if trimmed == "" || targetStructuralLine(asset, trimmed) {
			continue
		}
		kind := asset + "_semantic"
		ranges = append(ranges, newTargetRange(asset, "", kind, line.start, line.contentEnd, line.number, raw))
	}
	return ranges
}

func targetStructuralLine(asset, line string) bool {
	if strings.HasPrefix(line, "===") || strings.HasPrefix(line, "#Volume:") {
		return true
	}
	for _, marker := range []string{
		cognition.RootManifestMarker, cognition.MetaVolumeMarker, cognition.CodeVolumeMarker, cognition.DatabaseMarker,
		"#Format-Version:", "#Locale:", "#Object-Protocol:", "#FRAS-Discipline:",
		"#FRAS-v2-Limits-Authority:", "#S-Admission:", "#Object-Kinds:", "#[Tag dictionary:",
	} {
		if strings.HasPrefix(line, marker) {
			return true
		}
	}
	return false
}

func newTargetRange(asset, object, kind string, start, end int64, line int, raw []byte) TargetRange {
	data := raw[start:end]
	identity := sha256Hex([]byte(fmt.Sprintf("cognition-migration-target-range/v1\n%s\n%s\n%s\n%d\n%d\n%s\n", asset, object, kind, start, end, sha256Hex(data))))
	return TargetRange{Identity: identity, Asset: asset, Object: object, Kind: kind, ByteStart: start, ByteEnd: end, LineStart: line, LineEnd: line, SHA256: sha256Hex(data)}
}

func targetFromSourceRange(asset, object string, source ByteRange) TargetRange {
	identity := sha256Hex([]byte(fmt.Sprintf("cognition-migration-target-range/v1\n%s\n%s\n%s\n%d\n%d\n%s\n", asset, object, source.Kind, source.ByteStart, source.ByteEnd, source.SHA256)))
	return TargetRange{Identity: identity, Asset: asset, Object: object, Kind: source.Kind, ByteStart: source.ByteStart, ByteEnd: source.ByteEnd, LineStart: source.LineStart, LineEnd: source.LineEnd, SHA256: source.SHA256}
}

func mappingDigest(mapping *MigrationMapping) (string, error) {
	copyValue := *mapping
	copyValue.MappingSHA256 = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func percentage(mapped, total int) string {
	if total == 0 {
		return "100.00%"
	}
	return fmt.Sprintf("%.2f%%", float64(mapped)*100/float64(total))
}

func validTargetAsset(value string) bool {
	return value == "root" || value == "meta" || value == "code" || value == "database"
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sortedUnique(values []string) bool {
	for indexValue, value := range values {
		if strings.TrimSpace(value) == "" || (indexValue > 0 && values[indexValue-1] >= value) {
			return false
		}
	}
	return true
}
