package volumegovernance

import (
	"reflect"
	"testing"
)

// A source that has no Entry and is absent from the Baseline is filed under
// both Missing and Unbaselined; the unresolved-path set must still name it
// once, sorted, with Stale alongside.
func TestUnresolvedPathsNameEachPathOnce(t *testing.T) {
	drift := Drift{
		Missing:     []string{"b.go", "a.go"},
		Stale:       []string{"c.go"},
		Unbaselined: []string{"a.go", "b.go", "d.go"},
	}
	got := drift.UnresolvedPaths()
	want := []string{"a.go", "b.go", "c.go", "d.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnresolvedPaths = %v, want %v", got, want)
	}
	if got := (Drift{}).UnresolvedPaths(); len(got) != 0 {
		t.Fatalf("empty drift produced targets: %v", got)
	}
}
