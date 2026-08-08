//go:build windows

package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAtomicCreateCASRejectsWindowsJunctionAndReparseParent(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, realParent).CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v: %s", err, output)
	}
	if err := AtomicCreateCAS(filepath.Join(junction, "target.txt"), []byte("x")); err == nil {
		t.Fatal("junction parent was accepted")
	}

	link := filepath.Join(root, "reparse-link")
	if err := os.Symlink(realParent, link); err != nil {
		t.Skipf("directory reparse symlink unavailable: %v", err)
	}
	if err := AtomicCreateCAS(filepath.Join(link, "target.txt"), []byte("x")); err == nil {
		t.Fatal("directory reparse symlink parent was accepted")
	}
}
