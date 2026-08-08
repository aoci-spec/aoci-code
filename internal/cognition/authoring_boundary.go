package cognition

import (
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// validateVolumeAuthoringDelta separates tolerant formal-asset reads from the
// strict contract for newly authored candidate bytes. Existing dotted tags and
// bare relations remain loadable; only create or a true field change invokes
// the corresponding strict write gate.
func validateVolumeAuthoringDelta(set *Set, candidate ImpactCandidate, previous *Object, next Object) []ImpactFinding {
	if set == nil || set.LayoutMode != LayoutVolumesV1 || next.Entry == nil {
		return nil
	}
	create := candidate.Change == ImpactChangeCreate || previous == nil || previous.Entry == nil
	tagChanged := create || previous.Entry.TagsRaw != next.Entry.TagsRaw
	relationChanged := create || previous.Entry.R != next.Entry.R
	findings := []ImpactFinding{}
	if tagChanged && !isCompactAuthoringTag(next.Entry.TagsRaw) {
		findings = append(findings, ImpactFinding{
			Code: "impact_candidate_tag_not_compact", RuleCode: "impact_candidate_tag_not_compact",
			Field: "tag", Expected: "format=ABCDE_compact", Actual: next.Entry.TagsRaw,
			ObjectRef: candidate.ObjectRef, CanonicalObjectIdentity: candidate.ObjectRef,
			Cause:   "new or changed Volume tags must use compact A+B+C+[D]+E authoring form",
			Message: "new or changed Volume tags must use compact A+B+C+[D]+E authoring form",
		})
	}
	if relationChanged {
		for _, token := range nonCanonicalAuthoringRelations(next.Entry.R) {
			findings = append(findings, ImpactFinding{
				Code: "impact_candidate_relation_not_canonical", RuleCode: "impact_candidate_relation_not_canonical",
				Field: "R", Expected: "canonical_object_identity", Actual: token,
				ObjectRef: candidate.ObjectRef, CanonicalObjectIdentity: candidate.ObjectRef, Relation: token,
				Cause:   "new or changed Volume relations must use exact canonical object identities",
				Message: "new or changed Volume relations must use exact canonical object identities",
			})
		}
	}
	return findings
}

func isCompactAuthoringTag(raw string) bool {
	if raw == "" || strings.Contains(raw, ".") {
		return false
	}
	parsed := index.ParseTags(raw)
	if parsed["A"] == "" || parsed["B"] == "" || parsed["C"] == "" || parsed["E"] == "" {
		return false
	}
	return raw == parsed["A"]+parsed["B"]+parsed["C"]+parsed["D"]+parsed["E"]
}

func nonCanonicalAuthoringRelations(value string) []string {
	value = strings.TrimSpace(value)
	if value == "-" {
		return nil
	}
	parts := strings.Split(value, ",")
	invalid := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if !isCanonicalAuthoringRelation(token) {
			invalid = append(invalid, token)
		}
	}
	return invalid
}

func isCanonicalAuthoringRelation(token string) bool {
	if IsCanonicalDatabaseRef(token) {
		return true
	}
	if !strings.HasPrefix(token, "code:") {
		return false
	}
	rel, err := afs.NormalizeRelPath(strings.TrimPrefix(token, "code:"))
	return err == nil && token == "code:"+rel
}
