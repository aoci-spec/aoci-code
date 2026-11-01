package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// init on a fresh repository says what the first build costs while a
// narrower scope is still free (2026-11-02): scan prints the same estimate,
// but after the first scan a reduction is a governed Scope Change. A large
// first build also gets the narrowing example; a small one does not.
func TestInitShowsTheFirstBuildCostBeforeTheFirstScan(t *testing.T) {
	pinDefaultLocale(t)
	large := t.TempDir()
	initCLITestGitRepository(t, large)
	for index := 0; index < 1100; index++ {
		directory := filepath.Join(large, fmt.Sprintf("pkg%02d", index%30))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("package pkg%02d\n\nfunc F%d() int { return %d }\n", index%30, index, index)
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("f%04d.go", index)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	output, _, _, code := runCLIPrinted(t, large, "init", "--locale", "en-US", "--hooks=false", "--here=false")
	if code != ExitOK {
		t.Fatalf("init: %d\n%s", code, output)
	}
	for _, want := range []string{"1100 index files need an Entry: about", "Four Agent sessions can author at once", "exclude directories before the first scan"} {
		if !strings.Contains(output, want) {
			t.Fatalf("init on a large repository does not say %q:\n%s", want, output)
		}
	}
	small := t.TempDir()
	initCLITestGitRepository(t, small)
	if err := os.WriteFile(filepath.Join(small, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, _, _, code = runCLIPrinted(t, small, "init", "--locale", "en-US", "--hooks=false", "--here=false")
	if code != ExitOK {
		t.Fatalf("init: %d\n%s", code, output)
	}
	if !strings.Contains(output, "1 index file needs an Entry: one Maintain round") || strings.Contains(output, "exclude directories before the first scan") ||
		strings.Contains(output, "Agent sessions can author at once") {
		t.Fatalf("init on a one-file repository says too much or too little:\n%s", output)
	}
}
