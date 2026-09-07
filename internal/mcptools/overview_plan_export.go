package mcptools

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// OverviewChunkPlan reports how a complete Overview of one scope would be
// chunked at a given chunk_tokens. It runs the exact planner the delivery path
// runs, so a status surface reports what the server would do rather than an
// estimate of it.
type OverviewChunkPlan struct {
	Scope             string `json:"scope"`
	ScopeIdentity     string `json:"scope_identity,omitempty"`
	Available         bool   `json:"available"`
	ChunkTokens       int    `json:"chunk_tokens"`
	ChunkCount        int    `json:"chunk_count"`
	BodyUTF8Bytes     int    `json:"body_utf8_bytes"`
	EstimatedTokens   int    `json:"estimated_tokens"`
	EntryCount        int    `json:"entry_count"`
	LargestChunkBytes int    `json:"largest_chunk_bytes"`
}

// PlanOverviewChunks plans the delivery of scope for a loaded Volumes v1 set
// without touching the session, the Ledger, or any receipt: it is a pure
// projection of formal bytes and configuration.
func PlanOverviewChunks(root string, set *cognition.Set, scope string, chunkTokens int) (OverviewChunkPlan, error) {
	if set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return OverviewChunkPlan{}, fmt.Errorf("overview_plan_layout_unsupported")
	}
	view, err := set.Scope(scope)
	if err != nil {
		return OverviewChunkPlan{}, err
	}
	plan := OverviewChunkPlan{Scope: view.EffectiveScope, ScopeIdentity: view.ScopeIdentity,
		Available: view.Available, ChunkTokens: chunkTokens, EntryCount: view.ObjectCount}
	if !view.Available {
		return plan, nil
	}
	body, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		return OverviewChunkPlan{}, err
	}
	framed := frameOverviewBody(root, view.EffectiveScope, view.ScopeIdentity, view.ObjectCount, body)
	plan.BodyUTF8Bytes = framed.Receipt.BodyUTF8Bytes
	plan.EstimatedTokens = volumeScopeBytes(view) / 3
	spans, err := planOverviewChunks(framed.Text, chunkTokens, len(framed.Receipt.StartMarker)+1, sequence)
	if err != nil {
		return OverviewChunkPlan{}, err
	}
	plan.ChunkCount = len(spans)
	for _, span := range spans {
		if size := span.End - span.Start; size > plan.LargestChunkBytes {
			plan.LargestChunkBytes = size
		}
	}
	return plan, nil
}

// OverviewBody returns the complete Overview body of scope — every asset's
// banner and raw bytes in delivery order — from the same builder the delivery
// path frames and chunks. A status surface that shows "the whole index" shows
// this, never a second concatenation that could drift from what an agent
// receives.
func OverviewBody(set *cognition.Set, scope string) (string, error) {
	if set == nil || set.LayoutMode != cognition.LayoutVolumesV1 {
		return "", fmt.Errorf("overview_plan_layout_unsupported")
	}
	view, err := set.Scope(scope)
	if err != nil {
		return "", err
	}
	if !view.Available {
		return "", nil
	}
	body, _, err := buildVolumeOverviewBody(view)
	return body, err
}
