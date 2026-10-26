package engine

import (
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

// Repository is one opened AOCI repository: its resolved root and the
// effective team configuration, read once at Open.
type Repository struct {
	root string
	cfg  *config.Config
}

// Open resolves the repository that contains path the way the CLI does
// without --repo: it ascends to the nearest directory holding .git, then to
// the nearest holding .aoci, and loads the effective configuration without
// materializing any upgrade. It also binds the process-wide runtime text
// catalog to the repository's Locale. An empty path means the working
// directory.
func Open(path string) (*Repository, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	root, err := config.FindRepoRoot(path, "")
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	if err := textassets.SetActiveLocale(cfg.Locale); err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	return &Repository{root: root, cfg: cfg}, nil
}

// Root is the absolute repository root every other result is relative to.
func (r *Repository) Root() string {
	return r.root
}

// Locale is the team Locale the repository's runtime text and Entries use.
func (r *Repository) Locale() string {
	return r.cfg.Locale
}

// IndexPath is the repository-relative path of the Root manifest (Volumes
// v1) or the single index file (Legacy).
func (r *Repository) IndexPath() string {
	return r.cfg.IndexPath
}

func (r *Repository) load() (*cognition.Set, error) {
	set, err := cognition.Load(r.root, r.cfg.IndexPath)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	return set, nil
}
