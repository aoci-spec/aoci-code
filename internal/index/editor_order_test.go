package index

import (
	"strings"
	"testing"
)

// A new Entry lands in path order inside its section, not at its end.
func TestInsertEntryKeepsPathOrderInsideTheSection(t *testing.T) {
	root := "/repo"
	text := "===/<repository>/===\n\n===/<repository>/pkg/===\n" +
		"a.go[CG5T]: F:a | R:- | A:- | S:-\n" +
		"c.go[CG5T]: F:c | R:- | A:- | S:-\n" +
		"e.go[CG5T]: F:e | R:- | A:- | S:-\n"
	for _, test := range []struct{ path, before string }{
		{"pkg/b.go", "c.go["}, {"pkg/0.go", "a.go["}, {"pkg/d.go", "e.go["},
	} {
		line := strings.TrimPrefix(test.path, "pkg/") + "[CG5T]: F:new | R:- | A:- | S:-"
		got, err := InsertEntry(text, test.path, line, root)
		if err != nil {
			t.Fatal(err)
		}
		at, next := strings.Index(got, line), strings.Index(got, test.before)
		if at < 0 || next < 0 || at > next {
			t.Fatalf("%s was not placed before %s:\n%q", test.path, test.before, got)
		}
	}
	got, err := InsertEntry(text, "pkg/z.go", "z.go[CG5T]: F:z | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "z.go[CG5T]: F:z | R:- | A:- | S:-\n") {
		t.Fatalf("a name after every other one goes last:\n%s", got)
	}
}
