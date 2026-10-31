// Extended-reading tests (#58, #60): directory names holding whitespace, =, or (, and filenames holding [.
//
// Why these tests exist: the three regular expressions cut a directory path at the first whitespace, "=", "(", or "（", and an entry filename could not hold "[".
// Two kinds of consequence: (1) the writer took the truncated path it parsed back as the historical root, and every later section header was written under that truncated root;
// a subdirectory name holding a space made the entry parse to another path, reporting orphan and missing at once, and the repository could never align;
// (2) the dynamic-route filenames of Next.js / Nuxt / SvelteKit ([...id].jsx) could not be written at all, and since a batch is
// atomic, one unwritable line failed the whole batch forever.
//
// This file locks: the old reading is still the first reading (a line that parses today keeps its reading); a machine-written section header takes up to the last
// "/"; a hand-written header with a note or several paths is unchanged; an index already carrying a truncated root keeps parsing in place and after cloning;
// the write side fails closed for a directory that cannot be represented unambiguously.
package index

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestDirHeaderExtendedReadingKeepsLegacyFirst(t *testing.T) {
	for _, test := range []struct {
		line, abs, legacy string
		directory         bool
	}{
		{"===/tmp/example project/repo/===", "/tmp/example project/repo/", "/tmp/example", true},
		{"===/repo/pkg/validations/filetests/max=/===", "/repo/pkg/validations/filetests/max=/", "/repo/pkg/validations/filetests/max", true},
		{"===/repo/app/(home)/===", "/repo/app/(home)/", "/repo/app/", true},
		{"===/repo/docs/（全角）/===", "/repo/docs/（全角）/", "/repo/docs/", true},
		{"===C:/Users/John Doe/repo/src/===", "C:/Users/John Doe/repo/src/", "C:/Users/John", true},
		{"===Go sources/tmp/x/example project/repo/===", "/tmp/x/example project/repo/", "/tmp/x/example", true},
		// Shapes that parse correctly today: both readings agree
		{"===配置索引/opt/aoci-code/===", "/opt/aoci-code/", "/opt/aoci-code/", true},
		{"===/opt/app/[locale]/===", "/opt/app/[locale]/", "/opt/app/[locale]/", true},
		// A hand-written header keeps the old reading: a trailing note not ending in "/", a comment after the complete directory and whitespace, a second absolute path
		{"===/opt/aoci-code/ (备注)===", "/opt/aoci-code/", "/opt/aoci-code/", true},
		{"===/opt/x/ see docs/===", "/opt/x/", "/opt/x/", true},
		{"===/opt/a/ 与 /opt/b/===", "/opt/a/", "/opt/a/", true},
		{"===/opt/a/ and C:/b/===", "/opt/a/", "/opt/a/", true},
		// A relative multi-path header is not a directory section today and never will be
		{"===internal/ 与 testdata/===", "", "", false},
		{"==========", "", "", false},
	} {
		doc, _ := Parse(test.line + "\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n")
		if len(doc.Sections) != 1 {
			t.Fatalf("%q: sections=%d", test.line, len(doc.Sections))
		}
		sec := doc.Sections[0]
		if (sec.AbsPath != "") != test.directory || sec.AbsPath != test.abs || sec.LegacyAbsPath != test.legacy {
			t.Fatalf("%q: AbsPath=%q LegacyAbsPath=%q, want %q / %q", test.line, sec.AbsPath, sec.LegacyAbsPath, test.abs, test.legacy)
		}
	}
}

func TestEntryNamesMayContainBrackets(t *testing.T) {
	for _, test := range []struct{ line, name, tags string }{
		{"[...id].jsx[CG5T]: F:Renders one document page | R:- | A:- | S:-", "[...id].jsx", "CG5T"},
		{"[id].vue[CG5T]: F:Renders one item | R:- | A:- | S:-", "[id].vue", "CG5T"},
		{"a[b].go[CG7S]: F:Holds a bracketed name | R:- | A:- | S:-", "a[b].go", "CG7S"},
		{"x[1][CG5T]: F:Ends with a bracket pair | R:- | A:- | S:-", "x[1]", "CG5T"},
		// The old reading still comes first
		{"main.go[CG9L]: F:Runs the program | R:- | A:main | S:-", "main.go", "CG9L"},
		{"b]r.go[CG5T]: F:Holds a closing bracket | R:- | A:- | S:-", "b]r.go", "CG5T"},
		{"inner space.go[CG5T]: F:Holds a space | R:- | A:- | S:-", "inner space.go", "CG5T"},
	} {
		entry, ok := ParseEntryLine(test.line, 1)
		if !ok || entry.Filename != test.name || entry.TagsRaw != test.tags || entry.FullLine != test.line {
			t.Fatalf("%q: ok=%t entry=%+v", test.line, ok, entry)
		}
		if entry.F == "" {
			t.Fatalf("%q: F was not decomposed: %+v", test.line, entry)
		}
	}
	line := "[...id].jsx[CG5T]: F:Renders one document page | R:- | A:- | S:-"
	for _, violation := range ValidateEntryLine("pages/docs/[...id].jsx", line) {
		if violation.Level == LevelError {
			t.Fatalf("a bracketed file name must validate against its own path: %+v", violation)
		}
	}
	mismatch := false
	for _, violation := range ValidateEntryLine("pages/docs/[slug].jsx", line) {
		mismatch = mismatch || violation.Level == LevelError
	}
	if !mismatch {
		t.Fatal("the file-name gate must still refuse an Entry written for a different path")
	}
	// A line that is not an entry is still not an entry
	for _, line := range []string{"[CG5T]: F:no name", " lead.go[CG5T]: F:leading space", "see [x] for details"} {
		if _, ok := ParseEntryLine(line, 1); ok {
			t.Fatalf("%q must not parse as an Entry", line)
		}
	}
}

func TestSpecialDirectoriesResolveAndInsertUnderCleanRoot(t *testing.T) {
	root := "/repo"
	text := "#head\n===/repo/===\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n"
	for _, target := range []struct{ rel, line string }{
		{"dir space/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
		{"pkg/max=/b.go", "b.go[CG5T]: F:b | R:- | A:- | S:-"},
		{"app/(home)/page.tsx", "page.tsx[CG5T]: F:page | R:- | A:- | S:-"},
		{"pages/docs/[...id].jsx", "[...id].jsx[CG5T]: F:doc page | R:- | A:- | S:-"},
		{"dir space/deeper dir/c.go", "c.go[CG5T]: F:c | R:- | A:- | S:-"},
	} {
		var err error
		if text, err = InsertEntry(text, target.rel, target.line, root); err != nil {
			t.Fatalf("insert %s: %v", target.rel, err)
		}
		doc, warnings := Parse(text)
		if len(warnings) != 0 {
			t.Fatalf("insert %s left parse warnings: %+v", target.rel, warnings)
		}
		ResolveRelPaths(doc, root)
		if FindEntry(doc, target.rel) == nil {
			t.Fatalf("%s does not resolve after its own insert:\n%s", target.rel, text)
		}
	}
	for _, header := range []string{"===/repo/dir space/===", "===/repo/pkg/max=/===", "===/repo/app/(home)/===", "===/repo/dir space/deeper dir/==="} {
		if !strings.Contains(text, header+"\n") {
			t.Fatalf("missing natural header %s:\n%s", header, text)
		}
	}
	// Replacement and removal follow the same parsing
	replaced, err := ReplaceEntryForPath(text, root, "dir space/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-", "a.go[CG5T]: F:a revised | R:- | A:- | S:-")
	if err != nil || !strings.Contains(replaced, "F:a revised") {
		t.Fatalf("replace under a spaced directory: %v", err)
	}
	removed, err := RemoveEntryForPath(replaced, root, "pages/docs/[...id].jsx", "[...id].jsx[CG5T]: F:doc page | R:- | A:- | S:-")
	if err != nil || strings.Contains(removed, "[...id].jsx[") {
		t.Fatalf("remove a bracketed file name: %v", err)
	}
}

// An index written by rc13 or earlier under a root holding a space: the first section is the complete root, the later sections hang under the truncated root.
const truncatedRootIndex = "#head\n" +
	"===/tmp/example project/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
	"===/tmp/example/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n" +
	"===/tmp/example/src/deep dir/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"

func TestIndexWrittenWithTruncatedRootHealsInPlace(t *testing.T) {
	root := "/tmp/example project/repo"
	doc := buildDoc(t, truncatedRootIndex)
	ResolveRelPaths(doc, root)
	for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go"} {
		if FindEntry(doc, rel) == nil {
			t.Fatalf("%s must resolve in place; got %q %q %q", rel, relOf(t, doc, 0, 0), relOf(t, doc, 1, 0), relOf(t, doc, 2, 0))
		}
	}
	// A new section continues the truncated root this index family already uses: a copy cloned elsewhere has no repository root to compare against; it recognizes the
	// old writer by "exactly one complete-root section", and one more complete-root section would lose the relocation (a review measured it: CI checked out all orphans).
	text, err := InsertEntry(truncatedRootIndex, "lib/new dir/c.go", "c.go[CG5T]: F:c | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatalf("a root spelled two ways is one root, so a new section must be accepted: %v", err)
	}
	if !strings.Contains(text, "===/tmp/example/lib/new dir/===\n") || strings.Contains(text, "===/tmp/example project/repo/lib/") {
		t.Fatalf("new sections must continue the truncated spelling the family already uses:\n%s", text)
	}
	for _, at := range []string{root, "/srv/checkout", "C:/ci/work"} {
		doc = buildDoc(t, text)
		ResolveRelPaths(doc, at)
		for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go", "lib/new dir/c.go"} {
			if FindEntry(doc, rel) == nil {
				t.Fatalf("at %q: %s lost after inserting a new section", at, rel)
			}
		}
	}
}

// An index the new writer builds from scratch under a root holding special characters has exactly the shape every earlier version wrote: the root section spells the
// complete root, and the later sections hang under the root the old reading reads back. So there is only one index shape in the world; an old reader can still parse its
// ordinarily named sections, and every section parses the same in place and in a clone. (Four review rounds: the new shape "every section spells the complete root" is
// indistinguishable from the old writer's bytes on a section whose root path starts with a truncating character, and a clone could only guess.)
func TestFreshIndexUnderSpecialRootStaysConsistentAcrossInserts(t *testing.T) {
	root := "/work/my project (v2)/repo"
	text := "#head\n"
	var err error
	for _, target := range []struct{ rel, line string }{
		{"main.go", "main.go[CG5T]: F:m | R:- | A:- | S:-"},
		{"src/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
		{"src/deep dir/b.go", "b.go[CG5T]: F:b | R:- | A:- | S:-"},
		{"pages/[id]/[...slug].go", "[...slug].go[CG5T]: F:s | R:- | A:- | S:-"},
	} {
		if text, err = InsertEntry(text, target.rel, target.line, root); err != nil {
			t.Fatalf("insert %s: %v", target.rel, err)
		}
	}
	if strings.Contains(text, "/work/") || !strings.HasPrefix(text, "#head\n\n==="+NeutralRepositoryRoot+"/===\n") {
		t.Fatalf("a new index begins with the neutral root section and never records the runtime root:\n%s", text)
	}
	for _, header := range []string{"/src/===\n", "/src/deep dir/===\n", "/pages/[id]/===\n"} {
		if !strings.Contains(text, "==="+NeutralRepositoryRoot+header) {
			t.Fatalf("later sections continue the neutral root; missing %q:\n%s", header, text)
		}
	}
	for _, at := range []string{root, "/srv/checkout"} {
		doc := buildDoc(t, text)
		ResolveRelPaths(doc, at)
		for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go", "pages/[id]/[...slug].go"} {
			if FindEntry(doc, rel) == nil {
				t.Fatalf("at %q: %s does not resolve", at, rel)
			}
		}
	}
}

// Tag location has one implementation (EntryTagSpan): what the dictionary, E scale, and S quota read must be the real tag, not the
// first square bracket in the filename. A review measured it: [id].page.test.ts[XCR7T] was permanently refused by the dictionary hard gate,
// [v1].ts[CG9T] had its quota counted as C1, and a 401-line [big].ts reported no E scale.
func TestTagConsumersReadTheTagNotTheFileName(t *testing.T) {
	for _, test := range []struct {
		line    string
		nameEnd int
		tag     string
	}{
		{"[id].page.test.ts[XCR7T]: F:x | R:- | A:- | S:-", 17, "XCR7T"},
		{"[v1].ts[CG9T]: F:x | R:- | A:- | S:-", 7, "CG9T"},
		{"pages/docs/[...id].go[CD5S]: F:x | R:- | A:- | S:-", 21, "CD5S"},
		{"main.go[CG7T]: F:x | R:- | A:- | S:-", 7, "CG7T"},
		{"loose[T1]: no fras here", 5, "T1"},
	} {
		nameEnd, tagStart, tagEnd, ok := EntryTagSpan(test.line)
		if !ok || nameEnd != test.nameEnd || test.line[tagStart:tagEnd] != test.tag {
			t.Fatalf("%q: nameEnd=%d tag=%q ok=%t", test.line, nameEnd, test.line[tagStart:tagEnd], ok)
		}
	}
	long := strings.Repeat("s", 120)
	if v := CheckSQuotaWith("[id9].vue[CG3T]: F:x | R:- | A:- | S:"+long, nil); v == nil {
		t.Fatal("a C3 Entry with a 120-rune S must be measured against the C3 quota, not against the 9 in its file name")
	}
	if v := CheckSQuotaWith("[v1].ts[CG9T]: F:x | R:- | A:- | S:"+long, nil); v != nil {
		t.Fatalf("a C9 Entry must not be measured against C1 because its file name holds a 1: %+v", v)
	}
	thresholds := ExtractEScaleThresholds("#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100")
	if mismatch := CheckEScaleDetail("[big].ts[CG5T]: F:x | R:- | A:- | S:-", 401, thresholds); mismatch == nil || mismatch.Actual != "T" {
		t.Fatalf("the E tag of a bracketed name must be checked: %+v", mismatch)
	}
}

// resolvedRelPaths returns every entry path the index resolves to under the given repository root (sorted).
func resolvedRelPaths(t *testing.T, text, root string) []string {
	t.Helper()
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, root)
	var rels []string
	for _, sec := range doc.Sections {
		for _, entry := range sec.Entries {
			rels = append(rels, entry.RelPath)
		}
	}
	sort.Strings(rels)
	return rels
}

// rc13 and earlier: when a dot directory (.circleci/ .claude/ .cursor/ .devcontainer/) sorted before .gitattributes,
// the first section, written with the complete root spelling, was not the root section. The old reading read it as the root section, the old writer then registered the
// root files into it, and hung the remaining sections under the truncated root. A second review measured: if this section is read in place by its real directory while a clone
// reads it by the old reading, the same bytes resolve to different path sets in the two places; fixing in place breaks the clone, and the two can never align at once,
// worse than the old version (wrong the same way in both places). Locked: once the later sections confirm the old writer, this section follows the old reading in place as well.
const dotDirFirstIndex = "#head\n" +
	"===/tmp/example project/repo/.circleci/===\n" +
	"config.yml[CG5T]: F:c | R:- | A:- | S:-\n" +
	".gitattributes[CG5T]: F:g | R:- | A:- | S:-\n" +
	"AGENTS.md[CG5T]: F:a | R:- | A:- | S:-\n\n" +
	"===/tmp/example/src/===\na.go[CG5T]: F:s | R:- | A:- | S:-\n"

func TestOldWriterIndexReadsTheSameInPlaceAndInAClone(t *testing.T) {
	root := "/tmp/example project/repo"
	clones := []string{"/srv/checkout", "C:/ci/work", "/home/u/other place/repo"}
	want := []string{".gitattributes", "AGENTS.md", "config.yml", "src/a.go"}
	if got := resolvedRelPaths(t, dotDirFirstIndex, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("in place the old writer's section keeps the old reader's view: got %q want %q", got, want)
	}
	for _, at := range clones {
		if got := resolvedRelPaths(t, dotDirFirstIndex, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("clone at %q resolves %q, the origin resolves %q", at, got, want)
		}
	}

	// The repair the orphan flow asks for lands the same everywhere: config.yml, misregistered as a root file, is removed, and the real
	// .circleci/config.yml gets its own section under the truncated root.
	text, err := RemoveEntryForPath(dotDirFirstIndex, root, "config.yml", "config.yml[CG5T]: F:c | R:- | A:- | S:-")
	if err != nil {
		t.Fatalf("remove the misfiled Entry: %v", err)
	}
	text, err = InsertEntry(text, ".circleci/config.yml", "config.yml[CG5T]: F:ci | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatalf("author the real file: %v", err)
	}
	if !strings.Contains(text, "===/tmp/example/.circleci/===\n") {
		t.Fatalf("the new section continues the truncated root:\n%s", text)
	}
	healed := []string{".circleci/config.yml", ".gitattributes", "AGENTS.md", "src/a.go"}
	for _, at := range append([]string{root}, clones...) {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, healed) {
			t.Fatalf("after the repair, %q resolves %q, want %q", at, got, healed)
		}
	}
}

// A new entry never lands in the old writer's single complete-root-spelled section: it would resolve to lib/x.go in place and to
// x.go in a clone (a second review measured it). Under the old reading that section is the root section, lib/ gets its own section under the truncated root, and both places agree.
func TestInsertNeverLandsInTheOldWritersFullSpelledSection(t *testing.T) {
	root := "/tmp/example project/repo"
	text := "#head\n===/tmp/example project/repo/lib/===\nl.go[CG5T]: F:l | R:- | A:- | S:-\n\n" +
		"===/tmp/example/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "lib/x.go", "x.go[CG5T]: F:x | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "===/tmp/example/lib/===\nx.go[CG5T]") {
		t.Fatalf("lib/x.go must get its own section under the truncated root:\n%s", out)
	}
	origin := resolvedRelPaths(t, out, root)
	if !reflect.DeepEqual(origin, []string{"l.go", "lib/x.go", "src/a.go"}) {
		t.Fatalf("in place: %q", origin)
	}
	for _, at := range []string{"/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, origin) {
			t.Fatalf("clone at %q resolves %q, the origin resolves %q", at, got, origin)
		}
	}
}

// A first section spelled complete but not at the root, with no truncated-root section to corroborate it, is the directory it literally names: when the new writer builds an
// index from scratch under a special root and the first entry happens to land in a dot directory, the second entry (a root file) must get its own root section instead of being stuffed in.
func TestFreshIndexWhoseFirstSectionIsADotDirectoryKeepsItsOwnRootSection(t *testing.T) {
	root := "/tmp/example project/repo"
	text := "#head\n"
	var err error
	for _, target := range []struct{ rel, line string }{
		{".circleci/config.yml", "config.yml[CG5T]: F:c | R:- | A:- | S:-"},
		{".gitattributes", ".gitattributes[CG5T]: F:g | R:- | A:- | S:-"},
		{"src/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
	} {
		if text, err = InsertEntry(text, target.rel, target.line, root); err != nil {
			t.Fatalf("insert %s: %v", target.rel, err)
		}
	}
	want := []string{".circleci/config.yml", ".gitattributes", "src/a.go"}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("at %q: %q, want %q\n%s", at, got, want, text)
		}
	}
}

// A third review measured (a real rc13 binary): when the truncating character is the first character of some path segment (/w/(x)/repo,
// C:/w/=e/repo, /w/（旧）/repo), the truncated root /w aligns with a directory, the old writer's complete-spelled first section lands "below" the
// truncated root, the clone's old-reading fallback does not fire, and the root file resolves to (x)/repo/main.go; rc13 aligned in both places, so
// this was a regression. Locked: the first directory section that equals the common prefix under the old reading and differs under the extended reading is the
// old writer's section, whether its extended path lies outside or below the prefix.
func TestOldWriterIndexUnderASlashCutRootRelocates(t *testing.T) {
	for _, shape := range []struct{ root, truncated string }{
		{"/w/(x)/repo", "/w"},
		{"C:/w/=e/repo", "C:/w"},
		{"/w/（旧）/repo", "/w"},
	} {
		text := "#head\n===" + shape.root + "/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
			"===" + shape.truncated + "/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n" +
			"===" + shape.truncated + "/src/deep dir/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"
		want := []string{"main.go", "src/a.go", "src/deep dir/b.go"}
		for _, at := range []string{shape.root, "/srv/checkout", "D:/ci/work", "/home/u/other (place)/repo"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("index written at %q, read at %q: %q, want %q", shape.root, at, got, want)
			}
		}
		// A new section continues the truncated root, and every place still agrees after the insertion.
		grown, err := InsertEntry(text, "lib/c.go", "c.go[CG5T]: F:c | R:- | A:- | S:-", shape.root)
		if err != nil || !strings.Contains(grown, "==="+shape.truncated+"/lib/===\n") {
			t.Fatalf("root %q: %v\n%s", shape.root, err, grown)
		}
		want = []string{"lib/c.go", "main.go", "src/a.go", "src/deep dir/b.go"}
		for _, at := range []string{shape.root, "/srv/checkout"} {
			if got := resolvedRelPaths(t, grown, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("after growth, written at %q, read at %q: %q, want %q", shape.root, at, got, want)
			}
		}
	}
}

// A top-level directory name in an old writer's index starts with a truncating character (the route group (home)/): under the old reading its section also equals the
// truncated root, but it is not the first section, so it must resolve by its own extended path, the same in place and in a clone.
func TestOldWriterIndexWithATopLevelCutDirectoryReadsTheSameEverywhere(t *testing.T) {
	root := "/tmp/example project/repo"
	grown, err := InsertEntry(truncatedRootIndex, "(home)/page.go", "page.go[CG5T]: F:p | R:- | A:- | S:-", root)
	if err != nil || !strings.Contains(grown, "===/tmp/example/(home)/===\n") {
		t.Fatalf("%v\n%s", err, grown)
	}
	want := []string{"(home)/page.go", "main.go", "src/a.go", "src/deep dir/b.go"}
	for _, at := range []string{root, "/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, grown, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("read at %q: %q, want %q", at, got, want)
		}
	}
}

// No root section, and sibling directories sharing a truncating-character prefix (app/ and "app (old)/"): this is not the old writer, and a clone must not
// flatten both sections onto the root (third review, NIT 8). The old writer never wrote an ordinary section landing on the truncated root next to its first section.
func TestSiblingsSharingACutPrefixAreNotAnOldWriterIndex(t *testing.T) {
	for _, text := range []string{
		"#head\n===/repo/app/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n===/repo/app (old)/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n",
		"#head\n===/repo/app (old)/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n\n===/repo/app/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n",
	} {
		want := []string{"app (old)/b.go", "app/a.go"}
		for _, at := range []string{"/repo", "/srv/checkout"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("read at %q: %q, want %q\n%s", at, got, want, text)
			}
		}
	}
}

// The directory where the truncated root plus the directory spells the repository root itself (/w plus (x)/repo equals /w/(x)/repo): the old-writer criterion is
// pure text, and a non-first section always resolves by its extended path relative to the truncated root, so although this header has the same text as the first section, it resolves to its own
// directory, the same in place and in a clone.
func TestADirectoryRepeatingTheRootsTailResolvesTheSameEverywhere(t *testing.T) {
	root := "/w/(x)/repo"
	text := "#head\n===/w/(x)/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n===/w/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "(x)/repo/f.go", "f.go[CG5T]: F:f | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"(x)/repo/f.go", "main.go", "src/a.go"}
	for _, at := range []string{root, "/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("read at %q: %q, want %q", at, got, want)
		}
	}
}

// Reading the header back is not enough; the new section must also resolve to the directory it is meant for. When a clone sits "inside" the root the index records
// (recorded root /srv, running root /srv/work), the header /srv/work/x of directory work/x resolves to x by direct prefix comparison,
// and the entry is registered under another path. The write side fails closed.
func TestInsertRefusesASectionThatWouldResolveSomewhereElse(t *testing.T) {
	text := "#head\n===/srv/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n===/srv/lib/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "work/x/f.go", "f.go[CG5T]: F:f | R:- | A:- | S:-", "/srv/work")
	if !errors.Is(err, ErrDirectoryUnspellable) || out != "" {
		t.Fatalf("work/x would resolve as x under this root and must be refused: %v\n%s", err, out)
	}
	if err := CheckInsertable(text, "work/x/f.go", "/srv/work"); !errors.Is(err, ErrDirectoryUnspellable) {
		t.Fatalf("the probe must answer what the writer answers: %v", err)
	}
	if out, err := InsertEntry(text, "docs/f.go", "f.go[CG5T]: F:f | R:- | A:- | S:-", "/srv/work"); err != nil ||
		!strings.Contains(out, "===/srv/docs/===\n") {
		t.Fatalf("a directory that does not collide is accepted: %v", err)
	}
}

// The repository root itself cannot be written as a header that reads back (the first segment starts with a truncating character, or a segment starts or ends with whitespace): rc13
// "happened" to govern these roots through the old reading, and a third review measured the new writer refusing every batch. Locked: a new index records the root the header
// reads back to (a section root is a coordinate, not a runtime path), and when the first entry lands in a subdirectory it first writes an empty root section as the relocation anchor, so in-place
// and clone parsing agree; only when not even a read-back root exists does it refuse, naming the repository root.
func TestFreshIndexUnderARootNoHeaderCanSpell(t *testing.T) {
	for _, shape := range []struct{ root, recorded string }{
		{"/(x)/repo", NeutralRepositoryRoot},
		{"C:/(backup)/repo", NeutralRepositoryRoot},
		{"/home/u/s /repo", NeutralRepositoryRoot},
		{"/home/u/ s/repo", NeutralRepositoryRoot},
		{"/(x)", NeutralRepositoryRoot},
	} {
		text := "#head\n"
		var err error
		for _, target := range []struct{ rel, line string }{
			{".circleci/config.yml", "config.yml[CG5T]: F:c | R:- | A:- | S:-"},
			{".gitattributes", ".gitattributes[CG5T]: F:g | R:- | A:- | S:-"},
			{"src/deep dir/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
		} {
			if text, err = InsertEntry(text, target.rel, target.line, shape.root); err != nil {
				t.Fatalf("root %q insert %s: %v", shape.root, target.rel, err)
			}
		}
		if !strings.Contains(text, "==="+shape.recorded+"/===\n") || strings.Contains(text, shape.root) {
			t.Fatalf("root %q must be recorded as %q:\n%s", shape.root, shape.recorded, text)
		}
		want := []string{".circleci/config.yml", ".gitattributes", "src/deep dir/a.go"}
		for _, at := range []string{shape.root, "/srv/checkout"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("root %q read at %q: %q, want %q\n%s", shape.root, at, got, want, text)
			}
		}
	}
}

// The planning-side probe judges directories only: however odd the filename, it does not change the conclusion.
func TestCheckInsertableJudgesTheDirectoryOnly(t *testing.T) {
	text := "#head\n===/repo/===\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n"
	for rel, refused := range map[string]bool{
		"src/[id].go": false, "app/(home)/ odd.go": false, "main2.go": false, "trail /x.go": true, " lead/x.go": true,
	} {
		err := CheckInsertable(text, rel, "/repo")
		if refused != errors.Is(err, ErrDirectoryUnspellable) || (!refused && err != nil) {
			t.Fatalf("%q: refused=%t err=%v", rel, refused, err)
		}
	}
}

// authoredIndex inserts one plain Entry per path into an empty index, in order.
func authoredIndex(t *testing.T, root string, rels ...string) string {
	t.Helper()
	text := "#head\n"
	for _, rel := range rels {
		out, err := InsertEntry(text, rel, rel[strings.LastIndex(rel, "/")+1:]+"[CG5T]: F:x | R:- | A:- | S:-", root)
		if err != nil {
			t.Fatalf("insert %s at %s: %v", rel, root, err)
		}
		text = out
	}
	return text
}

// The two blocks of the fourth review (measured on a real binary) are one ambiguity: the header bytes ===<P>/(x)…/=== plus ===<P>/src/===
// may be an old writer's index whose "root path has a segment starting with a truncating character", or an index under a clean root whose "top-level directory starts with a
// truncating character and sorts first"; pure text cannot tell them apart, and a clone can only read them as the old writer. Two things locked:
// first, this writer never produces that set of bytes: a new index always starts with the root section (an empty root section first when the first entry lands in a directory), so an
// index it builds parses the same in place and in a clone even with no root-file entry. Second, that set of bytes can only come from rc13 or
// earlier, when the first section was used as the root section (root files were registered in it); in place it is read by the same pure-text criterion, agreeing with the clone and
// with rc13 itself, and an ordinary repair converges both places together.
func TestFreshIndexWithoutRootEntriesReadsTheSameEverywhere(t *testing.T) {
	for _, shape := range []struct {
		root string
		rels []string
	}{
		{"/w/(x)/repo", []string{"a/f.go", "b/g.go"}},
		{"/w/=x/repo", []string{"a/f.go", "b/g.go"}},
		{"C:/w/(x)/repo", []string{"a/f.go", "b/g.go"}},
		{"/w/x y/repo", []string{"a/f.go", "b/g.go"}},
		{"/w/x/repo", []string{"a/f.go", "b/g.go"}},
		{"/clean/repo", []string{"(home)/p.go", "src/q.go"}},
		{"/clean/repo", []string{"=x/p.go", "src/deep dir/q.go"}},
		{"/tmp/example project/repo", []string{".circleci/config.yml", "src/q.go"}},
	} {
		text := authoredIndex(t, shape.root, shape.rels...)
		want := append([]string{}, shape.rels...)
		sort.Strings(want)
		for _, at := range []string{shape.root, "/srv/checkout", "D:/ci/work", "/home/u/other (place)/repo"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("index created at %q, read at %q: %q, want %q\n%s", shape.root, at, got, want, text)
			}
		}
		if first := firstDirectorySection(buildDoc(t, text)); first == nil || len(first.Entries) != 0 ||
			normalizeRootPath(first.AbsPath) != NeutralRepositoryRoot {
			t.Fatalf("a new index whose first Entry lives in a directory begins with its empty neutral root section:\n%s", text)
		}
		if strings.Contains(text, shape.root) {
			t.Fatalf("a new index never records the runtime root:\n%s", text)
		}
	}
}

// The block of the fifth review (measured on a real binary): when a segment of the root path starts with a truncating character (/w/(x)/repo) and the repository happens to hold a
// directory of the same name (x)/, every section but the first lands below /w/(x). The criterion then also demanded "the common prefix of the extended paths equals the
// truncated root", so the criterion failed: in place it fell back to the runtime root and read correctly, while a clone could only relocate by the common prefix /w/(x),
// reading the (x) section as the root section and the root section as repo/; aligned in place, 5 missing + 5 orphan in every clone, the two sides fighting back and forth.
// Removing the last entry of src also cuts out the same bytes (F1b), so the write side alone cannot stop it. The criterion no longer has that clause.
func TestADirectoryNamedLikeTheRootsCutSegmentReadsTheSameEverywhere(t *testing.T) {
	for _, root := range []string{"/w/(x)/repo", "/home/u/(work)/proj", "C:/Users/(me)/repo", "/w/=x/repo", "/w/（全角）/repo", "/w/(x)/(y)/repo"} {
		segments := strings.Split(root, "/")
		cut := ""
		for _, segment := range segments {
			if strings.ContainsAny(segment, "=(（") {
				cut = segment
				break
			}
		}
		rels := []string{"AGENTS.md", cut + "/main.go", cut + "/lib/x.go"}
		text := authoredIndex(t, root, rels...)
		want := append([]string{}, rels...)
		sort.Strings(want)
		for _, at := range []string{root, "/srv/checkout", "D:/ci/work"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("created at %q, read at %q: %q, want %q\n%s", root, at, got, want, text)
			}
		}
	}
	// F1b: root section + (x)/ + src/; after the only src entry is removed the same bytes remain, and both places must still agree.
	root := "/w/(x)/repo"
	text := authoredIndex(t, root, "AGENTS.md", "(x)/main.go", "src/a.go")
	pruned, err := RemoveEntryForPath(text, root, "src/a.go", "a.go[CG5T]: F:x | R:- | A:- | S:-")
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, pruned, at); !reflect.DeepEqual(got, []string{"(x)/main.go", "AGENTS.md"}) {
			t.Fatalf("after the removal, read at %q: %q\n%s", at, got, pruned)
		}
	}
}

// An index where "every section carries the complete root and there is no root section" is the product of no released writer (this writer always starts with the root section, and later sections
// hang under the root the old reading reads back). When a root path segment starts with a truncating character, these bytes are identical, byte for byte, to what rc13 wrote under the clean
// root /w, and pure text cannot tell them apart; what is locked is that both places read alike, not one particular meaning.
func TestBytesSharedWithTheOldWriterReadTheSameEverywhere(t *testing.T) {
	text := "#head\n===/w/(x)/repo/a/===\nf.go[CG5T]: F:f | R:- | A:- | S:-\n\n===/w/(x)/repo/b/===\ng.go[CG5T]: F:g | R:- | A:- | S:-\n"
	origin := resolvedRelPaths(t, text, "/w/(x)/repo")
	for _, at := range []string{"/w", "/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, origin) {
			t.Fatalf("read at %q: %q, the origin reads %q", at, got, origin)
		}
	}
}

// The same header bytes can appear twice and point at two directories: the old writer's first section lands on the root under the old reading, while the section registered
// for the directory it literally names lands on the directory under the extended reading. Same-named files in the two sections are two different paths; Parse used to report a
// duplicate entry by "header path + filename", and code_parse_warning would then refuse every later write to this index, leaving the repository stuck in
// authoring_required forever (a fifth review measured it; rc13 was stuck there too). The first section gets its own key when the two readings differ; two entries that really resolve to
// the same path are still reported by path where objects are loaded, and an ordinary duplicate section with the same header still warns here.
func TestTheSameNameUnderTwoReadingsOfOneHeaderIsNotADuplicate(t *testing.T) {
	// rc13, clean root, top-level (home)/ sorted first, main.go on the root as well.
	root := "/clean/repo"
	text := "#head\n===/clean/repo/(home)/===\nmain.go[CG5T]: F:root main | R:- | A:- | S:-\n\n" +
		"===/clean/repo/src/===\nq.go[CG5T]: F:q | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "(home)/main.go", "main.go[CG5T]: F:home main | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, warnings := Parse(out); len(warnings) != 0 {
		t.Fatalf("two paths, not one duplicate: %+v\n%s", warnings, out)
	}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, []string{"(home)/main.go", "main.go", "src/q.go"}) {
			t.Fatalf("read at %q: %q", at, got)
		}
	}
	// This writer's own index: directory (x)/repo under /w/(x)/repo shares its header with the root section.
	own := authoredIndex(t, "/w/(x)/repo", "main.go", "src/a.go", "(x)/repo/other.go", "(x)/repo/main.go")
	if _, warnings := Parse(own); len(warnings) != 0 {
		t.Fatalf("this writer's own index must not warn: %+v\n%s", warnings, own)
	}
	// When the first section's two readings differ but the old-writer criterion fails (an ordinary root section landing on the root sits beside it), the first section reads by its extended path,
	// and the later section with the same header is the same directory: same-named entries are real duplicates and must warn. The single-file (Legacy) layout has nowhere else to report it
	// (sixth review: after the separate key, this shape fell silent under Legacy, and remove-entry would delete one of the two).
	sameDirectory := "#head\n===/clean/repo/app (old)/===\na.go[CG5T]: F:first | R:- | A:- | S:-\n\n" +
		"===/clean/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/clean/repo/app (old)/===\na.go[CG5T]: F:second | R:- | A:- | S:-\n"
	if _, warnings := Parse(sameDirectory); len(warnings) != 1 || !strings.Contains(warnings[0].Msg, "重复条目") {
		t.Fatalf("one directory spelled twice holds a true duplicate: %+v", warnings)
	}
	// Same-named entries in an ordinary duplicate section with the same header still warn.
	plain := "#head\n===/repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n===/repo/===\nm.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/repo/src/===\na.go[CG5T]: F:again | R:- | A:- | S:-\n"
	if _, warnings := Parse(plain); len(warnings) != 1 || !strings.Contains(warnings[0].Msg, "重复条目") {
		t.Fatalf("a true duplicate must still be reported: %+v", warnings)
	}
}

func TestOldWriterIndexUnderACleanRootReadsAsTheOldReaderDidEverywhere(t *testing.T) {
	// rc13 under a clean root with the default scope: "(" sorts before ".gitattributes", the first section was written as ===<root>/(home)/===,
	// which reads back as the root, so the root files were all registered in this section, and (home)/p.go itself resolved to p.go (orphan + missing).
	root := "/clean/repo"
	text := "#head\n===/clean/repo/(home)/===\np.go[CG5T]: F:p | R:- | A:- | S:-\n" +
		".gitattributes[CG5T]: F:g | R:- | A:- | S:-\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/clean/repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	want := []string{".gitattributes", "main.go", "p.go", "src/a.go"}
	for _, at := range []string{root, "C:/clean/repo", "/srv/checkout"} {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("read at %q: %q, want the old reader's view %q", at, got, want)
		}
	}
	// An ordinary repair: remove the misresolved p.go and register the real (home)/p.go; both places converge together.
	repaired, err := RemoveEntryForPath(text, root, "p.go", "p.go[CG5T]: F:p | R:- | A:- | S:-")
	if err != nil {
		t.Fatal(err)
	}
	if repaired, err = InsertEntry(repaired, "(home)/p.go", "p.go[CG5T]: F:page | R:- | A:- | S:-", root); err != nil {
		t.Fatal(err)
	}
	healed := []string{"(home)/p.go", ".gitattributes", "main.go", "src/a.go"}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, repaired, at); !reflect.DeepEqual(got, healed) {
			t.Fatalf("after the repair, read at %q: %q, want %q\n%s", at, got, healed, repaired)
		}
	}
}

// The root section is the anchor of the whole index's coordinates: relocation derives the root from the sections' common prefix, and that prefix is the root only when some section lands exactly on it.
// A fourth review measured: in a repository whose root cannot be written as a header, the empty root section was trimmed in passing by the next removal, and in place the live entries were
// at once reported as orphans (a.go, b.go), recoverable only by deleting everything and rewriting. As long as another directory section remains, the root section is never trimmed even when empty.
func TestTheRootSectionIsNeverPrunedWhileOtherSectionsRemain(t *testing.T) {
	for _, root := range []string{"/home/u/s /repo", "/clean/repo", "/tmp/example project/repo", "/w/(x)/repo"} {
		text := authoredIndex(t, root, "src/a.go", "src/b.go", "src/c.go")
		out, err := RemoveEntryForPath(text, root, "src/c.go", "c.go[CG5T]: F:x | R:- | A:- | S:-")
		if err != nil {
			t.Fatal(err)
		}
		for _, at := range []string{root, "/srv/checkout"} {
			if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, []string{"src/a.go", "src/b.go"}) {
				t.Fatalf("root %q read at %q after a removal: %q\n%s", root, at, got, out)
			}
		}
	}
	// The old writer's first section (complete spelling) is an anchor too: it is not trimmed once the root files are gone, and the section family under the truncated root still resolves.
	out, err := RemoveEntryForPath(truncatedRootIndex, "/tmp/example project/repo", "main.go", "main.go[CG5T]: F:m | R:- | A:- | S:-")
	if err != nil || !strings.Contains(out, "===/tmp/example project/repo/===\n") {
		t.Fatalf("the old writer's first section anchors its family and must stay: %v\n%s", err, out)
	}
	// An empty section that is not an anchor is trimmed as usual; so is the last directory section once it is empty.
	plain := "#head\n===/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n===/repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	if out, err := RemoveEntryForPath(plain, "/repo", "src/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"); err != nil || strings.Contains(out, "/repo/src/") {
		t.Fatalf("an empty section that anchors nothing is pruned: %v\n%s", err, out)
	}
	only := "#head\n===/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n"
	if out, err := RemoveEntryForPath(only, "/repo", "main.go", "main.go[CG5T]: F:m | R:- | A:- | S:-"); err != nil || strings.Contains(out, "===") {
		t.Fatalf("the last section is pruned when it empties: %v\n%s", err, out)
	}
}

func TestIndexWrittenWithTruncatedRootStillRelocates(t *testing.T) {
	// The same index cloned elsewhere: today it holds by "everything truncates to the same root" by chance; under the extended reading the first section becomes the complete root,
	// and it must still hold.
	doc := buildDoc(t, truncatedRootIndex)
	ResolveRelPaths(doc, "/srv/checkout")
	for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go"} {
		if FindEntry(doc, rel) == nil {
			t.Fatalf("%s must survive relocation; got %q %q %q", rel, relOf(t, doc, 0, 0), relOf(t, doc, 1, 0), relOf(t, doc, 2, 0))
		}
	}
}

func TestNewStyleIndexUnderSpecialRootRelocates(t *testing.T) {
	text := "#head\n" +
		"===/home/a/my repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/home/a/my repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n" +
		"===/home/a/my repo/src/deep dir/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"
	for _, root := range []string{"/home/a/my repo", "/srv/checkout", "C:/Users/John Doe/work"} {
		doc := buildDoc(t, text)
		ResolveRelPaths(doc, root)
		for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go"} {
			if FindEntry(doc, rel) == nil {
				t.Fatalf("root %q: %s must resolve; got %q %q %q", root, rel, relOf(t, doc, 0, 0), relOf(t, doc, 1, 0), relOf(t, doc, 2, 0))
			}
		}
	}
	// A new-style index with no root section must not be flattened onto one directory by the old reading
	noRoot := "#head\n===/home/a/my repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n===/home/a/my repo/lib/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"
	doc := buildDoc(t, noRoot)
	ResolveRelPaths(doc, "/srv/checkout")
	if FindEntry(doc, "src/a.go") == nil || FindEntry(doc, "lib/b.go") == nil {
		t.Fatalf("sections without a root section must keep their own directories: %q %q", relOf(t, doc, 0, 0), relOf(t, doc, 1, 0))
	}
}

func TestInsertEntryRefusesADirectoryTheHeaderCannotSpell(t *testing.T) {
	text := "#head\n===/repo/===\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n"
	for _, rel := range []string{" lead/a.go", "trail /a.go"} {
		out, err := InsertEntry(text, rel, "a.go[CG5T]: F:a | R:- | A:- | S:-", "/repo")
		if err == nil {
			t.Fatalf("%q would be written as a header the next read resolves elsewhere:\n%s", rel, out)
		}
	}
}
