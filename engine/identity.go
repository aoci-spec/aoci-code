package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// Identity is what a published index is: the exact formal bytes, their
// composite identity, the Baseline they were governed against, and the
// engine that reads them. Two clones with equal identities carry the same
// cognition.
type Identity struct {
	EngineVersion string
	Layout        string
	// RootSHA256 digests the Root manifest (Volumes) or the single index
	// (Legacy).
	RootSHA256     string
	MetaSHA256     string
	CodeSHA256     string
	DatabaseSHA256 string
	// CompositeIdentity is the domain-separated identity over every formal
	// asset, the same value governance facts and Overview receipts carry.
	CompositeIdentity string
	// ScopeIdentity is the index_sha256 of the complete (all) scope on a
	// Volumes layout.
	ScopeIdentity string
	// BaselineSHA256 digests .aoci/baseline.json when present.
	BaselineSHA256 string
	EntryCount     int
}

// Identity reads the formal assets and reports their identities.
func (r *Repository) Identity() (*Identity, error) {
	set, err := r.load()
	if err != nil {
		return nil, err
	}
	identity := &Identity{
		EngineVersion:     Version(),
		Layout:            set.LayoutMode,
		RootSHA256:        set.Root.SHA256,
		MetaSHA256:        set.Meta.SHA256,
		CompositeIdentity: set.CompositeIdentity,
	}
	if set.LayoutMode == cognition.LayoutLegacyMonolithic {
		identity.EntryCount = set.Root.ObjectCount
	}
	if asset := set.Volumes["code"]; asset != nil {
		identity.CodeSHA256 = asset.SHA256
		identity.EntryCount += asset.ObjectCount
	}
	if asset := set.Volumes["database"]; asset != nil {
		identity.DatabaseSHA256 = asset.SHA256
		identity.EntryCount += asset.ObjectCount
	}
	if set.LayoutMode == cognition.LayoutVolumesV1 {
		if view, err := set.Scope(cognition.ScopeAll); err == nil {
			identity.ScopeIdentity = view.ScopeIdentity
		}
	}
	paths := config.AOCIPaths(r.root, r.cfg.IndexPath)
	if data, err := os.ReadFile(paths.BaselinePath); err == nil {
		digest := sha256.Sum256(data)
		identity.BaselineSHA256 = hex.EncodeToString(digest[:])
	}
	return identity, nil
}
