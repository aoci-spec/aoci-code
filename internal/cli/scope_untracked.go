package cli

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// scopeGitUntrackedPaths is advisory: references come from the published index
// or Baseline, while Git membership comes from the current Safe Inventory.
// An unreadable index contributes no references; existing status guards still
// decide whether that index is a command error.
func scopeGitUntrackedPaths(root string, evaluation *managedscope.Evaluation) []string {
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil
	}
	active, _, err := baseline.Load(root)
	if err != nil {
		return nil
	}
	referenced := map[string]bool{}
	if active != nil {
		for path := range active.Files {
			referenced[path] = true
		}
	}
	if set, err := cognition.Load(root, cfg.IndexPath); err == nil {
		asset := &set.Root
		if set.LayoutMode == cognition.LayoutVolumesV1 {
			asset = set.Volumes[cognition.ScopeCode]
		}
		if asset != nil && asset.Document != nil {
			for _, section := range asset.Document.Sections {
				for _, entry := range section.Entries {
					referenced[entry.RelPath] = true
				}
			}
		}
	}
	paths := []string{}
	for _, group := range [][]managedscope.PathEvaluation{evaluation.Index, evaluation.Observe, evaluation.Exclude} {
		for _, item := range group {
			if !referenced[item.Path] || item.GitStatus != "untracked_or_non_git" || item.RuleSource == machinecontract.ScopeRuleSafety {
				continue
			}
			if _, formal := cognition.FormalAssetOwner(item.Path); formal {
				continue
			}
			path := filepath.Join(root, filepath.FromSlash(item.Path))
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if !evaluation.SafeInventory.GitRepository && !scopePathInChildGit(root, item.Path) {
				continue
			}
			paths = append(paths, item.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

// Safe Inventory has already validated every discovered child Git boundary.
// Its GitStatus also labels container files untracked_or_non_git, so test the
// boundary by name without another Git subprocess or any source-content read.
func scopePathInChildGit(root, rel string) bool {
	for dir := filepath.Dir(filepath.FromSlash(rel)); dir != "."; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(root, dir, ".git")); err == nil {
			return true
		}
	}
	return false
}
