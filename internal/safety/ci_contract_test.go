// The CI contract: every action reference must be pinned to a 40-character commit SHA.
//
// A real experience: release.yml was pinned from the start; it signs, attests, and publishes artifacts, the most
// visible risk. ci.yml / full-confidence.yml / release-rehearsal.yml kept using movable
// tags, while those three pipelines also check out source, run gates, and decide "green or not". A movable tag means
// one upstream rewrite can change this repository's verdict with nothing visible in this repository's commit history.
//
// This test is machine enforcement: a new job referencing one more tag fails here, rather than waiting for the day
// upstream is hijacked. The release chain has already proven pinning works; what remained was extending the discipline to every workflow.
package safety

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var (
	workflowUsesPattern = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*(\S+)`)
	pinnedRefPattern    = regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
)

func ciWorkflowDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(currentFile))), ".github", "workflows")
}

func TestEveryWorkflowActionIsPinnedToACommitSHA(t *testing.T) {
	dir := ciWorkflowDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range workflowUsesPattern.FindAllStringSubmatch(string(raw), -1) {
			reference := match[1]
			// A local composite action is repository content, already covered by
			// the commit it lives in.
			if strings.HasPrefix(reference, "./") {
				continue
			}
			inspected++
			if !pinnedRefPattern.MatchString(reference) {
				t.Errorf("%s references %q by a movable tag; an upstream rewrite would change this "+
					"repository's verdict with nothing visible in its own history",
					entry.Name(), reference)
			}
		}
	}
	if inspected == 0 {
		t.Fatal("no workflow action references were inspected; the gate would pass while covering nothing")
	}
}

// A pinned SHA is unreadable on its own, so every pin must carry the version it
// stands for. Without it an upgrade cannot be reviewed, only diffed.
func TestEveryPinnedActionNamesItsVersion(t *testing.T) {
	dir := ciWorkflowDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	versionComment := regexp.MustCompile(`@[0-9a-f]{40}\s+#\s*v\S+`)
	inspected := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "uses:") || !strings.Contains(line, "@") {
				continue
			}
			if strings.Contains(line, "./") {
				continue
			}
			inspected++
			if !versionComment.MatchString(line) {
				t.Errorf("%s: pinned reference carries no version comment: %s",
					entry.Name(), strings.TrimSpace(line))
			}
		}
	}
	// The sibling pin test has carried this guard from the start; this one did
	// not, so it passed vacuously over an empty set. A gate that reports success
	// while covering nothing is the same false green it exists to prevent.
	if inspected == 0 {
		t.Fatal("no workflow action references were inspected; the gate would pass while covering nothing")
	}
}

// Two workflows pinning the same action to different SHAs is not two correct
// pins; it is one silent divergence that both sibling gates above call clean.
//
// Real experience: release.yml led the repository onto actions/checkout v7.0.1,
// upload-artifact v7.0.1, download-artifact v8.0.1, setup-go v7.0.0 and
// goreleaser-action v7.2.3, while ci.yml, full-confidence.yml and
// release-rehearsal.yml stayed on v4.4.0, v4.6.2, v4.3.0, v5.6.0 and v7.2.1.
// Every one of those was correctly pinned and carried its version comment, so
// nothing failed — and release-rehearsal.yml, the gate whose entire job is to
// rehearse the signed release, was packaging artifacts with older tooling than
// the release it rehearsed. A packaging difference between those versions
// clears the rehearsal and surfaces only in the tag-triggered signed run, which
// is the one run that cannot be cheaply retried.
//
// Deliberately no exceptions list. If a workflow ever genuinely needs a
// different version of an action, that is a decision worth making visible by
// changing this test rather than by editing one line of YAML.
func TestAnActionIsPinnedToTheSameSHAEverywhere(t *testing.T) {
	dir := ciWorkflowDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	pinned := regexp.MustCompile(`uses:\s*([^@\s]+)@([0-9a-f]{40})\s*#\s*(\S+)`)
	// action -> sha -> "workflow (version comment)" sightings
	seen := map[string]map[string][]string{}
	inspected := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pinned.FindAllStringSubmatch(string(raw), -1) {
			action, sha, version := match[1], match[2], match[3]
			if strings.HasPrefix(action, "./") {
				continue
			}
			inspected++
			if seen[action] == nil {
				seen[action] = map[string][]string{}
			}
			sighting := entry.Name() + " (" + version + ")"
			if !slices.Contains(seen[action][sha], sighting) {
				seen[action][sha] = append(seen[action][sha], sighting)
			}
		}
	}
	for _, action := range slices.Sorted(maps.Keys(seen)) {
		if len(seen[action]) < 2 {
			continue
		}
		divergence := make([]string, 0, len(seen[action]))
		for _, sha := range slices.Sorted(maps.Keys(seen[action])) {
			divergence = append(divergence, sha[:12]+" in "+strings.Join(seen[action][sha], ", "))
		}
		t.Errorf("%s is pinned to %d different commits: %s", action, len(seen[action]),
			strings.Join(divergence, "; "))
	}
	if inspected == 0 {
		t.Fatal("no workflow action references were inspected; the gate would pass while covering nothing")
	}
}
