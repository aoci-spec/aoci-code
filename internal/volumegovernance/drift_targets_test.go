package volumegovernance

import (
	"reflect"
	"testing"
)

// A source that has no Entry and is absent from the Baseline is filed under
// both Missing and Unbaselined; the authoring-target set must still name it
// once, sorted, with Stale alongside.
func TestAuthoringTargetsNameEachPathOnce(t *testing.T) {
	drift := Drift{
		Missing:     []string{"b.go", "a.go"},
		Stale:       []string{"c.go"},
		Unbaselined: []string{"a.go", "b.go", "d.go"},
	}
	got := drift.AuthoringTargets()
	want := []string{"a.go", "b.go", "c.go", "d.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthoringTargets = %v, want %v", got, want)
	}
	if got := (Drift{}).AuthoringTargets(); len(got) != 0 {
		t.Fatalf("empty drift produced targets: %v", got)
	}
}
