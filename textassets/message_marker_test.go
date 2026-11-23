package textassets

import (
	"strings"
	"testing"
)

// "%!" inside an argument is data, such as a file named 100%!.go; only a
// marker fmt adds is a formatting failure (audit G8-02).
func TestMessageTellsArgumentMarkersFromFormattingFailures(t *testing.T) {
	got, err := Message("en-US", "config.bad_curation_path", "100%!.go")
	if err != nil || !strings.Contains(got, "100%!.go") {
		t.Fatalf("an argument holding %%! failed the message: %q %v", got, err)
	}
	if _, err := Message("en-US", "governance.volumes_missing", "not a number"); err == nil {
		t.Fatal("a string for a decimal verb was not reported as a formatting failure")
	}
	if _, err := Message("en-US", "governance.volumes_missing", "%!x"); err == nil {
		t.Fatal("a real failure was hidden by a marker in its own argument")
	}
}
