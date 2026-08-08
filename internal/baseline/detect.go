// 快照采集与四态差集。
//
// 原始事实:
//
//	Missing     = 磁盘有、索引无;
//	Orphan      = 索引有、磁盘无;
//	Stale       = 两边都有，但指纹不等价;
//	Unbaselined = 两边都有，但Baseline缺该文件。
//
// LineEndingOnly是附加信息态：原始字节不同，但双方可选规范化指纹相同。
// 是否允许该等价关系必须由调用方显式传入；旧Detect和IsStaleFile保持严格语义。
//
// 排除口径一致性：索引侧条目与磁盘快照使用同一套排除规则。
// 全部slice初始化为空并排序，保证JSON稳定且不产生null。
package baseline

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// DetectResult保存原始四态和可选换行等价信息。
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

// Snapshot遍历仓库并计算全部文件指纹。
//
// 单文件哈希失败不阻断全局，以warning返回。
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

	for _, relPath := range files {
		fingerprint, err := HashFile(
			filepath.Join(
				root,
				filepath.FromSlash(relPath),
			),
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

// EquivalentFingerprints是指纹等价判定的唯一入口。
//
// 返回值:
//   - equal=true,lineEndingOnly=false：原始字节完全一致;
//   - equal=true,lineEndingOnly=true：仅在显式宽容时，规范化指纹一致;
//   - equal=false：存在真实漂移，或任一侧缺少规范化指纹。
//
// 任一侧缺少NormalizedSHA256时退回严格比较，保证旧Baseline、二进制
// 和超限文件不会被静默放宽。
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

// isExcludedRel判断索引条目路径是否落在与磁盘快照相同的排除口径内。
func isExcludedRel(
	relPath string,
	options afs.WalkOptions,
) bool {
	if category, _ := afs.BuiltInSafetyCategory(relPath); category != "" {
		return true
	}
	excludedDirectories := map[string]bool{
		".aoci": true,
	}

	for _, directory := range options.ExcludeDirs {
		directory = strings.TrimSpace(
			directory,
		)
		if directory != "" {
			excludedDirectories[directory] = true
		}
	}

	clean := strings.TrimSuffix(
		relPath,
		"/",
	)
	segments := strings.Split(
		clean,
		"/",
	)

	lastDirectoryIndex := len(segments) - 1
	if !strings.HasSuffix(relPath, "/") {
		lastDirectoryIndex--
	}

	for index := 0; index <= lastDirectoryIndex; index++ {
		if excludedDirectories[segments[index]] {
			return true
		}
	}

	return afs.MatchExcludePattern(
		clean,
		options.ExcludeFiles,
	)
}

// Detect保持历史严格语义。
//
// 现有调用方在未明确接入配置前继续按原始字节比较。
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

// DetectWith按显式参数计算四态差集和换行等价信息。
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

// IsStaleFile保持历史严格语义。
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

// IsStaleFileWith执行单文件指纹等价速查。
//
// 文件不在磁盘时按未漂移处理，Orphan仍由全量Detect报告。
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
