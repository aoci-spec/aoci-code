package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Commands are registered once and reattached to every root, so a flag one
// in-process execution set used to stay set for the next: a dry-run scan in
// one test made the next test's plain scan a dry run that wrote no Baseline.
func TestInProcessExecutionsDoNotLeakCommandFlags(t *testing.T) {
	prepare := func() string {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := executeCLI([]string{"--repo", root, "--quiet", "init", "--locale", "en-US"}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("init: %d %s %s", code, stdout.String(), stderr.String())
		}
		return root
	}
	first := prepare()
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", first, "--quiet", "scan", "--dry-run"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("dry-run scan: %d %s %s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(first, ".aoci", "baseline.json")); !os.IsNotExist(err) {
		t.Fatalf("a dry run must not write a Baseline: %v", err)
	}
	second := prepare()
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", second, "--quiet", "scan"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("plain scan: %d %s %s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(second, ".aoci", "baseline.json")); err != nil {
		t.Fatalf("the plain scan after a dry run must write a Baseline: %v", err)
	}
}
