// The public single-entry CLI must reuse the source-bound atomic batch and the Baseline integrity terminal state.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestUpdateEntryCLIPreviewReturnsSharedRepairFindingJSON(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, false)
	fingerprint, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	output, stderr, code := runCLI(t, root, "--json", "update-entry",
		"--path", "main.go",
		"--entry", "main.go[CD7S]: F:run the deterministic CLI fixture | R:- | A:a,b,c,d,e,f,g | S:-",
		"--source-sha256", fingerprint.SHA256,
		"--preview",
	)
	// A silent machine report: the exit code says invalid, the JSON says why,
	// and nothing reaches stderr.
	if code != ExitInvalid || stderr != "" {
		t.Fatalf("repair Preview must use the silent machine report: exit %d stderr=%q", code, stderr)
	}
	var report updateEntryRepairReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("repair Preview did not emit one JSON object: %v\n%s", err, output)
	}
	if report.Status != entriesAutoStatusRepairRequired || report.Attempted != 1 ||
		report.Applied != 0 || report.Remaining != 1 || report.FormalWritesStarted ||
		report.FindingCount != 1 || !report.PreserveOtherCandidates ||
		len(report.RetryScope) != 1 || report.RetryScope[0] != "code:main.go" {
		t.Fatalf("single Preview repair contract mismatch: %+v", report)
	}
	finding := report.Findings[0]
	if finding.CandidateIndex != 1 || finding.Path != "main.go" ||
		finding.CanonicalObjectIdentity != "code:main.go" || finding.Domain != "code" ||
		finding.Field != "A" || finding.RuleCode != "fras_a_too_many_items" ||
		finding.Expected != "max_items=6" || finding.Actual != "item_count=7" ||
		finding.Cause == "" || finding.SafeRepairAction == "" {
		t.Fatalf("single Preview Finding mismatch: %+v", finding)
	}
}

// Since 0.2.0 the Legacy single-file layout is read-only: the human update
// command answers the same machine token as the MCP tools, even for a
// preview, and writes nothing.
func TestUpdateEntryCLIRefusesLegacyLayout(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	before, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--repo", root, "update-entry", "--path", "a.go", "--entry", manualAtomicNewA, "--preview"},
		{"--repo", root, "update-entry", "--path", "a.go", "--entry", manualAtomicNewA, "--source-sha256", strings.Repeat("0", 64)},
	} {
		var stdout, stderr bytes.Buffer
		code := executeCLI(args, &stdout, &stderr)
		blob := stdout.String() + stderr.String()
		if code == ExitOK || !strings.Contains(blob, machinecontract.StatusLegacyReadOnlyMigrateRequired) {
			t.Fatalf("%v: expected the read-only refusal, got exit %d:\n%s", args[2:], code, blob)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a refused Legacy update changed the index")
	}
}
