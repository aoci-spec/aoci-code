package index

import (
	"reflect"
	"strings"
	"testing"
)

func TestNeutralCodeRootSurvivesRelocationAndEdits(t *testing.T) {
	const anchor = "===project/.code/==="
	const line = "a.go[CG5T]: F:Provides a source fixture | R:- | A:- | S:-"
	for _, root := range []string{"/home/alice/repo", "/home/alice/repo/.worktrees/wt", "C:/Users/Alice/repo", "/.code", "/.code/src", "/"} {
		t.Run(root, func(t *testing.T) {
			text := "#AOCI-CODE-VOLUME: 1\n" + anchor + "\n"
			for _, rel := range []string{"src/a.go", "a.go", "app/(home)/a.go"} {
				if err := CheckInsertable(text, rel, root); err != nil {
					t.Fatalf("probe %s: %v", rel, err)
				}
				var err error
				text, err = InsertEntry(text, rel, line, root)
				if err != nil {
					t.Fatalf("insert %s: %v", rel, err)
				}
			}
			want := []string{"a.go", "app/(home)/a.go", "src/a.go"}
			for _, checkout := range []string{root, "/srv/clone", "D:/clone", "/.code/src", "/"} {
				if got := resolvedRelPaths(t, text, checkout); !reflect.DeepEqual(got, want) {
					t.Fatalf("read at %s: got %v, want %v", checkout, got, want)
				}
			}
			updated := strings.Replace(line, "source fixture", "updated fixture", 1)
			text, err := ReplaceEntryForPath(text, root, "src/a.go", line, updated)
			if err != nil {
				t.Fatal(err)
			}
			doc := buildDoc(t, text)
			ResolveRelPaths(doc, root)
			if FindEntry(doc, "a.go").FullLine != line || FindEntry(doc, "src/a.go").FullLine != updated {
				t.Fatal("same-named Entries must be updated by their relative paths")
			}
			for _, rel := range []string{"src/a.go", "a.go", "app/(home)/a.go"} {
				old := line
				if rel == "src/a.go" {
					old = updated
				}
				text, err = RemoveEntryForPath(text, root, rel, old)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !strings.Contains(text, anchor) {
				t.Fatal("removing the last Entry must retain the neutral coordinate anchor")
			}
			text, err = InsertEntry(text, "src/a.go", line, root)
			if err != nil || !strings.Contains(text, "===/.code/src/===") {
				t.Fatalf("reinsertion must keep neutral coordinates: %v\n%s", err, text)
			}
		})
	}
}

func TestNeutralCodeRootDoesNotReinterpretHistoricalSections(t *testing.T) {
	const line = "a.go[CG5T]: F:Provides a source fixture | R:- | A:- | S:-\n"
	text := "===/.code/===\n" + line + "===/.code/src/===\n" + line
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/.code/src")
	if got := doc.Sections[1].Entries[0].RelPath; got != "a.go" {
		t.Fatalf("unmarked historical sections retain direct matching: %s", got)
	}
}

func TestNeutralCodeRootRejectsAnOutsideSection(t *testing.T) {
	text := "===project/.code/===\n===/other/src/===\na.go[CG5T]: F:Provides a source fixture | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/other")
	if got := doc.Sections[1].Entries[0].RelPath; got != "" {
		t.Fatalf("a marked family with an outside section must stay unresolved: %s", got)
	}
	if _, err := InsertEntry(text, "lib/a.go", "a.go[CG5T]: F:Provides a source fixture | R:- | A:- | S:-", "/other"); err == nil {
		t.Fatal("must refuse to append to an inconsistent neutral family")
	}
}
