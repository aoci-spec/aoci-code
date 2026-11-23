package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

// An observe file edited where it stands is work to activate: its Baseline
// fingerprint moves, so activation is not already_aligned (audit G4-05).
func TestScopeActivateAdvancesAnObserveFileEditedInPlace(t *testing.T) {
	root := scopeActivationFixtureWith(t, map[string]string{"sample_test.go": "package sample\n"})
	before, exists, err := baseline.Load(root)
	if err != nil || !exists || before.Files["sample_test.go"].SHA256 == "" {
		t.Fatalf("fixture: observe file not in the Baseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte("package sample\n// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "scope", "activate", "--reviewed-by", "tester"}, &out, &errs)
	if strings.Contains(out.String(), "already_aligned") {
		t.Fatalf("an in-place observe edit read as already_aligned: code=%d %s", code, &out)
	}
	if code != ExitOK {
		t.Fatalf("activation: code=%d out=%s err=%s", code, &out, &errs)
	}
	after, _, err := baseline.Load(root)
	if err != nil || after.Files["sample_test.go"].SHA256 == before.Files["sample_test.go"].SHA256 {
		t.Fatalf("the observe fingerprint did not advance: %v", err)
	}
}
