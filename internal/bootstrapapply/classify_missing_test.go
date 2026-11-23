package bootstrapapply

import (
	"path/filepath"
	"testing"
)

// A target whose preimage has recorded bytes is unknown when missing, never
// its preimage; only a target created from nothing is absent before (audit
// G1-13).
func TestMissingTargetWithRecordedPreimageIsUnknown(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "aoci.txt")
	if state, _, err := classifyFormalPath(missing, "post", "pre"); err != nil || state != StateUnknown {
		t.Fatalf("missing Root with a preimage: %s %v", state, err)
	}
	if state, _, err := classifyFormalPath(missing, "post", ""); err != nil || state != StatePreimage {
		t.Fatalf("missing created target: %s %v", state, err)
	}
}
