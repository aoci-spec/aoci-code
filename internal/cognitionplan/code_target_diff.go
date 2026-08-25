package cognitionplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// CodeTargetDiffChange is one object-level difference between the active Code
// Volume and a complete, non-authoritative target Code Volume.
type CodeTargetDiffChange struct {
	ObjectRef     string   `json:"object_ref"`
	Change        string   `json:"change"`
	ChangedFields []string `json:"changed_fields"`
	CurrentEntry  string   `json:"current_entry,omitempty"`
	TargetEntry   string   `json:"target_entry,omitempty"`
}

type CodeTargetDiffSummary struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
}

// CodeTargetDiff is a read-only implementation plan. It never becomes formal
// cognition and cannot authorize Apply; after code is stable, ordinary AOCI
// maintenance must author and bind the formal postimage to current source.
type CodeTargetDiff struct {
	Version                    string                         `json:"version"`
	Status                     string                         `json:"status"`
	BaseCompositeIdentity      string                         `json:"base_composite_identity"`
	ProjectedCompositeIdentity string                         `json:"projected_composite_identity"`
	BaseCodeSHA256             string                         `json:"base_code_sha256"`
	TargetCodeSHA256           string                         `json:"target_code_sha256"`
	DiffSHA256                 string                         `json:"diff_sha256"`
	RawBytesChanged            bool                           `json:"raw_bytes_changed"`
	FormalTextOnly             bool                           `json:"formal_text_only"`
	Summary                    CodeTargetDiffSummary          `json:"summary"`
	Changes                    []CodeTargetDiffChange         `json:"changes"`
	AffectedCognition          cognition.AffectedCognitionSet `json:"affected_cognition"`
	Derived                    bool                           `json:"derived"`
	Authoritative              bool                           `json:"authoritative"`
	SourceBound                bool                           `json:"source_bound"`
	ApplyAllowed               bool                           `json:"apply_allowed"`
	FormalWritesStarted        bool                           `json:"formal_writes_started"`
	NetworkAccessed            bool                           `json:"network_accessed"`
	BusinessDataRead           bool                           `json:"business_data_read"`
	NextAction                 string                         `json:"next_action"`
}

type CodeTargetValidationError struct {
	Findings []cognition.Finding
}

func (e *CodeTargetValidationError) Error() string {
	if len(e.Findings) == 0 {
		return "code_target_index_invalid"
	}
	return "code_target_index_invalid: " + e.Findings[0].Code + ": " + e.Findings[0].Message
}

// CompareCodeTargetIndex compares the active Code Volume with a complete
// target Code Volume. The target is parsed only in memory and is deliberately
// not checked against current source bytes: it describes implementation intent,
// not current repository truth.
func CompareCodeTargetIndex(repositoryRoot, indexPath string, targetRaw []byte) (*CodeTargetDiff, error) {
	if len(targetRaw) == 0 || len(targetRaw) > machinecontract.EntriesRequestMaxBytes {
		return nil, fmt.Errorf("code_target_index_size_invalid")
	}
	current, err := cognition.Load(repositoryRoot, indexPath)
	if err != nil {
		return nil, err
	}
	if current.LayoutMode != cognition.LayoutVolumesV1 || current.Volumes[cognition.ScopeCode] == nil ||
		current.Volumes[cognition.ScopeCode].State != cognition.AssetPresent {
		return nil, fmt.Errorf("code_target_index_requires_active_code_volume")
	}
	projected, findings := cognition.ProjectObjectVolume(current, cognition.ScopeCode, targetRaw)
	if len(findings) != 0 {
		return nil, &CodeTargetValidationError{Findings: findings}
	}
	for _, conflict := range cognition.OwnershipConflicts(projected) {
		findings = append(findings, cognition.Finding{Code: "volume_ownership_conflict", AssetID: conflict.ActualOwner,
			Message: conflict.ObjectRef + " belongs to " + conflict.ExpectedOwner + ", not " + conflict.ActualOwner})
	}
	if len(findings) != 0 {
		return nil, &CodeTargetValidationError{Findings: findings}
	}

	baseAsset := current.Volumes[cognition.ScopeCode]
	targetAsset := projected.Volumes[cognition.ScopeCode]
	baseObjects := codeTargetObjectMap(baseAsset.Objects)
	targetObjects := codeTargetObjectMap(targetAsset.Objects)
	refs := make([]string, 0, len(baseObjects)+len(targetObjects))
	seen := make(map[string]bool, len(baseObjects)+len(targetObjects))
	for ref := range baseObjects {
		seen[ref] = true
		refs = append(refs, ref)
	}
	for ref := range targetObjects {
		if !seen[ref] {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)

	changes := make([]CodeTargetDiffChange, 0)
	candidates := make([]cognition.ImpactCandidate, 0)
	summary := CodeTargetDiffSummary{}
	for _, ref := range refs {
		before, beforeExists := baseObjects[ref]
		after, afterExists := targetObjects[ref]
		switch {
		case !beforeExists:
			summary.Created++
			changes = append(changes, CodeTargetDiffChange{ObjectRef: ref, Change: cognition.ImpactChangeCreate,
				ChangedFields: codeTargetWholeEntryFields(), TargetEntry: after.CanonicalLine})
			candidates = append(candidates, codeTargetImpactCandidate(cognition.ImpactChangeCreate, after, len(candidates)+1))
		case !afterExists:
			summary.Deleted++
			changes = append(changes, CodeTargetDiffChange{ObjectRef: ref, Change: cognition.ImpactChangeDelete,
				ChangedFields: codeTargetWholeEntryFields(), CurrentEntry: before.CanonicalLine})
			candidates = append(candidates, cognition.ImpactCandidate{Change: cognition.ImpactChangeDelete, ObjectRef: ref,
				VolumeID: cognition.ScopeCode, Path: strings.TrimPrefix(ref, "code:"), OriginalCandidateIndex: len(candidates) + 1})
		case before.CanonicalLine != after.CanonicalLine:
			summary.Updated++
			changes = append(changes, CodeTargetDiffChange{ObjectRef: ref, Change: cognition.ImpactChangeUpdate,
				ChangedFields: codeTargetChangedFields(before, after), CurrentEntry: before.CanonicalLine, TargetEntry: after.CanonicalLine})
			candidates = append(candidates, codeTargetImpactCandidate(cognition.ImpactChangeUpdate, after, len(candidates)+1))
		default:
			summary.Unchanged++
		}
	}

	affected := emptyCodeTargetAffected(current.LayoutMode)
	if len(candidates) != 0 {
		affected, err = cognition.ResolveImpact(current, candidates)
		if err != nil {
			return nil, fmt.Errorf("code_target_impact_invalid: %w", err)
		}
	}
	status := machinecontract.CognitionCodeTargetDiffReady
	nextAction := machinecontract.CognitionCodeTargetDiffNextAction
	if len(changes) == 0 {
		status = machinecontract.CognitionCodeTargetDiffNoChange
		nextAction = "none"
	}
	report := &CodeTargetDiff{
		Version: machinecontract.CognitionCodeTargetDiffV1, Status: status,
		BaseCompositeIdentity: current.CompositeIdentity, ProjectedCompositeIdentity: projected.CompositeIdentity,
		BaseCodeSHA256: baseAsset.SHA256, TargetCodeSHA256: targetAsset.SHA256,
		RawBytesChanged: baseAsset.SHA256 != targetAsset.SHA256,
		Summary:         summary, Changes: changes, AffectedCognition: affected,
		Derived: true, Authoritative: false, SourceBound: false, ApplyAllowed: false,
		FormalWritesStarted: false, NetworkAccessed: false, BusinessDataRead: false, NextAction: nextAction,
	}
	report.FormalTextOnly = report.RawBytesChanged && len(report.Changes) == 0
	report.DiffSHA256 = codeTargetDiffIdentity(report)
	return report, nil
}

func codeTargetObjectMap(objects []cognition.Object) map[string]cognition.Object {
	result := make(map[string]cognition.Object, len(objects))
	for _, object := range objects {
		result[object.CanonicalRef] = object
	}
	return result
}

func codeTargetImpactCandidate(change string, object cognition.Object, index int) cognition.ImpactCandidate {
	return cognition.ImpactCandidate{Change: change, ObjectRef: object.CanonicalRef, VolumeID: cognition.ScopeCode,
		Path: strings.TrimPrefix(object.CanonicalRef, "code:"), CanonicalLine: object.CanonicalLine, OriginalCandidateIndex: index}
}

func codeTargetWholeEntryFields() []string {
	return []string{"tag", "F", "R", "A", "S"}
}

func codeTargetChangedFields(before, after cognition.Object) []string {
	fields := make([]string, 0, 5)
	if before.Entry.TagsRaw != after.Entry.TagsRaw {
		fields = append(fields, "tag")
	}
	if before.Entry.F != after.Entry.F {
		fields = append(fields, "F")
	}
	if before.Entry.R != after.Entry.R {
		fields = append(fields, "R")
	}
	if before.Entry.Api != after.Entry.Api {
		fields = append(fields, "A")
	}
	if before.Entry.S != after.Entry.S {
		fields = append(fields, "S")
	}
	if len(fields) == 0 {
		fields = append(fields, "format")
	}
	return fields
}

func emptyCodeTargetAffected(layout string) cognition.AffectedCognitionSet {
	return cognition.AffectedCognitionSet{LayoutMode: layout, ReviewSet: []cognition.ImpactReviewObject{},
		WriteSet: []string{}, GuardSet: []string{}, Reasons: []cognition.ImpactReason{},
		Strategy: cognition.ImpactStrategyDependencyClosure, UpgradeReason: []string{}, Findings: []cognition.ImpactFinding{}}
}

func codeTargetDiffIdentity(report *CodeTargetDiff) string {
	identity := newIdentity("code_target_index_diff")
	identity.field("version", report.Version)
	identity.field("base_composite_identity", report.BaseCompositeIdentity)
	identity.field("projected_composite_identity", report.ProjectedCompositeIdentity)
	identity.field("base_code_sha256", report.BaseCodeSHA256)
	identity.field("target_code_sha256", report.TargetCodeSHA256)
	for _, change := range report.Changes {
		identity.field("change", change.Change)
		identity.field("object_ref", change.ObjectRef)
		identity.field("changed_fields", strings.Join(change.ChangedFields, ","))
		identity.field("current_entry", change.CurrentEntry)
		identity.field("target_entry", change.TargetEntry)
	}
	return identity.sum()
}
