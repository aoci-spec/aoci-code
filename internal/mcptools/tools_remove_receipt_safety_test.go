package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// receiptFromPlan writes the receipt commitVolumeRemove would have written
// just before the Volume write, which is the state a process death leaves.
func receiptFromPlan(t *testing.T, root string, plan *removePlan) {
	t.Helper()
	receipt := removeRecovery{
		Version: 1, Rel: plan.objectRef, RemovedLine: plan.out.RemovedLine,
		PreIndexSHA256: plan.indexHash, PostIndexSHA256: indexTextHash(plan.newText), VolumeMode: true,
		ObjectRef: plan.objectRef, VolumeID: plan.volumeID, VolumePath: plan.rc.cfg.IndexPath,
		VolumePostimage: plan.newText, GuardSHA256: plan.guardSHA256, EvidenceIdentity: plan.evidenceIdentity,
		BaselinePreSHA256: plan.baselinePreSHA256, BaselinePostSHA256: plan.baselinePostSHA256,
		BaselinePostimage: plan.baselinePostimage, OwnershipRepair: plan.out.OwnershipRepair,
		PreservedOwner: plan.out.PreservedOwner,
	}
	if err := saveRemoveRecovery(root, receipt); err != nil {
		t.Fatal(err)
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// A removal interrupted after its receipt was written resumes on the next
// call although that receipt now makes Verify report recovery_pending: the
// removal's own receipt is what the resume exists to finish, so it is never
// the reason the orphan or ownership proof refuses. Ownership repair is the
// arm that read facts.RecoveryPending directly.
func TestVolumeRemoveResumesPastItsOwnReceipt(t *testing.T) {
	t.Run("ownership_repair", func(t *testing.T) {
		root := buildVolumeRepo(t, true, false)
		writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
			"main.go[CD9S]: F:run the fixture | R:aoci.txt | A:main | S:Keep execution deterministic\n"+
			"aoci.txt[CD9S]: F:describe the repository cognition root | R:- | A:- | S:Root ownership remains exclusive\n")
		cfg, err := config.LoadReadOnly(root)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
		if err != nil {
			t.Fatal(err)
		}
		if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
			t.Fatal(err)
		}
		plan, fail := planVolumeRemoveEntry(root, "code:aoci.txt", true)
		if fail != nil || !plan.out.OwnershipRepair {
			t.Fatalf("fixture plan: %+v %+v", fail, plan)
		}
		receiptFromPlan(t, root, plan)
		outcome, fail := ApplyRemoveEntry(root, "code:aoci.txt", "agent", true, false)
		if fail != nil || outcome == nil || !outcome.OwnershipRepair {
			t.Fatalf("ownership repair must resume past its own receipt: fail=%+v outcome=%+v", fail, outcome)
		}
		if strings.Contains(volumeFileText(t, root, "aoci.code.txt"), "aoci.txt[") {
			t.Fatal("misplaced Entry survived the resumed removal")
		}
		if _, err := os.Stat(removeRecoveryPath(root, "code:aoci.txt")); !os.IsNotExist(err) {
			t.Fatalf("receipt must be gone after the resume: %v", err)
		}
	})
	t.Run("plain_orphan", func(t *testing.T) {
		root := buildVolumeRepo(t, true, false)
		if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
			t.Fatal(err)
		}
		plan, fail := planVolumeRemoveEntry(root, "code:main.go", true)
		if fail != nil {
			t.Fatalf("fixture plan: %+v", fail)
		}
		receiptFromPlan(t, root, plan)
		if facts := assessVolumeRepo(t, root); !facts.RecoveryPending {
			t.Fatal("the fixture must start with the receipt pending")
		}
		outcome, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false)
		if fail != nil || outcome == nil || outcome.Superseded || outcome.StaleReceiptDiscarded {
			t.Fatalf("plain removal must resume: fail=%+v outcome=%+v", fail, outcome)
		}
		if facts := assessVolumeRepo(t, root); !facts.GovernanceAligned {
			t.Fatalf("repository must be aligned after the resume: %+v", facts.Findings)
		}
	})
}

// A stale receipt is discarded only by a commit that gets as far as writing:
// a dry run and a removal refused for a live source both leave it in place,
// so the plan phase stays free of side effects and nothing unblocks Overview
// without a decision being applied.
func TestStaleReceiptSurvivesDryRunAndARefusedRemoval(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeStaleVolumeRemoveReceipt(t, root, "code:main.go", "main.go[CD9S]: F:run the fixture | R:aoci.meta.txt | A:main | S:Keep execution deterministic")
	receipt := removeRecoveryPath(root, "code:main.go")
	if _, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false); fail == nil {
		t.Fatal("a live source must not be removed")
	}
	if _, err := os.Stat(receipt); err != nil {
		t.Fatalf("a refused removal must leave the receipt: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	outcome, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, true)
	if fail != nil || outcome == nil || !outcome.DryRun {
		t.Fatalf("dry run must plan: fail=%+v outcome=%+v", fail, outcome)
	}
	if _, err := os.Stat(receipt); err != nil {
		t.Fatalf("a dry run must leave the receipt: %v", err)
	}
	if strings.Contains(RenderRemoveOutcome(outcome), "discard") {
		t.Fatalf("a dry run must not claim a discard: %s", RenderRemoveOutcome(outcome))
	}
	outcome, fail = ApplyRemoveEntry(root, "code:main.go", "agent", true, false)
	if fail != nil || outcome == nil || !outcome.StaleReceiptDiscarded {
		t.Fatalf("the live removal must discard and apply: fail=%+v outcome=%+v", fail, outcome)
	}
	if _, err := os.Stat(receipt); !os.IsNotExist(err) {
		t.Fatalf("no receipt may remain after the fresh removal: %v", err)
	}
}

// A removal writes over no receipt that is not its own: a foreign file or
// another object's receipt is state this write cannot prove it is extending.
func TestRemoveRefusesToWriteOverOtherReceipts(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"note.json", "remove-" + strings.Repeat("0", 64) + ".json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false)
		if fail == nil || !strings.Contains(fail.Msg, "remove_other_transaction_pending") || !strings.Contains(fail.Msg, name) {
			t.Fatalf("%s must refuse the removal: %+v", name, fail)
		}
		if strings.Contains(volumeFileText(t, root, "aoci.code.txt"), "main.go[") == false {
			t.Fatalf("%s: the Entry must be untouched", name)
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false); fail != nil {
		t.Fatalf("with the directory clear the removal applies: %+v", fail)
	}
}

// The check_only Overview counts blockers the way the result does: an aligned
// repository that holds an image reports none.
func TestOverviewBlockerCountIgnoresInformationalFindings(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeVolumeTestFile(t, root, "assets/logo.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00")
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, "assets", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	state.Files["assets/logo.png"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_overview", map[string]any{"check_only": true})
	if !strings.Contains(output, `"governance_aligned":true`) || !strings.Contains(output, `"governance_blocker_count":0`) {
		t.Fatalf("held sources must not count as blockers:\n%s", output)
	}
}
