//go:build !windows

package mcptools

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The Evidence guard reads exactly what governance reads: a symbolic link on
// the Evidence path or a file that is not regular (a FIFO would block) ends
// the reuse instead of being skipped or opened.
func TestOverviewDatabaseGuardRefusesLinksAndSpecialFiles(t *testing.T) {
	root := buildVolumeRepo(t, true, true)
	if _, ok := overviewDatabaseEvidenceDigest(root); !ok {
		t.Fatal("an ordinary Evidence tree must be bindable")
	}
	evidence := filepath.Join(root, ".aoci", "database", "evidence")
	if err := os.MkdirAll(evidence, 0o755); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(evidence, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("mkfifo unavailable: ", err)
	}
	done := make(chan bool, 1)
	go func() { _, ok := overviewDatabaseEvidenceDigest(root); done <- ok }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a FIFO in the Evidence tree was bound")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the guard blocked on a FIFO")
	}
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "elsewhere.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	baselinePath := filepath.Join(root, ".aoci", "database-baseline.json")
	_ = os.Remove(baselinePath)
	if err := os.Symlink(target, baselinePath); err != nil {
		t.Fatal(err)
	}
	if _, ok := overviewDatabaseEvidenceDigest(root); ok {
		t.Fatal("a linked Database Baseline was bound")
	}
}
