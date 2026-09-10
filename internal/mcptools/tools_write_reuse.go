package mcptools

import (
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// resolveReuseExistingItems replaces each reuse_existing item's absent entry
// with the exact current formal Entry line of its object, read from the set
// the planner itself loaded, so the bytes it resubmits and the preimage the
// batch is committed against are one and the same. It is the machine copying
// bytes the model could have copied by hand: no text is generated, and the
// byte-identical resubmission then takes the existing duplicate-apply path.
// A reuse_existing item that also carries new_entry, or names an object
// without a current Entry, is a Repair Finding before any write, indexed by
// the candidate's original position in the submitted array.
func resolveReuseExistingItems(set *cognition.Set, normalized []normalizedAtomicItem) *Fail {
	findings := []cognition.RepairFinding{}
	for position := range normalized {
		item := &normalized[position]
		if !item.reuseExisting {
			continue
		}
		identity, domain := item.objectRef, cognition.ScopeDatabase
		if item.rel != "" {
			identity, domain = "code:"+item.rel, cognition.ScopeCode
		}
		if item.newEntry != "" {
			findings = append(findings, cognition.RepairFinding{
				CandidateIndex: item.originalCandidateIndex, Path: item.rel, CanonicalObjectIdentity: identity, Domain: domain,
				Field: "new_entry", RuleCode: "reuse_existing_conflicts_with_new_entry",
				Expected: "new_entry=absent", Actual: "new_entry=present",
				Cause:            "reuse_existing resubmits the current Entry bytes, so new_entry must be absent",
				SafeRepairAction: "remove new_entry to keep the current Entry, or remove reuse_existing to submit the new text",
				Code:             "reuse_existing_conflicts_with_new_entry", ObjectRef: identity,
			})
			continue
		}
		object := reuseExistingObject(set, identity)
		if object == nil || object.CanonicalLine == "" {
			findings = append(findings, cognition.RepairFinding{
				CandidateIndex: item.originalCandidateIndex, Path: item.rel, CanonicalObjectIdentity: identity, Domain: domain,
				Field: "new_entry", RuleCode: "reuse_existing_requires_existing_entry",
				Expected: "existing_entry=present", Actual: "existing_entry=absent",
				Cause:            "the object has no current formal Entry to reuse",
				SafeRepairAction: "author new_entry for this candidate instead of reuse_existing",
				Code:             "reuse_existing_requires_existing_entry", ObjectRef: identity,
			})
			continue
		}
		item.newEntry = object.CanonicalLine
	}
	if len(findings) > 0 {
		return &Fail{Code: errCandidateInvalid, Msg: "reuse_existing_invalid", Findings: findings, Repairable: true}
	}
	return nil
}

// reuseExistingObject finds the current formal object for an identity in any
// declared Volume; a Legacy monolithic Index keys its objects by bare path.
func reuseExistingObject(set *cognition.Set, identity string) *cognition.Object {
	if set == nil {
		return nil
	}
	if set.LayoutMode == cognition.LayoutLegacyMonolithic {
		return cognitionObjectByRef(&set.Root, strings.TrimPrefix(identity, "code:"))
	}
	for _, volume := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		if object := cognitionObjectByRef(set.Volumes[volume], identity); object != nil {
			return object
		}
	}
	return nil
}
