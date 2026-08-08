package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

func cognitionFormalAssetPaths(set *cognition.Set) []string {
	if set == nil {
		return nil
	}
	paths := []string{set.Root.Descriptor.Path}
	if set.LayoutMode == cognition.LayoutVolumesV1 {
		for _, id := range set.DeclaredOrder {
			if asset := set.Volumes[id]; asset != nil {
				paths = append(paths, asset.Descriptor.Path)
			}
		}
	}
	seen := map[string]bool{}
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		unique = append(unique, path)
	}
	return unique
}

// confirmCognitionSnapshot re-reads every formal asset after rendering. A
// changed byte, replaced file type, or unreadable participant invalidates the
// whole delivery; callers never return a body assembled from mixed images.
func confirmCognitionSnapshot(root string, set *cognition.Set) *Fail {
	if set == nil {
		return &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage("overview.delivery.snapshot_changed")}
	}
	expected := map[string]string{set.Root.Descriptor.Path: set.Root.SHA256}
	if set.LayoutMode == cognition.LayoutVolumesV1 {
		for _, asset := range set.Volumes {
			if asset != nil && asset.State == cognition.AssetPresent {
				expected[asset.Descriptor.Path] = asset.SHA256
			}
		}
	}
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if set.LayoutMode == cognition.LayoutVolumesV1 {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage("overview.delivery.snapshot_changed"), Hint: mcpMessage("overview.delivery.snapshot_retry")}
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage("overview.delivery.snapshot_changed"), Hint: mcpMessage("overview.delivery.snapshot_retry")}
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != expected[rel] {
			return &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage("overview.delivery.snapshot_changed"), Hint: mcpMessage("overview.delivery.snapshot_retry")}
		}
	}
	return nil
}
