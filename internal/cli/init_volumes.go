package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/textassets"
)

type initialVolumeAssets struct {
	Root []byte
	Meta []byte
	Code []byte
}

// renderInitialVolumeAssets materializes only deterministic lifecycle and
// governance structure. It intentionally supplies no repository Entry or
// Database Cognition semantics; those remain model-authored after init.
func renderInitialVolumeAssets(root string) (initialVolumeAssets, error) {
	rootTemplate, err := textassets.Load(textassets.ActiveLocale(), textassets.TemplateVolumeRoot)
	if err != nil {
		return initialVolumeAssets{}, fmt.Errorf("init_volume_root_asset_invalid: %w", err)
	}
	metaTemplate, err := textassets.Load(textassets.ActiveLocale(), textassets.TemplateVolumeMeta)
	if err != nil {
		return initialVolumeAssets{}, fmt.Errorf("init_volume_meta_asset_invalid: %w", err)
	}
	data := hooks.NewTplData(root)
	rootText, err := hooks.RenderTemplate("volume-root.txt.tmpl", rootTemplate, data)
	if err != nil {
		return initialVolumeAssets{}, err
	}
	metaText, err := hooks.RenderTemplate("volume-meta.txt.tmpl", metaTemplate, data)
	if err != nil {
		return initialVolumeAssets{}, err
	}
	return initialVolumeAssets{
		Root: []byte(rootText),
		Meta: []byte(metaText),
		Code: []byte(cognition.CodeVolumeMarker + "\n"),
	}, nil
}

// initializeVolumeFirst creates dependencies before the Root activation
// marker. A retry accepts only the exact deterministic partial postimage; any
// other pre-existing bytes or unsafe file type fail closed.
func initializeVolumeFirst(root, indexPath string, assets initialVolumeAssets) (map[string]baseline.Fingerprint, error) {
	if filepath.ToSlash(indexPath) != "aoci.txt" {
		return nil, fmt.Errorf("init_volume_root_path_invalid")
	}
	targets := []struct {
		rel  string
		data []byte
	}{
		{rel: "aoci.meta.txt", data: assets.Meta},
		{rel: "aoci.code.txt", data: assets.Code},
		{rel: "aoci.txt", data: assets.Root},
	}
	for _, target := range targets {
		if err := validateInitialVolumeTarget(root, target.rel, target.data); err != nil {
			return nil, err
		}
	}
	postimages := make(map[string]baseline.Fingerprint, len(targets))
	for _, target := range targets {
		path := filepath.Join(root, filepath.FromSlash(target.rel))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if err := afs.AtomicCreateCASMode(path, target.data, 0o644); err != nil {
				return nil, fmt.Errorf("init_volume_create_failed[%s]: %w", target.rel, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("init_volume_target_inspect_failed[%s]: %w", target.rel, err)
		}
		postimages[target.rel] = baseline.HashBytes(target.rel, target.data)
	}
	set, err := cognition.Load(root, indexPath)
	if err != nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 ||
		set.Volumes[cognition.ScopeCode] == nil || set.Volumes[cognition.ScopeDatabase] != nil {
		return nil, fmt.Errorf("init_volume_postimage_invalid")
	}
	return postimages, nil
}

func validateInitialVolumeTarget(root, rel string, expected []byte) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("init_volume_target_inspect_failed[%s]: %w", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("init_volume_target_wrong_type[%s]", rel)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("init_volume_target_read_failed[%s]: %w", rel, err)
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("init_volume_target_conflict[%s]", rel)
	}
	return nil
}

func activeVolumeLayout(root, indexPath string) (bool, error) {
	path := filepath.Join(root, filepath.FromSlash(indexPath))
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	set, err := cognition.Load(root, indexPath)
	if err != nil {
		return false, nil
	}
	return set.LayoutMode == cognition.LayoutVolumesV1, nil
}
