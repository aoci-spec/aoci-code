package mcptools

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// Under line-ending tolerance a source converted to CRLF leaves governance
// aligned, but the write binds raw bytes: the current optimization batch is
// planned afresh over them instead of replayed until every update is refused,
// and a replayed batch keeps its transport budget (audit G3-07).
func TestOptimizationBatchReplansAfterALineEndingOnlyChange(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 3)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LineEndingTolerance = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	first := callCognitionOptimizationMaintain(t, session, cognitionOptimizationObjectRefs(3))
	replayed := callCognitionOptimizationMaintain(t, session, nil)
	if replayed.Batch.TransportBudgetBytes == 0 {
		t.Fatalf("a replayed optimization batch lost its transport budget: %+v", replayed.Batch)
	}

	path := filepath.Join(root, filepath.FromSlash(first.Candidates[0].Path))
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.ReplaceAll(source, []byte("\n"), []byte("\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	live, err := baseline.HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	again := callCognitionOptimizationMaintain(t, session, nil)
	for _, candidate := range again.Candidates {
		if candidate.Path == first.Candidates[0].Path && candidate.SourceSHA256 != live.SHA256 {
			t.Fatalf("the batch was replayed over a line-ending change: candidate %s, file %s", candidate.SourceSHA256, live.SHA256)
		}
	}
}
