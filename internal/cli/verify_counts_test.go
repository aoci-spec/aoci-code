package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

// A file created after the last scan is both Missing and Unbaselined. The
// raw drift count and the unresolved count each added the lists and counted
// it twice; both now count each path once while Orphan, Stale, and an
// Unbaselined path that already has an Entry still count.
func TestVerifyCountsAFreshUnscannedFileOnce(t *testing.T) {
	report := &verifyReport{
		Result: &baseline.DetectResult{
			Missing:     []string{"a.go", "b.go"},
			Unbaselined: []string{"a.go", "b.go", "c.go"},
			Stale:       []string{"d.go"},
			Orphan:      []string{"e.go"},
		},
		ActionableMissing: []string{"a.go", "b.go"},
	}
	if got := verifyRawDriftCount(report); got != 5 {
		t.Fatalf("raw drift count = %d, want 5 (a b c d + orphan e)", got)
	}
	if got := verifyUnresolvedDriftCount(report); got != 5 {
		t.Fatalf("unresolved count = %d, want 5 (actionable a b, orphan e, stale d, unbaselined-with-entry c)", got)
	}
}
