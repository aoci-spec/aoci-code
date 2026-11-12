package index

import (
	"strings"
	"testing"
)

const hashRoot = "/<repository>"

// A file may be named "#hash.txt" or "#draft#": the format writes names as
// they are. Its Entry line then begins with "#", and every reader used to
// take that for a comment, so the Entry was written, reported applied, and
// never read back. Inside a section only a "#" line without the Entry shape
// is a comment.
func TestHashNamedEntriesAreReadInsideASection(t *testing.T) {
	text := "#AOCI-CODE-VOLUME: 1\n\n===" + hashRoot + "/===\n" +
		"#hash.txt[CG5T]: F:Holds a word | R:- | A:- | S:-\n" +
		"# a real comment, kept as text\n" +
		"a.go[CG5T]: F:Runs | R:- | A:- | S:-\n\n" +
		"===" + hashRoot + "/sub/===\n" +
		"#draft#[CG5T]: F:Holds a draft | R:- | A:- | S:-\n"
	doc, warnings := Parse(text)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	ResolveRelPaths(doc, "/anywhere")
	var paths []string
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			paths = append(paths, entry.RelPath)
		}
	}
	if got := strings.Join(paths, ","); got != "#hash.txt,a.go,sub/#draft#" {
		t.Fatalf("hash-named Entries were not read: %s", got)
	}
	for line, want := range map[string]bool{
		"#hash.txt[CG5T]: F:x | R:- | A:- | S:-": false,
		"# note":                                 true,
		"#AOCI-CODE-VOLUME: 1":                   true,
		"a.go[CG5T]: F:x | R:- | A:- | S:-":      false,
		"":                                       false,
	} {
		if got := IsSectionCommentLine(line); got != want {
			t.Errorf("IsSectionCommentLine(%q) = %t, want %t", line, got, want)
		}
	}
}

// The writer must place a hash-named Entry where the reader finds it. The
// editor used to scan back over trailing "#" lines as if they were comments,
// so a new section header landed above a hash-named Entry and captured it.
func TestInsertKeepsHashNamedEntriesInTheirSection(t *testing.T) {
	text := "#AOCI-CODE-VOLUME: 1\n\n===" + hashRoot + "/===\n"
	var err error
	for _, step := range []struct{ rel, line string }{
		{"#hash.txt", "#hash.txt[CG5T]: F:Holds a word | R:- | A:- | S:-"},
		{"a.go", "a.go[CG5T]: F:Runs | R:- | A:- | S:-"},
		{"sub/#x.txt", "#x.txt[CG5T]: F:Holds x | R:- | A:- | S:-"},
		{"sub/ok.txt", "ok.txt[CG5T]: F:Holds ok | R:- | A:- | S:-"},
	} {
		if text, err = InsertEntry(text, step.rel, step.line, "/anywhere"); err != nil {
			t.Fatalf("insert %s: %v", step.rel, err)
		}
	}
	doc, warnings := Parse(text)
	ResolveRelPaths(doc, "/anywhere")
	var paths []string
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			paths = append(paths, entry.RelPath)
		}
	}
	if got := strings.Join(paths, ","); len(warnings) != 0 || got != "#hash.txt,a.go,sub/#x.txt,sub/ok.txt" {
		t.Fatalf("Entries are not where they were written: %s warnings=%v\n%s", got, warnings, text)
	}
}

// Releases up to 0.2.0-rc1 appended one more copy of a hash-named Entry on
// every Maintain round. Read as duplicates those copies would fail the whole
// Volume closed, and removing them by hand leaves a Volume edited outside the
// tool. The last copy is the Entry, the earlier ones stay comments, and the
// first write through the editor drops them. An ordinary name still reports
// its duplicate.
func TestPollutedHashCopiesReadAsOneEntryAndAreDroppedOnWrite(t *testing.T) {
	old := "#hash.txt[CG5T]: F:first copy | R:- | A:- | S:-"
	last := "#hash.txt[CG5T]: F:last copy | R:- | A:- | S:-"
	text := "#AOCI-CODE-VOLUME: 1\n\n===" + hashRoot + "/===\n" +
		"a.go[CG5T]: F:Runs | R:- | A:- | S:-\n" + old + "\n" + last + "\n" + last + "\n"
	doc, warnings := Parse(text)
	if len(warnings) != 0 || len(doc.SupersededLines) != 2 {
		t.Fatalf("copies must read as one Entry without a warning: warnings=%v superseded=%v", warnings, doc.SupersededLines)
	}
	if entries := doc.Sections[0].Entries; len(entries) != 2 || entries[1].F != "last copy" {
		t.Fatalf("the last copy must be the Entry: %+v", entries)
	}
	next, err := ReplaceEntryForPath(text, "/anywhere", "#hash.txt", last, "#hash.txt[CG5T]: F:rewritten | R:- | A:- | S:-")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(next, "#hash.txt[") != 1 || !strings.Contains(next, "F:rewritten") || !strings.Contains(next, "a.go[CG5T]") {
		t.Fatalf("a write must leave exactly the new Entry:\n%s", next)
	}
	if DropSupersededCopies(next) != next {
		t.Fatal("a clean Volume must come back byte-identical")
	}
	plain := "===" + hashRoot + "/===\na.go[CG5T]: F:one | R:- | A:- | S:-\na.go[CG5T]: F:two | R:- | A:- | S:-\n"
	if _, plainWarnings := Parse(plain); len(plainWarnings) != 1 {
		t.Fatalf("an ordinary duplicate must still be reported: %v", plainWarnings)
	}
}
