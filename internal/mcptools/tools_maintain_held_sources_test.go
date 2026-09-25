package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// buildHeldSourcesVolumeRepo is a Volumes repository at the state a first scan
// leaves it: one Entry already written, one ordinary source without an Entry,
// and three index-role files no model can author from their bytes. With
// orphan set, the Code Volume also names a file that no longer exists.
func buildHeldSourcesVolumeRepo(t *testing.T, orphan bool) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		"pkg/util.go":     "package pkg\n",
		"assets/logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00",
		"data/empty.txt":  "",
		"data/dump.txt":   strings.Repeat("row\n", (1<<20)/4+1),
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	buildVolumeRepoAt(t, root, true, false, "")
	if orphan {
		codePath := filepath.Join(root, "aoci.code.txt")
		text, err := os.ReadFile(codePath)
		if err != nil {
			t.Fatal(err)
		}
		text = append(text, []byte("gone.go[CD5T]: F:was removed from the tree | R:- | A:- | S:-\n")...)
		if err := os.WriteFile(codePath, text, 0o644); err != nil {
			t.Fatal(err)
		}
		state, _, err := baseline.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, err := baseline.HashFile(codePath)
		if err != nil {
			t.Fatal(err)
		}
		state.Files["aoci.code.txt"] = fingerprint
		if err := baseline.Save(root, state); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func maintainVolumeResult(t *testing.T, root string) volumeMaintainResult {
	t.Helper()
	session := connectMCPClient(t, root)
	var result volumeMaintainResult
	if err := json.Unmarshal([]byte(callVolumeTool(t, session, "aoci_maintain", nil)), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func heldCandidatePaths(result volumeMaintainResult) string {
	paths := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		paths = append(paths, candidate.Path)
	}
	return strings.Join(paths, ",")
}

// The first Maintain of a repository holding an image, an empty file, and a
// file above the read limit issues the ordinary batch and reports the three
// as skipped. Until v0.1.0-rc14 it stopped with pending_curation: markers in
// orphan_remove_candidates and no authoring contract, and Volumes v1 has no
// decision path that could have cleared them.
func TestFirstMaintainHoldsUnreadableSourcesOutWithoutBlocking(t *testing.T) {
	result := maintainVolumeResult(t, buildHeldSourcesVolumeRepo(t, false))
	if result.Status != autoStatusRepairRequired || result.Result != volumegovernance.ResultAuthoringRequired ||
		len(result.OrphanRemovals) != 0 || heldCandidatePaths(result) != "pkg/util.go" {
		t.Fatalf("held sources blocked the batch: status=%s result=%s orphans=%v candidates=%s",
			result.Status, result.Result, result.OrphanRemovals, heldCandidatePaths(result))
	}
	if result.AuthoringMeta == "" || len(result.Instructions) == 0 {
		t.Fatal("a batch with candidates must carry the authoring contract")
	}
	if result.Batch.TotalTargets != 1 || result.Batch.Remaining != 0 || !result.Batch.CompleteCandidateSet {
		t.Fatalf("held sources must not count as targets: %+v", result.Batch)
	}
	if got := strings.Join(result.Governance.CodeDrift.Skipped, ","); got != "assets/logo.png,data/dump.txt,data/empty.txt" {
		t.Fatalf("skipped sources must be reported: %q", got)
	}
}

// A real orphan still blocks, and the block is explicit: the orphan is named
// as a remove candidate, no candidate is issued for a model to author against
// a preimage the orphan removal will change, and the held sources stay
// informational rather than joining the block.
func TestOrphanStillBlocksExplicitlyWhileHeldSourcesStayInformational(t *testing.T) {
	result := maintainVolumeResult(t, buildHeldSourcesVolumeRepo(t, true))
	if result.Status != autoStatusStopped || result.NextAction != "explicit_orphan_remove_or_resolve_blocker" ||
		strings.Join(result.OrphanRemovals, ",") != "code:gone.go" || len(result.Candidates) != 0 {
		t.Fatalf("orphan must block explicitly: status=%s next=%s orphans=%v candidates=%s",
			result.Status, result.NextAction, result.OrphanRemovals, heldCandidatePaths(result))
	}
	for _, marker := range result.OrphanRemovals {
		if strings.HasPrefix(marker, "pending_curation:") {
			t.Fatalf("held sources must never be dressed as orphans: %v", result.OrphanRemovals)
		}
	}
	if got := strings.Join(result.Governance.CodeDrift.Skipped, ","); got != "assets/logo.png,data/dump.txt,data/empty.txt" {
		t.Fatalf("skipped sources must still be reported while blocked: %q", got)
	}
}
