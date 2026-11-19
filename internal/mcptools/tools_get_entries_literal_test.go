package mcptools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A file name that ends in a space, or holds a backslash, is its own file.
// Asked for it, aoci_get_entries never answers with the Entry of the file
// its normalized form names (reported by aoci-agent, 2026-11-19).
func TestGetEntriesNeverAnswersAnotherFilesEntry(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 3)
	buildToAligned(t, connectMCPClient(t, root))
	indexed := filepath.ToSlash(filepath.Join("pkg", "mod000", "file000.go"))
	// On Windows a backslash is a separator and a trailing space is dropped,
	// so both names denote the indexed file itself, which the loop below
	// asks for; they are distinct files only where POSIX names are.
	literals := []string{}
	if runtime.GOOS != "windows" {
		literals = append(literals, indexed+" ", strings.ReplaceAll(indexed, "/", `\`))
	}
	for _, literal := range literals {
		if err := os.WriteFile(filepath.Join(root, literal), []byte("package other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		answer := callVolumeTool(t, connectMCPClient(t, root), "aoci_get_entries", map[string]any{"paths": []string{literal}})
		if strings.Contains(answer, "object_ref=code:"+indexed+"]") {
			t.Fatalf("asked for %q, answered with %s's Entry:\n%s", literal, indexed, answer)
		}
		if !strings.Contains(answer, "volume_id=code object_ref="+wireObjectRef("code:"+literal)+"\n") {
			t.Fatalf("asked for an unindexed file %q, the answer does not say so:\n%s", literal, answer)
		}
		if err := os.Remove(filepath.Join(root, literal)); err != nil {
			t.Fatal(err)
		}
	}
	// A path that names no other file still reaches the indexed one: a
	// Windows separator, a dot segment, surrounding space.
	for _, request := range []string{strings.ReplaceAll(indexed, "/", `\`), "pkg/./mod000/file000.go", " " + indexed + " "} {
		answer := callVolumeTool(t, connectMCPClient(t, root), "aoci_get_entries", map[string]any{"paths": []string{request}})
		if !strings.Contains(answer, "object_ref=code:"+indexed+"]") {
			t.Fatalf("%q names %s and no other file, yet was not answered with it:\n%s", request, indexed, answer)
		}
	}
}

// An identity a reader could cut in the wrong place is written as a JSON
// string; every other one is written as it always was.
func TestAnswerLinesQuoteOnlyAmbiguousIdentities(t *testing.T) {
	for ref, want := range map[string]string{
		"code:pkg/a.go":         "code:pkg/a.go",
		"code:pkg/a b.go":       "code:pkg/a b.go",
		"code:pkg/a] b.go":      `"code:pkg/a] b.go"`,
		`code:pkg\a.go`:         `"code:pkg\\a.go"`,
		`code:pkg/"q".go`:       `"code:pkg/\"q\".go"`,
		"database://s/public/t": "database://s/public/t",
	} {
		if got := wireObjectRef(ref); got != want {
			t.Fatalf("wireObjectRef(%q) = %s, want %s", ref, got, want)
		}
	}
}

// A path with a ".." segment or one through a symbolic link is no Git path:
// it is answered as sent and never with the Entry its lexical form names.
func TestGetEntriesAnswersDotDotAndLinkedPathsAsSent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	root := buildManyFileVolumesRepo(t, 3)
	buildToAligned(t, connectMCPClient(t, root))
	if err := os.MkdirAll(filepath.Join(root, "nested", "pkg", "mod000"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nested"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{"alias/../pkg/mod000/file000.go", "alias/pkg/mod000/file000.go ", "missing/../pkg/mod000/file000.go"} {
		answer := callVolumeTool(t, connectMCPClient(t, root), "aoci_get_entries", map[string]any{"paths": []string{request}})
		if strings.Contains(answer, "object_ref=code:pkg/mod000/file000.go]") || strings.Contains(answer, "] file000.go[") {
			t.Fatalf("%q was answered with another file's Entry:\n%s", request, answer)
		}
	}
}
