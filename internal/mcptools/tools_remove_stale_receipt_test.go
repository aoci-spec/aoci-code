package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// writeStaleVolumeRemoveReceipt leaves a removal receipt whose Volume hashes
// no longer match anything: the shape #74 reported, where the Volume moved on
// through later transactions after the remove's receipt was written.
func writeStaleVolumeRemoveReceipt(t *testing.T, root, objectRef, removedLine string) string {
	t.Helper()
	receipt := removeRecovery{Version: 1, Rel: objectRef, RemovedLine: removedLine,
		PreIndexSHA256: strings.Repeat("a", 64), PostIndexSHA256: strings.Repeat("b", 64),
		VolumeMode: true, ObjectRef: objectRef, VolumeID: cognition.ScopeCode, VolumePath: "aoci.code.txt",
		VolumePostimage: "stale", GuardSHA256: map[string]string{"root": strings.Repeat("c", 64)},
		EvidenceIdentity: "not_applicable", BaselinePreSHA256: strings.Repeat("d", 64),
		BaselinePostSHA256: strings.Repeat("e", 64), BaselinePostimage: "stale"}
	if err := saveRemoveRecovery(root, receipt); err != nil {
		t.Fatal(err)
	}
	return filepath.Base(removeRecoveryPath(root, objectRef))
}

func assessVolumeRepo(t *testing.T, root string) *volumegovernance.Facts {
	t.Helper()
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

// A stale remove receipt is seen by Overview and Verify alike, named with its
// kind, and closed by the tool the Guide points at. Until v0.1.0-rc15 Overview
// refused full delivery on it while Verify, Guide, and Maintain reported the
// repository aligned, and every aoci_remove_entry retry stopped with
// recovery_postimage_drift (#74).
func TestStaleRemoveReceiptIsSeenEverywhereAndClosesAsSuperseded(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	name := writeStaleVolumeRemoveReceipt(t, root, "code:gone.go", "gone.go[CD5T]: F:was removed from the tree | R:- | A:- | S:-")
	session := connectMCPClient(t, root)
	if overview := callVolumeTool(t, session, "aoci_overview", nil); !strings.Contains(overview, `"fallback_reason":"recovery_pending"`) {
		t.Fatalf("Overview must refuse full delivery over the receipt:\n%s", overview)
	}
	facts := assessVolumeRepo(t, root)
	if facts.GovernanceAligned || !facts.RecoveryPending || strings.Join(facts.PendingTransactionFiles, ",") != name {
		t.Fatalf("Verify must see the same receipt Overview refuses on: aligned=%v pending=%v files=%v",
			facts.GovernanceAligned, facts.RecoveryPending, facts.PendingTransactionFiles)
	}
	named := false
	for _, finding := range facts.Findings {
		if finding.Code == "recovery_pending" && finding.Target == name && finding.Cause == "remove" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the recovery finding must name the receipt and its kind: %+v", facts.Findings)
	}
	volumeBefore := fileDigest(t, filepath.Join(root, "aoci.code.txt"))
	baselineBefore := fileDigest(t, filepath.Join(root, ".aoci", "baseline.json"))
	output := callVolumeTool(t, session, "aoci_remove_entry", map[string]any{"path": "code:gone.go"})
	if !(strings.Contains(output, "superseded") || strings.Contains(output, "取代")) || !strings.Contains(output, "code:gone.go") {
		t.Fatalf("the receipt must close as superseded:\n%s", output)
	}
	if fileDigest(t, filepath.Join(root, "aoci.code.txt")) != volumeBefore || fileDigest(t, filepath.Join(root, ".aoci", "baseline.json")) != baselineBefore {
		t.Fatal("a superseded closure must write neither the Volume nor the Baseline")
	}
	if _, err := os.Stat(removeRecoveryPath(root, "code:gone.go")); !os.IsNotExist(err) {
		t.Fatalf("receipt must be gone after closure: %v", err)
	}
	if overview := callVolumeTool(t, session, "aoci_overview", nil); strings.Contains(overview, `"fallback_reason":"recovery_pending"`) {
		t.Fatalf("Overview must deliver once the receipt is closed:\n%s", overview)
	}
	if facts := assessVolumeRepo(t, root); !facts.GovernanceAligned || facts.RecoveryPending {
		t.Fatalf("repository must be aligned once the receipt is closed: %+v", facts.Findings)
	}
}

// When the Volume moved on but the object is still there, the receipt bound
// a preimage that no longer exists: it is discarded and the removal the caller
// asked for is planned from the current Volume, instead of stopping with
// recovery_entry_reappeared or recovery_postimage_drift forever.
func TestStaleRemoveReceiptWithObjectPresentIsDiscardedAndRemovalReplanned(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildVolumeRepoAt(t, root, true, false, "")
	codePath := filepath.Join(root, "aoci.code.txt")
	text, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codePath, append(text, []byte("keep.go[CD5T]: F:stays in the tree | R:- | A:- | S:-\n")...), 0o644); err != nil {
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
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	writeStaleVolumeRemoveReceipt(t, root, "code:main.go", "main.go[CD9S]: F:run the fixture | R:aoci.meta.txt | A:main | S:Keep execution deterministic")
	session := connectMCPClient(t, root)
	output := callVolumeTool(t, session, "aoci_remove_entry", map[string]any{"path": "code:main.go"})
	if !(strings.Contains(output, "stale removal receipt") || strings.Contains(output, "旧删除收据")) || !strings.Contains(output, "main.go[CD9S]") {
		t.Fatalf("the stale receipt must be discarded and the removal applied:\n%s", output)
	}
	after, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "main.go[CD9S]") || !strings.Contains(string(after), "keep.go[CD5T]") {
		t.Fatalf("the orphan must be gone and the neighbour kept:\n%s", after)
	}
	if _, err := os.Stat(removeRecoveryPath(root, "code:main.go")); !os.IsNotExist(err) {
		t.Fatalf("no receipt may remain after the fresh removal: %v", err)
	}
	if facts := assessVolumeRepo(t, root); !facts.GovernanceAligned {
		t.Fatalf("repository must be aligned after the removal: %+v", facts.Findings)
	}
}
