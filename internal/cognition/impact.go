package cognition

import (
	"fmt"
	"path"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// ResolveImpact computes Review, Write, and Guard sets without filesystem,
// network, database, model, governance, or write side effects.
func ResolveImpact(set *Set, candidates []ImpactCandidate) (AffectedCognitionSet, error) {
	result := AffectedCognitionSet{
		ReviewSet: []ImpactReviewObject{}, WriteSet: []string{}, GuardSet: []string{}, Reasons: []ImpactReason{},
	}
	if set == nil {
		return impactFailure(result, ImpactFinding{Code: "impact_set_missing", Message: "CognitionSet is required"})
	}
	result.LayoutMode = set.LayoutMode
	if len(set.Errors) > 0 {
		return impactFailure(result, ImpactFinding{Code: "impact_set_invalid", Message: "CognitionSet contains validation errors"})
	}
	if set.LayoutMode != LayoutVolumesV1 && set.LayoutMode != LayoutLegacyMonolithic {
		return impactFailure(result, ImpactFinding{Code: "impact_layout_unsupported", Message: "unsupported cognition layout " + set.LayoutMode})
	}
	if len(candidates) == 0 {
		return impactFailure(result, ImpactFinding{Code: "impact_candidates_empty", Message: "at least one cognition change candidate is required"})
	}

	current, findings := collectImpactObjects(set)
	if len(findings) > 0 {
		result.Findings = findings
		return result, &ImpactValidationError{Findings: findings}
	}
	projected := cloneImpactRegistry(current)
	direct := map[string]map[string]bool{}
	writeVolumes := map[string]bool{}
	upgradeReasons := map[string]bool{}
	reasons := map[string]ImpactReason{}

	orderedCandidates := make([]indexedImpactCandidate, 0, len(candidates))
	seenOriginalCandidateIndices := make(map[int]bool, len(candidates))
	for _, candidate := range candidates {
		if candidate.OriginalCandidateIndex <= 0 || seenOriginalCandidateIndices[candidate.OriginalCandidateIndex] {
			return result, fmt.Errorf("impact candidate original index must be a unique positive value")
		}
		seenOriginalCandidateIndices[candidate.OriginalCandidateIndex] = true
		orderedCandidates = append(orderedCandidates, indexedImpactCandidate{candidate: candidate, index: candidate.OriginalCandidateIndex})
	}
	sort.SliceStable(orderedCandidates, func(i, j int) bool {
		return impactCandidateKey(orderedCandidates[i].candidate) < impactCandidateKey(orderedCandidates[j].candidate)
	})
	claimedTargets := map[string]int{}
	for _, indexedCandidate := range orderedCandidates {
		candidate := indexedCandidate.candidate
		key := impactCandidateKey(candidate)
		candidateConflict := false
		for _, claim := range impactCandidateClaims(candidate) {
			if previousIndex := claimedTargets[claim]; previousIndex != 0 {
				finding := ImpactFinding{Code: "impact_candidate_duplicate", CandidateIndex: indexedCandidate.index, ObjectRef: candidate.ObjectRef, Message: fmt.Sprintf("candidate target is already claimed by candidate %d", previousIndex)}
				bindImpactCandidateFinding(&finding, candidate)
				findings = append(findings, finding)
				candidateConflict = true
			} else {
				claimedTargets[claim] = indexedCandidate.index
			}
		}
		if previousIndex := claimedTargets["candidate\x00"+key]; previousIndex != 0 {
			finding := ImpactFinding{Code: "impact_candidate_duplicate", CandidateIndex: indexedCandidate.index, ObjectRef: candidate.ObjectRef, Message: fmt.Sprintf("the same cognition change candidate already appears at candidate %d", previousIndex)}
			bindImpactCandidateFinding(&finding, candidate)
			findings = append(findings, finding)
			continue
		}
		claimedTargets["candidate\x00"+key] = indexedCandidate.index
		if candidateConflict {
			continue
		}
		candidateFindings := applyImpactCandidate(set, current, projected, candidate, direct, writeVolumes, upgradeReasons, reasons)
		for index := range candidateFindings {
			candidateFindings[index].CandidateIndex = indexedCandidate.index
			bindImpactCandidateFinding(&candidateFindings[index], candidate)
		}
		findings = append(findings, candidateFindings...)
	}
	if len(findings) > 0 {
		SortRepairFindings(findings)
		result.Findings = findings
		return result, &ImpactValidationError{Findings: findings}
	}

	graph := buildImpactGraph(current, projected)
	allObjects := cloneImpactRegistry(current)
	for ref, object := range projected {
		allObjects[ref] = object
	}
	reviewReasons, graphFindings := resolveImpactClosure(graph, direct)
	if len(graphFindings) > 0 {
		candidatesByRef := make(map[string]indexedImpactCandidate, len(orderedCandidates))
		for _, candidate := range orderedCandidates {
			candidatesByRef[candidate.candidate.ObjectRef] = candidate
		}
		for index := range graphFindings {
			candidate, ok := candidatesByRef[graphFindings[index].ObjectRef]
			if !ok {
				continue
			}
			graphFindings[index].CandidateIndex = candidate.index
			bindImpactCandidateFinding(&graphFindings[index], candidate.candidate)
			bindImpactRelationFinding(&graphFindings[index])
		}
		SortRepairFindings(graphFindings)
		result.Findings = graphFindings
		return result, &ImpactValidationError{Findings: graphFindings}
	}

	guardVolumes := map[string]bool{}
	if set.LayoutMode == LayoutLegacyMonolithic {
		result.Strategy = ImpactStrategyLegacyMonolithic
		guardVolumes["legacy"] = true
		writeVolumes["legacy"] = true
	} else {
		result.Strategy = ImpactStrategyDependencyClosure
		guardVolumes["root"] = true
		guardVolumes["meta"] = true
		addImpactReason(reasons, ImpactReason{Code: "root_layout_guard", Volume: "root"})
		addImpactReason(reasons, ImpactReason{Code: "meta_contract_guard", Volume: "meta"})
		for volume := range writeVolumes {
			guardVolumes[volume] = true
			addImpactReason(reasons, ImpactReason{Code: "write_volume_guard", Volume: volume})
		}
		for ref := range reviewReasons {
			if object, ok := allObjects[ref]; ok {
				guardVolumes[object.VolumeID] = true
				addImpactReason(reasons, ImpactReason{Code: "review_volume_guard", To: ref, Volume: object.VolumeID})
			}
		}
		addVolumeDependencyGuards(set, guardVolumes, reasons)
		if len(upgradeReasons) > 0 {
			result.Strategy = ImpactStrategyFullCognitionSet
			result.Upgrade = true
			result.UpgradeReason = sortedKeys(upgradeReasons)
			guardVolumes["root"] = true
			for _, id := range set.DeclaredOrder {
				guardVolumes[id] = true
			}
			for volume := range writeVolumes {
				guardVolumes[volume] = true
			}
			for _, reason := range result.UpgradeReason {
				addImpactReason(reasons, ImpactReason{Code: "full_guard_upgrade", From: reason})
			}
		}
	}

	for ref, reasonSet := range reviewReasons {
		object, ok := allObjects[ref]
		if !ok {
			continue
		}
		result.ReviewSet = append(result.ReviewSet, ImpactReviewObject{Object: ref, Volume: object.VolumeID, Reasons: sortedKeys(reasonSet)})
	}
	sort.Slice(result.ReviewSet, func(i, j int) bool {
		if result.ReviewSet[i].Object != result.ReviewSet[j].Object {
			return result.ReviewSet[i].Object < result.ReviewSet[j].Object
		}
		return result.ReviewSet[i].Volume < result.ReviewSet[j].Volume
	})
	result.WriteSet = orderImpactVolumes(set, writeVolumes)
	result.GuardSet = orderImpactVolumes(set, guardVolumes)
	for _, reason := range graph.allRelationReasons {
		if reviewReasons[reason.From] != nil && reviewReasons[reason.To] != nil {
			addImpactReason(reasons, reason)
		}
	}
	result.Reasons = orderedImpactReasons(reasons)
	return result, nil
}

func applyImpactCandidate(
	set *Set,
	current, projected impactRegistry,
	candidate ImpactCandidate,
	direct map[string]map[string]bool,
	writeVolumes, upgradeReasons map[string]bool,
	reasons map[string]ImpactReason,
) []ImpactFinding {
	change := candidate.Change
	switch change {
	case ImpactChangeUpdate, ImpactChangeCreate, ImpactChangeDelete, ImpactChangeRename:
		return applyObjectImpactCandidate(set, current, projected, candidate, direct, writeVolumes, upgradeReasons, reasons)
	case ImpactChangeAsset:
		if set.LayoutMode != LayoutVolumesV1 || (candidate.VolumeID != "root" && candidate.VolumeID != "meta") {
			return []ImpactFinding{{Code: "impact_asset_change_invalid", Message: "asset_change is limited to the Volumes Root or Meta asset"}}
		}
		writeVolumes[candidate.VolumeID] = true
		upgradeReasons[candidate.VolumeID+"_change"] = true
		addImpactReason(reasons, ImpactReason{Code: "direct_asset_change", Volume: candidate.VolumeID})
		return nil
	case ImpactChangeVolumeCreate, ImpactChangeVolumeDelete:
		if set.LayoutMode != LayoutVolumesV1 || (candidate.VolumeID != "code" && candidate.VolumeID != "database") {
			return []ImpactFinding{{Code: "impact_volume_change_invalid", Message: "volume create/delete requires a supported Volumes v1 object Volume"}}
		}
		present := set.Volumes[candidate.VolumeID] != nil
		if change == ImpactChangeVolumeCreate && present {
			return []ImpactFinding{{Code: "impact_volume_already_present", Message: candidate.VolumeID + " Volume is already declared"}}
		}
		if change == ImpactChangeVolumeDelete && !present {
			return []ImpactFinding{{Code: "impact_volume_absent", Message: candidate.VolumeID + " Volume is not declared"}}
		}
		writeVolumes["root"] = true
		writeVolumes[candidate.VolumeID] = true
		upgradeReasons[change] = true
		addImpactReason(reasons, ImpactReason{Code: change, Volume: candidate.VolumeID})
		return nil
	case ImpactChangeLayout, ImpactChangeMigration:
		if set.LayoutMode != LayoutVolumesV1 {
			return []ImpactFinding{{Code: "impact_structural_change_invalid", Message: change + " requires a Volumes v1 CognitionSet prototype"}}
		}
		writeVolumes["root"] = true
		upgradeReasons[change] = true
		addImpactReason(reasons, ImpactReason{Code: change, Volume: "root"})
		return nil
	default:
		return []ImpactFinding{{Code: "impact_change_invalid", Message: "unsupported impact change " + change}}
	}
}

func applyObjectImpactCandidate(
	set *Set,
	current, projected impactRegistry,
	candidate ImpactCandidate,
	direct map[string]map[string]bool,
	writeVolumes, upgradeReasons map[string]bool,
	reasons map[string]ImpactReason,
) []ImpactFinding {
	change := candidate.Change
	ref := candidate.ObjectRef
	previous := candidate.PreviousObjectRef
	if ref != strings.TrimSpace(ref) || previous != strings.TrimSpace(previous) {
		return []ImpactFinding{{Code: "impact_object_identity_invalid", ObjectRef: ref, Message: "object identities may not contain surrounding whitespace"}}
	}
	if ref == "" && change != ImpactChangeDelete {
		return []ImpactFinding{{Code: "impact_object_ref_missing", Message: "object change requires a canonical object_ref"}}
	}
	if change == ImpactChangeDelete {
		if ref == "" {
			ref = previous
		}
		object, ok := current[ref]
		if !ok {
			return []ImpactFinding{{Code: "impact_object_missing", ObjectRef: ref, Message: "delete target does not exist in the current CognitionSet"}}
		}
		delete(projected, ref)
		markDirectImpact(direct, ref, "direct_delete")
		writeVolumes[object.VolumeID] = true
		addImpactReason(reasons, ImpactReason{Code: "direct_delete", From: ref, Volume: object.VolumeID})
		return nil
	}

	if change == ImpactChangeRename {
		if previous == "" {
			return []ImpactFinding{{Code: "impact_previous_ref_missing", ObjectRef: ref, Message: "rename requires previous_object_ref"}}
		}
		oldObject, ok := current[previous]
		if !ok {
			return []ImpactFinding{{Code: "impact_object_missing", ObjectRef: previous, Message: "rename source does not exist in the current CognitionSet"}}
		}
		if _, exists := current[ref]; exists {
			return []ImpactFinding{{Code: "impact_object_already_present", ObjectRef: ref, Message: "rename target already exists"}}
		}
		newObject, candidateFindings := parseImpactCandidateObject(set, candidate)
		if len(candidateFindings) > 0 {
			return candidateFindings
		}
		if boundaryFindings := validateVolumeAuthoringDelta(set, candidate, &oldObject, newObject); len(boundaryFindings) > 0 {
			return boundaryFindings
		}
		if newObject.VolumeID != oldObject.VolumeID {
			return []ImpactFinding{{Code: "impact_cross_volume_rename", ObjectRef: ref, Message: "cross-Volume identity changes require explicit delete and create candidates"}}
		}
		delete(projected, previous)
		projected[ref] = newObject
		markDirectImpact(direct, previous, "identity_previous")
		markDirectImpact(direct, ref, "direct_rename")
		writeVolumes[newObject.VolumeID] = true
		upgradeReasons["identity_change"] = true
		addImpactReason(reasons, ImpactReason{Code: "identity_change", From: previous, To: ref, Volume: newObject.VolumeID})
		return nil
	}

	previousObject, exists := current[ref]
	if change == ImpactChangeUpdate && !exists {
		return []ImpactFinding{{Code: "impact_object_missing", ObjectRef: ref, Message: "update target does not exist in the current CognitionSet"}}
	}
	if change == ImpactChangeCreate && exists {
		return []ImpactFinding{{Code: "impact_object_already_present", ObjectRef: ref, Message: "create target already exists in the current CognitionSet"}}
	}
	object, candidateFindings := parseImpactCandidateObject(set, candidate)
	if len(candidateFindings) > 0 {
		return candidateFindings
	}
	var previousObjectPointer *Object
	if exists {
		previousObjectPointer = &previousObject
	}
	if boundaryFindings := validateVolumeAuthoringDelta(set, candidate, previousObjectPointer, object); len(boundaryFindings) > 0 {
		return boundaryFindings
	}
	projected[ref] = object
	markDirectImpact(direct, ref, "direct_"+change)
	writeVolumes[object.VolumeID] = true
	addImpactReason(reasons, ImpactReason{Code: "direct_" + change, From: ref, Volume: object.VolumeID})
	return nil
}

func parseImpactCandidateObject(set *Set, candidate ImpactCandidate) (Object, []ImpactFinding) {
	entry, ok := index.ParseEntryLine(candidate.CanonicalLine, 0)
	if !ok {
		return Object{}, []ImpactFinding{{
			Code: "impact_candidate_fras_invalid", RuleCode: "fras_structure_invalid",
			Field: "FRAS", Expected: "canonical_object_line=true", Actual: "canonical_object_line=false",
			ObjectRef: candidate.ObjectRef, CanonicalObjectIdentity: candidate.ObjectRef,
			Cause:   "candidate must contain one complete canonical FRAS object line",
			Message: "candidate must contain one complete canonical FRAS object line",
		}}
	}
	frasFindings := validateFRASV2(entry)
	if len(frasFindings) > 0 {
		result := make([]ImpactFinding, 0, len(frasFindings))
		for _, finding := range frasFindings {
			result = append(result, ImpactFinding{
				Code: "impact_candidate_fras_invalid", RuleCode: finding.RuleCode,
				Field: finding.Field, Expected: finding.Expected, Actual: finding.Actual,
				ObjectRef: candidate.ObjectRef, CanonicalObjectIdentity: candidate.ObjectRef,
				Cause: finding.Cause, Message: finding.Message,
			})
		}
		return Object{}, result
	}
	ref := strings.TrimSpace(candidate.ObjectRef)
	if strings.HasPrefix(ref, "code:") {
		rel, err := afs.NormalizeRelPath(strings.TrimPrefix(ref, "code:"))
		if err != nil || ref != "code:"+rel || path.Base(rel) != entry.Filename {
			return Object{}, []ImpactFinding{{Code: "impact_object_identity_invalid", ObjectRef: ref, Message: "code object_ref must exactly match code:<normalized-path> and the candidate filename"}}
		}
		if set.LayoutMode == LayoutVolumesV1 && set.Volumes["code"] == nil {
			return Object{}, []ImpactFinding{{Code: "impact_volume_absent", ObjectRef: ref, Message: "code Volume is not declared"}}
		}
		if finding := validateImpactCandidateVolume(set, candidate, "code", entry); finding != nil {
			return Object{}, []ImpactFinding{*finding}
		}
		return Object{VolumeID: "code", Kind: "file", Name: entry.Filename, CanonicalRef: ref, Entry: entry, CanonicalLine: entry.FullLine}, nil
	}
	if IsCanonicalDatabaseRef(ref) {
		parts := strings.Split(ref, "/")
		if parts[len(parts)-1] != entry.Filename {
			return Object{}, []ImpactFinding{{Code: "impact_object_identity_invalid", ObjectRef: ref, Message: "database object_ref table must match the candidate object name"}}
		}
		if set.LayoutMode == LayoutVolumesV1 && set.Volumes["database"] == nil {
			return Object{}, []ImpactFinding{{Code: "impact_volume_absent", ObjectRef: ref, Message: "database Volume is not declared"}}
		}
		if finding := validateImpactCandidateVolume(set, candidate, "database", entry); finding != nil {
			return Object{}, []ImpactFinding{*finding}
		}
		namespace := strings.TrimSuffix(ref, "/"+entry.Filename)
		return Object{VolumeID: "database", Kind: "table", Name: entry.Filename, Namespace: namespace, CanonicalRef: ref, Entry: entry, CanonicalLine: entry.FullLine}, nil
	}
	if set.LayoutMode == LayoutLegacyMonolithic {
		current := findObjectByRef(set.Root.Objects, ref)
		if current.CanonicalRef == "" || current.Name != entry.Filename {
			return Object{}, []ImpactFinding{{Code: "impact_object_identity_invalid", ObjectRef: ref, Message: "Legacy candidates must retain an existing exact object identity"}}
		}
		current.Entry = entry
		current.CanonicalLine = entry.FullLine
		return current, nil
	}
	return Object{}, []ImpactFinding{{Code: "impact_object_identity_invalid", ObjectRef: ref, Message: "object_ref must use a supported canonical cognition object identity"}}
}

func validateImpactCandidateVolume(set *Set, candidate ImpactCandidate, volumeID string, entry *index.Entry) *ImpactFinding {
	if candidate.VolumeID != "" && candidate.VolumeID != volumeID {
		return &ImpactFinding{Code: "impact_candidate_volume_mismatch", ObjectRef: candidate.ObjectRef, Message: "candidate volume_id does not match the canonical object identity"}
	}
	if set.LayoutMode != LayoutVolumesV1 {
		return nil
	}
	dictionary := index.ExtractScopedTagDict(string(set.Meta.Raw), volumeID)
	if dictionary == nil || !dictionary.HasObjectContract() {
		return &ImpactFinding{Code: "impact_meta_tag_dictionary_invalid", ObjectRef: candidate.ObjectRef, Message: "Meta lacks a parseable " + volumeID + " tag dictionary"}
	}
	violations := index.ValidateTagAgainstDict(entry.TagsParsed, entry.TagsRaw, dictionary)
	if len(violations) > 0 {
		causes := make([]string, 0, len(violations))
		for _, violation := range violations {
			causes = append(causes, violation.Cause)
		}
		return &ImpactFinding{
			Code: "impact_candidate_tag_dictionary_violation", RuleCode: "object_tag_dictionary_violation",
			Field: "tag", Expected: violations[0].Expected, Actual: violations[0].Actual,
			ObjectRef: candidate.ObjectRef, Cause: strings.Join(causes, "; "), Message: strings.Join(causes, "; "),
		}
	}
	return nil
}

func collectImpactObjects(set *Set) (impactRegistry, []ImpactFinding) {
	registry := impactRegistry{}
	objects := set.Root.Objects
	if set.LayoutMode == LayoutVolumesV1 {
		objects = nil
		for _, id := range set.DeclaredOrder {
			if asset := set.Volumes[id]; asset != nil {
				objects = append(objects, asset.Objects...)
			}
		}
	}
	var findings []ImpactFinding
	for _, object := range objects {
		if object.CanonicalRef == "" {
			findings = append(findings, ImpactFinding{Code: "impact_object_identity_missing", Message: "managed cognition object lacks a canonical identity"})
			continue
		}
		if previous, exists := registry[object.CanonicalRef]; exists {
			findings = append(findings, ImpactFinding{Code: "impact_object_identity_duplicate", ObjectRef: object.CanonicalRef, Candidates: []string{previous.VolumeID, object.VolumeID}, Message: "canonical object identity occurs more than once"})
			continue
		}
		registry[object.CanonicalRef] = object
	}
	return registry, findings
}

func addVolumeDependencyGuards(set *Set, guards map[string]bool, reasons map[string]ImpactReason) {
	changed := true
	for changed {
		changed = false
		for volume := range copyStringSet(guards) {
			asset := set.Volumes[volume]
			if asset == nil {
				continue
			}
			for _, dependency := range asset.Descriptor.DependsOn {
				if !guards[dependency] {
					guards[dependency] = true
					changed = true
				}
				addImpactReason(reasons, ImpactReason{Code: "volume_dependency_guard", From: volume, Volume: dependency})
			}
		}
	}
}

func orderImpactVolumes(set *Set, values map[string]bool) []string {
	order := []string{"root", "meta", "code", "database", "legacy"}
	seen := map[string]bool{}
	var result []string
	for _, id := range order {
		if values[id] && !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	for _, id := range set.DeclaredOrder {
		if values[id] && !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	var rest []string
	for id := range values {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	return append(result, rest...)
}

func impactFailure(result AffectedCognitionSet, findings ...ImpactFinding) (AffectedCognitionSet, error) {
	result.Findings = findings
	return result, &ImpactValidationError{Findings: findings}
}

func impactCandidateKey(candidate ImpactCandidate) string {
	return strings.Join([]string{candidate.Change, candidate.PreviousObjectRef, candidate.ObjectRef, candidate.VolumeID, candidate.CanonicalLine}, "\x00")
}

func impactCandidateClaims(candidate ImpactCandidate) []string {
	switch candidate.Change {
	case ImpactChangeUpdate, ImpactChangeCreate, ImpactChangeDelete:
		ref := candidate.ObjectRef
		if ref == "" {
			ref = candidate.PreviousObjectRef
		}
		return []string{"object\x00" + ref}
	case ImpactChangeRename:
		claims := []string{"object\x00" + candidate.PreviousObjectRef, "object\x00" + candidate.ObjectRef}
		sort.Strings(claims)
		return claims
	case ImpactChangeAsset, ImpactChangeVolumeCreate, ImpactChangeVolumeDelete:
		return []string{"volume\x00" + candidate.VolumeID}
	case ImpactChangeLayout, ImpactChangeMigration:
		return []string{"layout"}
	default:
		return nil
	}
}

func markDirectImpact(direct map[string]map[string]bool, ref, reason string) {
	if direct[ref] == nil {
		direct[ref] = map[string]bool{}
	}
	direct[ref][reason] = true
}

func addImpactReason(reasons map[string]ImpactReason, reason ImpactReason) {
	key := strings.Join([]string{reason.Code, reason.From, reason.To, reason.Volume}, "\x00")
	reasons[key] = reason
}

func orderedImpactReasons(reasons map[string]ImpactReason) []ImpactReason {
	keys := sortedKeys(reasons)
	result := make([]ImpactReason, 0, len(keys))
	for _, key := range keys {
		result = append(result, reasons[key])
	}
	return result
}

func sortedObjectRefs(registry impactRegistry) []string { return sortedKeys(registry) }

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneImpactRegistry(source impactRegistry) impactRegistry {
	clone := make(impactRegistry, len(source))
	for ref, object := range source {
		clone[ref] = object
	}
	return clone
}

func copyStringSet(source map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(source))
	for value, included := range source {
		if included {
			clone[value] = true
		}
	}
	return clone
}

func bindImpactCandidateFinding(finding *ImpactFinding, candidate ImpactCandidate) {
	if finding == nil {
		return
	}
	ref := candidate.ObjectRef
	if finding.ObjectRef != "" {
		ref = finding.ObjectRef
	}
	finding.ObjectRef = ref
	finding.CanonicalObjectIdentity = ref
	if candidate.Path != "" {
		finding.Path = candidate.Path
	}
	switch {
	case strings.HasPrefix(ref, "code:"):
		finding.Domain = "code"
		if finding.Path == "" {
			finding.Path = strings.TrimPrefix(ref, "code:")
		}
	case IsCanonicalDatabaseRef(ref):
		finding.Domain = "database"
	case candidate.VolumeID != "":
		finding.Domain = candidate.VolumeID
	}
	if finding.RuleCode == "" {
		finding.RuleCode = finding.Code
	}
	if finding.Field == "" {
		switch finding.Code {
		case "impact_object_identity_invalid", "impact_candidate_duplicate":
			finding.Field = "canonical_object_identity"
		case "impact_candidate_volume_mismatch":
			finding.Field = "domain"
		case "impact_candidate_tag_dictionary_violation":
			finding.Field = "tag"
		}
	}
	if finding.Expected == "" {
		switch finding.Code {
		case "impact_object_identity_invalid":
			finding.Expected = "identity=canonical_object_ref"
		case "impact_candidate_duplicate":
			finding.Expected = "candidate_count=1"
		case "impact_candidate_volume_mismatch":
			finding.Expected = "volume_id=canonical_identity_domain"
		case "impact_candidate_tag_dictionary_violation":
			finding.Expected = "tag=current_meta_dictionary_member"
		}
	}
	if finding.Actual == "" {
		switch finding.Code {
		case "impact_object_identity_invalid":
			finding.Actual = "identity=invalid"
		case "impact_candidate_duplicate":
			finding.Actual = "candidate_count=2"
		case "impact_candidate_volume_mismatch":
			finding.Actual = "volume_id=" + candidate.VolumeID
		case "impact_candidate_tag_dictionary_violation":
			finding.Actual = "tag=not_in_current_meta_dictionary"
		}
	}
	if finding.Cause == "" {
		finding.Cause = finding.Message
	}
}

func bindImpactRelationFinding(finding *ImpactFinding) {
	if finding == nil || !strings.HasPrefix(finding.Code, "impact_relation_") {
		return
	}
	finding.Field = "R"
	finding.Expected = "relation_target=managed_canonical_object"
	switch finding.Code {
	case "impact_relation_unresolved":
		finding.Actual = "relation_target=missing"
	case "impact_relation_ambiguous":
		finding.Actual = "relation_target=ambiguous"
	case "impact_relation_invalid":
		finding.Actual = "relation_format=invalid"
	}
}

func findObjectByRef(objects []Object, ref string) Object {
	for _, object := range objects {
		if object.CanonicalRef == ref {
			return object
		}
	}
	return Object{}
}
