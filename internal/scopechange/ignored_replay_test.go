package scopechange

import (
	"os"
	"path/filepath"
	"testing"
)

// #101: another tool writing a lock file into a Git-ignored directory between
// preview and Apply must not change the replay envelope. The file is ignored
// through .git/info/exclude so the ignore rule itself is not a new candidate.
func TestBuildEnvelopeIgnoresNewGitIgnoredPaths(t *testing.T) {
	root, candidates, preview := buildAutoPreview(t)
	exclude := filepath.Join(root, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte(".codegraph/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeChangeFixture(t, root, ".codegraph/codegraph.lock", "pid 4242\n")

	rebuilt, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.EnvelopeDigest != preview.EnvelopeDigest {
		t.Fatalf("a new Git-ignored file changed the replay envelope:\n before=%s\n after=%s", preview.EnvelopeDigest, rebuilt.EnvelopeDigest)
	}
	for _, item := range rebuilt.Evaluation.Exclude {
		if item.Path == ".codegraph/codegraph.lock" {
			t.Fatalf("an ungoverned Git-ignored file entered the transaction evaluation: %+v", item)
		}
	}
	result, err := Apply(root, preview, nil)
	if err != nil {
		t.Fatalf("Apply of the original preview failed after an ignored file appeared: %v", err)
	}
	if result.Status != "applied" {
		t.Fatalf("unexpected Apply status: %+v", result)
	}
}
