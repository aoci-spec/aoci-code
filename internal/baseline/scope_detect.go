package baseline

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// DetectManagedScope separates formal index drift from observe-only evidence
// drift. Snapshot contains only index and observe paths and carries the desired
// role in each Fingerprint. Callers must first prove the policy identity equals
// Baseline.ManagedScope.PolicyIdentity; a direct policy edit is a Scope Change,
// not permission to reinterpret the active Baseline.
func DetectManagedScope(root string, document *index.Document, baselineValue *Baseline,
	snapshot map[string]Fingerprint, options afs.WalkOptions, tolerateLineEndings bool) *DetectResult {
	result := &DetectResult{Missing: []string{}, Orphan: []string{}, Stale: []string{}, Unbaselined: []string{},
		LineEndingOnly: []string{}, ObservedNew: []string{}, ObservedChanged: []string{}, ObservedRemoved: []string{}, Warnings: []string{}}
	indexFiles := map[string]bool{}
	indexDirectories := []string{}
	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			if entry.RelPath == "" || isExcludedRel(entry.RelPath, options) {
				continue
			}
			if strings.HasSuffix(entry.RelPath, "/") {
				indexDirectories = append(indexDirectories, entry.RelPath)
				continue
			}
			indexFiles[entry.RelPath] = true
		}
	}
	for rel, current := range snapshot {
		role := EffectiveRole(current)
		before, existed := Fingerprint{}, false
		if baselineValue != nil {
			before, existed = baselineValue.Files[rel]
		}
		switch role {
		case machinecontract.ScopeRoleIndex:
			if !indexFiles[rel] {
				result.Missing = append(result.Missing, rel)
			}
			if !existed || EffectiveRole(before) != machinecontract.ScopeRoleIndex {
				result.Unbaselined = append(result.Unbaselined, rel)
				continue
			}
			equal, lineEndingOnly := EquivalentFingerprints(before, current, tolerateLineEndings)
			if !equal {
				result.Stale = append(result.Stale, rel)
			} else if lineEndingOnly {
				result.LineEndingOnly = append(result.LineEndingOnly, rel)
			}
		case machinecontract.ScopeRoleObserve:
			if indexFiles[rel] {
				result.Orphan = append(result.Orphan, rel)
			}
			if !existed || EffectiveRole(before) != machinecontract.ScopeRoleObserve {
				result.ObservedNew = append(result.ObservedNew, rel)
				continue
			}
			equal, _ := EquivalentFingerprints(before, current, tolerateLineEndings)
			if !equal {
				result.ObservedChanged = append(result.ObservedChanged, rel)
			}
		}
	}
	if baselineValue != nil {
		for rel, before := range baselineValue.Files {
			if _, exists := snapshot[rel]; exists {
				continue
			}
			if EffectiveRole(before) == machinecontract.ScopeRoleObserve {
				result.ObservedRemoved = append(result.ObservedRemoved, rel)
			} else if indexFiles[rel] {
				result.Orphan = append(result.Orphan, rel)
			}
		}
	}
	for rel := range indexFiles {
		if _, exists := snapshot[rel]; !exists {
			result.Orphan = append(result.Orphan, rel)
		}
	}
	for _, relDirectory := range indexDirectories {
		absolute := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(relDirectory, "/")))
		if stat, err := os.Stat(absolute); err != nil || !stat.IsDir() {
			result.Orphan = append(result.Orphan, relDirectory)
		}
	}
	result.Missing = sortedUnique(result.Missing)
	result.Orphan = sortedUnique(result.Orphan)
	result.Stale = sortedUnique(result.Stale)
	result.Unbaselined = sortedUnique(result.Unbaselined)
	result.LineEndingOnly = sortedUnique(result.LineEndingOnly)
	result.ObservedNew = sortedUnique(result.ObservedNew)
	result.ObservedChanged = sortedUnique(result.ObservedChanged)
	result.ObservedRemoved = sortedUnique(result.ObservedRemoved)
	return result
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
