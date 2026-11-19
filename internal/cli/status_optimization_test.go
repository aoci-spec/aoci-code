package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionoptimization"
)

// aoci status shows an open cognition optimization pass: how many Entries it
// has left and that a new request with object_refs replaces it (scipy, C4).
func TestStatusShowsAnOpenOptimizationPass(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--locale", "en-US", "--hooks=false", "--here=false"}, {"scan"}} {
		var stdout, stderr bytes.Buffer
		if code := executeCLI(append([]string{"--repo", root}, args...), &stdout, &stderr); code != ExitOK {
			t.Fatalf("%v: exit=%d %s %s", args, code, &stdout, &stderr)
		}
	}
	status := func() statusReport {
		var stdout, stderr bytes.Buffer
		if code := executeCLI([]string{"--repo", root, "status", "--json"}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("status: exit=%d %s %s", code, &stdout, &stderr)
		}
		var report statusReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	if report := status(); report.OpenOptimizationPass != nil {
		t.Fatalf("no pass is open yet: %+v", report.OpenOptimizationPass)
	}
	if _, err := cognitionoptimization.Create(root, cognitionoptimization.CreateInput{
		OptimizationID: strings.Repeat("a", 64), RemainingObjectRefs: []string{"code:a.go", "code:b.go"},
	}); err != nil {
		t.Fatal(err)
	}
	if report := status(); report.OpenOptimizationPass == nil || report.OpenOptimizationPass.Remaining != 2 {
		t.Fatalf("the open pass is not reported: %+v", report.OpenOptimizationPass)
	}
	var stdout, stderr bytes.Buffer
	executeCLI([]string{"--repo", root, "status"}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "2 Entries left to review") {
		t.Fatalf("the human status does not name the open pass:\n%s", &stdout)
	}
}
