package hooks

import (
	"strings"
	"testing"
)

// A managed snippet converted to CRLF by a Windows checkout is still found
// (audit G5-11).
func TestCodexManagedSnippetSurvivesCRLF(t *testing.T) {
	snippet := "# aoci:codex-compact-hook:v1\n[[hooks.SessionStart]]\nmatcher = \"^compact$\"\n"
	installed := "model = \"x\"\n" + snippet
	if !containsCodexManagedSnippet(strings.ReplaceAll(installed, "\n", "\r\n"), snippet) {
		t.Fatal("a CRLF copy of the managed snippet was not recognized")
	}
}
