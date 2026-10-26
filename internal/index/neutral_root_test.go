package index

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// A committed index must not publish the machine it was written on: a new index
// names NeutralRepositoryRoot, resolves to the same paths wherever it is read,
// and an existing index keeps the root it already records.
func TestNewIndexRecordsTheNeutralRootAndReadsTheSameEverywhere(t *testing.T) {
	author := "/home/alice/projects/app"
	rels := []string{"README.md", "cmd/app/main.go", "internal/store/db.go"}
	text := "#head\n"
	var err error
	for _, rel := range rels {
		name := rel[strings.LastIndex(rel, "/")+1:]
		if text, err = InsertEntry(text, rel, name+"[CG5T]: F:x | R:- | A:- | S:-", author); err != nil {
			t.Fatalf("insert %s: %v", rel, err)
		}
	}
	if strings.Contains(text, "/home/alice") || !strings.Contains(text, "==="+NeutralRepositoryRoot+"/===\n") {
		t.Fatalf("the index must carry the neutral root and no author path:\n%s", text)
	}
	want := append([]string{}, rels...)
	sort.Strings(want)
	for _, at := range []string{author, "/srv/checkout", "C:/Users/bob/app", "/repo/internal"} {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("read at %q: %q, want %q\n%s", at, got, want, text)
		}
	}

	existing := "#head\n\n===/home/carol/app/===\nREADME.md[CG5T]: F:x | R:- | A:- | S:-\n"
	extended, err := InsertEntry(existing, "lib/x.go", "x.go[CG5T]: F:x | R:- | A:- | S:-", "/srv/carol")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extended, "===/home/carol/app/lib/===\n") || strings.Contains(extended, NeutralRepositoryRoot) {
		t.Fatalf("an existing index keeps the root it records:\n%s", extended)
	}
}
