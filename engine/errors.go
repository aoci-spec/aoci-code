package engine

import "errors"

// ErrLayoutUnsupported is returned by the Volumes-only operations (Overview,
// Governance) on a Legacy single-file layout, which is read-only since 0.2.0
// and governed through its migration to Volumes v1; Cognition, Identity, and
// the nine tools still answer on that layout.
var ErrLayoutUnsupported = errors.New("engine: operation requires the Volumes v1 layout")
