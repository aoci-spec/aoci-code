// Manifest decoding is implemented by the deterministic runmanifest package so
// Draft, Check, Guide, and MCP Maintain share one strict machine interpretation.
package draft

import "github.com/aoci-spec/aoci-code/internal/runmanifest"

func LoadManifest(root, runID string) (*Manifest, error) {
	return runmanifest.Load(root, runID)
}
