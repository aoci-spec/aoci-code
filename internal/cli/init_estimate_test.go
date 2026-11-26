package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/testkit"
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
	for _, want := range []string{"1102 index files need an Entry: about", "up to that many Agent sessions author at once", "exclude directories before the first scan"} {
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
	if !strings.Contains(output, "3 index files need an Entry: one Maintain round") || strings.Contains(output, "exclude directories before the first scan") ||
		strings.Contains(output, "Agent sessions can author at once") {
		t.Fatalf("init on a one-file repository says too much or too little:\n%s", output)
	}
}

func TestInitFirstBuildEstimateIncludesGeneratedFiles(t *testing.T) {
	pinDefaultLocale(t)
	t.Setenv(baseline.FingerprintCacheEnv, "off")
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	for i := range 27 {
		testkit.WriteFile(t, root, fmt.Sprintf("src/module0000/file%03d.go", i), "package fixture\n")
	}
	printed, _, stderr, code := runCLIPrinted(t, root, "init", "--locale", "en-US", "--agent=", "--hooks=false", "--here=false")
	if code != ExitOK {
		t.Fatalf("init failed: exit %d: %s", code, stderr)
	}
	estimate := readScanAuthoringEstimate(t, root)
	if estimate.Targets != 29 || estimate.Rounds != 2 {
		t.Fatalf("scan must include AGENTS.md and .gitattributes: %+v", estimate)
	}
	sizes := authorFirstBuild(t, root, estimate.ParallelBatches, estimate.Targets, false)
	if len(sizes) != estimate.Rounds {
		t.Fatalf("scan estimated %d rounds, reached alignment with batches %v", estimate.Rounds, sizes)
	}
	for _, line := range strings.Split(printed, "\n") {
		if strings.Contains(line, "index files need an Entry:") {
			t.Logf("init=%q scan=%d targets in %d rounds, Maintain batch sizes=%v", strings.TrimSpace(line), estimate.Targets, estimate.Rounds, sizes)
			break
		}
	}
	if want := scanAuthoringEstimateLine(estimate); !strings.Contains(printed, want) {
		t.Fatalf("init estimate must agree with scan and the actual build; want %q", want)
	}
}
