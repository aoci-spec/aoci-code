package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// requireLegacyWriteLayout is the shared precondition for CLI paths that can
// alter formal cognition or its governance state. Missing cognition is allowed
// only when the caller is an initialization path.
func requireLegacyWriteLayout(root string, cfg *config.Config, allowMissing bool) error {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cfg.IndexPath)))
	if err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	layout, err := cognition.DetectLayout(raw)
	if err != nil {
		return err
	}
	if layout == cognition.LayoutVolumesV1 {
		return errors.New(cliMessage("mcp.error.volume_read_only"))
	}
	return nil
}
