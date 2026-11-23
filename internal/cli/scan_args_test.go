package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanRejectsPositionalArgumentsBeforeWritingBaseline(t *testing.T) {
	root := initGitRepo(t)
	mustCLI(t, root, "init", "--locale", "en-US", "--hooks=false", "--here=false")

	_, stderr, code := runCLI(t, root, "scan", "svc-a")
	if code != ExitInvalid || !strings.Contains(stderr, "--repo") {
		t.Errorf("scan with a path: exit=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".aoci", "baseline.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no Baseline after invalid scan, stat returned %v", err)
	}

	stdout, stderr, code := runCLI(t, root, "scan", "svc-a", "--dry-run", "--json")
	var failure cliJSONErrorEnvelope
	if err := json.Unmarshal([]byte(stdout), &failure); err != nil {
		t.Fatalf("expected a JSON error envelope: %v; stdout=%q stderr=%q", err, stdout, stderr)
	}
	if code != ExitInvalid || failure.OK || failure.ExitCode != ExitInvalid ||
		failure.ErrorCode != "invalid" || !strings.Contains(failure.Message, "--repo") {
		t.Fatalf("dry-run with a path: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
