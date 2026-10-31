// Repository root location and .aoci path resolution
// Index entry: paths.go[Cfg.Paths.8.Xp.S]
//
// Root order: an explicit --repo override > walking up for .git (the git root is canonical) > walking up for .aoci.
// Two independent upward scans: a whole round for .git first, and only when that fails a whole round for .aoci,
// so that "a .aoci in a subdirectory with a .git above" resolves to the git root and is not intercepted by the nearer .aoci.
// rel_path inside the index is always forward-slashed; a Windows drive letter exists only in the local access layer.
package config

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNoRepoRoot means no repository root was located (the caller maps it to exit code 3 = configuration error)
var ErrNoRepoRoot = errors.New("未找到仓库根: 当前目录及各级父目录均无 .git 或 .aoci;可用 --repo 显式指定,或在项目根执行 aoci init")

// FindRepoRoot locates the repository root.
// A non-empty override (--repo) is used directly (it must be an existing directory); otherwise two upward scans start from start.
func FindRepoRoot(start, override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			return "", errors.New("--repo 指定的路径不存在或不是目录: " + override)
		}
		return abs, nil
	}

	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}

	// Round one: look for .git (canonical)
	if root, ok := ascendFind(abs, ".git"); ok {
		return root, nil
	}

	// Round two: look for .aoci (the bare directory scenario without git)
	if root, ok := ascendFind(abs, ".aoci"); ok {
		return root, nil
	}

	return "", ErrNoRepoRoot
}

// ascendFind walks up from dir level by level looking for a directory holding the marker child.
func ascendFind(dir, marker string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Paths is the set of absolute paths of the runtime artifacts inside a repository.
type Paths struct {
	Root             string // repository root
	AOCIDir          string // .aoci/
	IndexPath        string // index file (may be overridden by config.index_path)
	BaselinePath     string // baseline fingerprints
	ConfigPath       string // team configuration
	CurationPath     string // file-level semantic curation asset
	ReportsPath      string // report follow-ups
	LedgerPath       string // telemetry ledger
	VerifyHistoryDir string // verify snapshot directory
}

// AOCIPaths builds the path set from the repository root and the index relative path.
// An empty indexRel takes the historical default .aoci/index.txt.
func AOCIPaths(root, indexRel string) Paths {
	if indexRel == "" {
		indexRel = ".aoci/index.txt"
	}

	dir := filepath.Join(root, ".aoci")

	return Paths{
		Root:             root,
		AOCIDir:          dir,
		IndexPath:        filepath.Join(root, filepath.FromSlash(indexRel)),
		BaselinePath:     filepath.Join(dir, "baseline.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
		CurationPath:     filepath.Join(dir, "curation.json"),
		ReportsPath:      filepath.Join(dir, "reports.jsonl"),
		LedgerPath:       filepath.Join(dir, "ledger.jsonl"),
		VerifyHistoryDir: filepath.Join(dir, "verify_history"),
	}
}
