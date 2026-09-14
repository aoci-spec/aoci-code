package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// Guide computed its authoring batch by adding Missing, Stale, and Unbaselined,
// the same sum Maintain used, so a file created after the last scan, which
// sits in both Missing and Unbaselined, was promised twice. Guide and Maintain
// now read one deduplicated set; this pins the Guide side on a repository
// initialized and scanned the way aoci init does it.
func TestVolumeGuideCountsAFreshUnscannedFileOnce(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package sample\n")
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "init", "--agent=", "--hooks=false"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit %d: %s%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "scan"}, &stdout, &stderr); code != 0 {
		t.Fatalf("scan exit %d: %s%s", code, stdout.String(), stderr.String())
	}
	write("extra_a.go", "package sample\n\nconst ExtraA = 1\n")
	write("extra_b.go", "package sample\n\nconst ExtraB = 2\n")

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	unique := map[string]bool{}
	for _, list := range [][]string{facts.CodeDrift.Missing, facts.CodeDrift.Stale, facts.CodeDrift.Unbaselined} {
		for _, rel := range list {
			unique[rel] = true
		}
	}
	naive := len(facts.CodeDrift.Missing) + len(facts.CodeDrift.Stale) + len(facts.CodeDrift.Unbaselined)
	if naive == len(unique) {
		t.Fatalf("fixture does not reproduce the Missing/Unbaselined overlap: %#v", facts.CodeDrift)
	}
	guide, err := buildVolumeAgentGuide(root, cfg, set, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if guide.Batch == nil || guide.Batch.TotalTargets != len(unique) {
		t.Fatalf("Guide total must count each path once (%d unique, naive sum %d): %#v", len(unique), naive, guide.Batch)
	}
}
