// The machine gate of the database driver version table.
//
// A real experience: the "Database driver dependency audit" table in docs/supply-chain.md wrote the pinned versions of the three driver
// modules as four lines of Markdown for people. Nothing in the table read it, so after go.mod was bumped by
// dependabot the table still sat at the old versions: only the adversarial review before the rc11 cut found that pgx v5.10.0
// and go-sql-driver/mysql v1.10.0 were outdated (see the commit "docs: correct the driver audit
// versions and the README boundary claims"), and the table had to be changed by hand. supply-chain.md ships with the tag
// and is referenced through tag-pinned links, so the versions it declares outward are part of the release declaration;
// a table without a gate goes stale again sooner or later, and this time it went stale exactly in the document declared as supply-chain evidence.
//
// This test only compares "the versions in the table" with "the versions go.mod requires" and does not judge what the version "should be"; that is
// the dependency upgrade's decision. The openGauss row has one more item: its module identity comes from the go.mod require, while the bytes
// really compiled into the binary come from the third_party/ carried copy the replace points at, so both places must still
// point at that carried copy, or the "official identity + locally reviewed patch" the document describes no longer matches the real build.
package dbevidence

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// driverAuditSectionHeading is the heading of the section the four-row table belongs to.
const driverAuditSectionHeading = "## Database driver dependency audit"

// carriedOpenGaussModule is the driver module whose reviewed patch the root module carries through a local replace.
const carriedOpenGaussModule = "gitcode.com/opengauss/openGauss-connector-go-pq"

// carriedOpenGaussTree is the in-repository path carriedOpenGaussModule is swapped for.
const carriedOpenGaussTree = "third_party/openGauss-connector-go-pq"

// driverAuditTableRow matches a table row "| `module` | `version` ... |" and strips the backticks around the version as needed.
// The openGauss row's version is "`v1.0.8` + reviewed AOCI patch", so only the leading
// "| `" and the "`" right after the module name are anchored, and a change of column width or order does not make it mismatch silently.
var driverAuditTableRow = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|\\s*`([^`]+)`")

// driverAuditRepoRoot resolves the repository root from this test file's location, as the other contract tests of this package do; the test path
// does not depend on the caller's working directory.
func driverAuditRepoRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
}

func readDriverAuditFile(t *testing.T, root, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("%s: %v", relative, err)
	}
	return string(raw)
}

// driverAuditSection returns the body between the section heading and the next second-level heading. When the section is renamed or moved it returns
// an empty string, which the caller judges as "the gate no longer covers it" rather than passing silently.
func driverAuditSection(document string) string {
	start := strings.Index(document, driverAuditSectionHeading)
	if start < 0 {
		return ""
	}
	remainder := document[start+len(driverAuditSectionHeading):]
	if end := strings.Index(remainder, "\n## "); end >= 0 {
		remainder = remainder[:end]
	}
	return remainder
}

// driverAuditTableEntries returns each module's documented version and the line where it appears in the text, and checks that
// each module has exactly one row.
func driverAuditTableEntries(t *testing.T, document string) (map[string]string, map[string]int) {
	t.Helper()
	section := driverAuditSection(document)
	if section == "" {
		t.Fatalf("docs/supply-chain.md carries no %q section; this gate has stopped covering it",
			driverAuditSectionHeading)
	}
	versions := map[string]string{}
	lines := map[string]int{}
	for index, line := range strings.Split(section, "\n") {
		match := driverAuditTableRow.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		module, version := match[1], match[2]
		if previous, seen := versions[module]; seen {
			t.Fatalf("the driver audit table lists %s twice (%s and %s)", module, previous, version)
		}
		versions[module] = version
		lines[module] = index + 1
	}
	if len(versions) == 0 {
		t.Fatalf("the %q table lists no module rows; the gate would pass while covering nothing",
			driverAuditSectionHeading)
	}
	return versions, lines
}

// goModGraph is the go.mod fact the machine criterion uses: the resolved version of every require, and the target path of every
// replace. It reads only the file text and calls no go command, so it runs under any Go version the same as
// the other tests of make fast / make full.
type goModGraph struct {
	required map[string]string
	replaced map[string]string
}

func parseGoModGraph(t *testing.T, text string) goModGraph {
	t.Helper()
	graph := goModGraph{required: map[string]string{}, replaced: map[string]string{}}
	inRequireBlock := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		if line == "" {
			continue
		}
		if inRequireBlock {
			if line == ")" {
				inRequireBlock = false
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				graph.required[fields[0]] = fields[1]
			}
			continue
		}
		if strings.HasPrefix(line, "replace ") {
			if module, target, err := parseReplaceDirective(line); err == nil {
				graph.replaced[module] = target
			}
			continue
		}
		if line == "require (" {
			inRequireBlock = true
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 3 && fields[0] == "require" {
			graph.required[fields[1]] = fields[2]
		}
	}
	return graph
}

// parseReplaceDirective parses "replace <module> [<version>] => <target> [<version>]".
// The version in a replace target appears only when swapping in another published module; what is needed here is the target identity, so
// it returns an empty string rather than an error when no target version is given.
func parseReplaceDirective(line string) (string, string, error) {
	arrow := strings.Index(line, "=>")
	if arrow < 0 {
		return "", "", errors.New("replace directive carries no =>")
	}
	left := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "replace")))
	right := strings.Fields(strings.TrimSpace(line[arrow+2:]))
	if len(left) == 0 || len(right) == 0 {
		return "", "", errors.New("replace directive names no module or no target")
	}
	return left[0], right[0], nil
}

// TestDocumentedDriverVersionsMatchGoMod is the gate itself. The failure message names the module, the version in the table,
// and the go.mod version at once, so the upgrader need not find out which side is behind. A module named in the table that go.mod does not
// require at all is a failure too: deleting a driver while leaving its table row is exactly the kind of stale declaration this gate was first meant to catch, the one fixed by hand before the rc11
// cut, and passing silently would mean the gate covers only two of the three modules.
func TestDocumentedDriverVersionsMatchGoMod(t *testing.T) {
	root := driverAuditRepoRoot(t)
	versions, _ := driverAuditTableEntries(t, readDriverAuditFile(t, root, "docs/supply-chain.md"))
	graph := parseGoModGraph(t, readDriverAuditFile(t, root, "go.mod"))

	inspected := 0
	for module, documented := range versions {
		declared, ok := graph.required[module]
		if !ok {
			t.Errorf("the driver audit table lists %s, but go.mod does not require it\n"+
				"a driver removed from go.mod leaves the released supply-chain document claiming a dependency the module set no longer has",
				module)
			continue
		}
		inspected++
		if documented != declared {
			t.Errorf("the driver audit table pins %s at %s where go.mod requires %s\n"+
				"a dependency bump moves go.mod and the released supply-chain document keeps its old table row",
				module, documented, declared)
		}
	}
	if inspected == 0 {
		t.Fatal("the driver audit table named no module that go.mod requires; the gate covered nothing")
	}
}

// TestDocumentedOpenGaussDriverStillResolvesToTheCarriedTree covers the extra item of the openGauss row:
// the identity required in go.mod is the official module, while the bytes really compiled come from the third_party/ carried copy the replace
// points at. Once the replace is deleted or pointed elsewhere, the build falls back to the unpatched upstream
// behaviour, and "v1.0.8 + reviewed AOCI patch" in the table and the document's review conclusion both become untrue at once.
func TestDocumentedOpenGaussDriverStillResolvesToTheCarriedTree(t *testing.T) {
	root := driverAuditRepoRoot(t)
	versions, _ := driverAuditTableEntries(t, readDriverAuditFile(t, root, "docs/supply-chain.md"))
	graph := parseGoModGraph(t, readDriverAuditFile(t, root, "go.mod"))

	if _, documented := versions[carriedOpenGaussModule]; !documented {
		t.Errorf("the driver audit table no longer names %s, so its version and its carried patch are unguarded",
			carriedOpenGaussModule)
	}
	declared, required := graph.required[carriedOpenGaussModule]
	if !required {
		t.Fatalf("go.mod no longer requires %s, but docs/supply-chain.md documents the carried patched module",
			carriedOpenGaussModule)
	}
	if documented, want := versions[carriedOpenGaussModule], declared; documented != "" && documented != want {
		t.Errorf("the driver audit table pins %s at %s where go.mod requires %s",
			carriedOpenGaussModule, documented, want)
	}
	target, replaced := graph.replaced[carriedOpenGaussModule]
	if !replaced {
		t.Fatalf("go.mod no longer replaces %s, so it resolves to unpatched upstream behavior while "+
			"docs/supply-chain.md still documents the reviewed AOCI patch carried under %s",
			carriedOpenGaussModule, carriedOpenGaussTree)
	}
	if want := "./" + carriedOpenGaussTree; strings.TrimSuffix(target, "/") != want {
		t.Errorf("go.mod replaces %s with %q, but the documented carried copy is %s",
			carriedOpenGaussModule, target, carriedOpenGaussTree)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(carriedOpenGaussTree), "go.mod")); err != nil {
		t.Fatalf("the replace target %s is not a readable module tree: %v", carriedOpenGaussTree, err)
	}
}
