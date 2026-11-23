//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/testkit"
)

// A host file reached through a link is refused before init reads it, so a
// FIFO behind the link cannot block init and nothing outside is read.
func TestInitRefusesLinkedHostFileBeforeReadingIt(t *testing.T) {
	linked := filepath.Join(t.TempDir(), "linked")
	testkit.WriteFile(t, linked, "main.go", "package main\n")
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(fifo, filepath.Join(linked, ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, code := runCLIPrinted(t, linked, "init", "--agent=claude", "--hooks=false", "--here=false"); code == ExitOK {
		t.Fatal("a .mcp.json link must be refused")
	}
}
