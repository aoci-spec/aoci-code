// issue #8: an initialized, unscanned Volumes v1 skeleton (zero entries) once walked into a dead end on the Guide:
// the blocked state carried only a bare finding code with no actionable remedy; the Legacy Guide had long had the baseline_first
// phase, but Volumes was never wired to it. Locked here: a Volumes Guide without a Baseline must carry the scan command and a
// structured stop; after scan the same repository goes straight into authoring_required.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func volumeGuideJSON(t *testing.T, root string) volumeAgentGuide {
	t.Helper()
	output := mustCLI(t, root, "--json", "index", "agent", "guide", "--agent", "codex")
	var guide volumeAgentGuide
	if err := json.Unmarshal([]byte(output), &guide); err != nil {
		t.Fatalf("guide is not JSON: %v\n%s", err, output)
	}
	return guide
}

func TestVolumeGuideWithoutBaselineCarriesScanRemediationThenAuthors(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package src\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}

	// Not scanned: the skeleton exists, zero entries, no Baseline; the exact state of issue #8.
	guide := volumeGuideJSON(t, root)
	if guide.Stage != "blocked" {
		t.Fatalf("an unscanned Volumes skeleton must still report blocked, got %q", guide.Stage)
	}
	if guide.Commands.Scan == "" || !strings.Contains(guide.Commands.Scan, " scan") {
		t.Fatalf("blocked Guide without a Baseline must carry the scan command: %+v", guide.Commands)
	}
	if guide.Stop == nil || guide.Stop.RuleCode != "baseline_missing" || guide.Stop.Cause == "" || guide.Stop.SafeNextAction == "" {
		t.Fatalf("blocked Guide must carry a structured baseline_missing stop: %+v", guide.Stop)
	}
	joined := strings.Join(guide.Instructions, "\n")
	if !strings.Contains(joined, "aoci scan") {
		t.Fatalf("instructions must name aoci scan as the remediation:\n%s", joined)
	}
	if guide.AuthoringMeta != "" || guide.Batch != nil {
		t.Fatal("no authoring contract may be issued before a Baseline exists")
	}

	// After scan the same repository no longer blocks and goes straight into the Volumes authoring path.
	var scanOut, scanErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &scanOut, &scanErr); code != ExitOK {
		t.Fatalf("scan failed: code=%d stdout=%s stderr=%s", code, scanOut.String(), scanErr.String())
	}
	guide = volumeGuideJSON(t, root)
	if guide.Stage != "authoring_required" || guide.Stop != nil || guide.Commands.Scan != "" {
		t.Fatalf("after scan the Guide must route to authoring, got stage=%q stop=%+v scan=%q", guide.Stage, guide.Stop, guide.Commands.Scan)
	}
	if guide.Batch == nil || guide.Batch.TotalTargets == 0 || guide.AuthoringMeta == "" {
		t.Fatalf("authoring_required Guide must carry the batch facts and formal Meta: %+v", guide.Batch)
	}
}
