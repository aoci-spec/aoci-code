package index

import (
	"strings"
	"testing"
)

const relocateFixture = "#AOCI-CODE-VOLUME: 1\n\n===Code /home/author/proj/===\nREADME.md[CG5T]: F:a | R:- | A:- | S:-\n\n===/home/author/proj/docs/===\nguide.md[CG5T]: F:b | R:- | A:- | S:-\n\n===/home/author/proj/internal/cli/===\nroot.go[CG8L]: F:c | R:- | A:- | S:-\n"

// A clone at another path rewrites every header to the neutral root, keeps
// each header's description and every Entry byte, and reads back to the same
// directories; a second pass changes nothing.
func TestRelocateSectionRootsRewritesMachineRoots(t *testing.T) {
	out, changed, err := RelocateSectionRoots(relocateFixture, "/elsewhere/clone")
	if err != nil || changed != 3 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	for _, want := range []string{"===Code /<repository>/===", "===/<repository>/docs/===", "===/<repository>/internal/cli/===",
		"README.md[CG5T]: F:a | R:- | A:- | S:-", "root.go[CG8L]: F:c | R:- | A:- | S:-"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "/home/author") {
		t.Fatalf("machine path survived:\n%s", out)
	}
	doc, _ := Parse(out)
	ResolveRelPaths(doc, "/elsewhere/clone")
	rels := map[string]bool{}
	for _, sec := range doc.Sections {
		for _, entry := range sec.Entries {
			rels[entry.RelPath] = true
		}
	}
	for _, want := range []string{"README.md", "docs/guide.md", "internal/cli/root.go"} {
		if !rels[want] {
			t.Fatalf("entry %s lost its directory: %v", want, rels)
		}
	}
	again, changed, err := RelocateSectionRoots(out, "/yet/another")
	if err != nil || changed != 0 || again != out {
		t.Fatalf("second pass must be a no-op: changed=%d err=%v", changed, err)
	}
}

// Windows line endings and a missing trailing newline survive the rewrite.
func TestRelocateSectionRootsKeepsLineEndings(t *testing.T) {
	crlf := strings.TrimSuffix(strings.ReplaceAll(relocateFixture, "\n", "\r\n"), "\r\n")
	out, changed, err := RelocateSectionRoots(crlf, "/elsewhere")
	if err != nil || changed != 3 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") || strings.HasSuffix(out, "\n") {
		t.Fatalf("line endings changed:\n%q", out)
	}
}

// The note follows the identity example when there is one, precedes the first
// dictionary otherwise, and is never added twice.
func TestEnsureSectionRootNotePlacement(t *testing.T) {
	note := "#Section root: /<repository> stands for the checkout root"
	withExample := "#AOCI-META-VOLUME: 1\n#Code canonical identity example: code:path/to/file.go\n#Code Entry example: x\n#[Tag dictionary: code]\n#A Layer: C-Code\n"
	out, added := EnsureSectionRootNote([]byte(withExample), note)
	if !added || !strings.Contains(string(out), "code:path/to/file.go\n"+note+"\n#Code Entry example") {
		t.Fatalf("note not placed after the identity example:\n%s", out)
	}
	if _, again := EnsureSectionRootNote(out, note); again {
		t.Fatal("a Meta that names the root must not gain the note twice")
	}
	withoutExample := "#AOCI-META-VOLUME: 1\r\n#Object-Protocol: x\r\n#[Tag dictionary: code]\r\n#A Layer: C-Code\r\n"
	out, added = EnsureSectionRootNote([]byte(withoutExample), note)
	if !added || !strings.Contains(string(out), "#Object-Protocol: x\r\n"+note+"\r\n#[Tag dictionary: code]") {
		t.Fatalf("note not placed before the first dictionary with CRLF kept:\n%q", out)
	}
	bare := "#AOCI-META-VOLUME: 1"
	out, added = EnsureSectionRootNote([]byte(bare), note)
	if !added || string(out) != bare+"\n"+note+"\n" {
		t.Fatalf("note not appended with a closing newline:\n%q", out)
	}
}
