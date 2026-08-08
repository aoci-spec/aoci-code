package cognition

import (
	"path/filepath"
	"strings"
)

const (
	OwnerRoot     = "root"
	OwnerMeta     = "meta"
	OwnerCode     = "code"
	OwnerDatabase = "database"
)

// OwnershipConflict reports one object whose physical Volume disagrees with
// the single deterministic owner of its canonical identity.
type OwnershipConflict struct {
	ObjectRef     string `json:"object_ref"`
	Path          string `json:"path"`
	ExpectedOwner string `json:"expected_owner"`
	ActualOwner   string `json:"actual_owner"`
}

// ExpectedOwner classifies every Volumes v1 object identity into exactly one
// owner. Formal asset paths come from the same production registry used by
// loading, planning, Bootstrap, and Migration; all other repository paths are
// Code-owned and canonical database identities are Database-owned.
func ExpectedOwner(identity string) string {
	value := strings.TrimSpace(identity)
	if strings.HasPrefix(value, "database://") {
		return OwnerDatabase
	}
	value = strings.TrimPrefix(value, "code:")
	path := filepath.ToSlash(value)
	if owner, formal := FormalAssetOwner(path); formal {
		return owner
	}
	return OwnerCode
}

// FormalAssetOwner reports whether a repository path is one of the Root,
// Meta, Code, or Database formal assets and returns its single owner.
func FormalAssetOwner(path string) (string, bool) {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	if strings.EqualFold(normalized, "aoci.txt") {
		return OwnerRoot, true
	}
	for _, registration := range volumeRegistryEntries {
		if strings.EqualFold(normalized, registration.Path) {
			return registration.Kind, true
		}
	}
	return "", false
}

// OwnershipConflicts applies ExpectedOwner to every formal object without
// changing or repairing the CognitionSet.
func OwnershipConflicts(set *Set) []OwnershipConflict {
	if set == nil || set.LayoutMode != LayoutVolumesV1 {
		return nil
	}
	conflicts := []OwnershipConflict{}
	for _, actualOwner := range []string{OwnerCode, OwnerDatabase} {
		asset := set.Volumes[actualOwner]
		if asset == nil {
			continue
		}
		for _, object := range asset.Objects {
			expectedOwner := ExpectedOwner(object.CanonicalRef)
			if expectedOwner == actualOwner {
				continue
			}
			path := strings.TrimPrefix(object.CanonicalRef, "code:")
			conflicts = append(conflicts, OwnershipConflict{
				ObjectRef: object.CanonicalRef, Path: path,
				ExpectedOwner: expectedOwner, ActualOwner: actualOwner,
			})
		}
	}
	return conflicts
}
