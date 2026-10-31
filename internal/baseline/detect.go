// Snapshot collection and the four-state difference.
//
// Raw facts:
//
//	Missing     = on disk, not in the index;
//	Orphan      = in the index, not on disk;
//	Stale       = on both sides, but the fingerprints are not equivalent;
//	Unbaselined = on both sides, but the Baseline lacks the file.
//
// LineEndingOnly is an additional informational state: the raw bytes differ, but both optional normalized fingerprints agree.
// Whether that equivalence is allowed must be passed explicitly by the caller; the old Detect and IsStaleFile keep strict semantics.
//
// Exclusion reading consistency: index-side entries and the disk snapshot use one set of exclusion rules.
// Every slice is initialized empty and sorted, so the JSON is stable and never null.
package baseline

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// DetectResult holds the raw four states and the optional line-ending equivalence information.
type DetectResult struct {
	Missing         []string `json:"missing"`
	Orphan          []string `json:"orphan"`
	Stale           []string `json:"stale"`
	Unbaselined     []string `json:"unbaselined"`
	LineEndingOnly  []string `json:"line_ending_only,omitempty"`
	ObservedNew     []string `json:"observed_new,omitempty"`
	ObservedChanged []string `json:"observed_changed,omitempty"`
	ObservedRemoved []string `json:"observed_removed,omitempty"`
	Warnings        []string `json:"warnings"`
}

// Snapshot walks the repository and computes every file fingerprint.
//
// A single file's hash failure does not block the whole and is returned as a warning.
func Snapshot(
	root string,
	options afs.WalkOptions,
) (
	map[string]Fingerprint,
	[]string,
	error,
) {
	snapshot, warnings, _, err := SnapshotWithInventory(root, options)
	return snapshot, warnings, err
}

// SnapshotWithInventory returns the same Baseline fingerprints plus the
// Safe Inventory v2 receipt that selected which paths were allowed to be read.
func SnapshotWithInventory(root string, options afs.WalkOptions) (map[string]Fingerprint, []string, *afs.SafeInventory, error) {
	inventory, err := afs.BuildSafeInventory(root, options)
	if err != nil {
		return nil, nil, nil, err
	}
	files := inventory.ManagedCandidates

	snapshot := make(
		map[string]Fingerprint,
		len(files),
	)
	warnings := []string{}
	cache := OpenFingerprintCache(root)
	defer cache.Save(true)

	for _, relPath := range files {
		fingerprint, err := cache.HashFile(
			filepath.Join(
				root,
				filepath.FromSlash(relPath),
			),
			relPath,
			Fingerprint{},
		)
		if err != nil {
			warnings = append(
				warnings,
				"读取失败跳过: "+
					relPath+
					" ("+
					err.Error()+
					")",
			)
			continue
		}

		snapshot[relPath] = fingerprint
	}

	return snapshot, warnings, inventory, nil
}

// EquivalentFingerprints is the single entry of fingerprint equivalence judgement.
//
// Return values:
//   - equal=true, lineEndingOnly=false: the raw bytes agree exactly;
//   - equal=true, lineEndingOnly=true: only under explicit tolerance, the normalized fingerprints agree;
//   - equal=false: real drift exists, or either side lacks a normalized fingerprint.
//
// When either side lacks NormalizedSHA256 it falls back to strict comparison, so an old Baseline, a binary file,
// and an oversize file are never silently relaxed.
func EquivalentFingerprints(
	baselineFingerprint Fingerprint,
	currentFingerprint Fingerprint,
	tolerateLineEndings bool,
) (
	equal bool,
	lineEndingOnly bool,
) {
	if baselineFingerprint.SHA256 ==
		currentFingerprint.SHA256 {
		return true, false
	}

	if !tolerateLineEndings {
		return false, false
	}

	if baselineFingerprint.NormalizedSHA256 == "" ||
		currentFingerprint.NormalizedSHA256 == "" {
		return false, false
	}

	if baselineFingerprint.NormalizedSHA256 ==
		currentFingerprint.NormalizedSHA256 {
		return true, true
	}

	return false, false
}

// isExcludedRel judges whether an index entry path falls inside the same exclusion reading as the disk snapshot.
//
// The configured list has one implementation: the directory list is delegated to fs.ConfiguredExclusionSource, judging exclude_dirs
// and exclude_root_dirs the same way as the Safe Inventory (before rc20 this place carried its own implementation that knew only
// exclude_dirs, and entries under a new repository's backup/ were reported Orphan instead of skipped).
// A directory entry keeps its trailing slash, and its last segment is still a directory to the list; file patterns still match
// the name with the slash stripped.
func isExcludedRel(
	relPath string,
	options afs.WalkOptions,
) bool {
	if category, _ := afs.BuiltInSafetyCategory(relPath, options.SensitiveNameRules); category != "" {
		return true
	}

	clean := strings.TrimSuffix(
		relPath,
		"/",
	)
	directories := strings.Split(
		clean,
		"/",
	)
	if !strings.HasSuffix(relPath, "/") {
		directories = directories[:len(directories)-1]
	}
	for _, segment := range directories {
		if segment == ".aoci" {
			return true
		}
	}

	if _, excluded := afs.ConfiguredExclusionSource(
		relPath,
		afs.WalkOptions{
			ExcludeDirs:     options.ExcludeDirs,
			ExcludeRootDirs: options.ExcludeRootDirs,
		},
	); excluded {
		return true
	}

	return afs.MatchExcludePattern(
		clean,
		options.ExcludeFiles,
	)
}

// Detect keeps the historical strict semantics.
//
// Existing callers keep comparing raw bytes until they explicitly adopt the configuration.
func Detect(
	root string,
	document *index.Document,
	baselineValue *Baseline,
	snapshot map[string]Fingerprint,
	options afs.WalkOptions,
) *DetectResult {
	return DetectWith(
		root,
		document,
		baselineValue,
		snapshot,
		options,
		false,
	)
}

// DetectWith computes the four-state difference and the line-ending equivalence information by explicit parameters.
func DetectWith(
	root string,
	document *index.Document,
	baselineValue *Baseline,
	snapshot map[string]Fingerprint,
	options afs.WalkOptions,
	tolerateLineEndings bool,
) *DetectResult {
	result := &DetectResult{
		Missing:         []string{},
		Orphan:          []string{},
		Stale:           []string{},
		Unbaselined:     []string{},
		LineEndingOnly:  []string{},
		ObservedNew:     []string{},
		ObservedChanged: []string{},
		ObservedRemoved: []string{},
		Warnings:        []string{},
	}

	indexFiles := map[string]bool{}
	indexDirectories := []string{}

	for _, section := range document.Sections {
		for _, entry := range section.Entries {
			if entry.RelPath == "" ||
				isExcludedRel(
					entry.RelPath,
					options,
				) {
				continue
			}

			if strings.HasSuffix(
				entry.RelPath,
				"/",
			) {
				indexDirectories = append(
					indexDirectories,
					entry.RelPath,
				)
				continue
			}

			indexFiles[entry.RelPath] = true
		}
	}

	for relPath := range snapshot {
		if !indexFiles[relPath] {
			result.Missing = append(
				result.Missing,
				relPath,
			)
		}
	}

	for relPath := range indexFiles {
		currentFingerprint, onDisk := snapshot[relPath]
		if !onDisk {
			result.Orphan = append(
				result.Orphan,
				relPath,
			)
			continue
		}

		if baselineValue == nil {
			result.Unbaselined = append(
				result.Unbaselined,
				relPath,
			)
			continue
		}

		baselineFingerprint, inBaseline :=
			baselineValue.Files[relPath]

		if !inBaseline {
			result.Unbaselined = append(
				result.Unbaselined,
				relPath,
			)
			continue
		}

		equal, lineEndingOnly :=
			EquivalentFingerprints(
				baselineFingerprint,
				currentFingerprint,
				tolerateLineEndings,
			)

		if equal {
			if lineEndingOnly {
				result.LineEndingOnly = append(
					result.LineEndingOnly,
					relPath,
				)
			}
			continue
		}

		result.Stale = append(
			result.Stale,
			relPath,
		)
	}

	for _, relDirectory := range indexDirectories {
		absolutePath := filepath.Join(
			root,
			filepath.FromSlash(
				strings.TrimSuffix(
					relDirectory,
					"/",
				),
			),
		)

		stat, err := os.Stat(absolutePath)
		if err != nil || !stat.IsDir() {
			result.Orphan = append(
				result.Orphan,
				relDirectory,
			)
		}
	}

	sort.Strings(result.Missing)
	sort.Strings(result.Orphan)
	sort.Strings(result.Stale)
	sort.Strings(result.Unbaselined)
	sort.Strings(result.LineEndingOnly)

	return result
}

// IsStaleFile keeps the historical strict semantics.
func IsStaleFile(
	root string,
	relPath string,
	baselineValue *Baseline,
) (
	stale bool,
	unbaselined bool,
) {
	stale, unbaselined, _ = IsStaleFileWith(
		root,
		relPath,
		baselineValue,
		false,
	)

	return stale, unbaselined
}

// IsStaleFileWith runs the single-file fingerprint equivalence quick check.
//
// A file not on disk is treated as not drifted; Orphan is still reported by the full Detect.
func IsStaleFileWith(
	root string,
	relPath string,
	baselineValue *Baseline,
	tolerateLineEndings bool,
) (
	stale bool,
	unbaselined bool,
	lineEndingOnly bool,
) {
	if baselineValue == nil {
		return false, true, false
	}

	baselineFingerprint, exists :=
		baselineValue.Files[relPath]
	if !exists {
		return false, true, false
	}

	currentFingerprint, err := HashFile(
		filepath.Join(
			root,
			filepath.FromSlash(relPath),
		),
	)
	if err != nil {
		return false, false, false
	}

	equal, lineEndingOnly :=
		EquivalentFingerprints(
			baselineFingerprint,
			currentFingerprint,
			tolerateLineEndings,
		)

	return !equal, false, lineEndingOnly
}
