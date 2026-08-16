package volumegovernance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// A repository can lose the Baseline binding of a formal Volume in two ways that
// need opposite fixes: its bytes were rewritten, or Git hid it when the Baseline
// was built. Neither used to be distinguishable, and one of them was not
// reportable at all.

func gitInit(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
	} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
}

func writeVolume(t *testing.T, root, rel, body string) baseline.Fingerprint {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func TestLineEndingRewriteIsEquivalentNotDrift(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	stored := writeVolume(t, root, "volume.txt", "#AOCI\nline one\nline two\n")
	state := baseline.NewBaseline(map[string]baseline.Fingerprint{"volume.txt": stored})

	// The Windows default core.autocrlf produces exactly this rewrite on checkout.
	writeVolume(t, root, "volume.txt", "#AOCI\r\nline one\r\nline two\r\n")

	cfg := &config.Config{LineEndingTolerance: true}
	matched, lineEndingOnly := baselineMatches(root, cfg, state, "volume.txt")
	if !matched || !lineEndingOnly {
		t.Fatalf("a pure line-ending rewrite must be equivalent under tolerance: matched=%v lineEndingOnly=%v",
			matched, lineEndingOnly)
	}

	// The tolerance is a team policy, so switching it off must restore strictness.
	strict := &config.Config{LineEndingTolerance: false}
	if matched, _ := baselineMatches(root, strict, state, "volume.txt"); matched {
		t.Fatal("tolerance=false must still report the rewrite as drift")
	}
}

func TestRealContentChangeIsStillDrift(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	stored := writeVolume(t, root, "volume.txt", "#AOCI\nline one\n")
	state := baseline.NewBaseline(map[string]baseline.Fingerprint{"volume.txt": stored})
	writeVolume(t, root, "volume.txt", "#AOCI\nline one changed\n")

	cfg := &config.Config{LineEndingTolerance: true}
	// Tolerance must not become a blanket pass: this is the assertion that keeps
	// the fix from turning a real drift detector into a no-op.
	if matched, _ := baselineMatches(root, cfg, state, "volume.txt"); matched {
		t.Fatal("a content change must remain drift even under line-ending tolerance")
	}
}

func TestLineEndingOnlyFindingNeverBlocks(t *testing.T) {
	facts := &Facts{StructureValid: true, Findings: []Finding{
		{Code: "code_volume_line_ending_only", Target: "aoci.code.txt"},
		{Code: "root_volume_line_ending_only", Target: "aoci.txt"},
		{Code: "meta_volume_line_ending_only", Target: "aoci.meta.txt"},
		{Code: "root_volume_baseline_drift", Target: "aoci.txt"},
		{Code: "meta_volume_baseline_drift", Target: "aoci.meta.txt"},
		{Code: "database_volume_line_ending_only", Target: "aoci.database.txt"},
	}}
	finalize(facts)
	// finalize's default arm means blocked, so an unclassified new code silently
	// becomes a hard stop. That is precisely how the original lockout was built.
	if facts.Result != ResultAligned {
		t.Fatalf("line-ending-only findings must not change the result, got %q", facts.Result)
	}
	if !facts.GovernanceAligned {
		t.Fatal("a repository whose only findings are line-ending-only is aligned")
	}
}

func TestUnbaselinedVolumeNamesWhichOfTwoCausesItIs(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeVolume(t, root, "volume.txt", "#AOCI\nbody\n")

	// Cause 1: recorded once, bytes moved since.
	recorded := baseline.NewBaseline(map[string]baseline.Fingerprint{
		"volume.txt": {SHA256: "0000000000000000000000000000000000000000000000000000000000000000"},
	})
	if action := volumeUnbaselinedRepairAction(root, "volume.txt", recorded); !strings.Contains(action, "bytes changed") {
		t.Fatalf("a recorded-but-moved Volume must be reported as byte drift, got %q", action)
	}

	// Cause 2: never recorded, because Git hides it.
	excludePath := filepath.Join(root, ".git", "info", "exclude")
	if err := os.WriteFile(excludePath, []byte("volume.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := baseline.NewBaseline(map[string]baseline.Fingerprint{})
	action := volumeUnbaselinedRepairAction(root, "volume.txt", empty)
	if !strings.Contains(action, "hidden from Git") {
		t.Fatalf("a git-hidden Volume must be reported as hidden, got %q", action)
	}
	// The two causes need opposite fixes, so conflating them is the defect.
	if strings.Contains(action, "bytes changed") {
		t.Fatal("a never-recorded Volume must not be described as byte drift")
	}
}
