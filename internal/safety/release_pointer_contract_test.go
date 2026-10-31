// The release pointer contract: the user-facing version pointers must agree with each other and with the latest CHANGELOG version section.
//
// A real experience: these pointers had no machine gate, so three releases in a row missed one. The rc4 release missed the two
// README status badges on line 9, so rc3's badge went out with rc4; rc5 kept the same badge;
// only after the rc6 tag was it noticed that README, install.md, supply-chain.md, the NOTICE title, and
// release.yml's dispatch default all still sat at rc5, while the badge still sat at rc4.
//
// That is exactly a counterexample of this repository's own rule "a public number must be self-checked by what produces it": the numbers were written into documents
// with nothing comparing them. This test does not judge "what the current version should be" (that is the releaser's decision);
// it only enforces "every pointer names the same version, and it equals the latest CHANGELOG section".
//
// NOTICE's availability date lines are legal history, append-only: only the latest version's presence is checked, never the old lines.
package safety

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func releaseRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the test file path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func readReleaseFile(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(raw)
}

// currentReleaseVersion is the newest "## vX.Y.Z..." heading in CHANGELOG.md.
// The changelog is the one document a release cannot ship without, because the
// tag build extracts its notes from that exact heading.
func currentReleaseVersion(t *testing.T, root string) string {
	t.Helper()
	heading := regexp.MustCompile(`(?m)^## v([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?)\s*$`)
	match := heading.FindStringSubmatch(readReleaseFile(t, root, "CHANGELOG.md"))
	if match == nil {
		t.Fatal("CHANGELOG.md carries no released version heading")
	}
	return match[1]
}

func TestEveryReleaseVersionPointerNamesTheCurrentVersion(t *testing.T) {
	root := releaseRepoRoot(t)
	version := currentReleaseVersion(t, root)
	previous := regexp.MustCompile(`0\.1\.0-rc[0-9]+|[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?`)

	for _, target := range []struct {
		file string
		// line, when set, restricts the check to lines containing it, so a file
		// may legitimately mention other versions elsewhere.
		contains string
	}{
		{file: "README.md", contains: "is the current release candidate"},
		{file: "README.zh-CN.md", contains: "是当前发布候选版本"},
		{file: "NOTICE", contains: "Notice"},
		{file: ".github/workflows/release.yml", contains: "default:"},
		// Named in this file's own header as rc6 casualties, and unguarded until
		// an audit pointed out that a gate short of its stated scope is the same
		// false green it exists to prevent.
		{file: "docs/install.md", contains: "aoci version"},
		{file: "docs/supply-chain.md", contains: "provenance.sigstore.json"},
	} {
		text := readReleaseFile(t, root, target.file)
		found := false
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, target.contains) {
				continue
			}
			hits := previous.FindAllString(line, -1)
			if len(hits) == 0 {
				continue
			}
			found = true
			for _, hit := range hits {
				if hit != version {
					t.Errorf("%s names version %s where CHANGELOG's current release is %s\n  line: %s\n"+
						"the release ceremony moved some pointers and not this one",
						target.file, hit, version, strings.TrimSpace(line))
				}
			}
		}
		if !found {
			t.Errorf("%s carries no version pointer matching %q; the check has stopped covering it",
				target.file, target.contains)
		}
	}
}

// The status badge shipped two releases stale once already, because it is the
// one pointer that is an image URL rather than prose and no reader notices it
// in a diff.
func TestBothReadmeStatusBadgesNameTheCurrentVersion(t *testing.T) {
	root := releaseRepoRoot(t)
	version := currentReleaseVersion(t, root)
	// shields.io escapes a literal hyphen as "--".
	want := "status-v" + strings.ReplaceAll(version, "-", "--")
	badge := regexp.MustCompile(`status-v[0-9A-Za-z.\-]+`)

	for _, file := range []string{"README.md", "README.zh-CN.md"} {
		text := readReleaseFile(t, root, file)
		hits := badge.FindAllString(text, -1)
		if len(hits) == 0 {
			t.Errorf("%s carries no shields.io status badge", file)
			continue
		}
		for _, hit := range hits {
			if !strings.HasPrefix(hit, want) {
				t.Errorf("%s status badge reads %q but the current release is v%s (expected prefix %q)",
					file, hit, version, want)
			}
		}
	}
}

// Every version that has a CHANGELOG section must keep its NOTICE availability
// line. That is the invariant a blanket search and replace over NOTICE breaks:
// it rewrites the previous version's line into the new version's, so the old
// version silently loses its recorded date and the new one is backdated to it.
// Checking only that the current version is present does not catch that, which
// this test learned the hard way.
func TestNoticeKeepsAnAvailabilityDateForEveryReleasedVersion(t *testing.T) {
	root := releaseRepoRoot(t)
	changelog := readReleaseFile(t, root, "CHANGELOG.md")
	notice := readReleaseFile(t, root, "NOTICE")

	released := regexp.MustCompile(`(?m)^## v([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?)\s*$`)
	line := regexp.MustCompile(`(?m)^First public availability date for v([0-9][0-9A-Za-z.\-]*): ([0-9]{4}-[0-9]{2}-[0-9]{2})$`)

	dates := map[string]string{}
	for _, match := range line.FindAllStringSubmatch(notice, -1) {
		if previous, seen := dates[match[1]]; seen {
			t.Fatalf("NOTICE records v%s twice (%s and %s)", match[1], previous, match[2])
		}
		dates[match[1]] = match[2]
	}

	sections := released.FindAllStringSubmatch(changelog, -1)
	if len(sections) == 0 {
		t.Fatal("CHANGELOG.md carries no released version heading")
	}
	for _, match := range sections {
		version := match[1]
		if _, ok := dates[version]; !ok {
			t.Errorf("v%s has a CHANGELOG section but no NOTICE availability date.\n"+
				"NOTICE is legal metadata whose lines are appended, never rewritten; "+
				"a blanket replace over the file deletes an earlier version's date and backdates the new one.",
				version)
		}
	}
	if len(dates) < len(sections) {
		t.Errorf("NOTICE records %d availability date(s) for %d released version(s)", len(dates), len(sections))
	}
}
