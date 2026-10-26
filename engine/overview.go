package engine

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
)

// Overview is the complete formal cognition body of one scope, exactly the
// bytes aoci_overview delivers for that scope, plus the delivery plan the
// repository's chunk budget implies.
type Overview struct {
	// Scope is the requested scope: all, project, meta, code, or database.
	Scope string
	// EffectiveScope is the scope the repository could serve.
	EffectiveScope string
	// Available is false when the requested scope has no asset.
	Available bool
	// Text is the exact delivered body: start marker, asset banners, formal
	// bytes, end marker. SHA256 and Bytes are the body_sha256 and
	// body_utf8_bytes a Host confirms, computed by the delivery path itself.
	Text   string
	SHA256 string
	Bytes  int
	// ScopeIdentity is the index_sha256 an Overview receipt carries for the
	// scope.
	ScopeIdentity   string
	EntryCount      int
	EstimatedTokens int
	ChunkTokens     int
	ChunkCount      int
}

// Overview renders the complete formal body of scope through the delivery
// path's own builder, touching no session, Ledger, or receipt. It returns
// ErrLayoutUnsupported on a Legacy layout.
func (r *Repository) Overview(scope string) (*Overview, error) {
	set, err := r.load()
	if err != nil {
		return nil, err
	}
	if set.LayoutMode != cognition.LayoutVolumesV1 {
		return nil, ErrLayoutUnsupported
	}
	if scope == "" {
		scope = cognition.ScopeAll
	}
	view, err := set.Scope(scope)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	plan, err := mcptools.PlanOverviewChunks(r.root, set, scope, r.cfg.OverviewDelivery.ChunkTokens)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	text, digest, size, err := mcptools.FramedOverviewBody(r.root, set, scope)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	return &Overview{
		Scope:           view.RequestedScope,
		EffectiveScope:  view.EffectiveScope,
		Available:       view.Available,
		Text:            text,
		SHA256:          digest,
		Bytes:           size,
		ScopeIdentity:   plan.ScopeIdentity,
		EntryCount:      plan.EntryCount,
		EstimatedTokens: plan.EstimatedTokens,
		ChunkTokens:     plan.ChunkTokens,
		ChunkCount:      plan.ChunkCount,
	}, nil
}
