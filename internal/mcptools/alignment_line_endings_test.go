package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Code Volume checked out with CRLF line endings differs from its LF
// Baseline record only by line endings; under the team's tolerance the
// alignment inspector agrees with governance that it is aligned (audit G2-04:
// it compared raw digests, so a zero-write batch on such a checkout never
// closed).
func TestAlignmentInspectorToleratesLineEndingOnlyVolumes(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 1)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(maintain))); !applied.Aligned {
		t.Fatalf("fixture must align: %#v", applied)
	}
	path := filepath.Join(root, "aoci.code.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatalf("load: %+v", fail)
	}
	outcome := &AtomicBatchOutcome{Volumes: []string{"code"}, Items: []*UpdateOutcome{{Action: "no_change", Rel: "generated/0000.go"}}, BaselineComplete: true}
	_, _, findings, _ := inspectCognitionVolumeAlignment(root, "test", loaded, outcome)
	for _, finding := range findings {
		if strings.HasPrefix(finding, "unaligned: aoci.code.txt") {
			t.Fatalf("a line-ending-only Volume must count as aligned: %v", findings)
		}
	}
}
