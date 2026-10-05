//go:build windows

package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestSafeInventoryWindowsAttributesJunctionAndLongPath(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "src", "hidden.go")
	system := filepath.Join(root, "src", "system.go")
	mustWrite(t, root, "src/hidden.go", "package source\r\n")
	mustWrite(t, root, "src/system.go", "package source\n")
	setAttributes := func(path string, attributes uint32) {
		pointer, err := windows.UTF16PtrFromString(path)
		if err != nil || windows.SetFileAttributes(pointer, attributes) != nil {
			t.Fatalf("set Windows attributes for %s", path)
		}
	}
	setAttributes(hidden, windows.FILE_ATTRIBUTE_HIDDEN)
	setAttributes(system, windows.FILE_ATTRIBUTE_SYSTEM)

	target := filepath.Join(root, "junction-target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, root, "junction-target/inside.go", "package target\n")
	junction := filepath.Join(root, "junction")
	command := exec.Command("cmd", "/c", "mklink", "/J", junction, target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v: %s", err, output)
	}

	longParts := make([]string, 10)
	for index := range longParts {
		longParts[index] = strings.Repeat(string(rune('a'+index)), 28)
	}
	longRel := filepath.ToSlash(filepath.Join(append(longParts, "long.go")...))
	mustWrite(t, root, longRel, "package longpath\n")

	report, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	managed := strings.Join(report.ManagedCandidates, "\n")
	if !strings.Contains(managed, "src/hidden.go") || !strings.Contains(managed, longRel) {
		t.Fatalf("hidden regular or long source path lost: %#v", report)
	}
	if strings.Contains(managed, "src/system.go") || strings.Contains(managed, "junction/inside.go") ||
		exclusionCategory(report, "src/system.go") != SafetyUnsafe || exclusionCategory(report, "junction") != SafetyUnsafe {
		t.Fatalf("Windows unsafe objects were followed: %#v", report)
	}
}

// #97 on its home platform: a directory junction inside an ignored
// node_modules that points back at node_modules. Windows readdir presents the
// junction as an ordinary directory, so a listing that walks ignored trees
// recurses until MAX_PATH. The inventory must finish quickly with node_modules
// as one line. A junction needs no privilege, unlike a symlink.
func TestSafeInventoryFinishesOnAnIgnoredJunctionCycle(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	mustWrite(t, root, ".gitignore", "node_modules/\n")
	mustWrite(t, root, "src/main.go", "package main\n")
	gitCommand(t, root, "add", ".gitignore", "src/main.go")
	mustWrite(t, root, "node_modules/pkg/index.js", "module.exports = 1\n")
	link := filepath.Join(root, "node_modules", "pkg", "loop")
	target := filepath.Join(root, "node_modules")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v: %s", err, output)
	}
	started := time.Now()
	report, err := BuildSafeInventory(root, WalkOptions{IncludeIgnoredCandidates: true})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("inventory took %s on a junction cycle", elapsed)
	}
	if exclusionCategory(report, "node_modules/") != SafetyGenerated || !containsPath(report.ManagedCandidates, "src/main.go") {
		t.Fatalf("node_modules must be one collapsed line: %#v", report)
	}
}
