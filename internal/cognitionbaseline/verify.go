package cognitionbaseline

import (
	"fmt"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
)

type FormalTarget struct {
	Path   string
	SHA256 string
}

func VerifyVolumeState(root, compositeIdentity, baselineSHA string, targets []FormalTarget, enabledVolumeIDs []string) (*cognition.Set, error) {
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 || set.CompositeIdentity != compositeIdentity {
		return nil, fmt.Errorf("projected composite identity mismatch")
	}
	for _, target := range targets {
		actual, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(target.Path)))
		if hashErr != nil || actual.SHA256 != target.SHA256 {
			return nil, fmt.Errorf("formal target mismatch: %s", target.Path)
		}
	}
	actualBaseline, hashErr := baseline.HashFile(filepath.Join(root, ".aoci", "baseline.json"))
	if hashErr != nil || actualBaseline.SHA256 != baselineSHA {
		return nil, fmt.Errorf("baseline mismatch")
	}
	baselineValue, exists, err := baseline.Load(root)
	if err != nil || !exists || baselineValue == nil {
		return nil, fmt.Errorf("baseline is unavailable: %v", err)
	}
	for path, expected := range baselineValue.Files {
		actual, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(path)))
		if hashErr != nil || actual.SHA256 != expected.SHA256 {
			return nil, fmt.Errorf("baseline fingerprint mismatch: %s", path)
		}
	}
	for _, id := range enabledVolumeIDs {
		asset := set.Volumes[id]
		if asset == nil || asset.State != cognition.AssetPresent {
			return nil, fmt.Errorf("enabled Volume unavailable: %s", id)
		}
	}
	if set.Volumes["database"] != nil {
		cfg, cfgErr := config.LoadReadOnly(root)
		if cfgErr != nil {
			return nil, fmt.Errorf("database configuration unavailable: %v", cfgErr)
		}
		assessment := dbcognition.Assess(root, cfg.DatabaseSources, set, baselineValue)
		if !assessment.CognitionCurrent || assessment.NetworkAccessed {
			return nil, fmt.Errorf("database cognition binding is not current")
		}
	}
	return set, nil
}
